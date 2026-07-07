package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
)

type UserHandler struct {
	DB *db.Database
}

func NewUserHandler(database *db.Database) *UserHandler {
	return &UserHandler{DB: database}
}

func RegisterUserRoutes(rg *gin.RouterGroup, h *UserHandler) {
	rg.GET("/users", h.List)
	rg.POST("/users", h.Create)
	rg.PUT("/users/:id", h.Update)
	rg.POST("/users/:id/disable", h.Disable)
	rg.POST("/users/:id/enable", h.Enable)
	rg.POST("/users/:id/reset-password", h.ResetPassword)
	rg.DELETE("/users/:id", h.Delete)
	rg.GET("/me", h.Me)
}

type createUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=6"`
	Role     string `json:"role" binding:"required"`
}

type updateUserRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

type resetPasswordRequest struct {
	Password string `json:"password" binding:"required,min=6"`
}

type userResponse struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	Role        string     `json:"role"`
	DisabledAt  *time.Time `json:"disabled_at,omitempty"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (h *UserHandler) List(c *gin.Context) {
	var users []db.User
	if err := h.DB.DB.Order("created_at DESC").Find(&users).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	out := make([]userResponse, 0, len(users))
	for _, user := range users {
		out = append(out, newUserResponse(user))
	}
	writeDataResponse(c, http.StatusOK, out)
}

func (h *UserHandler) Create(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}
	role := normalizeRole(req.Role)
	if role == "" {
		writeErrorResponse(c, http.StatusBadRequest, "invalid role")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "password hashing failed")
		return
	}
	user := db.User{Username: strings.TrimSpace(req.Username), PasswordHash: string(hash), Role: role}
	if user.Username == "" {
		writeErrorResponse(c, http.StatusBadRequest, "username is required")
		return
	}
	if err := h.DB.DB.Create(&user).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	writeDataResponse(c, http.StatusCreated, newUserResponse(user))
}

func (h *UserHandler) Update(c *gin.Context) {
	var user db.User
	if err := h.DB.DB.First(&user, "id = ?", c.Param("id")).Error; err != nil {
		writeUserLookupError(c, err)
		return
	}
	var req updateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}
	if strings.TrimSpace(req.Username) != "" {
		user.Username = strings.TrimSpace(req.Username)
	}
	if strings.TrimSpace(req.Role) != "" {
		role := normalizeRole(req.Role)
		if role == "" {
			writeErrorResponse(c, http.StatusBadRequest, "invalid role")
			return
		}
		user.Role = role
	}
	if err := h.DB.DB.Save(&user).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	writeDataResponse(c, http.StatusOK, newUserResponse(user))
}

func (h *UserHandler) Disable(c *gin.Context) {
	h.setDisabled(c, true)
}

func (h *UserHandler) Enable(c *gin.Context) {
	h.setDisabled(c, false)
}

func (h *UserHandler) setDisabled(c *gin.Context, disabled bool) {
	var user db.User
	if err := h.DB.DB.First(&user, "id = ?", c.Param("id")).Error; err != nil {
		writeUserLookupError(c, err)
		return
	}
	if disabled {
		now := nowFunc()
		user.DisabledAt = &now
	} else {
		user.DisabledAt = nil
	}
	if err := h.DB.DB.Save(&user).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	writeDataResponse(c, http.StatusOK, newUserResponse(user))
}

func (h *UserHandler) ResetPassword(c *gin.Context) {
	var user db.User
	if err := h.DB.DB.First(&user, "id = ?", c.Param("id")).Error; err != nil {
		writeUserLookupError(c, err)
		return
	}
	var req resetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorResponse(c, http.StatusBadRequest, "invalid request")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "password hashing failed")
		return
	}
	user.PasswordHash = string(hash)
	if err := h.DB.DB.Save(&user).Error; err != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	writeDataResponse(c, http.StatusOK, newUserResponse(user))
}

func (h *UserHandler) Delete(c *gin.Context) {
	result := h.DB.DB.Delete(&db.User{}, "id = ?", c.Param("id"))
	if result.Error != nil {
		writeErrorResponse(c, http.StatusInternalServerError, "database error")
		return
	}
	if result.RowsAffected == 0 {
		writeErrorResponse(c, http.StatusNotFound, "user not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *UserHandler) Me(c *gin.Context) {
	actor := currentActor(c)
	if actor == nil || actor.UserID == "" {
		writeErrorResponse(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeDataResponse(c, http.StatusOK, actor)
}

func newUserResponse(user db.User) userResponse {
	role := normalizeRole(user.Role)
	if role == "" {
		role = RoleAdmin
	}
	return userResponse{
		ID:          user.ID,
		Username:    user.Username,
		Role:        role,
		DisabledAt:  user.DisabledAt,
		LastLoginAt: user.LastLoginAt,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}

func writeUserLookupError(c *gin.Context, err error) {
	if err == gorm.ErrRecordNotFound {
		writeErrorResponse(c, http.StatusNotFound, "user not found")
		return
	}
	writeErrorResponse(c, http.StatusInternalServerError, "database error")
}
