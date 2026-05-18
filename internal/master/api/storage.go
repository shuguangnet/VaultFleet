package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
)

type ConfigHandler struct {
	DB        *db.Database
	MasterKey []byte
}

func NewConfigHandler(database *db.Database) *ConfigHandler {
	return &ConfigHandler{
		DB:        database,
		MasterKey: database.MasterKey,
	}
}

type createStorageRequest struct {
	Name         string         `json:"name" binding:"required"`
	RcloneType   string         `json:"rclone_type" binding:"required"`
	RcloneConfig map[string]any `json:"rclone_config" binding:"required"`
}

type updateStorageRequest struct {
	Name         string         `json:"name"`
	RcloneType   string         `json:"rclone_type"`
	RcloneConfig map[string]any `json:"rclone_config"`
}

type storageResponse struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	RcloneType   string         `json:"rclone_type"`
	RcloneConfig map[string]any `json:"rclone_config"`
	CreatedAt    time.Time      `json:"created_at"`
}

func RegisterStorageRoutes(rg *gin.RouterGroup, h *ConfigHandler) {
	rg.POST("/storage", h.CreateStorage)
	rg.GET("/storage", h.ListStorage)
	rg.GET("/storage/:id", h.GetStorage)
	rg.PUT("/storage/:id", h.UpdateStorage)
	rg.DELETE("/storage/:id", h.DeleteStorage)
}

func (h *ConfigHandler) CreateStorage(c *gin.Context) {
	var request createStorageRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	encryptedConfig, ok := h.encryptMap(c, request.RcloneConfig)
	if !ok {
		return
	}

	storage := db.StorageConfig{
		Name:         request.Name,
		RcloneType:   request.RcloneType,
		RcloneConfig: encryptedConfig,
	}
	if err := h.DB.DB.Create(&storage).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	h.writeStorageResponse(c, http.StatusCreated, storage)
}

func (h *ConfigHandler) ListStorage(c *gin.Context) {
	var configs []db.StorageConfig
	if err := h.DB.DB.Order("created_at DESC").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	responses := make([]storageResponse, 0, len(configs))
	for _, storage := range configs {
		response, err := h.newStorageResponse(storage)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "decrypt storage config"})
			return
		}
		responses = append(responses, response)
	}

	c.JSON(http.StatusOK, responses)
}

func (h *ConfigHandler) GetStorage(c *gin.Context) {
	storage, ok := h.findStorageByID(c, c.Param("id"))
	if !ok {
		return
	}

	h.writeStorageResponse(c, http.StatusOK, storage)
}

func (h *ConfigHandler) UpdateStorage(c *gin.Context) {
	storage, ok := h.findStorageByID(c, c.Param("id"))
	if !ok {
		return
	}

	var request updateStorageRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if request.Name != "" {
		storage.Name = request.Name
	}
	if request.RcloneType != "" {
		storage.RcloneType = request.RcloneType
	}
	if request.RcloneConfig != nil {
		encryptedConfig, ok := h.encryptMap(c, request.RcloneConfig)
		if !ok {
			return
		}
		storage.RcloneConfig = encryptedConfig
	}

	if err := h.DB.DB.Save(&storage).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	h.writeStorageResponse(c, http.StatusOK, storage)
}

func (h *ConfigHandler) DeleteStorage(c *gin.Context) {
	result := h.DB.DB.Delete(&db.StorageConfig{}, "id = ?", c.Param("id"))
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "storage config not found"})
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *ConfigHandler) findStorageByID(c *gin.Context, id string) (db.StorageConfig, bool) {
	var storage db.StorageConfig
	if err := h.DB.DB.First(&storage, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "storage config not found"})
			return db.StorageConfig{}, false
		}

		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return db.StorageConfig{}, false
	}

	return storage, true
}

func (h *ConfigHandler) writeStorageResponse(c *gin.Context, status int, storage db.StorageConfig) {
	response, err := h.newStorageResponse(storage)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decrypt storage config"})
		return
	}

	c.JSON(status, response)
}

func (h *ConfigHandler) newStorageResponse(storage db.StorageConfig) (storageResponse, error) {
	config, err := h.decryptMap(storage.RcloneConfig)
	if err != nil {
		return storageResponse{}, err
	}

	return storageResponse{
		ID:           storage.ID,
		Name:         storage.Name,
		RcloneType:   storage.RcloneType,
		RcloneConfig: config,
		CreatedAt:    storage.CreatedAt,
	}, nil
}

func (h *ConfigHandler) encryptMap(c *gin.Context, value map[string]any) (string, bool) {
	plaintext, err := json.Marshal(value)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return "", false
	}

	ciphertext, err := db.Encrypt(string(plaintext), h.MasterKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "encryption failed"})
		return "", false
	}

	return ciphertext, true
}

func (h *ConfigHandler) decryptMap(ciphertext string) (map[string]any, error) {
	plaintext, err := db.Decrypt(ciphertext, h.MasterKey)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(plaintext), &result); err != nil {
		return nil, err
	}

	return result, nil
}
