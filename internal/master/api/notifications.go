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

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/notify"
)

type NotificationHandler struct {
	DB              *db.Database
	notifierFactory notify.NotifierFactory
}

func NewNotificationHandler(database *db.Database) *NotificationHandler {
	return &NotificationHandler{
		DB:              database,
		notifierFactory: notify.NewNotifierFromConfig,
	}
}

type notificationRequest struct {
	Type   string         `json:"type"`
	Config map[string]any `json:"config"`
	Events []string       `json:"events"`
}

type updateNotificationRequest struct {
	Type   *string        `json:"type"`
	Config map[string]any `json:"config"`
	Events []string       `json:"events"`
}

type notificationResponse struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Config    map[string]any `json:"config"`
	Events    []string       `json:"events"`
	CreatedAt time.Time      `json:"created_at"`
}

func RegisterNotificationRoutes(rg *gin.RouterGroup, h *NotificationHandler) {
	rg.POST("/notifications", h.Create)
	rg.GET("/notifications", h.List)
	rg.GET("/notifications/:id", h.Get)
	rg.PUT("/notifications/:id", h.Update)
	rg.DELETE("/notifications/:id", h.Delete)
	rg.POST("/notifications/:id/test", h.TestSend)
}

func (h *NotificationHandler) Create(c *gin.Context) {
	var request notificationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	configJSON, eventsJSON, ok := h.prepareNotificationInput(c, request.Type, request.Config, request.Events)
	if !ok {
		return
	}

	config := db.NotificationConfig{
		Type:   request.Type,
		Config: configJSON,
		Events: eventsJSON,
	}
	if err := h.DB.DB.Create(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	h.writeNotificationResponse(c, http.StatusCreated, config)
}

func (h *NotificationHandler) List(c *gin.Context) {
	var configs []db.NotificationConfig
	if err := h.DB.DB.Order("created_at DESC").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	responses := make([]notificationResponse, 0, len(configs))
	for _, config := range configs {
		response, err := newNotificationResponse(config)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "decode notification config"})
			return
		}
		responses = append(responses, response)
	}

	c.JSON(http.StatusOK, responses)
}

func (h *NotificationHandler) Get(c *gin.Context) {
	config, ok := h.findNotificationByID(c, c.Param("id"))
	if !ok {
		return
	}

	h.writeNotificationResponse(c, http.StatusOK, config)
}

func (h *NotificationHandler) Update(c *gin.Context) {
	config, ok := h.findNotificationByID(c, c.Param("id"))
	if !ok {
		return
	}

	var request updateNotificationRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	nextType := config.Type
	if request.Type != nil {
		nextType = *request.Type
	}

	nextConfig := config.Config
	if request.Config != nil {
		configJSON, ok := marshalNotificationConfig(c, request.Config)
		if !ok {
			return
		}
		nextConfig = configJSON
	}

	nextEvents := config.Events
	if request.Events != nil {
		eventsJSON, ok := marshalNotificationEvents(c, request.Events)
		if !ok {
			return
		}
		nextEvents = eventsJSON
	}

	if ok := validateNotificationConfig(c, nextType, json.RawMessage(nextConfig)); !ok {
		return
	}
	if ok := validateNotificationEvents(c, nextEvents); !ok {
		return
	}

	config.Type = nextType
	config.Config = nextConfig
	config.Events = nextEvents
	if err := h.DB.DB.Save(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	h.writeNotificationResponse(c, http.StatusOK, config)
}

func (h *NotificationHandler) Delete(c *gin.Context) {
	result := h.DB.DB.Delete(&db.NotificationConfig{}, "id = ?", c.Param("id"))
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "notification config not found"})
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *NotificationHandler) TestSend(c *gin.Context) {
	config, ok := h.findNotificationByID(c, c.Param("id"))
	if !ok {
		return
	}

	factory := h.notifierFactory
	if factory == nil {
		factory = notify.NewNotifierFromConfig
	}
	notifier, err := factory(config.Type, json.RawMessage(config.Config))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid notification config"})
		return
	}

	msg := notify.NotifyMessage{
		Title:     "Test Notification",
		Body:      "VaultFleet notification test message.",
		Level:     notify.LevelInfo,
		AgentName: "VaultFleet",
		Timestamp: time.Now().UTC(),
	}
	if err := notifier.Send(c.Request.Context(), msg); err != nil {
		if errors.Is(err, context.Canceled) {
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "request cancelled"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "send notification failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *NotificationHandler) findNotificationByID(c *gin.Context, id string) (db.NotificationConfig, bool) {
	var config db.NotificationConfig
	if err := h.DB.DB.First(&config, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "notification config not found"})
			return db.NotificationConfig{}, false
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return db.NotificationConfig{}, false
	}

	return config, true
}

func (h *NotificationHandler) prepareNotificationInput(c *gin.Context, notificationType string, config map[string]any, events []string) (string, string, bool) {
	configJSON, ok := marshalNotificationConfig(c, config)
	if !ok {
		return "", "", false
	}
	eventsJSON, ok := marshalNotificationEvents(c, events)
	if !ok {
		return "", "", false
	}
	if ok := validateNotificationConfig(c, notificationType, json.RawMessage(configJSON)); !ok {
		return "", "", false
	}
	if ok := validateNotificationEvents(c, eventsJSON); !ok {
		return "", "", false
	}
	return configJSON, eventsJSON, true
}

func (h *NotificationHandler) writeNotificationResponse(c *gin.Context, status int, config db.NotificationConfig) {
	response, err := newNotificationResponse(config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode notification config"})
		return
	}

	c.JSON(status, response)
}

func newNotificationResponse(config db.NotificationConfig) (notificationResponse, error) {
	var rawConfig map[string]any
	if config.Config != "" {
		if err := json.Unmarshal([]byte(config.Config), &rawConfig); err != nil {
			return notificationResponse{}, err
		}
	}

	var eventNames []string
	if config.Events != "" {
		if err := json.Unmarshal([]byte(config.Events), &eventNames); err != nil {
			return notificationResponse{}, err
		}
	}
	if eventNames == nil {
		eventNames = []string{}
	}

	return notificationResponse{
		ID:        config.ID,
		Type:      config.Type,
		Config:    rawConfig,
		Events:    eventNames,
		CreatedAt: config.CreatedAt,
	}, nil
}

func marshalNotificationConfig(c *gin.Context, value map[string]any) (string, bool) {
	if value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return "", false
	}

	data, err := json.Marshal(value)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return "", false
	}
	return string(data), true
}

func marshalNotificationEvents(c *gin.Context, events []string) (string, bool) {
	if len(events) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return "", false
	}

	data, err := json.Marshal(events)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return "", false
	}
	return string(data), true
}

func validateNotificationConfig(c *gin.Context, notificationType string, raw json.RawMessage) bool {
	if strings.TrimSpace(notificationType) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid notification type"})
		return false
	}
	if _, err := notify.NewNotifierFromConfig(notificationType, raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return false
	}
	return true
}

func validateNotificationEvents(c *gin.Context, rawEvents string) bool {
	var events []string
	if err := json.Unmarshal([]byte(rawEvents), &events); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid events"})
		return false
	}
	if len(events) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid events"})
		return false
	}
	for _, eventName := range events {
		switch eventName {
		case notify.EventBackupFailed, notify.EventAgentOffline:
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid event"})
			return false
		}
	}
	return true
}
