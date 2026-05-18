package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

func TestUpsertSnapshotsFromTaskResult(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	firstSeen := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	updatedSeen := firstSeen.Add(time.Hour)

	require.NoError(t, recordTaskResult(database, agent.ID, protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "snap-1",
		DurationMs: 1200,
		RepoSize:   4096,
		StartedAt:  firstSeen.Add(-1200 * time.Millisecond),
		FinishedAt: firstSeen,
		Snapshots: []protocol.SnapshotInfo{
			{ID: "snap-1", Time: firstSeen, Paths: []string{"/etc"}, Size: 4096},
		},
	}))
	require.NoError(t, recordTaskResult(database, agent.ID, protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "snap-1",
		DurationMs: 1300,
		RepoSize:   8192,
		StartedAt:  updatedSeen.Add(-1300 * time.Millisecond),
		FinishedAt: updatedSeen,
		Snapshots: []protocol.SnapshotInfo{
			{ID: "snap-1", Time: updatedSeen, Paths: []string{"/etc", "/home"}, Size: 8192},
		},
	}))

	var histories []db.TaskHistory
	require.NoError(t, database.DB.Order("created_at ASC").Find(&histories).Error)
	require.Len(t, histories, 2)
	assert.Equal(t, "backup", histories[0].Type)
	assert.Equal(t, "success", histories[0].Status)
	assert.Equal(t, "snap-1", histories[0].SnapshotID)
	assert.Equal(t, int64(1200), histories[0].DurationMs)

	var snapshots []db.Snapshot
	require.NoError(t, database.DB.Find(&snapshots, "agent_id = ?", agent.ID).Error)
	require.Len(t, snapshots, 1)
	assert.Equal(t, "snap-1", snapshots[0].SnapshotID)
	assert.True(t, snapshots[0].Timestamp.Equal(updatedSeen))
	assert.Equal(t, int64(8192), snapshots[0].Size)
	assert.JSONEq(t, `["/etc","/home"]`, snapshots[0].Paths)
}

func TestTaskResultProcessorRecordsHistoryAndSnapshots(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	finishedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	msg, err := protocol.NewMessage(protocol.TypeTaskResult, protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "snap-processor",
		DurationMs: 1500,
		StartedAt:  finishedAt.Add(-1500 * time.Millisecond),
		FinishedAt: finishedAt,
		Snapshots: []protocol.SnapshotInfo{
			{ID: "snap-processor", Time: finishedAt, Paths: []string{"/srv"}, Size: 1024},
		},
	})
	require.NoError(t, err)

	processor := NewTaskResultProcessor(database)
	require.NoError(t, processor(agent.ID, *msg))

	var history db.TaskHistory
	require.NoError(t, database.DB.First(&history, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-processor").Error)
	assert.Equal(t, "backup", history.Type)
	assert.Equal(t, "success", history.Status)

	var snapshot db.Snapshot
	require.NoError(t, database.DB.First(&snapshot, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-processor").Error)
	assert.True(t, snapshot.Timestamp.Equal(finishedAt))
	assert.JSONEq(t, `["/srv"]`, snapshot.Paths)
}

func TestTaskResultProcessorCompletesRunningRestoreHistory(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	masterStartedAt := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	agentStartedAt := masterStartedAt.Add(2 * time.Second)
	finishedAt := agentStartedAt.Add(45 * time.Second)
	running := db.TaskHistory{
		AgentID:    agent.ID,
		Type:       "restore",
		Status:     "running",
		SnapshotID: "snap-restore",
		StartedAt:  &masterStartedAt,
	}
	require.NoError(t, database.DB.Create(&running).Error)
	msg, err := protocol.NewMessage(protocol.TypeTaskResult, protocol.TaskResultPayload{
		AgentID:    agent.ID,
		TaskType:   "restore",
		Status:     "success",
		SnapshotID: "snap-restore",
		DurationMs: 45000,
		StartedAt:  agentStartedAt,
		FinishedAt: finishedAt,
	})
	require.NoError(t, err)

	processor := NewTaskResultProcessor(database)
	require.NoError(t, processor(agent.ID, *msg))

	var histories []db.TaskHistory
	require.NoError(t, database.DB.Find(&histories, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-restore").Error)
	require.Len(t, histories, 1)
	assert.Equal(t, running.ID, histories[0].ID)
	assert.Equal(t, "success", histories[0].Status)
	assert.Equal(t, int64(45000), histories[0].DurationMs)
	require.NotNil(t, histories[0].FinishedAt)
	assert.True(t, histories[0].FinishedAt.Equal(finishedAt))
}

func TestUpsertSnapshotsConcurrentSameSnapshotDoesNotDuplicate(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	agent := createSnapshotTestAgent(t, database, "online")
	snapshotTime := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	snapshots := []protocol.SnapshotInfo{
		{ID: "snap-race", Time: snapshotTime, Paths: []string{"/srv"}, Size: 512},
	}

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- upsertSnapshots(database, agent.ID, snapshots)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var count int64
	require.NoError(t, database.DB.Model(&db.Snapshot{}).Where("agent_id = ? AND snapshot_id = ?", agent.ID, "snap-race").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestListSnapshots(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	older := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	newer := older.Add(24 * time.Hour)
	createSnapshotRecord(t, setup.database, agent.ID, "snap-old", older, []string{"/etc"}, 100)
	createSnapshotRecord(t, setup.database, agent.ID, "snap-new", newer, []string{"/home", "/srv"}, 200)

	w := getJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body []snapshotAPITestResponse
	parseJSONInto(t, w, &body)
	require.Len(t, body, 2)
	assert.Equal(t, "snap-new", body[0].SnapshotID)
	assert.True(t, body[0].Timestamp.Equal(newer))
	assert.Equal(t, []string{"/home", "/srv"}, body[0].Paths)
	assert.Equal(t, int64(200), body[0].Size)
	assert.Equal(t, "snap-old", body[1].SnapshotID)
}

func TestListSnapshotsMissingAgent(t *testing.T) {
	setup := setupSnapshotAPI(t)

	w := getJSON(t, setup.router, "/api/agents/missing/snapshots")

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestRefreshSnapshots(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	snapshotTime := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	setup.hub.online[agent.ID] = true
	setup.handler.timeout = time.Second
	setup.hub.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		assert.Equal(t, agent.ID, agentID)
		assert.Equal(t, protocol.TypeSnapshotListReq, msg.Type)
		assert.Equal(t, time.Second, timeout)
		req, err := protocol.ParsePayload[protocol.SnapshotListReqPayload](&msg)
		require.NoError(t, err)
		assert.Equal(t, agent.ID, req.AgentID)

		resp, err := protocol.NewMessage(protocol.TypeSnapshotListResp, protocol.SnapshotListRespPayload{
			AgentID: agent.ID,
			Snapshots: []protocol.SnapshotInfo{
				{ID: "snap-1", Time: snapshotTime, Paths: []string{"/etc"}, Size: 512},
			},
		})
		require.NoError(t, err)
		resp.ID = msg.ID
		ch := make(chan protocol.Message, 1)
		ch <- *resp
		close(ch)
		return ch, nil
	}

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots/refresh", map[string]any{})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body snapshotRefreshAPITestResponse
	parseJSONInto(t, w, &body)
	assert.Equal(t, 1, body.Count)
	require.Len(t, body.Snapshots, 1)
	assert.Equal(t, "snap-1", body.Snapshots[0].SnapshotID)

	var stored db.Snapshot
	require.NoError(t, setup.database.DB.First(&stored, "agent_id = ? AND snapshot_id = ?", agent.ID, "snap-1").Error)
	assert.True(t, stored.Timestamp.Equal(snapshotTime))
	assert.JSONEq(t, `["/etc"]`, stored.Paths)
}

func TestRefreshSnapshotsOffline(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "offline")

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots/refresh", map[string]any{})

	require.Equal(t, http.StatusBadGateway, w.Code)
}

func TestRefreshSnapshotsTimeout(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	setup.handler.timeout = time.Second
	setup.hub.sendAndWait = func(string, protocol.Message, time.Duration) (<-chan protocol.Message, error) {
		ch := make(chan protocol.Message)
		close(ch)
		return ch, nil
	}

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots/refresh", map[string]any{})

	require.Equal(t, http.StatusGatewayTimeout, w.Code)
}

func TestRefreshSnapshotsAgentError(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	setup.hub.sendAndWait = func(_ string, msg protocol.Message, _ time.Duration) (<-chan protocol.Message, error) {
		resp, err := protocol.NewMessage(protocol.TypeSnapshotListResp, protocol.SnapshotListRespPayload{
			AgentID: agent.ID,
			Error:   "restic repository locked",
		})
		require.NoError(t, err)
		resp.ID = msg.ID
		ch := make(chan protocol.Message, 1)
		ch <- *resp
		close(ch)
		return ch, nil
	}

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots/refresh", map[string]any{})

	require.Equal(t, http.StatusBadGateway, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, "restic repository locked", body["error"])
}

func TestRefreshSnapshotsRejectsWrongResponseType(t *testing.T) {
	setup := setupSnapshotAPI(t)
	agent := createSnapshotTestAgent(t, setup.database, "online")
	setup.hub.online[agent.ID] = true
	setup.hub.sendAndWait = func(_ string, msg protocol.Message, _ time.Duration) (<-chan protocol.Message, error) {
		resp, err := protocol.NewMessage(protocol.TypeDirBrowseResp, protocol.DirBrowseRespPayload{Path: "/", Entries: []protocol.DirEntry{}})
		require.NoError(t, err)
		resp.ID = msg.ID
		ch := make(chan protocol.Message, 1)
		ch <- *resp
		close(ch)
		return ch, nil
	}

	w := postAnyJSON(t, setup.router, "/api/agents/"+agent.ID+"/snapshots/refresh", map[string]any{})

	require.Equal(t, http.StatusBadGateway, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, "invalid agent response", body["error"])
}

type snapshotAPISetup struct {
	database *db.Database
	hub      *fakeSnapshotHub
	handler  *SnapshotHandler
	router   *gin.Engine
}

func setupSnapshotAPI(t *testing.T) snapshotAPISetup {
	t.Helper()

	gin.SetMode(gin.TestMode)
	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	hub := &fakeSnapshotHub{online: map[string]bool{}}
	handler := NewSnapshotHandler(database, hub)
	router := gin.New()
	RegisterSnapshotRoutes(router.Group("/api"), handler)

	return snapshotAPISetup{database: database, hub: hub, handler: handler, router: router}
}

type fakeSnapshotHub struct {
	online      map[string]bool
	sendAndWait func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

func (h *fakeSnapshotHub) IsOnline(agentID string) bool {
	return h.online[agentID]
}

func (h *fakeSnapshotHub) SendAndWait(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
	if h.sendAndWait == nil {
		return nil, errors.New("not connected")
	}
	return h.sendAndWait(agentID, msg, timeout)
}

type snapshotAPITestResponse struct {
	ID         string    `json:"id"`
	SnapshotID string    `json:"snapshot_id"`
	Timestamp  time.Time `json:"timestamp"`
	Paths      []string  `json:"paths"`
	Size       int64     `json:"size"`
}

type snapshotRefreshAPITestResponse struct {
	Count     int                       `json:"count"`
	Snapshots []snapshotAPITestResponse `json:"snapshots"`
}

func createSnapshotTestAgent(t *testing.T, database *db.Database, status string) db.Agent {
	t.Helper()

	agent := db.Agent{Name: "Snapshot Agent", Status: status}
	require.NoError(t, database.DB.Create(&agent).Error)
	return agent
}

func createSnapshotRecord(t *testing.T, database *db.Database, agentID string, snapshotID string, timestamp time.Time, paths []string, size int64) db.Snapshot {
	t.Helper()

	pathsJSON, err := json.Marshal(paths)
	require.NoError(t, err)
	snapshot := db.Snapshot{
		AgentID:    agentID,
		SnapshotID: snapshotID,
		Timestamp:  timestamp,
		Paths:      string(pathsJSON),
		Size:       size,
	}
	require.NoError(t, database.DB.Create(&snapshot).Error)
	return snapshot
}
