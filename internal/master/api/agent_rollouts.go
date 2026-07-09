package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/agentrollout"
	"vaultfleet/internal/master/db"
)

type AgentRolloutHandler struct {
	DB         *db.Database
	Service    *agentrollout.Service
	Version    string
	GitHubRepo string
}

func NewAgentRolloutHandler(database *db.Database, service *agentrollout.Service) *AgentRolloutHandler {
	return &AgentRolloutHandler{DB: database, Service: service}
}

func RegisterAgentRolloutRoutes(rg *gin.RouterGroup, h *AgentRolloutHandler) {
	rg.POST("/agent-upgrade-rollouts", h.Create)
	rg.GET("/agent-upgrade-rollouts", h.List)
	rg.GET("/agent-upgrade-rollouts/:id", h.Get)
	rg.POST("/agent-upgrade-rollouts/:id/cancel", h.Cancel)
}

type createAgentRolloutRequest struct {
	TargetVersion  string   `json:"target_version"`
	GitHubRepo     string   `json:"github_repo"`
	TargetTags     []string `json:"target_tags"`
	TargetAgentIDs []string `json:"target_agent_ids"`
	CanaryCount    int      `json:"canary_count"`
	BatchSize      int      `json:"batch_size"`
}

type cancelAgentRolloutRequest struct {
	Reason string `json:"reason"`
}

type agentRolloutResponse struct {
	ID             string                 `json:"id"`
	TargetVersion  string                 `json:"target_version"`
	GitHubRepo     string                 `json:"github_repo,omitempty"`
	TargetTags     []string               `json:"target_tags"`
	TargetAgentIDs []string               `json:"target_agent_ids"`
	CanaryCount    int                    `json:"canary_count"`
	BatchSize      int                    `json:"batch_size"`
	Status         string                 `json:"status"`
	FailureReason  string                 `json:"failure_reason,omitempty"`
	Counts         map[string]int         `json:"counts"`
	Items          []agentRolloutItemView `json:"items,omitempty"`
	CreatedByType  string                 `json:"created_by_type,omitempty"`
	CreatedByID    string                 `json:"created_by_id,omitempty"`
	CreatedByName  string                 `json:"created_by_name,omitempty"`
	StartedAt      *time.Time             `json:"started_at,omitempty"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

type agentRolloutItemView struct {
	ID              string     `json:"id"`
	RolloutID       string     `json:"rollout_id"`
	AgentID         string     `json:"agent_id"`
	AgentName       string     `json:"agent_name,omitempty"`
	Phase           string     `json:"phase,omitempty"`
	BatchIndex      int        `json:"batch_index"`
	Status          string     `json:"status"`
	CurrentVersion  string     `json:"current_version,omitempty"`
	TargetVersion   string     `json:"target_version"`
	Architecture    string     `json:"architecture,omitempty"`
	MessageID       string     `json:"message_id,omitempty"`
	Error           string     `json:"error,omitempty"`
	SkipReason      string     `json:"skip_reason,omitempty"`
	LastSeenVersion string     `json:"last_seen_version,omitempty"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	DeadlineAt      *time.Time `json:"deadline_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (h *AgentRolloutHandler) Create(c *gin.Context) {
	var request createAgentRolloutRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}
	if strings.TrimSpace(request.TargetVersion) == "" {
		request.TargetVersion = defaultAgentUpdateVersion(h.Version)
	}
	if strings.TrimSpace(request.GitHubRepo) == "" {
		request.GitHubRepo = strings.TrimSpace(h.GitHubRepo)
	}
	actor := currentActor(c)
	input := agentrollout.CreateInput{
		TargetVersion:  request.TargetVersion,
		GitHubRepo:     request.GitHubRepo,
		TargetTags:     request.TargetTags,
		TargetAgentIDs: request.TargetAgentIDs,
		CanaryCount:    request.CanaryCount,
		BatchSize:      request.BatchSize,
	}
	if actor != nil {
		input.Actor = agentrollout.Actor{
			Type: actor.Type,
			ID:   firstNonEmptyString(actor.UserID, actor.TokenID),
			Name: firstNonEmptyString(actor.Username, actor.TokenName),
		}
	}

	rollout, items, err := h.service().CreateRollout(c.Request.Context(), input)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, agentrollout.ErrDuplicateActiveTarget) {
			status = http.StatusConflict
		}
		writeErrorResponse(c, status, err.Error())
		return
	}
	RecordAudit(h.DB, c, "agent_rollout.create", "agent_rollout", rollout.ID, AuditResultSuccess, rollout.TargetVersion)
	go func() {
		_ = h.service().AdvanceRollout(context.Background(), rollout.ID)
	}()

	response := h.rolloutResponse(rollout, items)
	writeDataResponse(c, http.StatusOK, response)
}

func (h *AgentRolloutHandler) List(c *gin.Context) {
	var rollouts []db.AgentUpgradeRollout
	if err := h.DB.DB.Order("created_at DESC").Limit(queryLimit(c, 50, 200)).Find(&rollouts).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	responses := make([]agentRolloutResponse, 0, len(rollouts))
	for _, rollout := range rollouts {
		responses = append(responses, h.rolloutResponse(rollout, nil))
	}
	writeDataResponse(c, http.StatusOK, responses)
}

func (h *AgentRolloutHandler) Get(c *gin.Context) {
	var rollout db.AgentUpgradeRollout
	if err := h.DB.DB.First(&rollout, "id = ?", c.Param("id")).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeErrorResponse(c, http.StatusNotFound, "rollout not found")
			return
		}
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	var items []db.AgentUpgradeRolloutItem
	if err := h.DB.DB.Where("rollout_id = ?", rollout.ID).Order("phase ASC, batch_index ASC, created_at ASC").Find(&items).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	writeDataResponse(c, http.StatusOK, h.rolloutResponse(rollout, items))
}

func (h *AgentRolloutHandler) Cancel(c *gin.Context) {
	var request cancelAgentRolloutRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			writeErrorResponse(c, http.StatusBadRequest, "invalid request")
			return
		}
	}
	if err := h.service().CancelRollout(c.Request.Context(), c.Param("id"), request.Reason); err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	RecordAudit(h.DB, c, "agent_rollout.cancel", "agent_rollout", c.Param("id"), AuditResultSuccess, request.Reason)
	writeDataResponse(c, http.StatusOK, gin.H{"id": c.Param("id"), "status": agentrollout.RolloutStatusCancelled})
}

func (h *AgentRolloutHandler) rolloutResponse(rollout db.AgentUpgradeRollout, items []db.AgentUpgradeRolloutItem) agentRolloutResponse {
	if items == nil {
		_ = h.DB.DB.Where("rollout_id = ?", rollout.ID).Find(&items).Error
	}
	return agentRolloutResponse{
		ID:             rollout.ID,
		TargetVersion:  rollout.TargetVersion,
		GitHubRepo:     rollout.GitHubRepo,
		TargetTags:     decodeStringSlice(rollout.TargetTags),
		TargetAgentIDs: decodeStringSlice(rollout.TargetAgentIDs),
		CanaryCount:    rollout.CanaryCount,
		BatchSize:      rollout.BatchSize,
		Status:         rollout.Status,
		FailureReason:  rollout.FailureReason,
		Counts:         rolloutItemCounts(items),
		Items:          h.itemResponses(items),
		CreatedByType:  rollout.CreatedByType,
		CreatedByID:    rollout.CreatedByID,
		CreatedByName:  rollout.CreatedByName,
		StartedAt:      rollout.StartedAt,
		CompletedAt:    rollout.CompletedAt,
		CreatedAt:      rollout.CreatedAt,
		UpdatedAt:      rollout.UpdatedAt,
	}
}

func (h *AgentRolloutHandler) itemResponses(items []db.AgentUpgradeRolloutItem) []agentRolloutItemView {
	if len(items) == 0 {
		return nil
	}
	agentIDs := make([]string, 0, len(items))
	for _, item := range items {
		agentIDs = append(agentIDs, item.AgentID)
	}
	var agents []db.Agent
	_ = h.DB.DB.Where("id IN ?", agentIDs).Find(&agents).Error
	names := map[string]string{}
	for _, agent := range agents {
		names[agent.ID] = agent.Name
	}

	responses := make([]agentRolloutItemView, 0, len(items))
	for _, item := range items {
		responses = append(responses, agentRolloutItemView{
			ID:              item.ID,
			RolloutID:       item.RolloutID,
			AgentID:         item.AgentID,
			AgentName:       names[item.AgentID],
			Phase:           item.Phase,
			BatchIndex:      item.BatchIndex,
			Status:          item.Status,
			CurrentVersion:  item.CurrentVersion,
			TargetVersion:   item.TargetVersion,
			Architecture:    item.Architecture,
			MessageID:       item.MessageID,
			Error:           item.Error,
			SkipReason:      item.SkipReason,
			LastSeenVersion: item.LastSeenVersion,
			StartedAt:       item.StartedAt,
			CompletedAt:     item.CompletedAt,
			DeadlineAt:      item.DeadlineAt,
			CreatedAt:       item.CreatedAt,
			UpdatedAt:       item.UpdatedAt,
		})
	}
	return responses
}

func (h *AgentRolloutHandler) service() *agentrollout.Service {
	if h.Service != nil {
		return h.Service
	}
	return agentrollout.NewService(h.DB, nil)
}

func rolloutItemCounts(items []db.AgentUpgradeRolloutItem) map[string]int {
	counts := map[string]int{
		agentrollout.ItemStatusPending: 0,
		agentrollout.ItemStatusRunning: 0,
		agentrollout.ItemStatusSuccess: 0,
		agentrollout.ItemStatusFailed:  0,
		agentrollout.ItemStatusSkipped: 0,
	}
	for _, item := range items {
		counts[item.Status]++
	}
	return counts
}

func decodeStringSlice(raw string) []string {
	var values []string
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	return values
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
