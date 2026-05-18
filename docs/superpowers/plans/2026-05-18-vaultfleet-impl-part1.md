### Task 1: Project Scaffolding + Go Module + Shared Protocol Types

**Files:**
- `go.mod`
- `pkg/protocol/message.go`
- `pkg/protocol/message_test.go`

**Steps:**

- [ ] 1.1 — Initialize Go module and create directory structure

```bash
cd /home/nstar/code_temp/VaultFleet
go mod init vaultfleet
mkdir -p cmd/{master,agent}
mkdir -p internal/master/{api,db,ws,notify,events,backup}
mkdir -p internal/agent/{connect,executor,filebrowse,policy,scheduler}
mkdir -p pkg/protocol
mkdir -p web build
```

- [ ] 1.2 — Write protocol tests (`pkg/protocol/message_test.go`)

```go
// pkg/protocol/message_test.go
package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageMarshalUnmarshal(t *testing.T) {
	hb := HeartbeatPayload{
		CPUPercent:    45.5,
		MemoryPercent: 72.3,
		DiskPercent:   30.0,
		ResticVersion: "0.16.0",
		RcloneVersion: "1.65.0",
		Uptime:        86400,
	}
	msg, err := NewMessage(TypeHeartbeat, hb)
	require.NoError(t, err)
	assert.Equal(t, TypeHeartbeat, msg.Type)
	assert.NotEmpty(t, msg.ID)

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, TypeHeartbeat, decoded.Type)
	assert.Equal(t, msg.ID, decoded.ID)

	parsed, err := ParsePayload[HeartbeatPayload](&decoded)
	require.NoError(t, err)
	assert.InDelta(t, 45.5, parsed.CPUPercent, 0.01)
	assert.Equal(t, "0.16.0", parsed.ResticVersion)
}

func TestPolicyPushPayload(t *testing.T) {
	policy := PolicyPushPayload{
		AgentID: "agent-001",
		Storage: StorageConfig{
			RcloneType: "s3",
			RcloneConfig: map[string]string{
				"provider":          "Cloudflare",
				"access_key_id":     "AKID",
				"secret_access_key": "SECRET",
				"endpoint":          "https://xxx.r2.cloudflarestorage.com",
				"bucket":            "backups",
			},
			RepoPath: "vaultfleet/agent-001",
		},
		ResticPassword:  "secure-password",
		BackupDirs:      []string{"/etc", "/home", "/opt/myapp/data"},
		ExcludePatterns: []string{"*.log", "*.tmp", "node_modules"},
		Schedule:        "0 3 * * *",
		Retention: RetentionPolicy{
			KeepLast:    3,
			KeepDaily:   7,
			KeepWeekly:  4,
			KeepMonthly: 6,
		},
	}

	msg, err := NewMessage(TypePolicyPush, policy)
	require.NoError(t, err)

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	parsed, err := ParsePayload[PolicyPushPayload](&decoded)
	require.NoError(t, err)
	assert.Equal(t, "agent-001", parsed.AgentID)
	assert.Equal(t, "s3", parsed.Storage.RcloneType)
	assert.Equal(t, "Cloudflare", parsed.Storage.RcloneConfig["provider"])
	assert.Len(t, parsed.BackupDirs, 3)
	assert.Equal(t, 6, parsed.Retention.KeepMonthly)
}

func TestTaskResultPayload(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	result := TaskResultPayload{
		AgentID:    "agent-002",
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "abc123def456",
		DurationMs: 45000,
		RepoSize:   1073741824,
		StartedAt:  now.Add(-45 * time.Second),
		FinishedAt: now,
	}

	msg, err := NewMessage(TypeTaskResult, result)
	require.NoError(t, err)

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	parsed, err := ParsePayload[TaskResultPayload](&decoded)
	require.NoError(t, err)
	assert.Equal(t, "backup", parsed.TaskType)
	assert.Equal(t, "success", parsed.Status)
	assert.Equal(t, int64(45000), parsed.DurationMs)
	assert.Equal(t, int64(1073741824), parsed.RepoSize)
}

func TestDirBrowseRoundTrip(t *testing.T) {
	reqPayload := DirBrowseReqPayload{Path: "/home", Depth: 2}
	msg, err := NewMessage(TypeDirBrowseReq, reqPayload)
	require.NoError(t, err)

	parsed, err := ParsePayload[DirBrowseReqPayload](msg)
	require.NoError(t, err)
	assert.Equal(t, "/home", parsed.Path)
	assert.Equal(t, 2, parsed.Depth)

	resp := DirBrowseRespPayload{
		Path: "/home",
		Entries: []DirEntry{
			{Path: "/home/user1", Type: "dir", Size: 1048576},
			{Path: "/home/user2", Type: "dir", Size: 2097152},
		},
	}
	respMsg, err := NewMessage(TypeDirBrowseResp, resp)
	require.NoError(t, err)

	parsedResp, err := ParsePayload[DirBrowseRespPayload](respMsg)
	require.NoError(t, err)
	assert.Len(t, parsedResp.Entries, 2)
	assert.Equal(t, "dir", parsedResp.Entries[0].Type)
}

func TestSnapshotListRoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	resp := SnapshotListRespPayload{
		AgentID: "agent-003",
		Snapshots: []SnapshotInfo{
			{ID: "snap1", Time: now, Paths: []string{"/etc", "/home"}, Size: 500000},
			{ID: "snap2", Time: now.Add(-24 * time.Hour), Paths: []string{"/etc"}, Size: 300000},
		},
	}

	msg, err := NewMessage(TypeSnapshotListResp, resp)
	require.NoError(t, err)

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	parsed, err := ParsePayload[SnapshotListRespPayload](&decoded)
	require.NoError(t, err)
	assert.Len(t, parsed.Snapshots, 2)
	assert.Equal(t, "snap1", parsed.Snapshots[0].ID)
}

func TestRestorePayloads(t *testing.T) {
	reqPayload := RestoreReqPayload{SnapshotID: "abc123", Target: "/restore/20260518"}
	msg, err := NewMessage(TypeRestoreReq, reqPayload)
	require.NoError(t, err)

	parsed, err := ParsePayload[RestoreReqPayload](msg)
	require.NoError(t, err)
	assert.Equal(t, "abc123", parsed.SnapshotID)
	assert.Equal(t, "/restore/20260518", parsed.Target)

	progress := RestoreProgressPayload{
		AgentID:       "agent-001",
		SnapshotID:    "abc123",
		FilesRestored: 1500,
		BytesRestored: 104857600,
		Percent:       75.5,
	}
	progressMsg, err := NewMessage(TypeRestoreProgress, progress)
	require.NoError(t, err)

	parsedProgress, err := ParsePayload[RestoreProgressPayload](progressMsg)
	require.NoError(t, err)
	assert.Equal(t, int64(1500), parsedProgress.FilesRestored)
	assert.InDelta(t, 75.5, parsedProgress.Percent, 0.01)
}

func TestAllMessageTypeConstants(t *testing.T) {
	types := []string{
		TypeHeartbeat,
		TypeDirBrowseReq,
		TypeDirBrowseResp,
		TypePolicyPush,
		TypePolicyAck,
		TypeBackupNow,
		TypeTaskResult,
		TypeRestoreReq,
		TypeRestoreProgress,
		TypeSnapshotListReq,
		TypeSnapshotListResp,
	}
	assert.Len(t, types, 11)
	seen := make(map[string]bool)
	for _, typ := range types {
		assert.NotEmpty(t, typ)
		assert.False(t, seen[typ], "duplicate type constant: %s", typ)
		seen[typ] = true
	}
}

func TestNewMessage_InvalidPayload(t *testing.T) {
	_, err := NewMessage(TypeHeartbeat, make(chan int))
	assert.Error(t, err)
}

func TestParsePayload_InvalidJSON(t *testing.T) {
	msg := &Message{
		Type:    TypeHeartbeat,
		ID:      "test",
		Payload: []byte(`{invalid json`),
	}
	_, err := ParsePayload[HeartbeatPayload](msg)
	assert.Error(t, err)
}
```

- [ ] 1.3 — Run tests (expect compilation failure — types not yet defined)

```bash
go test ./pkg/protocol/... -v
```

- [ ] 1.4 — Implement protocol types (`pkg/protocol/message.go`)

```go
// pkg/protocol/message.go
package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

const (
	TypeHeartbeat        = "heartbeat"
	TypeDirBrowseReq     = "dir_browse_req"
	TypeDirBrowseResp    = "dir_browse_resp"
	TypePolicyPush       = "policy_push"
	TypePolicyAck        = "policy_ack"
	TypeBackupNow        = "backup_now"
	TypeTaskResult       = "task_result"
	TypeRestoreReq       = "restore_req"
	TypeRestoreProgress  = "restore_progress"
	TypeSnapshotListReq  = "snapshot_list_req"
	TypeSnapshotListResp = "snapshot_list_resp"
)

type Message struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload"`
}

func NewMessage(msgType string, payload interface{}) (*Message, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 16)
	rand.Read(id)
	return &Message{
		Type:    msgType,
		ID:      hex.EncodeToString(id),
		Payload: json.RawMessage(data),
	}, nil
}

func ParsePayload[T any](msg *Message) (*T, error) {
	var payload T
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// --- Agent → Master payloads ---

type HeartbeatPayload struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryPercent float64 `json:"memory_percent"`
	DiskPercent   float64 `json:"disk_percent"`
	ResticVersion string  `json:"restic_version"`
	RcloneVersion string  `json:"rclone_version"`
	Uptime        int64   `json:"uptime"`
}

type DirBrowseRespPayload struct {
	Path    string     `json:"path"`
	Entries []DirEntry `json:"entries"`
	Error   string     `json:"error,omitempty"`
}

type DirEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

type PolicyAckPayload struct {
	AgentID string `json:"agent_id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type TaskResultPayload struct {
	AgentID    string    `json:"agent_id"`
	TaskType   string    `json:"task_type"`
	Status     string    `json:"status"`
	SnapshotID string    `json:"snapshot_id,omitempty"`
	DurationMs int64     `json:"duration_ms"`
	RepoSize   int64     `json:"repo_size"`
	ErrorLog   string    `json:"error_log,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

type RestoreProgressPayload struct {
	AgentID       string  `json:"agent_id"`
	SnapshotID    string  `json:"snapshot_id"`
	FilesRestored int64   `json:"files_restored"`
	BytesRestored int64   `json:"bytes_restored"`
	Percent       float64 `json:"percent"`
}

type SnapshotListRespPayload struct {
	AgentID   string         `json:"agent_id"`
	Snapshots []SnapshotInfo `json:"snapshots"`
	Error     string         `json:"error,omitempty"`
}

type SnapshotInfo struct {
	ID    string    `json:"id"`
	Time  time.Time `json:"time"`
	Paths []string  `json:"paths"`
	Size  int64     `json:"size"`
}

// --- Master → Agent payloads ---

type DirBrowseReqPayload struct {
	Path  string `json:"path"`
	Depth int    `json:"depth"`
}

type PolicyPushPayload struct {
	AgentID         string          `json:"agent_id"`
	Storage         StorageConfig   `json:"storage"`
	ResticPassword  string          `json:"restic_password"`
	BackupDirs      []string        `json:"backup_dirs"`
	ExcludePatterns []string        `json:"exclude_patterns"`
	Schedule        string          `json:"schedule"`
	Retention       RetentionPolicy `json:"retention"`
}

type StorageConfig struct {
	RcloneType   string            `json:"rclone_type"`
	RcloneConfig map[string]string `json:"rclone_config"`
	RepoPath     string            `json:"repo_path"`
}

type RetentionPolicy struct {
	KeepLast    int `json:"keep_last"`
	KeepDaily   int `json:"keep_daily"`
	KeepWeekly  int `json:"keep_weekly"`
	KeepMonthly int `json:"keep_monthly"`
}

type BackupNowPayload struct {
	AgentID string `json:"agent_id"`
}

type RestoreReqPayload struct {
	SnapshotID string `json:"snapshot_id"`
	Target     string `json:"target"`
}

type SnapshotListReqPayload struct {
	AgentID string `json:"agent_id"`
}
```

- [ ] 1.5 — Install dependencies and verify all tests pass

```bash
go get github.com/stretchr/testify
go mod tidy
go test ./pkg/protocol/... -v
go test ./pkg/protocol/... -run TestMessageMarshalUnmarshal -v
go test ./pkg/protocol/... -run TestPolicyPushPayload -v
go test ./pkg/protocol/... -run TestTaskResultPayload -v
go test ./pkg/protocol/... -run TestDirBrowseRoundTrip -v
go test ./pkg/protocol/... -run TestSnapshotListRoundTrip -v
go test ./pkg/protocol/... -run TestRestorePayloads -v
go test ./pkg/protocol/... -run TestAllMessageTypeConstants -v
```

- [ ] 1.6 — Commit

```bash
git add go.mod go.sum cmd/ internal/ pkg/ web/ build/
git commit -m "feat: initialize project structure and shared protocol types

- Go module init with full directory scaffolding for master/agent
- Protocol message envelope with JSON RawMessage payload
- All 11 WebSocket message type constants matching design spec
- Typed payload structs for heartbeat, policy, task, dir browse, snapshot, restore
- NewMessage constructor and generic ParsePayload helper
- Full marshal/unmarshal test coverage for all payload types"
```

---

### Task 2: Master Database Layer

**Files:**
- `internal/master/db/crypto.go`
- `internal/master/db/crypto_test.go`
- `internal/master/db/models.go`
- `internal/master/db/db.go`
- `internal/master/db/db_test.go`

**Steps:**

- [ ] 2.1 — Write crypto tests (`internal/master/db/crypto_test.go`)

```go
// internal/master/db/crypto_test.go
package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitMasterKey_Generate(t *testing.T) {
	dir := t.TempDir()
	key, err := InitMasterKey(dir)
	require.NoError(t, err)
	assert.Len(t, key, 32)

	data, err := os.ReadFile(filepath.Join(dir, "master.key"))
	require.NoError(t, err)
	assert.Equal(t, key, data)
}

func TestInitMasterKey_LoadExisting(t *testing.T) {
	dir := t.TempDir()

	key1, err := InitMasterKey(dir)
	require.NoError(t, err)

	key2, err := InitMasterKey(dir)
	require.NoError(t, err)
	assert.Equal(t, key1, key2)
}

func TestInitMasterKey_InvalidSize(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "master.key"), []byte("too-short"), 0600)

	_, err := InitMasterKey(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid master.key")
}

func TestEncryptDecrypt(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	plaintext := "super-secret-rclone-credential"
	encrypted, err := Encrypt(plaintext, key)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, encrypted)

	decrypted, err := Decrypt(encrypted, key)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptDecrypt_DifferentNonces(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	enc1, _ := Encrypt("same-text", key)
	enc2, _ := Encrypt("same-text", key)
	assert.NotEqual(t, enc1, enc2, "random nonce should produce different ciphertexts")
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	for i := range key1 {
		key1[i] = byte(i)
		key2[i] = byte(i + 1)
	}

	encrypted, err := Encrypt("secret", key1)
	require.NoError(t, err)

	_, err = Decrypt(encrypted, key2)
	assert.Error(t, err)
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	key := make([]byte, 32)
	_, err := Decrypt("not-valid-base64!!!", key)
	assert.Error(t, err)
}

func TestEncryptDecrypt_EmptyString(t *testing.T) {
	key := make([]byte, 32)

	encrypted, err := Encrypt("", key)
	require.NoError(t, err)

	decrypted, err := Decrypt(encrypted, key)
	require.NoError(t, err)
	assert.Equal(t, "", decrypted)
}

func TestEncryptDecrypt_LongString(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	longText := ""
	for i := 0; i < 1000; i++ {
		longText += "a]bcdefghij"
	}

	encrypted, err := Encrypt(longText, key)
	require.NoError(t, err)

	decrypted, err := Decrypt(encrypted, key)
	require.NoError(t, err)
	assert.Equal(t, longText, decrypted)
}

func TestMasterKeyFilePermissions(t *testing.T) {
	dir := t.TempDir()
	_, err := InitMasterKey(dir)
	require.NoError(t, err)

	info, err := os.Stat(filepath.Join(dir, "master.key"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
```

- [ ] 2.2 — Write DB and model tests (`internal/master/db/db_test.go`)

```go
// internal/master/db/db_test.go
package db

import (
	"testing"
	"time"

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
	dir := t.TempDir() + "/nested/data"
	database, err := New(dir)
	require.NoError(t, err)
	assert.NotNil(t, database.DB)
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
	database.DB.First(&updated, "id = ?", user.ID)
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

	database.DB.Model(&found).Update("status", "online")
	var updated Agent
	database.DB.First(&updated, "id = ?", agent.ID)
	assert.Equal(t, "online", updated.Status)

	database.DB.Delete(&Agent{}, "id = ?", agent.ID)
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

	database.DB.Model(&found).Update("name", "Updated R2")
	var updated StorageConfig
	database.DB.First(&updated, "id = ?", sc.ID)
	assert.Equal(t, "Updated R2", updated.Name)

	database.DB.Delete(&StorageConfig{}, "id = ?", sc.ID)
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
		Synced:          false,
	}
	require.NoError(t, database.DB.Create(&policy).Error)
	assert.NotEmpty(t, policy.ID)

	var found BackupPolicy
	require.NoError(t, database.DB.First(&found, "id = ?", policy.ID).Error)
	assert.Equal(t, "0 3 * * *", found.Schedule)
	assert.False(t, found.Synced)

	database.DB.Model(&found).Update("synced", true)
	var updated BackupPolicy
	database.DB.First(&updated, "id = ?", policy.ID)
	assert.True(t, updated.Synced)

	database.DB.Delete(&BackupPolicy{}, "id = ?", policy.ID)
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

	database.DB.Model(&found).Update("status", "failed")
	var updated TaskHistory
	database.DB.First(&updated, "id = ?", th.ID)
	assert.Equal(t, "failed", updated.Status)

	database.DB.Delete(&TaskHistory{}, "id = ?", th.ID)
	result := database.DB.First(&TaskHistory{}, "id = ?", th.ID)
	assert.Error(t, result.Error)
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

	database.DB.Model(&found).Update("size", 600000)
	var updated Snapshot
	database.DB.First(&updated, "id = ?", snap.ID)
	assert.Equal(t, int64(600000), updated.Size)

	database.DB.Delete(&Snapshot{}, "id = ?", snap.ID)
	result := database.DB.First(&Snapshot{}, "id = ?", snap.ID)
	assert.Error(t, result.Error)
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

	database.DB.Model(&found).Update("type", "webhook")
	var updated NotificationConfig
	database.DB.First(&updated, "id = ?", nc.ID)
	assert.Equal(t, "webhook", updated.Type)

	database.DB.Delete(&NotificationConfig{}, "id = ?", nc.ID)
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
```

- [ ] 2.3 — Run tests (expect compilation failure — implementation not yet written)

```bash
go test ./internal/master/db/... -v
```

- [ ] 2.4 — Implement crypto (`internal/master/db/crypto.go`)

```go
// internal/master/db/crypto.go
package db

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func InitMasterKey(dataDir string) ([]byte, error) {
	keyPath := filepath.Join(dataDir, "master.key")

	if data, err := os.ReadFile(keyPath); err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("invalid master.key: expected 32 bytes, got %d", len(data))
		}
		return data, nil
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}

	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		return nil, fmt.Errorf("write master.key: %w", err)
	}

	return key, nil
}

func Encrypt(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func Decrypt(ciphertext string, key []byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, sealed := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
```

- [ ] 2.5 — Implement models (`internal/master/db/models.go`)

```go
// internal/master/db/models.go
package db

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type User struct {
	ID           string    `gorm:"type:text;primaryKey" json:"id"`
	Username     string    `gorm:"type:text;uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"type:text;not null" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
	return nil
}

type Agent struct {
	ID          string     `gorm:"type:text;primaryKey" json:"id"`
	Name        string     `gorm:"type:text;not null" json:"name"`
	EnrollToken string     `gorm:"type:text" json:"enroll_token,omitempty"`
	AgentToken  string     `gorm:"type:text" json:"-"`
	Status      string     `gorm:"type:text;default:offline" json:"status"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
	SystemInfo  string     `gorm:"type:text" json:"system_info"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (a *Agent) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return nil
}

type StorageConfig struct {
	ID           string    `gorm:"type:text;primaryKey" json:"id"`
	Name         string    `gorm:"type:text;not null" json:"name"`
	RcloneType   string    `gorm:"type:text;not null" json:"rclone_type"`
	RcloneConfig string    `gorm:"type:text" json:"rclone_config"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *StorageConfig) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

type BackupPolicy struct {
	ID              string    `gorm:"type:text;primaryKey" json:"id"`
	AgentID         string    `gorm:"type:text;index;not null" json:"agent_id"`
	StorageID       string    `gorm:"type:text;not null" json:"storage_id"`
	RepoPath        string    `gorm:"type:text" json:"repo_path"`
	ResticPassword  string    `gorm:"type:text" json:"-"`
	BackupDirs      string    `gorm:"type:text" json:"backup_dirs"`
	ExcludePatterns string    `gorm:"type:text" json:"exclude_patterns"`
	Schedule        string    `gorm:"type:text" json:"schedule"`
	Retention       string    `gorm:"type:text" json:"retention"`
	Synced          bool      `gorm:"default:false" json:"synced"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (b *BackupPolicy) BeforeCreate(tx *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.New().String()
	}
	return nil
}

type TaskHistory struct {
	ID         string     `gorm:"type:text;primaryKey" json:"id"`
	AgentID    string     `gorm:"type:text;index;not null" json:"agent_id"`
	Type       string     `gorm:"type:text;not null" json:"type"`
	Status     string     `gorm:"type:text;not null" json:"status"`
	SnapshotID string     `gorm:"type:text" json:"snapshot_id"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	DurationMs int64      `json:"duration_ms"`
	RepoSize   int64      `json:"repo_size"`
	ErrorLog   string     `gorm:"type:text" json:"error_log"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (th *TaskHistory) BeforeCreate(tx *gorm.DB) error {
	if th.ID == "" {
		th.ID = uuid.New().String()
	}
	return nil
}

type Snapshot struct {
	ID         string    `gorm:"type:text;primaryKey" json:"id"`
	AgentID    string    `gorm:"type:text;index;not null" json:"agent_id"`
	SnapshotID string    `gorm:"type:text;not null" json:"snapshot_id"`
	Timestamp  time.Time `json:"timestamp"`
	Paths      string    `gorm:"type:text" json:"paths"`
	Size       int64     `json:"size"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Snapshot) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	return nil
}

type NotificationConfig struct {
	ID        string    `gorm:"type:text;primaryKey" json:"id"`
	Type      string    `gorm:"type:text;not null" json:"type"`
	Config    string    `gorm:"type:text" json:"config"`
	Events    string    `gorm:"type:text" json:"events"`
	CreatedAt time.Time `json:"created_at"`
}

func (n *NotificationConfig) BeforeCreate(tx *gorm.DB) error {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	return nil
}
```

- [ ] 2.6 — Implement database init (`internal/master/db/db.go`)

```go
// internal/master/db/db.go
package db

import (
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Database struct {
	DB        *gorm.DB
	DataDir   string
	MasterKey []byte
}

func New(dataDir string) (*Database, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "vaultfleet.db")
	gormDB, err := gorm.Open(sqlite.Open(dbPath+"?_journal_mode=WAL"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := gormDB.AutoMigrate(
		&User{},
		&Agent{},
		&StorageConfig{},
		&BackupPolicy{},
		&TaskHistory{},
		&Snapshot{},
		&NotificationConfig{},
	); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}

	masterKey, err := InitMasterKey(dataDir)
	if err != nil {
		return nil, fmt.Errorf("init master key: %w", err)
	}

	return &Database{DB: gormDB, DataDir: dataDir, MasterKey: masterKey}, nil
}
```

- [ ] 2.7 — Install dependencies and verify all tests pass

```bash
go get github.com/google/uuid
go get gorm.io/gorm
go get gorm.io/driver/sqlite
go mod tidy
go test ./internal/master/db/... -v
go test ./internal/master/db/... -run TestInitMasterKey -v
go test ./internal/master/db/... -run TestEncryptDecrypt -v
go test ./internal/master/db/... -run TestDecrypt_WrongKey -v
go test ./internal/master/db/... -run TestDatabaseInit -v
go test ./internal/master/db/... -run TestUserCRUD -v
go test ./internal/master/db/... -run TestAgentCRUD -v
go test ./internal/master/db/... -run TestStorageConfigCRUD -v
go test ./internal/master/db/... -run TestBackupPolicyCRUD -v
go test ./internal/master/db/... -run TestTaskHistoryCRUD -v
go test ./internal/master/db/... -run TestSnapshotCRUD -v
go test ./internal/master/db/... -run TestNotificationConfigCRUD -v
```

- [ ] 2.8 — Commit

```bash
git add internal/master/db/
git commit -m "feat(master): add database layer with GORM models and AES-256-GCM crypto

- SQLite database with WAL mode and --data-dir support
- All 7 GORM models: User, Agent, StorageConfig, BackupPolicy, TaskHistory, Snapshot, NotificationConfig
- UUID auto-generation via BeforeCreate hooks
- AES-256-GCM encrypt/decrypt for sensitive field storage
- master.key auto-generation on first startup (0600 permissions)
- Full CRUD test coverage for every model plus crypto round-trip tests"
```

---

### Task 3: Master Auth + Init Wizard

**Files:**
- `internal/master/api/auth.go`
- `internal/master/api/middleware.go`
- `internal/master/api/auth_test.go`

**Steps:**

- [ ] 3.1 — Write auth and middleware tests (`internal/master/api/auth_test.go`)

```go
// internal/master/api/auth_test.go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
)

func setupTestAuth(t *testing.T) (*AuthHandler, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	handler := NewAuthHandler(database)

	r := gin.New()
	r.GET("/api/auth/check", handler.CheckInit)
	r.POST("/api/auth/init", handler.InitSetup)
	r.POST("/api/auth/login", handler.Login)

	protected := r.Group("/api")
	protected.Use(RequireInit(database), RequireAuth(handler.Sessions))
	protected.GET("/protected", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true, "user": c.GetString("username")})
	})

	return handler, r
}

func postJSON(r *gin.Engine, path string, body interface{}, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	r.ServeHTTP(w, req)
	return w
}

func parseJSON(w *httptest.ResponseRecorder) map[string]interface{} {
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp
}

func getSessionCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	return nil
}

func TestCheckInit_NoUsers(t *testing.T) {
	_, r := setupTestAuth(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/auth/check", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(w)
	data := resp["data"].(map[string]interface{})
	assert.False(t, data["initialized"].(bool))
}

func TestCheckInit_WithUsers(t *testing.T) {
	_, r := setupTestAuth(t)

	postJSON(r, "/api/auth/init", map[string]string{
		"username": "admin", "password": "secret123",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/auth/check", nil)
	r.ServeHTTP(w, req)

	resp := parseJSON(w)
	data := resp["data"].(map[string]interface{})
	assert.True(t, data["initialized"].(bool))
}

func TestInitSetup(t *testing.T) {
	_, r := setupTestAuth(t)

	w := postJSON(r, "/api/auth/init", map[string]string{
		"username": "admin", "password": "secret123",
	})

	assert.Equal(t, 200, w.Code)
	cookie := getSessionCookie(w)
	require.NotNil(t, cookie, "session cookie should be set")
	assert.NotEmpty(t, cookie.Value)
	assert.True(t, cookie.HttpOnly)
}

func TestInitSetup_BlockedAfterFirstUser(t *testing.T) {
	_, r := setupTestAuth(t)

	w1 := postJSON(r, "/api/auth/init", map[string]string{
		"username": "admin", "password": "secret123",
	})
	assert.Equal(t, 200, w1.Code)

	w2 := postJSON(r, "/api/auth/init", map[string]string{
		"username": "admin2", "password": "secret456",
	})
	assert.Equal(t, 400, w2.Code)
	resp := parseJSON(w2)
	assert.Equal(t, "system already initialized", resp["error"])
}

func TestInitSetup_PasswordTooShort(t *testing.T) {
	_, r := setupTestAuth(t)

	w := postJSON(r, "/api/auth/init", map[string]string{
		"username": "admin", "password": "short",
	})
	assert.Equal(t, 400, w.Code)
}

func TestLogin(t *testing.T) {
	_, r := setupTestAuth(t)

	postJSON(r, "/api/auth/init", map[string]string{
		"username": "admin", "password": "secret123",
	})

	w := postJSON(r, "/api/auth/login", map[string]string{
		"username": "admin", "password": "secret123",
	})

	assert.Equal(t, 200, w.Code)
	cookie := getSessionCookie(w)
	require.NotNil(t, cookie)
	assert.NotEmpty(t, cookie.Value)
}

func TestLogin_InvalidPassword(t *testing.T) {
	_, r := setupTestAuth(t)

	postJSON(r, "/api/auth/init", map[string]string{
		"username": "admin", "password": "secret123",
	})

	w := postJSON(r, "/api/auth/login", map[string]string{
		"username": "admin", "password": "wrongpassword",
	})
	assert.Equal(t, 401, w.Code)
}

func TestLogin_NonexistentUser(t *testing.T) {
	_, r := setupTestAuth(t)

	postJSON(r, "/api/auth/init", map[string]string{
		"username": "admin", "password": "secret123",
	})

	w := postJSON(r, "/api/auth/login", map[string]string{
		"username": "nobody", "password": "secret123",
	})
	assert.Equal(t, 401, w.Code)
}

func TestRequireAuth_ValidSession(t *testing.T) {
	_, r := setupTestAuth(t)

	w1 := postJSON(r, "/api/auth/init", map[string]string{
		"username": "admin", "password": "secret123",
	})
	cookie := getSessionCookie(w1)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/protected", nil)
	req.AddCookie(cookie)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(w)
	assert.Equal(t, "admin", resp["user"])
}

func TestRequireAuth_NoSession(t *testing.T) {
	_, r := setupTestAuth(t)

	postJSON(r, "/api/auth/init", map[string]string{
		"username": "admin", "password": "secret123",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/protected", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
}

func TestRequireAuth_InvalidSession(t *testing.T) {
	_, r := setupTestAuth(t)

	postJSON(r, "/api/auth/init", map[string]string{
		"username": "admin", "password": "secret123",
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/protected", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "ss_invalid_token"})
	r.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
}

func TestRequireInit_NoUsers(t *testing.T) {
	_, r := setupTestAuth(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/protected", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 409, w.Code)
	resp := parseJSON(w)
	assert.Equal(t, "init_required", resp["error"])
}

func TestSessionStore_CreateAndGet(t *testing.T) {
	store := NewSessionStore()
	session := &Session{UserID: "user-1", Username: "admin"}

	token := store.Create(session)
	assert.Contains(t, token, "ss_")

	found, ok := store.Get(token)
	assert.True(t, ok)
	assert.Equal(t, "user-1", found.UserID)
}

func TestSessionStore_Delete(t *testing.T) {
	store := NewSessionStore()
	session := &Session{UserID: "user-1", Username: "admin"}

	token := store.Create(session)
	store.Delete(token)

	_, ok := store.Get(token)
	assert.False(t, ok)
}

func TestSessionStore_GetNonexistent(t *testing.T) {
	store := NewSessionStore()
	_, ok := store.Get("ss_nonexistent")
	assert.False(t, ok)
}
```

- [ ] 3.2 — Run tests (expect compilation failure — handlers not yet implemented)

```bash
go test ./internal/master/api/... -v
```

- [ ] 3.3 — Implement auth handler (`internal/master/api/auth.go`)

```go
// internal/master/api/auth.go
package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"vaultfleet/internal/master/db"
)

type Session struct {
	UserID   string
	Username string
	CreateAt time.Time
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*Session)}
}

func (s *SessionStore) Create(session *Session) string {
	token := generateToken("ss_")
	s.mu.Lock()
	s.sessions[token] = session
	s.mu.Unlock()
	return token
}

func (s *SessionStore) Get(token string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[token]
	return sess, ok
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

func generateToken(prefix string) string {
	b := make([]byte, 24)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

type AuthHandler struct {
	DB       *db.Database
	Sessions *SessionStore
}

func NewAuthHandler(database *db.Database) *AuthHandler {
	return &AuthHandler{
		DB:       database,
		Sessions: NewSessionStore(),
	}
}

func (h *AuthHandler) CheckInit(c *gin.Context) {
	var count int64
	h.DB.DB.Model(&db.User{}).Count(&count)
	c.JSON(http.StatusOK, gin.H{
		"ok":   true,
		"data": gin.H{"initialized": count > 0},
	})
}

func (h *AuthHandler) InitSetup(c *gin.Context) {
	var count int64
	h.DB.DB.Model(&db.User{}).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "system already initialized"})
		return
	}

	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to hash password"})
		return
	}

	user := db.User{
		Username:     req.Username,
		PasswordHash: string(hash),
	}
	if err := h.DB.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to create user"})
		return
	}

	token := h.Sessions.Create(&Session{
		UserID:   user.ID,
		Username: user.Username,
		CreateAt: time.Now(),
	})
	c.SetCookie("session", token, 86400*7, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{"username": user.Username}})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var user db.User
	if err := h.DB.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "invalid credentials"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "invalid credentials"})
		return
	}

	token := h.Sessions.Create(&Session{
		UserID:   user.ID,
		Username: user.Username,
		CreateAt: time.Now(),
	})
	c.SetCookie("session", token, 86400*7, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": gin.H{"username": user.Username}})
}
```

- [ ] 3.4 — Implement middleware (`internal/master/api/middleware.go`)

```go
// internal/master/api/middleware.go
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"vaultfleet/internal/master/db"
)

func RequireAuth(sessions *SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Cookie("session")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "unauthorized"})
			return
		}
		session, ok := sessions.Get(cookie)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "unauthorized"})
			return
		}
		c.Set("user_id", session.UserID)
		c.Set("username", session.Username)
		c.Next()
	}
}

func RequireInit(database *db.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		var count int64
		database.DB.Model(&db.User{}).Count(&count)
		if count == 0 {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{
				"ok":    false,
				"error": "init_required",
			})
			return
		}
		c.Next()
	}
}
```

- [ ] 3.5 — Install dependencies and verify all tests pass

```bash
go get github.com/gin-gonic/gin
go get golang.org/x/crypto/bcrypt
go mod tidy
go test ./internal/master/api/... -v
go test ./internal/master/api/... -run TestCheckInit -v
go test ./internal/master/api/... -run TestInitSetup -v
go test ./internal/master/api/... -run TestLogin -v
go test ./internal/master/api/... -run TestRequireAuth -v
go test ./internal/master/api/... -run TestRequireInit -v
go test ./internal/master/api/... -run TestSessionStore -v
```

- [ ] 3.6 — Commit

```bash
git add internal/master/api/auth.go internal/master/api/middleware.go internal/master/api/auth_test.go
git commit -m "feat(master): add authentication, session management, and init wizard

- Login with bcrypt password verification and cookie-based sessions
- Init wizard endpoint creates first admin user (blocks after first user)
- CheckInit endpoint for frontend to detect first-run state
- RequireAuth middleware validates session cookie on protected routes
- RequireInit middleware returns 409 init_required when system is uninitialized
- In-memory thread-safe SessionStore with ss_ prefixed tokens
- Full test coverage: login, init, session validation, middleware chains"
```

---

### Task 4: Master Agent Management API

**Files:**
- `internal/master/api/agents.go`
- `internal/master/api/enroll.go`
- `internal/master/api/agents_test.go`

**Steps:**

- [ ] 4.1 — Write agent management and enrollment tests (`internal/master/api/agents_test.go`)

```go
// internal/master/api/agents_test.go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
)

func setupTestAgents(t *testing.T) (*AgentHandler, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	handler := NewAgentHandler(database)

	r := gin.New()
	r.POST("/api/agents", handler.Create)
	r.GET("/api/agents", handler.List)
	r.GET("/api/agents/:id", handler.Get)
	r.DELETE("/api/agents/:id", handler.Delete)
	r.POST("/api/agents/:id/regenerate-token", handler.RegenerateToken)
	r.POST("/api/agent/enroll", handler.Enroll)

	return handler, r
}

func createTestAgent(t *testing.T, r *gin.Engine, name string) map[string]interface{} {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/agents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp["data"].(map[string]interface{})
}

func TestCreateAgent(t *testing.T) {
	_, r := setupTestAgents(t)

	data := createTestAgent(t, r, "Tokyo-1")
	assert.NotEmpty(t, data["id"])
	assert.Equal(t, "Tokyo-1", data["name"])
	assert.Contains(t, data["enroll_token"].(string), "ek_")
}

func TestCreateAgent_MissingName(t *testing.T) {
	_, r := setupTestAgents(t)

	body, _ := json.Marshal(map[string]string{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/agents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}

func TestListAgents(t *testing.T) {
	_, r := setupTestAgents(t)
	createTestAgent(t, r, "Tokyo-1")
	createTestAgent(t, r, "London-1")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/agents", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	agents := resp["data"].([]interface{})
	assert.Len(t, agents, 2)
}

func TestListAgents_Empty(t *testing.T) {
	_, r := setupTestAgents(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/agents", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	agents := resp["data"].([]interface{})
	assert.Len(t, agents, 0)
}

func TestGetAgent(t *testing.T) {
	_, r := setupTestAgents(t)
	data := createTestAgent(t, r, "Tokyo-1")
	id := data["id"].(string)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/agents/"+id, nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	agent := resp["data"].(map[string]interface{})
	assert.Equal(t, "Tokyo-1", agent["name"])
}

func TestGetAgent_NotFound(t *testing.T) {
	_, r := setupTestAgents(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/agents/nonexistent-id", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, 404, w.Code)
}

func TestDeleteAgent(t *testing.T) {
	_, r := setupTestAgents(t)
	data := createTestAgent(t, r, "Tokyo-1")
	id := data["id"].(string)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/agents/"+id, nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/agents/"+id, nil)
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 404, w2.Code)
}

func TestDeleteAgent_NotFound(t *testing.T) {
	_, r := setupTestAgents(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/agents/nonexistent-id", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 404, w.Code)
}

func TestRegenerateToken(t *testing.T) {
	_, r := setupTestAgents(t)
	data := createTestAgent(t, r, "Tokyo-1")
	id := data["id"].(string)
	oldToken := data["enroll_token"].(string)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/agents/"+id+"/regenerate-token", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 200, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	newData := resp["data"].(map[string]interface{})
	newToken := newData["enroll_token"].(string)
	assert.NotEqual(t, oldToken, newToken)
	assert.Contains(t, newToken, "ek_")
}

func TestRegenerateToken_NotFound(t *testing.T) {
	_, r := setupTestAgents(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/agents/nonexistent-id/regenerate-token", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 404, w.Code)
}

func TestEnrollAgent(t *testing.T) {
	_, r := setupTestAgents(t)
	data := createTestAgent(t, r, "Tokyo-1")
	enrollToken := data["enroll_token"].(string)

	body, _ := json.Marshal(map[string]string{
		"enroll_token": enrollToken,
		"system_info":  `{"os":"linux","arch":"amd64"}`,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/agent/enroll", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	enrollData := resp["data"].(map[string]interface{})
	assert.NotEmpty(t, enrollData["agent_id"])
	assert.Contains(t, enrollData["agent_token"].(string), "ak_")
}

func TestEnrollAgent_InvalidToken(t *testing.T) {
	_, r := setupTestAgents(t)

	body, _ := json.Marshal(map[string]string{
		"enroll_token": "ek_invalid_nonexistent_token",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/agent/enroll", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 401, w.Code)
}

func TestEnrollAgent_TokenConsumedAfterUse(t *testing.T) {
	_, r := setupTestAgents(t)
	data := createTestAgent(t, r, "Tokyo-1")
	enrollToken := data["enroll_token"].(string)

	body1, _ := json.Marshal(map[string]string{"enroll_token": enrollToken})
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("POST", "/api/agent/enroll", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)

	body2, _ := json.Marshal(map[string]string{"enroll_token": enrollToken})
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/api/agent/enroll", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 401, w2.Code, "consumed enrollment token should be rejected")
}

func TestEnrollAgent_RegenerateAndReEnroll(t *testing.T) {
	_, r := setupTestAgents(t)
	data := createTestAgent(t, r, "Tokyo-1")
	id := data["id"].(string)
	enrollToken := data["enroll_token"].(string)

	// First enrollment
	body1, _ := json.Marshal(map[string]string{"enroll_token": enrollToken})
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("POST", "/api/agent/enroll", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w1, req1)
	assert.Equal(t, 200, w1.Code)

	// Regenerate token (simulates reinstall scenario)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/api/agents/"+id+"/regenerate-token", nil)
	r.ServeHTTP(w2, req2)
	assert.Equal(t, 200, w2.Code)

	var regenResp map[string]interface{}
	json.Unmarshal(w2.Body.Bytes(), &regenResp)
	newToken := regenResp["data"].(map[string]interface{})["enroll_token"].(string)

	// Re-enroll with new token
	body3, _ := json.Marshal(map[string]string{"enroll_token": newToken})
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("POST", "/api/agent/enroll", bytes.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w3, req3)
	assert.Equal(t, 200, w3.Code)

	var enrollResp map[string]interface{}
	json.Unmarshal(w3.Body.Bytes(), &enrollResp)
	assert.Equal(t, id, enrollResp["data"].(map[string]interface{})["agent_id"])
}

func TestEnrollAgent_MissingToken(t *testing.T) {
	_, r := setupTestAgents(t)

	body, _ := json.Marshal(map[string]string{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/agent/enroll", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
}
```

- [ ] 4.2 — Run tests (expect compilation failure — handlers not yet implemented)

```bash
go test ./internal/master/api/... -v
```

- [ ] 4.3 — Implement agent management handlers (`internal/master/api/agents.go`)

```go
// internal/master/api/agents.go
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"vaultfleet/internal/master/db"
)

type AgentHandler struct {
	DB *db.Database
}

func NewAgentHandler(database *db.Database) *AgentHandler {
	return &AgentHandler{DB: database}
}

func (h *AgentHandler) Create(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	enrollToken := generateToken("ek_")
	agent := db.Agent{
		Name:        req.Name,
		EnrollToken: enrollToken,
		Status:      "offline",
	}
	if err := h.DB.DB.Create(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to create agent"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"data": gin.H{
			"id":           agent.ID,
			"name":         agent.Name,
			"enroll_token": agent.EnrollToken,
		},
	})
}

func (h *AgentHandler) List(c *gin.Context) {
	var agents []db.Agent
	if err := h.DB.DB.Order("created_at DESC").Find(&agents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"ok": false, "error": "failed to list agents"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": agents})
}

func (h *AgentHandler) Get(c *gin.Context) {
	id := c.Param("id")
	var agent db.Agent
	if err := h.DB.DB.First(&agent, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "agent not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "data": agent})
}

func (h *AgentHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	result := h.DB.DB.Delete(&db.Agent{}, "id = ?", id)
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "agent not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AgentHandler) RegenerateToken(c *gin.Context) {
	id := c.Param("id")
	var agent db.Agent
	if err := h.DB.DB.First(&agent, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "agent not found"})
		return
	}

	newToken := generateToken("ek_")
	h.DB.DB.Model(&agent).Updates(map[string]interface{}{
		"enroll_token": newToken,
		"agent_token":  "",
	})

	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"data": gin.H{
			"id":           agent.ID,
			"enroll_token": newToken,
		},
	})
}
```

- [ ] 4.4 — Implement enrollment endpoint (`internal/master/api/enroll.go`)

```go
// internal/master/api/enroll.go
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"vaultfleet/internal/master/db"
)

func (h *AgentHandler) Enroll(c *gin.Context) {
	var req struct {
		EnrollToken string `json:"enroll_token" binding:"required"`
		SystemInfo  string `json:"system_info"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": err.Error()})
		return
	}

	var agent db.Agent
	if err := h.DB.DB.Where("enroll_token = ?", req.EnrollToken).First(&agent).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "invalid enrollment token"})
		return
	}

	if agent.AgentToken != "" {
		c.JSON(http.StatusConflict, gin.H{"ok": false, "error": "agent already enrolled"})
		return
	}

	agentToken := generateToken("ak_")
	h.DB.DB.Model(&agent).Updates(map[string]interface{}{
		"agent_token":  agentToken,
		"enroll_token": "",
		"system_info":  req.SystemInfo,
	})

	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"data": gin.H{
			"agent_id":    agent.ID,
			"agent_token": agentToken,
		},
	})
}
```

- [ ] 4.5 — Verify all tests pass

```bash
go test ./internal/master/api/... -v
go test ./internal/master/api/... -run TestCreateAgent -v
go test ./internal/master/api/... -run TestListAgents -v
go test ./internal/master/api/... -run TestGetAgent -v
go test ./internal/master/api/... -run TestDeleteAgent -v
go test ./internal/master/api/... -run TestRegenerateToken -v
go test ./internal/master/api/... -run TestEnrollAgent -v
```

- [ ] 4.6 — Run full test suite to verify no regressions

```bash
go test ./... -v
```

- [ ] 4.7 — Commit

```bash
git add internal/master/api/agents.go internal/master/api/enroll.go internal/master/api/agents_test.go
git commit -m "feat(master): add agent management API and enrollment endpoint

- Create agent with auto-generated ek_ enrollment token
- List, get, delete agent CRUD operations
- Regenerate enrollment token (clears agent_token for re-enrollment)
- POST /api/agent/enroll: validates one-time token, issues ak_ agent token
- Enrollment token consumed on use (cleared from DB), preventing replay
- Re-enrollment supported via token regeneration (reinstall scenario)
- Full test coverage: CRUD, enrollment flow, token lifecycle, edge cases"
```
