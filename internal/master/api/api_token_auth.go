package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"vaultfleet/internal/master/db"
)

const apiTokenPrefix = "vf"

type CreatedAPIToken struct {
	Token db.APIToken
	Plain string
}

func CreateAPIToken(database *db.Database, input CreateAPITokenInput) (CreatedAPIToken, error) {
	if database == nil || database.DB == nil {
		return CreatedAPIToken{}, errors.New("database not configured")
	}
	role := normalizeRole(input.Role)
	if role == "" {
		return CreatedAPIToken{}, errors.New("invalid role")
	}
	scopes := effectivePermissions(role, input.Scopes, true)
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return CreatedAPIToken{}, err
	}
	prefix, plain, err := generateAPITokenParts()
	if err != nil {
		return CreatedAPIToken{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return CreatedAPIToken{}, err
	}
	token := db.APIToken{
		Name:        strings.TrimSpace(input.Name),
		TokenPrefix: prefix,
		SecretHash:  string(hash),
		OwnerUserID: input.OwnerUserID,
		Role:        role,
		Scopes:      string(scopesJSON),
		ExpiresAt:   input.ExpiresAt,
	}
	if token.Name == "" {
		return CreatedAPIToken{}, errors.New("token name is required")
	}
	if token.OwnerUserID == "" {
		return CreatedAPIToken{}, errors.New("owner user id is required")
	}
	if err := database.DB.Create(&token).Error; err != nil {
		return CreatedAPIToken{}, err
	}
	return CreatedAPIToken{Token: token, Plain: plain}, nil
}

type CreateAPITokenInput struct {
	Name        string
	OwnerUserID string
	Role        string
	Scopes      []string
	ExpiresAt   *time.Time
}

func AuthenticateAPIToken(database *db.Database, plain string) (*Actor, error) {
	prefix, ok := parseAPITokenPrefix(plain)
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	var token db.APIToken
	if err := database.DB.First(&token, "token_prefix = ?", prefix).Error; err != nil {
		return nil, err
	}
	now := nowFunc()
	if token.RevokedAt != nil || (token.ExpiresAt != nil && !token.ExpiresAt.After(now)) {
		return nil, gorm.ErrRecordNotFound
	}
	if err := bcrypt.CompareHashAndPassword([]byte(token.SecretHash), []byte(plain)); err != nil {
		return nil, gorm.ErrRecordNotFound
	}
	var user db.User
	if err := database.DB.First(&user, "id = ?", token.OwnerUserID).Error; err != nil {
		return nil, err
	}
	if user.DisabledAt != nil {
		return nil, gorm.ErrRecordNotFound
	}
	scopes, _ := decodeScopes(token.Scopes)
	permissions := effectivePermissions(token.Role, scopes, true)
	if err := database.DB.Model(&db.APIToken{}).Where("id = ?", token.ID).Update("last_used_at", now).Error; err != nil {
		return nil, err
	}
	return &Actor{
		Type:        ActorTypeAPIToken,
		UserID:      user.ID,
		Username:    user.Username,
		Role:        normalizeRole(token.Role),
		TokenID:     token.ID,
		TokenName:   token.Name,
		Scopes:      scopes,
		Permissions: permissions,
	}, nil
}

func decodeScopes(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var scopes []string
	if err := json.Unmarshal([]byte(raw), &scopes); err != nil {
		return nil, err
	}
	return scopes, nil
}

func generateAPITokenParts() (string, string, error) {
	prefixBytes := make([]byte, 8)
	secretBytes := make([]byte, 24)
	if _, err := rand.Read(prefixBytes); err != nil {
		return "", "", err
	}
	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", err
	}
	prefix := hex.EncodeToString(prefixBytes)
	secret := hex.EncodeToString(secretBytes)
	return prefix, fmt.Sprintf("%s_%s_%s", apiTokenPrefix, prefix, secret), nil
}

func parseAPITokenPrefix(plain string) (string, bool) {
	parts := strings.Split(strings.TrimSpace(plain), "_")
	if len(parts) != 3 || parts[0] != apiTokenPrefix || parts[1] == "" || parts[2] == "" {
		return "", false
	}
	return parts[1], true
}
