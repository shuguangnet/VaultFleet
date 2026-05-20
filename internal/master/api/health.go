package api

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
)

type AgentStatusProvider interface {
	OnlineAgentCount() int
}

type HealthHandler struct {
	DB     *db.Database
	Agents AgentStatusProvider
}

func NewHealthHandler(database *db.Database, agents AgentStatusProvider) *HealthHandler {
	return &HealthHandler{DB: database, Agents: agents}
}

func RegisterHealthRoutes(r *gin.Engine, h *HealthHandler) {
	r.GET("/health", h.Health)
	r.GET("/ready", h.Ready)
	r.GET("/metrics", h.Metrics)
}

func (h *HealthHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true, "status": "healthy"})
}

func (h *HealthHandler) Ready(c *gin.Context) {
	if !h.ready(c) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false, "error": "not ready"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "status": "ready"})
}

func (h *HealthHandler) Metrics(c *gin.Context) {
	var buf bytes.Buffer
	if err := h.writeMetrics(c, &buf); err != nil {
		c.Data(http.StatusInternalServerError, "text/plain; charset=utf-8", []byte("metrics unavailable\n"))
		return
	}

	c.Data(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", buf.Bytes())
}

func (h *HealthHandler) ready(c *gin.Context) bool {
	if h == nil || h.DB == nil || h.DB.DB == nil || len(h.DB.MasterKey) != 32 || strings.TrimSpace(h.DB.DataDir) == "" {
		return false
	}

	sqlDB, err := h.DB.DB.DB()
	if err != nil {
		return false
	}
	return sqlDB.PingContext(c.Request.Context()) == nil
}

func (h *HealthHandler) writeMetrics(c *gin.Context, buf *bytes.Buffer) error {
	if h == nil || h.DB == nil || h.DB.DB == nil {
		return fmt.Errorf("database not configured")
	}

	ctx := c.Request.Context()
	var totalAgents int64
	if err := h.DB.DB.WithContext(ctx).Model(&db.Agent{}).Count(&totalAgents).Error; err != nil {
		return err
	}

	fmt.Fprintf(buf, "vaultfleet_agents_total %d\n", totalAgents)
	fmt.Fprintf(buf, "vaultfleet_agents_online %d\n", h.onlineAgents())

	var commandRows []metricGroupRow
	if err := h.DB.DB.WithContext(ctx).
		Model(&db.AgentCommand{}).
		Select("status, type, count(*) as count").
		Group("status, type").
		Order("status, type").
		Scan(&commandRows).Error; err != nil {
		return err
	}
	for _, row := range commandRows {
		fmt.Fprintf(buf, "vaultfleet_agent_commands_total{status=%q,type=%q} %d\n", row.Status, row.Type, row.Count)
	}

	var taskRows []metricGroupRow
	if err := h.DB.DB.WithContext(ctx).
		Model(&db.TaskHistory{}).
		Select("status, type, count(*) as count").
		Group("status, type").
		Order("status, type").
		Scan(&taskRows).Error; err != nil {
		return err
	}
	for _, row := range taskRows {
		fmt.Fprintf(buf, "vaultfleet_tasks_total{status=%q,type=%q} %d\n", row.Status, row.Type, row.Count)
	}

	lastBackupTimestamp, err := h.lastSuccessfulBackupTimestamp(c)
	if err != nil {
		return err
	}
	fmt.Fprintf(buf, "vaultfleet_last_successful_backup_timestamp_seconds %d\n", lastBackupTimestamp)

	return nil
}

func (h *HealthHandler) onlineAgents() int {
	if h == nil || h.Agents == nil {
		return 0
	}
	return h.Agents.OnlineAgentCount()
}

func (h *HealthHandler) lastSuccessfulBackupTimestamp(c *gin.Context) (int64, error) {
	var latest db.TaskHistory
	err := h.DB.DB.WithContext(c.Request.Context()).
		Where("type = ? AND status = ? AND finished_at IS NOT NULL", "backup", "success").
		Order("finished_at DESC").
		First(&latest).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	if latest.FinishedAt == nil {
		return 0, nil
	}
	return latest.FinishedAt.Unix(), nil
}

type metricGroupRow struct {
	Status string
	Type   string
	Count  int64
}
