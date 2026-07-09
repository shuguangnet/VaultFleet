package agentrollout

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

func TestCreateRolloutResolvesTagsAndPlansPreflight(t *testing.T) {
	database := setupRolloutDB(t)
	createRolloutAgent(t, database, "web-a", "online", "v0.5.41", "amd64", []string{"prod", "web"})
	createRolloutAgent(t, database, "web-b", "offline", "v0.5.41", "amd64", []string{"prod", "web"})
	createRolloutAgent(t, database, "db-a", "online", "v0.5.42", "amd64", []string{"prod", "db"})
	service := NewService(database, &fakeHub{online: map[string]bool{}})

	rollout, items, err := service.CreateRollout(context.Background(), CreateInput{
		TargetVersion: "v0.5.42",
		TargetTags:    []string{"prod", "web"},
		CanaryCount:   1,
		BatchSize:     5,
	})

	require.NoError(t, err)
	assert.Equal(t, RolloutStatusPending, rollout.Status)
	require.Len(t, items, 2)
	assert.Equal(t, PhaseCanary, items[0].Phase)
	assert.Equal(t, ItemStatusPending, items[0].Status)
	assert.Equal(t, ItemStatusSkipped, items[1].Status)
	assert.Equal(t, "agent offline", items[1].SkipReason)
}

func TestCreateRolloutRejectsEmptyTargetsAndActiveConflict(t *testing.T) {
	database := setupRolloutDB(t)
	agent := createRolloutAgent(t, database, "node-a", "online", "v0.5.41", "amd64", nil)
	service := NewService(database, &fakeHub{online: map[string]bool{agent.ID: true}})

	_, _, err := service.CreateRollout(context.Background(), CreateInput{TargetVersion: "v0.5.42"})
	require.ErrorIs(t, err, ErrNoTargets)

	_, _, err = service.CreateRollout(context.Background(), CreateInput{
		TargetVersion:  "v0.5.42",
		TargetAgentIDs: []string{agent.ID},
	})
	require.NoError(t, err)

	_, _, err = service.CreateRollout(context.Background(), CreateInput{
		TargetVersion:  "v0.5.43",
		TargetAgentIDs: []string{agent.ID},
	})
	require.ErrorIs(t, err, ErrDuplicateActiveTarget)
}

func TestAdvanceRolloutCanarySuccessThenBatchSuccess(t *testing.T) {
	database := setupRolloutDB(t)
	a := createRolloutAgent(t, database, "a", "online", "v0.5.41", "amd64", nil)
	b := createRolloutAgent(t, database, "b", "online", "v0.5.41", "amd64", nil)
	hub := &fakeHub{
		online:   map[string]bool{a.ID: true, b.ID: true},
		accepted: map[string]bool{a.ID: true, b.ID: true},
	}
	service := NewService(database, hub)
	service.ACKTimeout = time.Second
	rollout, _, err := service.CreateRollout(context.Background(), CreateInput{
		TargetVersion:  "v0.5.42",
		TargetAgentIDs: []string{a.ID, b.ID},
		CanaryCount:    1,
		BatchSize:      1,
	})
	require.NoError(t, err)

	require.NoError(t, service.AdvanceRollout(context.Background(), rollout.ID))
	assert.Equal(t, []string{a.ID}, hub.sentAgents)
	assertRolloutItemStatus(t, database, rollout.ID, a.ID, ItemStatusRunning)
	assertRolloutItemStatus(t, database, rollout.ID, b.ID, ItemStatusPending)

	require.NoError(t, service.HandleHeartbeat(context.Background(), a.ID, "v0.5.42"))
	assert.Equal(t, []string{a.ID, b.ID}, hub.sentAgents)
	assertRolloutItemStatus(t, database, rollout.ID, a.ID, ItemStatusSuccess)
	assertRolloutItemStatus(t, database, rollout.ID, b.ID, ItemStatusRunning)

	require.NoError(t, service.HandleHeartbeat(context.Background(), b.ID, "v0.5.42"))
	assertRolloutStatus(t, database, rollout.ID, RolloutStatusSucceeded)
}

func TestAdvanceRolloutStopsOnCanaryRejection(t *testing.T) {
	database := setupRolloutDB(t)
	a := createRolloutAgent(t, database, "a", "online", "v0.5.41", "amd64", nil)
	b := createRolloutAgent(t, database, "b", "online", "v0.5.41", "amd64", nil)
	hub := &fakeHub{
		online:   map[string]bool{a.ID: true, b.ID: true},
		accepted: map[string]bool{a.ID: false, b.ID: true},
		errText:  map[string]string{a.ID: "agent self-update is disabled"},
	}
	service := NewService(database, hub)
	rollout, _, err := service.CreateRollout(context.Background(), CreateInput{
		TargetVersion:  "v0.5.42",
		TargetAgentIDs: []string{a.ID, b.ID},
		CanaryCount:    1,
		BatchSize:      1,
	})
	require.NoError(t, err)

	require.NoError(t, service.AdvanceRollout(context.Background(), rollout.ID))

	assertRolloutItemStatus(t, database, rollout.ID, a.ID, ItemStatusFailed)
	assertRolloutItemStatus(t, database, rollout.ID, b.ID, ItemStatusSkipped)
	assertRolloutStatus(t, database, rollout.ID, RolloutStatusFailed)
}

func TestAdvanceRolloutExpiresRunningItem(t *testing.T) {
	database := setupRolloutDB(t)
	agent := createRolloutAgent(t, database, "a", "online", "v0.5.41", "amd64", nil)
	service := NewService(database, &fakeHub{online: map[string]bool{agent.ID: true}, accepted: map[string]bool{agent.ID: true}})
	now := time.Date(2026, 7, 9, 1, 0, 0, 0, time.UTC)
	service.Now = func() time.Time { return now }
	service.ItemTimeout = time.Minute
	rollout, _, err := service.CreateRollout(context.Background(), CreateInput{
		TargetVersion:  "v0.5.42",
		TargetAgentIDs: []string{agent.ID},
	})
	require.NoError(t, err)
	require.NoError(t, service.AdvanceRollout(context.Background(), rollout.ID))

	now = now.Add(2 * time.Minute)
	require.NoError(t, service.AdvanceRollout(context.Background(), rollout.ID))

	assertRolloutItemStatus(t, database, rollout.ID, agent.ID, ItemStatusFailed)
	assertRolloutStatus(t, database, rollout.ID, RolloutStatusFailed)
}

func TestCreateRolloutMarksAlreadyCurrentSuccess(t *testing.T) {
	database := setupRolloutDB(t)
	agent := createRolloutAgent(t, database, "a", "online", "v0.5.42", "amd64", nil)
	service := NewService(database, &fakeHub{online: map[string]bool{agent.ID: true}})

	rollout, _, err := service.CreateRollout(context.Background(), CreateInput{
		TargetVersion:  "v0.5.42",
		TargetAgentIDs: []string{agent.ID},
	})
	require.NoError(t, err)
	require.NoError(t, service.AdvanceRollout(context.Background(), rollout.ID))

	assertRolloutItemStatus(t, database, rollout.ID, agent.ID, ItemStatusSuccess)
	assertRolloutStatus(t, database, rollout.ID, RolloutStatusSucceeded)
}

func TestAdvanceAllResumesWithoutDuplicatingSuccessfulItems(t *testing.T) {
	database := setupRolloutDB(t)
	a := createRolloutAgent(t, database, "a", "online", "v0.5.42", "amd64", nil)
	b := createRolloutAgent(t, database, "b", "online", "v0.5.41", "amd64", nil)
	rollout := db.AgentUpgradeRollout{
		TargetVersion: "v0.5.42",
		CanaryCount:   1,
		BatchSize:     1,
		Status:        RolloutStatusRunning,
	}
	require.NoError(t, database.DB.Create(&rollout).Error)
	require.NoError(t, database.DB.Create(&db.AgentUpgradeRolloutItem{
		RolloutID:      rollout.ID,
		AgentID:        a.ID,
		Phase:          PhaseCanary,
		Status:         ItemStatusSuccess,
		CurrentVersion: "v0.5.41",
		TargetVersion:  "v0.5.42",
	}).Error)
	require.NoError(t, database.DB.Create(&db.AgentUpgradeRolloutItem{
		RolloutID:      rollout.ID,
		AgentID:        b.ID,
		Phase:          PhaseBatch,
		BatchIndex:     1,
		Status:         ItemStatusPending,
		CurrentVersion: "v0.5.41",
		TargetVersion:  "v0.5.42",
	}).Error)
	hub := &fakeHub{online: map[string]bool{a.ID: true, b.ID: true}, accepted: map[string]bool{b.ID: true}}
	service := NewService(database, hub)

	require.NoError(t, service.AdvanceAll(context.Background()))

	assert.Equal(t, []string{b.ID}, hub.sentAgents)
	assertRolloutItemStatus(t, database, rollout.ID, a.ID, ItemStatusSuccess)
	assertRolloutItemStatus(t, database, rollout.ID, b.ID, ItemStatusRunning)
}

func setupRolloutDB(t *testing.T) *db.Database {
	t.Helper()
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	return database
}

func createRolloutAgent(t *testing.T, database *db.Database, name string, status string, version string, arch string, tags []string) db.Agent {
	t.Helper()
	info, err := json.Marshal(map[string]any{
		"version": version,
		"arch":    arch,
	})
	require.NoError(t, err)
	rawTags, err := json.Marshal(tags)
	require.NoError(t, err)
	agent := db.Agent{
		Name:       name,
		Status:     status,
		SystemInfo: string(info),
		Tags:       string(rawTags),
	}
	require.NoError(t, database.DB.Create(&agent).Error)
	return agent
}

func assertRolloutItemStatus(t *testing.T, database *db.Database, rolloutID string, agentID string, status string) {
	t.Helper()
	var item db.AgentUpgradeRolloutItem
	require.NoError(t, database.DB.First(&item, "rollout_id = ? AND agent_id = ?", rolloutID, agentID).Error)
	assert.Equal(t, status, item.Status)
}

func assertRolloutStatus(t *testing.T, database *db.Database, rolloutID string, status string) {
	t.Helper()
	var rollout db.AgentUpgradeRollout
	require.NoError(t, database.DB.First(&rollout, "id = ?", rolloutID).Error)
	assert.Equal(t, status, rollout.Status)
}

type fakeHub struct {
	online     map[string]bool
	accepted   map[string]bool
	errText    map[string]string
	sendErr    map[string]error
	sentAgents []string
}

func (h *fakeHub) IsOnline(agentID string) bool {
	return h.online[agentID]
}

func (h *fakeHub) SendAndWait(agentID string, msg protocol.Message, _ time.Duration) (<-chan protocol.Message, error) {
	if err := h.sendErr[agentID]; err != nil {
		return nil, err
	}
	if h.accepted == nil {
		h.accepted = map[string]bool{agentID: true}
	}
	h.sentAgents = append(h.sentAgents, agentID)
	ch := make(chan protocol.Message, 1)
	accepted, ok := h.accepted[agentID]
	if !ok {
		accepted = true
	}
	resp, err := protocol.NewMessage(protocol.TypeUpdateAgentResp, protocol.UpdateAgentRespPayload{
		Accepted: accepted,
		Error:    h.errText[agentID],
	})
	if err != nil {
		close(ch)
		return ch, err
	}
	resp.ID = msg.ID
	ch <- *resp
	close(ch)
	return ch, nil
}

var _ Hub = (*fakeHub)(nil)
