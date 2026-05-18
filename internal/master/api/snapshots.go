package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

const snapshotRequestTimeout = 30 * time.Second

type SnapshotHandler struct {
	DB  *db.Database
	Hub SnapshotHub

	timeout     time.Duration
	sendAndWait func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

type SnapshotHub interface {
	IsOnline(agentID string) bool
	SendAndWait(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

type snapshotResponse struct {
	ID         string    `json:"id"`
	SnapshotID string    `json:"snapshot_id"`
	Timestamp  time.Time `json:"timestamp"`
	Paths      []string  `json:"paths"`
	Size       int64     `json:"size"`
}

type snapshotRefreshResponse struct {
	Count     int                `json:"count"`
	Snapshots []snapshotResponse `json:"snapshots"`
}

func NewSnapshotHandler(database *db.Database, hub SnapshotHub) *SnapshotHandler {
	handler := &SnapshotHandler{
		DB:      database,
		Hub:     hub,
		timeout: snapshotRequestTimeout,
	}
	handler.sendAndWait = func(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error) {
		return handler.Hub.SendAndWait(agentID, msg, timeout)
	}
	return handler
}

func RegisterSnapshotRoutes(rg *gin.RouterGroup, h *SnapshotHandler) {
	rg.GET("/agents/:id/snapshots", h.ListSnapshots)
	rg.POST("/agents/:id/snapshots/refresh", h.RefreshSnapshots)
}

func (h *SnapshotHandler) ListSnapshots(c *gin.Context) {
	agentID := c.Param("id")
	if !agentExistsByID(c, h.DB, agentID) {
		return
	}

	snapshots, ok := h.loadSnapshotResponses(c, agentID)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, snapshots)
}

func (h *SnapshotHandler) RefreshSnapshots(c *gin.Context) {
	agentID := c.Param("id")
	if !agentExistsByID(c, h.DB, agentID) {
		return
	}
	if h.Hub == nil || !h.Hub.IsOnline(agentID) {
		c.JSON(http.StatusBadGateway, gin.H{"error": "agent offline"})
		return
	}

	msg, err := protocol.NewMessage(protocol.TypeSnapshotListReq, protocol.SnapshotListReqPayload{AgentID: agentID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encode snapshot list request"})
		return
	}

	wait := h.sendAndWait
	if wait == nil && h.Hub != nil {
		wait = h.Hub.SendAndWait
	}
	if wait == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "agent offline"})
		return
	}

	respCh, err := wait(agentID, *msg, h.timeout)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "agent offline"})
		return
	}

	select {
	case resp, ok := <-respCh:
		if !ok {
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "timeout waiting for agent response"})
			return
		}
		if resp.Type != protocol.TypeSnapshotListResp {
			c.JSON(http.StatusBadGateway, gin.H{"error": "invalid agent response"})
			return
		}
		payload, err := protocol.ParsePayload[protocol.SnapshotListRespPayload](&resp)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "invalid agent response"})
			return
		}
		if payload.Error != "" {
			c.JSON(http.StatusBadGateway, gin.H{"error": payload.Error})
			return
		}
		if err := upsertSnapshots(h.DB, agentID, payload.Snapshots); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
			return
		}
		snapshots, ok := h.loadSnapshotResponses(c, agentID)
		if !ok {
			return
		}
		c.JSON(http.StatusOK, snapshotRefreshResponse{Count: len(payload.Snapshots), Snapshots: snapshots})
	case <-c.Request.Context().Done():
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "request cancelled"})
	}
}

func (h *SnapshotHandler) loadSnapshotResponses(c *gin.Context, agentID string) ([]snapshotResponse, bool) {
	var snapshots []db.Snapshot
	if err := h.DB.DB.Where("agent_id = ?", agentID).Order("timestamp DESC").Find(&snapshots).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return nil, false
	}

	responses := make([]snapshotResponse, 0, len(snapshots))
	for _, snapshot := range snapshots {
		response, err := newSnapshotResponse(snapshot)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "decode snapshot"})
			return nil, false
		}
		responses = append(responses, response)
	}
	return responses, true
}

func newSnapshotResponse(snapshot db.Snapshot) (snapshotResponse, error) {
	var paths []string
	if snapshot.Paths != "" {
		if err := json.Unmarshal([]byte(snapshot.Paths), &paths); err != nil {
			return snapshotResponse{}, err
		}
	}
	return snapshotResponse{
		ID:         snapshot.ID,
		SnapshotID: snapshot.SnapshotID,
		Timestamp:  snapshot.Timestamp,
		Paths:      paths,
		Size:       snapshot.Size,
	}, nil
}

func NewTaskResultProcessor(database *db.Database) func(agentID string, msg protocol.Message) error {
	return func(agentID string, msg protocol.Message) error {
		result, err := protocol.ParsePayload[protocol.TaskResultPayload](&msg)
		if err != nil {
			return err
		}
		return recordTaskResult(database, agentID, msg.ID, *result)
	}
}

func recordTaskResult(database *db.Database, agentID string, messageID string, result protocol.TaskResultPayload) error {
	if database == nil || database.DB == nil {
		return errors.New("database not configured")
	}
	if agentID == "" {
		agentID = result.AgentID
	}
	if result.TaskType == "restore" {
		return completeRestoreTaskResult(database, agentID, messageID, result)
	}
	startedAt := result.StartedAt
	finishedAt := result.FinishedAt
	history := db.TaskHistory{
		AgentID:    agentID,
		Type:       result.TaskType,
		Status:     result.Status,
		SnapshotID: result.SnapshotID,
		StartedAt:  &startedAt,
		FinishedAt: &finishedAt,
		DurationMs: result.DurationMs,
		RepoSize:   result.RepoSize,
		ErrorLog:   result.ErrorLog,
	}
	if result.StartedAt.IsZero() {
		history.StartedAt = nil
	}
	if result.FinishedAt.IsZero() {
		history.FinishedAt = nil
	}
	if err := database.DB.Create(&history).Error; err != nil {
		return err
	}
	if result.TaskType == "backup" && result.Status == "success" && len(result.Snapshots) > 0 {
		return upsertSnapshots(database, agentID, result.Snapshots)
	}
	return nil
}

func completeRestoreTaskResult(database *db.Database, agentID string, messageID string, result protocol.TaskResultPayload) error {
	startedAt := result.StartedAt
	finishedAt := result.FinishedAt
	if messageID != "" {
		var history db.TaskHistory
		err := database.DB.
			Where("agent_id = ? AND type = ? AND status = ? AND message_id = ?", agentID, "restore", "running", messageID).
			First(&history).Error
		if err == nil {
			history.Status = result.Status
			history.DurationMs = result.DurationMs
			history.RepoSize = result.RepoSize
			history.ErrorLog = result.ErrorLog
			if result.SnapshotID != "" {
				history.SnapshotID = result.SnapshotID
			}
			if !result.StartedAt.IsZero() {
				history.StartedAt = &startedAt
			}
			if !result.FinishedAt.IsZero() {
				history.FinishedAt = &finishedAt
			}
			return database.DB.Save(&history).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}

	history := db.TaskHistory{
		AgentID:    agentID,
		Type:       result.TaskType,
		Status:     result.Status,
		SnapshotID: result.SnapshotID,
		MessageID:  messageID,
		StartedAt:  &startedAt,
		FinishedAt: &finishedAt,
		DurationMs: result.DurationMs,
		RepoSize:   result.RepoSize,
		ErrorLog:   result.ErrorLog,
	}
	if result.StartedAt.IsZero() {
		history.StartedAt = nil
	}
	if result.FinishedAt.IsZero() {
		history.FinishedAt = nil
	}
	return database.DB.Create(&history).Error
}

func upsertSnapshots(database *db.Database, agentID string, snapshots []protocol.SnapshotInfo) error {
	if database == nil || database.DB == nil {
		return errors.New("database not configured")
	}
	for _, snapshotInfo := range snapshots {
		if snapshotInfo.ID == "" {
			continue
		}
		paths, err := json.Marshal(snapshotInfo.Paths)
		if err != nil {
			return err
		}

		snapshot := db.Snapshot{
			AgentID:    agentID,
			SnapshotID: snapshotInfo.ID,
			Timestamp:  snapshotInfo.Time,
			Paths:      string(paths),
			Size:       snapshotInfo.Size,
		}
		err = database.DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "agent_id"},
				{Name: "snapshot_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"timestamp", "paths", "size"}),
		}).Create(&snapshot).Error
		if err != nil {
			return err
		}
	}
	return nil
}

func agentExistsByID(c *gin.Context, database *db.Database, agentID string) bool {
	var agent db.Agent
	if err := database.DB.First(&agent, "id = ?", agentID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "agent not found"})
			return false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return false
	}
	return true
}
