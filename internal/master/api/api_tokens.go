package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
)

type APITokenHandler struct {
	DB *db.Database
}

func NewAPITokenHandler(database *db.Database) *APITokenHandler {
	return &APITokenHandler{DB: database}
}

func RegisterAPITokenRoutes(rg *gin.RouterGroup, h *APITokenHandler) {
	rg.GET("/api-tokens", h.List)
	rg.POST("/api-tokens", h.Create)
	rg.POST("/api-tokens/:id/revoke", h.Revoke)
	rg.DELETE("/api-tokens/:id", h.Delete)
}

type createAPITokenRequest struct {
	Name      string     `json:"name" binding:"required"`
	Role      string     `json:"role" binding:"required"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type apiTokenResponse struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	OwnerUserID string     `json:"owner_user_id"`
	Role        string     `json:"role"`
	Scopes      []string   `json:"scopes"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Token       string     `json:"token,omitempty"`
}

func (h *APITokenHandler) List(c *gin.Context) {
	var tokens []db.APIToken
	if err := h.DB.DB.Order("created_at DESC").Find(&tokens).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]apiTokenResponse, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, newAPITokenResponse(token, ""))
	}
	writeDataResponse(c, http.StatusOK, out)
}

func (h *APITokenHandler) Create(c *gin.Context) {
	var req createAPITokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}
	actor := currentActor(c)
	if actor == nil || actor.UserID == "" {
		writeErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	created, err := CreateAPIToken(h.DB, CreateAPITokenInput{
		Name:        req.Name,
		OwnerUserID: actor.UserID,
		Role:        req.Role,
		Scopes:      req.Scopes,
		ExpiresAt:   req.ExpiresAt,
	})
	if err != nil {
		writeErrorResponse(c, http.StatusBadRequest, err.Error())
		return
	}
	writeDataResponse(c, http.StatusCreated, newAPITokenResponse(created.Token, created.Plain))
}

func (h *APITokenHandler) Revoke(c *gin.Context) {
	var token db.APIToken
	if err := h.DB.DB.First(&token, "id = ?", c.Param("id")).Error; err != nil {
		writeAPITokenLookupError(c, err)
		return
	}
	now := nowFunc()
	token.RevokedAt = &now
	if err := h.DB.DB.Save(&token).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	writeDataResponse(c, http.StatusOK, newAPITokenResponse(token, ""))
}

func (h *APITokenHandler) Delete(c *gin.Context) {
	result := h.DB.DB.Delete(&db.APIToken{}, "id = ?", c.Param("id"))
	if result.Error != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	if result.RowsAffected == 0 {
		writeErrorResponse(c, http.StatusNotFound, "api token not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func newAPITokenResponse(token db.APIToken, plain string) apiTokenResponse {
	scopes, _ := decodeScopes(token.Scopes)
	return apiTokenResponse{
		ID:          token.ID,
		Name:        token.Name,
		TokenPrefix: token.TokenPrefix,
		OwnerUserID: token.OwnerUserID,
		Role:        normalizeRole(token.Role),
		Scopes:      scopes,
		ExpiresAt:   token.ExpiresAt,
		RevokedAt:   token.RevokedAt,
		LastUsedAt:  token.LastUsedAt,
		CreatedAt:   token.CreatedAt,
		UpdatedAt:   token.UpdatedAt,
		Token:       plain,
	}
}

func writeAPITokenLookupError(c *gin.Context, err error) {
	if err == gorm.ErrRecordNotFound {
		writeErrorResponse(c, http.StatusNotFound, "api token not found")
		return
	}
	writeErrorResponse(c, http.StatusInternalServerError, "database error")
}

func scopesJSON(scopes []string) string {
	raw, _ := json.Marshal(scopes)
	return string(raw)
}
