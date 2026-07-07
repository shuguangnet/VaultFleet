package db

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserDefaultsToAdminRole(t *testing.T) {
	database, err := New(t.TempDir())
	require.NoError(t, err)

	user := User{Username: "admin", PasswordHash: "hash"}
	require.NoError(t, database.DB.Create(&user).Error)

	var stored User
	require.NoError(t, database.DB.First(&stored, "id = ?", user.ID).Error)
	assert.Equal(t, "admin", stored.Role)
	assert.Nil(t, stored.DisabledAt)
}

func TestBackfillsLegacyUsersToAdmin(t *testing.T) {
	dataDir := t.TempDir()
	legacyDB, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "vaultfleet.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, legacyDB.Exec(`
		CREATE TABLE users (
			id text PRIMARY KEY,
			username text NOT NULL UNIQUE,
			password_hash text NOT NULL,
			created_at datetime,
			updated_at datetime
		)
	`).Error)
	require.NoError(t, legacyDB.Exec(`INSERT INTO users (id, username, password_hash) VALUES ('u1', 'legacy', 'hash')`).Error)

	database, err := New(dataDir)
	require.NoError(t, err)

	var user User
	require.NoError(t, database.DB.First(&user, "id = ?", "u1").Error)
	assert.Equal(t, "admin", user.Role)
}

func TestAPITokenAndAuditEventPersistence(t *testing.T) {
	database, err := New(t.TempDir())
	require.NoError(t, err)

	user := User{Username: "admin", PasswordHash: "hash"}
	require.NoError(t, database.DB.Create(&user).Error)
	token := APIToken{
		Name:        "deploy",
		TokenPrefix: "prefix",
		SecretHash:  "hash",
		OwnerUserID: user.ID,
		Role:        "operator",
		Scopes:      `["read:operational"]`,
	}
	require.NoError(t, database.DB.Create(&token).Error)
	event := AuditEvent{
		ActorType:  "user",
		ActorID:    user.ID,
		ActorName:  "admin",
		ActorRole:  "admin",
		Action:     "storage.create",
		TargetType: "storage",
		Result:     "success",
	}
	require.NoError(t, database.DB.Create(&event).Error)

	var storedToken APIToken
	require.NoError(t, database.DB.First(&storedToken, "token_prefix = ?", "prefix").Error)
	assert.Equal(t, "operator", storedToken.Role)

	var storedEvent AuditEvent
	require.NoError(t, database.DB.First(&storedEvent, "action = ?", "storage.create").Error)
	assert.Equal(t, user.ID, storedEvent.ActorID)
}
