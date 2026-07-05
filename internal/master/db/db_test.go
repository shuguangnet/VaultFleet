package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *Database {
	t.Helper()

	database, err := New(t.TempDir())
	require.NoError(t, err)
	return database
}

func TestDatabaseInit(t *testing.T) {
	database := setupTestDB(t)

	assert.NotNil(t, database.DB)
	assert.NotEmpty(t, database.DataDir)
	assert.Len(t, database.MasterKey, 32)
}

func TestDatabaseInit_CreatesDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")

	database, err := New(dir)

	require.NoError(t, err)
	assert.NotNil(t, database.DB)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestDatabaseInit_DeduplicatesLegacySnapshotsBeforeUniqueIndex(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	sqlDB, err := sql.Open("sqlite", filepath.Join(dir, "vaultfleet.db"))
	require.NoError(t, err)
	_, err = sqlDB.Exec(`
		CREATE TABLE snapshots (
			id text primary key,
			agent_id text not null,
			snapshot_id text not null,
			timestamp datetime,
			paths text,
			size integer,
			created_at datetime
		);
		INSERT INTO snapshots (id, agent_id, snapshot_id, timestamp, paths, size, created_at)
		VALUES
			('old', 'agent-1', 'snap-1', '2026-05-18T10:00:00Z', '["/old"]', 100, '2026-05-18T10:01:00Z'),
			('new', 'agent-1', 'snap-1', '2026-05-18T11:00:00Z', '["/new"]', 200, '2026-05-18T11:01:00Z'),
			('other', 'agent-2', 'snap-1', '2026-05-18T09:00:00Z', '["/other"]', 50, '2026-05-18T09:01:00Z');
	`)
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	database, err := New(dir)
	require.NoError(t, err)

	var snapshots []Snapshot
	require.NoError(t, database.DB.Order("agent_id ASC").Find(&snapshots).Error)
	require.Len(t, snapshots, 2)
	assert.Equal(t, "agent-1", snapshots[0].AgentID)
	assert.Equal(t, "snap-1", snapshots[0].SnapshotID)
	assert.Equal(t, `["/new"]`, snapshots[0].Paths)
	assert.Equal(t, int64(200), snapshots[0].Size)
	assert.Equal(t, "agent-2", snapshots[1].AgentID)
}

func TestDatabaseInit_MigratesLegacyTaskArtifactPathsAndIndexes(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	sqlDB, err := sql.Open("sqlite", filepath.Join(dir, "vaultfleet.db"))
	require.NoError(t, err)
	_, err = sqlDB.Exec(`
		CREATE TABLE task_histories (
			id text primary key,
			agent_id text not null,
			type text not null,
			status text not null,
			artifact_path text,
			artifact_name text,
			created_at datetime,
			updated_at datetime
		);
		INSERT INTO task_histories (id, agent_id, type, status, artifact_path, artifact_name, created_at, updated_at)
		VALUES ('task-1', 'agent-1', 'backup', 'success', '/etc/vaultfleet/artifacts/agent-1/archive.zip', 'archive.zip', '2026-06-23T16:19:56Z', '2026-06-23T16:19:57Z');
	`)
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	database, err := New(dir)
	require.NoError(t, err)

	var history TaskHistory
	require.NoError(t, database.DB.First(&history, "id = ?", "task-1").Error)
	assert.Equal(t, "artifacts/agent-1/archive.zip", history.ArtifactPath)

	var count int
	require.NoError(t, database.DB.Raw(`SELECT count(*) FROM pragma_index_list('task_histories') WHERE name = ?`, "idx_task_histories_type_status_created_at").Scan(&count).Error)
	assert.Equal(t, 1, count)
	require.NoError(t, database.DB.Raw(`SELECT count(*) FROM pragma_index_list('task_histories') WHERE name = ?`, "idx_task_histories_artifact_name").Scan(&count).Error)
	assert.Equal(t, 1, count)
}

func TestDatabaseInit_AddsDockerBackupColumnsToLegacySchema(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	sqlDB, err := sql.Open("sqlite", filepath.Join(dir, "vaultfleet.db"))
	require.NoError(t, err)
	_, err = sqlDB.Exec(`
		CREATE TABLE backup_policies (
			id text primary key,
			agent_id text not null,
			storage_id text not null,
			backup_mode text,
			archive_format text,
			repo_path text,
			restic_password text,
			backup_dirs text,
			exclude_patterns text,
			schedule text,
			retention text,
			rclone_args text,
			timeout_hours integer,
			synced numeric,
			created_at datetime,
			updated_at datetime
		);
		CREATE TABLE task_histories (
			id text primary key,
			agent_id text not null,
			type text not null,
			status text not null,
			snapshot_id text,
			artifact_path text,
			artifact_name text,
			artifact_size integer,
			artifact_content_type text,
			backup_mode text,
			archive_format text,
			message_id text,
			command_id text,
			policy_id text,
			storage_id text,
			started_at datetime,
			finished_at datetime,
			duration_ms integer,
			repo_size integer,
			error_log text,
			created_at datetime,
			updated_at datetime
		);
	`)
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	database, err := New(dir)
	require.NoError(t, err)

	assert.True(t, database.DB.Migrator().HasColumn(&BackupPolicy{}, "BackupSources"))
	assert.True(t, database.DB.Migrator().HasColumn(&TaskHistory{}, "Docker"))

	policy := BackupPolicy{
		AgentID:         "agent-001",
		StorageID:       "storage-001",
		BackupDirs:      `["/etc"]`,
		BackupSources:   `[{"type":"docker_container","docker_container":{"container_id":"container-1","include_volumes":true}}]`,
		ExcludePatterns: `[]`,
		Schedule:        "0 3 * * *",
		Retention:       `{"keep_last":3}`,
	}
	require.NoError(t, database.DB.Create(&policy).Error)

	history := TaskHistory{
		AgentID: "agent-001",
		Type:    "backup",
		Status:  "success",
		Docker:  `{"warnings":["compose file missing"]}`,
	}
	require.NoError(t, database.DB.Create(&history).Error)

	var storedPolicy BackupPolicy
	require.NoError(t, database.DB.First(&storedPolicy, "id = ?", policy.ID).Error)
	assert.JSONEq(t, policy.BackupSources, storedPolicy.BackupSources)

	var storedHistory TaskHistory
	require.NoError(t, database.DB.First(&storedHistory, "id = ?", history.ID).Error)
	assert.JSONEq(t, history.Docker, storedHistory.Docker)
}

func TestUserCRUD(t *testing.T) {
	database := setupTestDB(t)

	user := User{Username: "admin", PasswordHash: "$2a$10$fakehash"}
	require.NoError(t, database.DB.Create(&user).Error)
	assert.NotEmpty(t, user.ID)

	var found User
	require.NoError(t, database.DB.First(&found, "id = ?", user.ID).Error)
	assert.Equal(t, "admin", found.Username)

	require.NoError(t, database.DB.Model(&found).Update("username", "superadmin").Error)

	var updated User
	require.NoError(t, database.DB.First(&updated, "id = ?", user.ID).Error)
	assert.Equal(t, "superadmin", updated.Username)

	require.NoError(t, database.DB.Delete(&User{}, "id = ?", user.ID).Error)
	result := database.DB.First(&User{}, "id = ?", user.ID)
	assert.Error(t, result.Error)
}

func TestUserUniqueUsername(t *testing.T) {
	database := setupTestDB(t)

	u1 := User{Username: "admin", PasswordHash: "hash1"}
	require.NoError(t, database.DB.Create(&u1).Error)

	u2 := User{Username: "admin", PasswordHash: "hash2"}
	err := database.DB.Create(&u2).Error
	assert.Error(t, err)
}

func TestAgentCRUD(t *testing.T) {
	database := setupTestDB(t)
	now := time.Now()

	agent := Agent{
		Name:        "Tokyo-1",
		EnrollToken: "ek_test123",
		Status:      "offline",
		LastSeenAt:  &now,
		SystemInfo:  `{"os":"linux","arch":"amd64"}`,
	}
	require.NoError(t, database.DB.Create(&agent).Error)
	assert.NotEmpty(t, agent.ID)

	var found Agent
	require.NoError(t, database.DB.First(&found, "id = ?", agent.ID).Error)
	assert.Equal(t, "Tokyo-1", found.Name)
	assert.Equal(t, "ek_test123", found.EnrollToken)

	require.NoError(t, database.DB.Model(&found).Update("status", "online").Error)

	var updated Agent
	require.NoError(t, database.DB.First(&updated, "id = ?", agent.ID).Error)
	assert.Equal(t, "online", updated.Status)

	require.NoError(t, database.DB.Delete(&Agent{}, "id = ?", agent.ID).Error)
	result := database.DB.First(&Agent{}, "id = ?", agent.ID)
	assert.Error(t, result.Error)
}

func TestStorageConfigCRUD(t *testing.T) {
	database := setupTestDB(t)

	sc := StorageConfig{
		Name:         "Cloudflare R2",
		RcloneType:   "s3",
		RcloneConfig: `{"provider":"Cloudflare","endpoint":"https://xxx.r2.cloudflarestorage.com"}`,
	}
	require.NoError(t, database.DB.Create(&sc).Error)
	assert.NotEmpty(t, sc.ID)

	var found StorageConfig
	require.NoError(t, database.DB.First(&found, "id = ?", sc.ID).Error)
	assert.Equal(t, "s3", found.RcloneType)
	assert.Contains(t, found.RcloneConfig, "Cloudflare")

	require.NoError(t, database.DB.Model(&found).Update("name", "Updated R2").Error)

	var updated StorageConfig
	require.NoError(t, database.DB.First(&updated, "id = ?", sc.ID).Error)
	assert.Equal(t, "Updated R2", updated.Name)

	require.NoError(t, database.DB.Delete(&StorageConfig{}, "id = ?", sc.ID).Error)
	result := database.DB.First(&StorageConfig{}, "id = ?", sc.ID)
	assert.Error(t, result.Error)
}

func TestBackupPolicyCRUD(t *testing.T) {
	database := setupTestDB(t)

	policy := BackupPolicy{
		AgentID:         "agent-001",
		StorageID:       "storage-001",
		RepoPath:        "vaultfleet/agent-001",
		ResticPassword:  "encrypted-password",
		BackupDirs:      `["/etc","/home"]`,
		ExcludePatterns: `["*.log"]`,
		Schedule:        "0 3 * * *",
		Retention:       `{"keep_last":3,"keep_daily":7}`,
		RcloneArgs:      `{"--bwlimit":"10M","--transfers":"4"}`,
		Synced:          false,
	}
	require.NoError(t, database.DB.Create(&policy).Error)
	assert.NotEmpty(t, policy.ID)

	var found BackupPolicy
	require.NoError(t, database.DB.First(&found, "id = ?", policy.ID).Error)
	assert.Equal(t, "0 3 * * *", found.Schedule)
	assert.Equal(t, `{"--bwlimit":"10M","--transfers":"4"}`, found.RcloneArgs)
	assert.False(t, found.Synced)

	require.NoError(t, database.DB.Model(&found).Update("synced", true).Error)

	var updated BackupPolicy
	require.NoError(t, database.DB.First(&updated, "id = ?", policy.ID).Error)
	assert.True(t, updated.Synced)

	require.NoError(t, database.DB.Delete(&BackupPolicy{}, "id = ?", policy.ID).Error)
	result := database.DB.First(&BackupPolicy{}, "id = ?", policy.ID)
	assert.Error(t, result.Error)
}

func TestTaskHistoryCRUD(t *testing.T) {
	database := setupTestDB(t)
	now := time.Now()
	finished := now.Add(45 * time.Second)

	th := TaskHistory{
		AgentID:    "agent-001",
		Type:       "backup",
		Status:     "success",
		SnapshotID: "snap123",
		StartedAt:  &now,
		FinishedAt: &finished,
		DurationMs: 45000,
		RepoSize:   1073741824,
	}
	require.NoError(t, database.DB.Create(&th).Error)
	assert.NotEmpty(t, th.ID)

	var found TaskHistory
	require.NoError(t, database.DB.First(&found, "id = ?", th.ID).Error)
	assert.Equal(t, "success", found.Status)
	assert.Equal(t, int64(45000), found.DurationMs)

	require.NoError(t, database.DB.Model(&found).Update("status", "failed").Error)

	var updated TaskHistory
	require.NoError(t, database.DB.First(&updated, "id = ?", th.ID).Error)
	assert.Equal(t, "failed", updated.Status)

	require.NoError(t, database.DB.Delete(&TaskHistory{}, "id = ?", th.ID).Error)
	result := database.DB.First(&TaskHistory{}, "id = ?", th.ID)
	assert.Error(t, result.Error)
}

func TestAgentCommandCRUD(t *testing.T) {
	database := setupTestDB(t)
	now := time.Now().UTC()
	completed := now.Add(time.Minute)

	command := AgentCommand{
		AgentID:      "agent-001",
		Type:         "backup_now",
		Status:       "pending",
		MessageID:    "msg-001",
		Payload:      "encrypted-payload",
		Result:       "",
		ErrorMessage: "",
		Attempts:     0,
		DeadlineAt:   &completed,
	}
	require.NoError(t, database.DB.Create(&command).Error)
	assert.NotEmpty(t, command.ID)

	var found AgentCommand
	require.NoError(t, database.DB.First(&found, "id = ?", command.ID).Error)
	assert.Equal(t, "agent-001", found.AgentID)
	assert.Equal(t, "backup_now", found.Type)
	assert.Equal(t, "pending", found.Status)
	assert.Equal(t, "msg-001", found.MessageID)

	require.NoError(t, database.DB.Model(&found).Updates(map[string]any{
		"status":       "succeeded",
		"completed_at": &completed,
		"result":       `{"status":"success"}`,
	}).Error)

	var updated AgentCommand
	require.NoError(t, database.DB.First(&updated, "id = ?", command.ID).Error)
	assert.Equal(t, "succeeded", updated.Status)
	assert.JSONEq(t, `{"status":"success"}`, updated.Result)
	assert.NotNil(t, updated.CompletedAt)
}

func TestTaskHistoryRunFieldsCRUD(t *testing.T) {
	database := setupTestDB(t)
	now := time.Now().UTC()
	history := TaskHistory{
		AgentID:   "agent-001",
		Type:      "backup",
		Status:    "pending",
		CommandID: "command-001",
		PolicyID:  "policy-001",
		StorageID: "storage-001",
		StartedAt: &now,
	}
	require.NoError(t, database.DB.Create(&history).Error)

	var found TaskHistory
	require.NoError(t, database.DB.First(&found, "id = ?", history.ID).Error)
	assert.Equal(t, "command-001", found.CommandID)
	assert.Equal(t, "policy-001", found.PolicyID)
	assert.Equal(t, "storage-001", found.StorageID)
	assert.False(t, found.UpdatedAt.IsZero())
}

func TestSnapshotCRUD(t *testing.T) {
	database := setupTestDB(t)

	snap := Snapshot{
		AgentID:    "agent-001",
		SnapshotID: "restic-snap-123",
		Timestamp:  time.Now(),
		Paths:      `["/etc","/home"]`,
		Size:       500000,
	}
	require.NoError(t, database.DB.Create(&snap).Error)
	assert.NotEmpty(t, snap.ID)

	var found Snapshot
	require.NoError(t, database.DB.First(&found, "id = ?", snap.ID).Error)
	assert.Equal(t, "restic-snap-123", found.SnapshotID)
	assert.Equal(t, int64(500000), found.Size)

	require.NoError(t, database.DB.Model(&found).Update("size", 600000).Error)

	var updated Snapshot
	require.NoError(t, database.DB.First(&updated, "id = ?", snap.ID).Error)
	assert.Equal(t, int64(600000), updated.Size)

	require.NoError(t, database.DB.Delete(&Snapshot{}, "id = ?", snap.ID).Error)
	result := database.DB.First(&Snapshot{}, "id = ?", snap.ID)
	assert.Error(t, result.Error)
}

func TestSnapshotUniqueAgentSnapshotID(t *testing.T) {
	database := setupTestDB(t)

	first := Snapshot{
		AgentID:    "agent-001",
		SnapshotID: "restic-snap-123",
		Timestamp:  time.Now(),
		Paths:      `["/etc"]`,
		Size:       100,
	}
	require.NoError(t, database.DB.Create(&first).Error)

	duplicate := Snapshot{
		AgentID:    "agent-001",
		SnapshotID: "restic-snap-123",
		Timestamp:  time.Now(),
		Paths:      `["/home"]`,
		Size:       200,
	}
	err := database.DB.Create(&duplicate).Error
	require.Error(t, err)

	otherAgent := Snapshot{
		AgentID:    "agent-002",
		SnapshotID: "restic-snap-123",
		Timestamp:  time.Now(),
		Paths:      `["/srv"]`,
		Size:       300,
	}
	require.NoError(t, database.DB.Create(&otherAgent).Error)
}

func TestNotificationConfigCRUD(t *testing.T) {
	database := setupTestDB(t)

	nc := NotificationConfig{
		Type:   "telegram",
		Config: `{"bot_token":"123:ABC","chat_id":"-100123"}`,
		Events: `["backup_failed","agent_offline"]`,
	}
	require.NoError(t, database.DB.Create(&nc).Error)
	assert.NotEmpty(t, nc.ID)

	var found NotificationConfig
	require.NoError(t, database.DB.First(&found, "id = ?", nc.ID).Error)
	assert.Equal(t, "telegram", found.Type)

	require.NoError(t, database.DB.Model(&found).Update("type", "webhook").Error)

	var updated NotificationConfig
	require.NoError(t, database.DB.First(&updated, "id = ?", nc.ID).Error)
	assert.Equal(t, "webhook", updated.Type)

	require.NoError(t, database.DB.Delete(&NotificationConfig{}, "id = ?", nc.ID).Error)
	result := database.DB.First(&NotificationConfig{}, "id = ?", nc.ID)
	assert.Error(t, result.Error)
}

func TestAgentByEnrollToken(t *testing.T) {
	database := setupTestDB(t)

	agent := Agent{Name: "Tokyo-1", EnrollToken: "ek_unique_token", Status: "offline"}
	require.NoError(t, database.DB.Create(&agent).Error)

	var found Agent
	err := database.DB.Where("enroll_token = ?", "ek_unique_token").First(&found).Error
	require.NoError(t, err)
	assert.Equal(t, agent.ID, found.ID)

	err = database.DB.Where("enroll_token = ?", "ek_nonexistent").First(&Agent{}).Error
	assert.Error(t, err)
}

func TestAgentEnrollTokenUniqueWhenNonEmpty(t *testing.T) {
	database := setupTestDB(t)

	first := Agent{Name: "Tokyo-1", EnrollToken: "ek_duplicate", Status: "offline"}
	require.NoError(t, database.DB.Create(&first).Error)

	duplicate := Agent{Name: "Tokyo-2", EnrollToken: "ek_duplicate", Status: "offline"}
	err := database.DB.Create(&duplicate).Error

	require.Error(t, err)
}

func TestAgentAgentTokenUniqueWhenNonEmpty(t *testing.T) {
	database := setupTestDB(t)

	first := Agent{Name: "Tokyo-1", AgentToken: "ak_duplicate", Status: "online"}
	require.NoError(t, database.DB.Create(&first).Error)

	duplicate := Agent{Name: "Tokyo-2", AgentToken: "ak_duplicate", Status: "online"}
	err := database.DB.Create(&duplicate).Error

	require.Error(t, err)
}

func TestAgentEmptyConsumedTokensAreNotUnique(t *testing.T) {
	database := setupTestDB(t)

	first := Agent{Name: "Tokyo-1", EnrollToken: "", AgentToken: "", Status: "offline"}
	require.NoError(t, database.DB.Create(&first).Error)

	second := Agent{Name: "Tokyo-2", EnrollToken: "", AgentToken: "", Status: "offline"}
	require.NoError(t, database.DB.Create(&second).Error)
}
