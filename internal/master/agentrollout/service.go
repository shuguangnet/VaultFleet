package agentrollout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

const (
	RolloutStatusPending   = "pending"
	RolloutStatusRunning   = "running"
	RolloutStatusSucceeded = "succeeded"
	RolloutStatusFailed    = "failed"
	RolloutStatusCancelled = "cancelled"

	ItemStatusPending = "pending"
	ItemStatusRunning = "running"
	ItemStatusSuccess = "success"
	ItemStatusFailed  = "failed"
	ItemStatusSkipped = "skipped"

	PhaseCanary = "canary"
	PhaseBatch  = "batch"

	DefaultCanaryCount = 1
	DefaultBatchSize   = 5
	MaxCanaryCount     = 10
	MaxBatchSize       = 50
)

var (
	ErrNoTargets             = errors.New("rollout target selection is required")
	ErrNoMatchedTargets      = errors.New("rollout target selection matched no agents")
	ErrDuplicateActiveTarget = errors.New("agent already has an active rollout")
)

type Hub interface {
	IsOnline(agentID string) bool
	SendAndWait(agentID string, msg protocol.Message, timeout time.Duration) (<-chan protocol.Message, error)
}

type Actor struct {
	Type string
	ID   string
	Name string
}

type CreateInput struct {
	TargetVersion  string
	GitHubRepo     string
	TargetTags     []string
	TargetAgentIDs []string
	CanaryCount    int
	BatchSize      int
	Actor          Actor
}

type Service struct {
	DB          *db.Database
	Hub         Hub
	Now         func() time.Time
	ACKTimeout  time.Duration
	ItemTimeout time.Duration
}

func NewService(database *db.Database, hub Hub) *Service {
	return &Service{
		DB:          database,
		Hub:         hub,
		Now:         time.Now,
		ACKTimeout:  15 * time.Second,
		ItemTimeout: 10 * time.Minute,
	}
}

func (s *Service) CreateRollout(ctx context.Context, input CreateInput) (db.AgentUpgradeRollout, []db.AgentUpgradeRolloutItem, error) {
	if s == nil || s.DB == nil || s.DB.DB == nil {
		return db.AgentUpgradeRollout{}, nil, errors.New("database not configured")
	}
	input, err := normalizeCreateInput(input)
	if err != nil {
		return db.AgentUpgradeRollout{}, nil, err
	}

	agents, err := s.resolveTargets(ctx, input.TargetAgentIDs, input.TargetTags)
	if err != nil {
		return db.AgentUpgradeRollout{}, nil, err
	}
	if len(agents) == 0 {
		return db.AgentUpgradeRollout{}, nil, ErrNoMatchedTargets
	}
	if conflict := s.firstActiveConflict(ctx, agents); conflict != "" {
		return db.AgentUpgradeRollout{}, nil, fmt.Errorf("%w: %s", ErrDuplicateActiveTarget, conflict)
	}

	targetTags, _ := json.Marshal(input.TargetTags)
	targetIDs, _ := json.Marshal(input.TargetAgentIDs)
	now := s.now()
	rollout := db.AgentUpgradeRollout{
		TargetVersion:  input.TargetVersion,
		GitHubRepo:     input.GitHubRepo,
		TargetTags:     string(targetTags),
		TargetAgentIDs: string(targetIDs),
		CanaryCount:    input.CanaryCount,
		BatchSize:      input.BatchSize,
		Status:         RolloutStatusPending,
		CreatedByType:  input.Actor.Type,
		CreatedByID:    input.Actor.ID,
		CreatedByName:  input.Actor.Name,
	}

	items := s.planItems(agents, input.TargetVersion, input.CanaryCount, input.BatchSize, now)
	err = s.DB.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&rollout).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].RolloutID = rollout.ID
			if err := tx.Create(&items[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return db.AgentUpgradeRollout{}, nil, err
	}
	return rollout, items, nil
}

func (s *Service) AdvanceAll(ctx context.Context) error {
	if s == nil || s.DB == nil || s.DB.DB == nil {
		return nil
	}
	var rollouts []db.AgentUpgradeRollout
	if err := s.DB.DB.WithContext(ctx).
		Where("status IN ?", []string{RolloutStatusPending, RolloutStatusRunning}).
		Order("created_at ASC").
		Find(&rollouts).Error; err != nil {
		return err
	}
	for _, rollout := range rollouts {
		if err := s.AdvanceRollout(ctx, rollout.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) AdvanceRollout(ctx context.Context, rolloutID string) error {
	if s == nil || s.DB == nil || s.DB.DB == nil || rolloutID == "" {
		return nil
	}
	if err := s.expireTimedOutItems(ctx, rolloutID); err != nil {
		return err
	}

	var rollout db.AgentUpgradeRollout
	if err := s.DB.DB.WithContext(ctx).First(&rollout, "id = ?", rolloutID).Error; err != nil {
		return err
	}
	if rolloutTerminal(rollout.Status) {
		return nil
	}

	var failedCount int64
	if err := s.DB.DB.WithContext(ctx).Model(&db.AgentUpgradeRolloutItem{}).
		Where("rollout_id = ? AND status = ?", rolloutID, ItemStatusFailed).
		Count(&failedCount).Error; err != nil {
		return err
	}
	if failedCount > 0 {
		return s.failRollout(ctx, rolloutID, "rollout item failed")
	}

	var runningCount int64
	if err := s.DB.DB.WithContext(ctx).Model(&db.AgentUpgradeRolloutItem{}).
		Where("rollout_id = ? AND status = ?", rolloutID, ItemStatusRunning).
		Count(&runningCount).Error; err != nil {
		return err
	}
	if runningCount > 0 {
		return s.markRolloutRunning(ctx, rolloutID)
	}

	var remainingCount int64
	if err := s.DB.DB.WithContext(ctx).Model(&db.AgentUpgradeRolloutItem{}).
		Where("rollout_id = ? AND status = ?", rolloutID, ItemStatusPending).
		Count(&remainingCount).Error; err != nil {
		return err
	}
	if remainingCount == 0 {
		return s.completeRollout(ctx, rolloutID)
	}

	items, err := s.nextItems(ctx, rollout)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return s.markRolloutRunning(ctx, rolloutID)
	}
	if err := s.markRolloutRunning(ctx, rolloutID); err != nil {
		return err
	}
	for _, item := range items {
		if err := s.startItem(ctx, rollout, item); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) HandleHeartbeat(ctx context.Context, agentID string, version string) error {
	if s == nil || s.DB == nil || s.DB.DB == nil || agentID == "" || strings.TrimSpace(version) == "" {
		return nil
	}
	now := s.now()
	var items []db.AgentUpgradeRolloutItem
	if err := s.DB.DB.WithContext(ctx).
		Where("agent_id = ? AND status = ?", agentID, ItemStatusRunning).
		Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		updates := map[string]any{
			"last_seen_version": version,
			"updated_at":        now,
		}
		if version == item.TargetVersion {
			updates["status"] = ItemStatusSuccess
			updates["completed_at"] = &now
			updates["error"] = ""
		}
		if err := s.DB.DB.WithContext(ctx).Model(&db.AgentUpgradeRolloutItem{}).
			Where("id = ?", item.ID).
			Updates(updates).Error; err != nil {
			return err
		}
		if version == item.TargetVersion {
			s.recordAudit("agent_rollout.item_success", item.ID, "success", item.AgentID)
			if err := s.AdvanceRollout(ctx, item.RolloutID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) HasActiveItem(ctx context.Context, agentID string) (bool, error) {
	if s == nil || s.DB == nil || s.DB.DB == nil || agentID == "" {
		return false, nil
	}
	var count int64
	err := s.DB.DB.WithContext(ctx).Model(&db.AgentUpgradeRolloutItem{}).
		Joins("JOIN agent_upgrade_rollouts ON agent_upgrade_rollouts.id = agent_upgrade_rollout_items.rollout_id").
		Where("agent_upgrade_rollout_items.agent_id = ? AND agent_upgrade_rollout_items.status IN ? AND agent_upgrade_rollouts.status IN ?",
			agentID,
			[]string{ItemStatusPending, ItemStatusRunning},
			[]string{RolloutStatusPending, RolloutStatusRunning},
		).
		Count(&count).Error
	return count > 0, err
}

func (s *Service) CancelRollout(ctx context.Context, rolloutID string, reason string) error {
	if s == nil || s.DB == nil || s.DB.DB == nil || rolloutID == "" {
		return nil
	}
	now := s.now()
	if strings.TrimSpace(reason) == "" {
		reason = "cancelled by operator"
	}
	err := s.DB.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.AgentUpgradeRollout{}).
			Where("id = ? AND status IN ?", rolloutID, []string{RolloutStatusPending, RolloutStatusRunning}).
			Updates(map[string]any{
				"status":         RolloutStatusCancelled,
				"failure_reason": reason,
				"completed_at":   &now,
				"updated_at":     now,
			}).Error; err != nil {
			return err
		}
		return tx.Model(&db.AgentUpgradeRolloutItem{}).
			Where("rollout_id = ? AND status IN ?", rolloutID, []string{ItemStatusPending, ItemStatusRunning}).
			Updates(map[string]any{
				"status":       ItemStatusSkipped,
				"skip_reason":  reason,
				"completed_at": &now,
				"updated_at":   now,
			}).Error
	})
	if err == nil {
		s.recordAudit("agent_rollout.cancelled", rolloutID, "success", reason)
		s.recordSkippedItems(rolloutID, reason)
	}
	return err
}

func (s *Service) startItem(ctx context.Context, rollout db.AgentUpgradeRollout, item db.AgentUpgradeRolloutItem) error {
	if s.Hub == nil || !s.Hub.IsOnline(item.AgentID) {
		return s.failItem(ctx, item, "agent offline")
	}
	msg, err := protocol.NewMessage(protocol.TypeUpdateAgent, protocol.UpdateAgentPayload{
		Version:    rollout.TargetVersion,
		GitHubRepo: rollout.GitHubRepo,
	})
	if err != nil {
		return s.failItem(ctx, item, "encode update request")
	}
	now := s.now()
	deadline := now.Add(s.itemTimeout())
	if err := s.DB.DB.WithContext(ctx).Model(&db.AgentUpgradeRolloutItem{}).
		Where("id = ?", item.ID).
		Updates(map[string]any{
			"status":      ItemStatusRunning,
			"message_id":  msg.ID,
			"started_at":  &now,
			"deadline_at": &deadline,
			"updated_at":  now,
		}).Error; err != nil {
		return err
	}

	respCh, err := s.Hub.SendAndWait(item.AgentID, *msg, s.ackTimeout())
	if err != nil {
		return s.failItem(ctx, item, err.Error())
	}
	select {
	case resp, ok := <-respCh:
		if !ok {
			return s.failItem(ctx, item, "timeout waiting for update acknowledgement")
		}
		payload, err := protocol.ParsePayload[protocol.UpdateAgentRespPayload](&resp)
		if err != nil {
			return s.failItem(ctx, item, "invalid update acknowledgement")
		}
		if !payload.Accepted {
			msg := strings.TrimSpace(payload.Error)
			if msg == "" {
				msg = "agent rejected update"
			}
			return s.failItem(ctx, item, msg)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) failItem(ctx context.Context, item db.AgentUpgradeRolloutItem, reason string) error {
	now := s.now()
	if strings.TrimSpace(reason) == "" {
		reason = "item failed"
	}
	if err := s.DB.DB.WithContext(ctx).Model(&db.AgentUpgradeRolloutItem{}).
		Where("id = ?", item.ID).
		Updates(map[string]any{
			"status":       ItemStatusFailed,
			"error":        reason,
			"completed_at": &now,
			"updated_at":   now,
		}).Error; err != nil {
		return err
	}
	s.recordAudit("agent_rollout.item_failed", item.ID, "failure", reason)
	return s.failRollout(ctx, item.RolloutID, reason)
}

func (s *Service) failRollout(ctx context.Context, rolloutID string, reason string) error {
	now := s.now()
	if strings.TrimSpace(reason) == "" {
		reason = "rollout failed"
	}
	err := s.DB.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&db.AgentUpgradeRollout{}).
			Where("id = ? AND status IN ?", rolloutID, []string{RolloutStatusPending, RolloutStatusRunning}).
			Updates(map[string]any{
				"status":         RolloutStatusFailed,
				"failure_reason": reason,
				"completed_at":   &now,
				"updated_at":     now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&db.AgentUpgradeRolloutItem{}).
			Where("rollout_id = ? AND status = ?", rolloutID, ItemStatusPending).
			Updates(map[string]any{
				"status":       ItemStatusSkipped,
				"skip_reason":  "skipped because rollout stopped: " + reason,
				"completed_at": &now,
				"updated_at":   now,
			}).Error; err != nil {
			return err
		}
		return nil
	})
	if err == nil {
		s.recordAudit("agent_rollout.failed", rolloutID, "failure", reason)
		s.recordSkippedItems(rolloutID, "skipped because rollout stopped: "+reason)
	}
	return err
}

func (s *Service) markRolloutRunning(ctx context.Context, rolloutID string) error {
	now := s.now()
	updates := map[string]any{
		"status":     RolloutStatusRunning,
		"updated_at": now,
	}
	var rollout db.AgentUpgradeRollout
	if err := s.DB.DB.WithContext(ctx).First(&rollout, "id = ?", rolloutID).Error; err == nil && rollout.StartedAt == nil {
		updates["started_at"] = &now
	}
	return s.DB.DB.WithContext(ctx).Model(&db.AgentUpgradeRollout{}).
		Where("id = ? AND status IN ?", rolloutID, []string{RolloutStatusPending, RolloutStatusRunning}).
		Updates(updates).Error
}

func (s *Service) completeRollout(ctx context.Context, rolloutID string) error {
	now := s.now()
	var failedCount int64
	if err := s.DB.DB.WithContext(ctx).Model(&db.AgentUpgradeRolloutItem{}).
		Where("rollout_id = ? AND status = ?", rolloutID, ItemStatusFailed).
		Count(&failedCount).Error; err != nil {
		return err
	}
	status := RolloutStatusSucceeded
	reason := ""
	if failedCount > 0 {
		status = RolloutStatusFailed
		reason = "rollout item failed"
	}
	err := s.DB.DB.WithContext(ctx).Model(&db.AgentUpgradeRollout{}).
		Where("id = ? AND status IN ?", rolloutID, []string{RolloutStatusPending, RolloutStatusRunning}).
		Updates(map[string]any{
			"status":         status,
			"failure_reason": reason,
			"completed_at":   &now,
			"updated_at":     now,
		}).Error
	if err == nil {
		result := "success"
		action := "agent_rollout.succeeded"
		if status == RolloutStatusFailed {
			result = "failure"
			action = "agent_rollout.failed"
		}
		s.recordAudit(action, rolloutID, result, reason)
	}
	return err
}

func (s *Service) expireTimedOutItems(ctx context.Context, rolloutID string) error {
	now := s.now()
	var items []db.AgentUpgradeRolloutItem
	if err := s.DB.DB.WithContext(ctx).
		Where("rollout_id = ? AND status = ? AND deadline_at IS NOT NULL AND deadline_at <= ?", rolloutID, ItemStatusRunning, now).
		Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		if err := s.failItem(ctx, item, "timeout waiting for target version heartbeat"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) nextItems(ctx context.Context, rollout db.AgentUpgradeRollout) ([]db.AgentUpgradeRolloutItem, error) {
	var canaryPending []db.AgentUpgradeRolloutItem
	if err := s.DB.DB.WithContext(ctx).
		Where("rollout_id = ? AND status = ? AND phase = ?", rollout.ID, ItemStatusPending, PhaseCanary).
		Order("batch_index ASC, created_at ASC").
		Find(&canaryPending).Error; err != nil {
		return nil, err
	}
	if len(canaryPending) > 0 {
		return canaryPending, nil
	}

	var canaryUnsuccessful int64
	if err := s.DB.DB.WithContext(ctx).Model(&db.AgentUpgradeRolloutItem{}).
		Where("rollout_id = ? AND phase = ? AND status NOT IN ?", rollout.ID, PhaseCanary, []string{ItemStatusSuccess, ItemStatusSkipped}).
		Count(&canaryUnsuccessful).Error; err != nil {
		return nil, err
	}
	if canaryUnsuccessful > 0 {
		return nil, nil
	}

	var firstPending db.AgentUpgradeRolloutItem
	if err := s.DB.DB.WithContext(ctx).
		Where("rollout_id = ? AND status = ? AND phase = ?", rollout.ID, ItemStatusPending, PhaseBatch).
		Order("batch_index ASC, created_at ASC").
		First(&firstPending).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var items []db.AgentUpgradeRolloutItem
	err := s.DB.DB.WithContext(ctx).
		Where("rollout_id = ? AND status = ? AND phase = ? AND batch_index = ?", rollout.ID, ItemStatusPending, PhaseBatch, firstPending.BatchIndex).
		Order("created_at ASC").
		Limit(positiveOrDefault(rollout.BatchSize, DefaultBatchSize)).
		Find(&items).Error
	return items, err
}

func (s *Service) planItems(agents []db.Agent, targetVersion string, canaryCount int, batchSize int, now time.Time) []db.AgentUpgradeRolloutItem {
	items := make([]db.AgentUpgradeRolloutItem, 0, len(agents))
	runnableIndex := 0
	for _, agent := range agents {
		info := parseSystemInfo(agent.SystemInfo)
		item := db.AgentUpgradeRolloutItem{
			AgentID:        agent.ID,
			Status:         ItemStatusPending,
			CurrentVersion: info.Version,
			TargetVersion:  targetVersion,
			Architecture:   info.Arch,
		}
		switch {
		case agent.Status != "online":
			item.Status = ItemStatusSkipped
			item.SkipReason = "agent offline"
			item.CompletedAt = &now
		case info.Version == "":
			item.Status = ItemStatusSkipped
			item.SkipReason = "agent version unknown"
			item.CompletedAt = &now
		case info.Arch == "":
			item.Status = ItemStatusSkipped
			item.SkipReason = "agent architecture unknown"
			item.CompletedAt = &now
		case info.Version == targetVersion:
			item.Status = ItemStatusSuccess
			item.LastSeenVersion = info.Version
			item.CompletedAt = &now
		default:
			if runnableIndex < canaryCount {
				item.Phase = PhaseCanary
				item.BatchIndex = 0
			} else {
				item.Phase = PhaseBatch
				item.BatchIndex = (runnableIndex-canaryCount)/positiveOrDefault(batchSize, DefaultBatchSize) + 1
			}
			runnableIndex++
		}
		items = append(items, item)
	}
	return items
}

func (s *Service) resolveTargets(ctx context.Context, explicitIDs []string, tags []string) ([]db.Agent, error) {
	var agents []db.Agent
	if err := s.DB.DB.WithContext(ctx).Order("name ASC, id ASC").Find(&agents).Error; err != nil {
		return nil, err
	}
	explicitSet := make(map[string]bool, len(explicitIDs))
	for _, id := range explicitIDs {
		explicitSet[id] = true
	}
	matches := make([]db.Agent, 0, len(agents))
	seen := map[string]bool{}
	for _, agent := range agents {
		if explicitSet[agent.ID] || hasAllTags(agentTags(agent), tags) {
			if !seen[agent.ID] {
				matches = append(matches, agent)
				seen[agent.ID] = true
			}
		}
	}
	return matches, nil
}

func (s *Service) firstActiveConflict(ctx context.Context, agents []db.Agent) string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.ID)
	}
	if len(ids) == 0 {
		return ""
	}
	var item db.AgentUpgradeRolloutItem
	err := s.DB.DB.WithContext(ctx).
		Joins("JOIN agent_upgrade_rollouts ON agent_upgrade_rollouts.id = agent_upgrade_rollout_items.rollout_id").
		Where("agent_upgrade_rollout_items.agent_id IN ? AND agent_upgrade_rollout_items.status IN ? AND agent_upgrade_rollouts.status IN ?",
			ids,
			[]string{ItemStatusPending, ItemStatusRunning},
			[]string{RolloutStatusPending, RolloutStatusRunning},
		).
		Order("agent_upgrade_rollout_items.created_at ASC").
		First(&item).Error
	if err != nil {
		return ""
	}
	return item.AgentID
}

func normalizeCreateInput(input CreateInput) (CreateInput, error) {
	input.TargetVersion = strings.TrimSpace(input.TargetVersion)
	input.GitHubRepo = strings.TrimSpace(input.GitHubRepo)
	input.TargetAgentIDs = normalizeIDs(input.TargetAgentIDs)
	tags, err := normalizeTags(input.TargetTags)
	if err != nil {
		return input, err
	}
	input.TargetTags = tags
	if input.TargetVersion == "" {
		return input, errors.New("target version is required")
	}
	if len(input.TargetAgentIDs) == 0 && len(input.TargetTags) == 0 {
		return input, ErrNoTargets
	}
	if input.CanaryCount <= 0 {
		input.CanaryCount = DefaultCanaryCount
	}
	if input.BatchSize <= 0 {
		input.BatchSize = DefaultBatchSize
	}
	if input.CanaryCount > MaxCanaryCount {
		return input, fmt.Errorf("canary count cannot exceed %d", MaxCanaryCount)
	}
	if input.BatchSize > MaxBatchSize {
		return input, fmt.Errorf("batch size cannot exceed %d", MaxBatchSize)
	}
	return input, nil
}

func normalizeIDs(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeTags(values []string) ([]string, error) {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		tag := strings.ToLower(strings.TrimSpace(value))
		if tag == "" {
			continue
		}
		if len(tag) > 40 {
			return nil, errors.New("tag is too long")
		}
		for _, ch := range tag {
			ok := ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '.' || ch == ':'
			if !ok {
				return nil, errors.New("tag contains unsupported characters")
			}
		}
		if !seen[tag] {
			seen[tag] = true
			result = append(result, tag)
		}
	}
	sort.Strings(result)
	return result, nil
}

func agentTags(agent db.Agent) []string {
	var tags []string
	if strings.TrimSpace(agent.Tags) == "" {
		return tags
	}
	if err := json.Unmarshal([]byte(agent.Tags), &tags); err != nil {
		return nil
	}
	normalized, err := normalizeTags(tags)
	if err != nil {
		return nil
	}
	return normalized
}

func hasAllTags(agentTags []string, required []string) bool {
	if len(required) == 0 {
		return false
	}
	set := map[string]bool{}
	for _, tag := range agentTags {
		set[tag] = true
	}
	for _, tag := range required {
		if !set[tag] {
			return false
		}
	}
	return true
}

type systemInfo struct {
	Version string `json:"version"`
	Arch    string `json:"arch"`
}

func parseSystemInfo(raw string) systemInfo {
	var info systemInfo
	if strings.TrimSpace(raw) == "" {
		return info
	}
	_ = json.Unmarshal([]byte(raw), &info)
	return info
}

func rolloutTerminal(status string) bool {
	switch status {
	case RolloutStatusSucceeded, RolloutStatusFailed, RolloutStatusCancelled:
		return true
	default:
		return false
	}
}

func positiveOrDefault(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) ackTimeout() time.Duration {
	if s != nil && s.ACKTimeout > 0 {
		return s.ACKTimeout
	}
	return 15 * time.Second
}

func (s *Service) itemTimeout() time.Duration {
	if s != nil && s.ItemTimeout > 0 {
		return s.ItemTimeout
	}
	return 10 * time.Minute
}

func (s *Service) recordSkippedItems(rolloutID string, message string) {
	if s == nil || s.DB == nil || s.DB.DB == nil {
		return
	}
	var items []db.AgentUpgradeRolloutItem
	if err := s.DB.DB.Where("rollout_id = ? AND status = ?", rolloutID, ItemStatusSkipped).Find(&items).Error; err != nil {
		return
	}
	for _, item := range items {
		s.recordAudit("agent_rollout.item_skipped", item.ID, "success", message)
	}
}

func (s *Service) recordAudit(action string, targetID string, result string, message string) {
	if s == nil || s.DB == nil || s.DB.DB == nil || strings.TrimSpace(action) == "" {
		return
	}
	if strings.TrimSpace(result) == "" {
		result = "success"
	}
	if len(message) > 500 {
		message = message[:500]
	}
	_ = s.DB.DB.Create(&db.AuditEvent{
		Action:     action,
		TargetType: "agent_rollout",
		TargetID:   targetID,
		Result:     result,
		Message:    message,
	}).Error
}
