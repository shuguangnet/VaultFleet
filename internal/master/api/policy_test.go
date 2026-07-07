package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/internal/master/db"
	"vaultfleet/internal/master/events"
	"vaultfleet/pkg/protocol"
)

type testPolicySetup struct {
	database *db.Database
	bus      *events.Bus
	router   *gin.Engine
}

func setupTestPolicyAPI(t *testing.T) testPolicySetup {
	t.Helper()

	gin.SetMode(gin.TestMode)

	database, err := db.New(t.TempDir())
	require.NoError(t, err)

	bus := events.NewBus()
	handler := NewPolicyHandler(database, bus)
	router := gin.New()
	api := router.Group("/api")
	RegisterPolicyRoutes(api, handler)

	return testPolicySetup{
		database: database,
		bus:      bus,
		router:   router,
	}
}

func TestCreatePolicy(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":         agent.ID,
		"storage_id":       storage.ID,
		"backup_dirs":      []string{"/etc", "/home"},
		"exclude_patterns": []string{"*.log", "*.tmp"},
		"schedule":         "0 3 * * *",
		"retention": map[string]any{
			"keep_last":    3,
			"keep_daily":   7,
			"keep_weekly":  4,
			"keep_monthly": 6,
		},
	})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.NotEmpty(t, body["id"])
	assert.Equal(t, agent.ID, body["agent_id"])
	assert.Equal(t, storage.ID, body["storage_id"])
	assert.Equal(t, "vaultfleet/"+agent.ID, body["repo_path"])
	assert.NotContains(t, body, "restic_password")
	assert.Equal(t, false, body["synced"])
	assertJSONList(t, body["backup_dirs"], []string{"/etc", "/home"})
	assertJSONList(t, body["exclude_patterns"], []string{"*.log", "*.tmp"})
	retention := requireMap(t, body["retention"])
	assert.Equal(t, float64(3), retention["keep_last"])
	assert.Equal(t, float64(7), retention["keep_daily"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", body["id"]).Error)
	assert.Equal(t, `["/etc","/home"]`, stored.BackupDirs)
	assert.Equal(t, `["*.log","*.tmp"]`, stored.ExcludePatterns)
	assert.JSONEq(t, `{"keep_last":3,"keep_daily":7,"keep_weekly":4,"keep_monthly":6}`, stored.Retention)
	assert.NotEmpty(t, stored.ResticPassword)
}

func TestCreatePolicyDefaultsTimeoutHours(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	created := createPolicy(t, setup.router, agent.ID, storage.ID)

	assert.Equal(t, float64(6), created["timeout_hours"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", created["id"]).Error)
	assert.Equal(t, 6, stored.TimeoutHours)
}

func TestBulkAssignPoliciesClonesSourcePolicyToSelectedAgents(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	sourceAgent := createPolicyTestAgent(t, setup.database)
	targetA := createNamedPolicyTestAgent(t, setup.database, "Target A")
	targetB := createNamedPolicyTestAgent(t, setup.database, "Target B")
	storage := createPolicyTestStorage(t, setup.database)
	source := createPolicy(t, setup.router, sourceAgent.ID, storage.ID)

	w := postAnyJSON(t, setup.router, "/api/policies/bulk-assign", map[string]any{
		"source_policy_id": source["id"],
		"target_agent_ids": []string{targetA.ID, "missing-agent", targetB.ID},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, float64(3), body["requested_count"])
	assert.Equal(t, float64(2), body["matched_count"])
	assert.Equal(t, float64(2), body["created_count"])
	assert.Equal(t, float64(1), body["failed_count"])

	results := body["results"].([]any)
	require.Len(t, results, 3)
	var createdPolicies []db.BackupPolicy
	require.NoError(t, setup.database.DB.Where("agent_id IN ?", []string{targetA.ID, targetB.ID}).Order("agent_id").Find(&createdPolicies).Error)
	require.Len(t, createdPolicies, 2)
	for _, policy := range createdPolicies {
		assert.Equal(t, storage.ID, policy.StorageID)
		assert.Equal(t, "vaultfleet/"+policy.AgentID, policy.RepoPath)
		assert.False(t, policy.Synced)
	}
}

func TestBulkAssignPoliciesResolvesTargetsByTags(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	sourceAgent := createPolicyTestAgent(t, setup.database)
	targetA := createNamedPolicyTestAgent(t, setup.database, "Web A")
	targetB := createNamedPolicyTestAgent(t, setup.database, "DB B")
	targetC := createNamedPolicyTestAgent(t, setup.database, "Web C")
	setPolicyAgentTags(t, setup.database, targetA.ID, []string{"prod", "web"})
	setPolicyAgentTags(t, setup.database, targetB.ID, []string{"prod", "db"})
	setPolicyAgentTags(t, setup.database, targetC.ID, []string{"stage", "web"})
	storage := createPolicyTestStorage(t, setup.database)
	source := createPolicy(t, setup.router, sourceAgent.ID, storage.ID)

	w := postAnyJSON(t, setup.router, "/api/policies/bulk-assign", map[string]any{
		"source_policy_id": source["id"],
		"target_tags":      []string{" Prod ", "web"},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, float64(1), body["matched_count"])
	assert.Equal(t, float64(1), body["created_count"])
	results := body["results"].([]any)
	require.Len(t, results, 1)
	assert.Equal(t, targetA.ID, results[0].(map[string]any)["agent_id"])
}

func TestBulkAssignPoliciesReportsDockerCapabilityPartialFailures(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	sourceAgent := createPolicyTestAgent(t, setup.database)
	markPolicyAgentCapabilities(t, setup.database, sourceAgent.ID, []string{protocol.CapabilityDockerWorkloadBackups})
	supportedTarget := createNamedPolicyTestAgent(t, setup.database, "Docker Target")
	markPolicyAgentCapabilities(t, setup.database, supportedTarget.ID, []string{protocol.CapabilityDockerWorkloadBackups})
	unsupportedTarget := createNamedPolicyTestAgent(t, setup.database, "Plain Target")
	storage := createPolicyTestStorage(t, setup.database)

	w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":   sourceAgent.ID,
		"storage_id": storage.ID,
		"backup_sources": []map[string]any{
			{
				"type": "docker_container",
				"docker_container": map[string]any{
					"name":                "app",
					"include_bind_mounts": true,
				},
			},
		},
		"schedule":  "0 3 * * *",
		"retention": map[string]any{"keep_last": 3},
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	source := parseJSON(t, w)

	w = postAnyJSON(t, setup.router, "/api/policies/bulk-assign", map[string]any{
		"source_policy_id": source["id"],
		"target_agent_ids": []string{supportedTarget.ID, unsupportedTarget.ID},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, float64(1), body["created_count"])
	assert.Equal(t, float64(1), body["failed_count"])
	results := body["results"].([]any)
	require.Len(t, results, 2)
	assert.Equal(t, true, results[0].(map[string]any)["ok"])
	assert.Equal(t, false, results[1].(map[string]any)["ok"])
	assert.Equal(t, "agent does not support Docker workload backups", results[1].(map[string]any)["error"])
}

func TestCreatePolicySupportsArchiveBackupMode(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":       agent.ID,
		"storage_id":     storage.ID,
		"backup_mode":    "archive",
		"archive_format": "zip",
		"backup_dirs":    []string{"/etc", "/var/lib/app"},
		"schedule":       "0 3 * * *",
		"retention":      map[string]any{"keep_last": 3},
	})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, "archive", body["backup_mode"])
	assert.Equal(t, "zip", body["archive_format"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", body["id"]).Error)
	assert.Equal(t, "archive", stored.BackupMode)
	assert.Equal(t, "zip", stored.ArchiveFormat)

	payload, err := policyPushPayload(setup.database, stored, storage)
	require.NoError(t, err)
	assert.Equal(t, "archive", payload.BackupMode)
	assert.Equal(t, "zip", payload.ArchiveFormat)
	assert.Empty(t, payload.ResticPassword)
	assert.True(t, payload.PlainBackup)
}

func TestCreatePolicyWithDockerBackupSources(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	markPolicyAgentCapabilities(t, setup.database, agent.ID, []string{protocol.CapabilityDockerWorkloadBackups})
	storage := createPolicyTestStorage(t, setup.database)

	w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":    agent.ID,
		"storage_id":  storage.ID,
		"backup_dirs": []string{"/etc"},
		"backup_sources": []map[string]any{
			{"type": "path", "path": "/etc"},
			{
				"type": "docker_container",
				"docker_container": map[string]any{
					"container_id":          "container-1",
					"name":                  "db",
					"include_bind_mounts":   true,
					"include_volumes":       true,
					"include_compose_files": true,
				},
			},
		},
		"schedule":  "0 3 * * *",
		"retention": map[string]any{"keep_last": 3},
	})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assertJSONList(t, body["backup_dirs"], []string{"/etc"})
	sources := body["backup_sources"].([]any)
	require.Len(t, sources, 2)
	assert.Equal(t, "docker_container", sources[1].(map[string]any)["type"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", body["id"]).Error)
	assert.JSONEq(t, `[{"type":"path","path":"/etc"},{"type":"docker_container","docker_container":{"container_id":"container-1","name":"db","include_bind_mounts":true,"include_volumes":true,"include_compose_files":true}}]`, stored.BackupSources)
}

func TestCreatePolicyRejectsDockerSourceForUnsupportedAgent(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":   agent.ID,
		"storage_id": storage.ID,
		"backup_sources": []map[string]any{
			{
				"type": "docker_container",
				"docker_container": map[string]any{
					"container_id":        "container-1",
					"include_bind_mounts": true,
				},
			},
		},
		"schedule":  "0 3 * * *",
		"retention": map[string]any{"keep_last": 3},
	})

	require.Equal(t, http.StatusBadRequest, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, "agent does not support Docker workload backups", body["error"])
}

func TestCreatePolicyPersistsTimeoutHours(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":      agent.ID,
		"storage_id":    storage.ID,
		"backup_dirs":   []string{"/etc"},
		"schedule":      "0 3 * * *",
		"retention":     map[string]any{"keep_last": 3},
		"timeout_hours": 12,
	})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, float64(12), body["timeout_hours"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", body["id"]).Error)
	assert.Equal(t, 12, stored.TimeoutHours)
}

func TestCreatePolicyRejectsInvalidTimeoutHours(t *testing.T) {
	for _, timeoutHours := range []int{0, -1, 73} {
		t.Run(strconv.Itoa(timeoutHours), func(t *testing.T) {
			setup := setupTestPolicyAPI(t)
			agent := createPolicyTestAgent(t, setup.database)
			storage := createPolicyTestStorage(t, setup.database)

			w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
				"agent_id":      agent.ID,
				"storage_id":    storage.ID,
				"backup_dirs":   []string{"/etc"},
				"schedule":      "0 3 * * *",
				"retention":     map[string]any{"keep_last": 3},
				"timeout_hours": timeoutHours,
			})

			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			body := parseJSON(t, w)
			assert.Contains(t, body["error"], "timeout_hours")
		})
	}
}

func TestCreatePolicyWithRclone(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	rcloneArgs := map[string]string{
		"transfers":     "4",
		"tpslimit":      "2.5",
		"retries-sleep": "10s",
	}

	w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":    agent.ID,
		"storage_id":  storage.ID,
		"backup_dirs": []string{"/etc"},
		"schedule":    "0 3 * * *",
		"retention":   map[string]any{"keep_last": 3},
		"rclone_args": rcloneArgs,
	})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := parseJSON(t, w)
	responseArgs := requireMap(t, body["rclone_args"])
	assert.Equal(t, "4", responseArgs["transfers"])
	assert.Equal(t, "2.5", responseArgs["tpslimit"])
	assert.Equal(t, "10s", responseArgs["retries-sleep"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", body["id"]).Error)
	assert.JSONEq(t, `{"transfers":"4","tpslimit":"2.5","retries-sleep":"10s"}`, stored.RcloneArgs)

	payload, err := policyPushPayload(setup.database, stored, storage)
	require.NoError(t, err)
	assert.Equal(t, rcloneArgs, payload.Storage.RcloneArgs)
}

func TestCreatePolicyRejectsInvalidRcloneArgs(t *testing.T) {
	tests := []struct {
		name  string
		args  map[string]string
		error string
	}{
		{
			name:  "leading dashes",
			args:  map[string]string{"--transfers": "4"},
			error: "invalid rclone_args",
		},
		{
			name:  "unsupported option",
			args:  map[string]string{"bwlimit": "10M"},
			error: "invalid rclone_args",
		},
		{
			name:  "invalid positive integer",
			args:  map[string]string{"transfers": "0"},
			error: "invalid rclone_args",
		},
		{
			name:  "invalid duration",
			args:  map[string]string{"timeout": "not-a-duration"},
			error: "invalid rclone_args",
		},
		{
			name:  "whitespace injection",
			args:  map[string]string{"timeout": "10s --config /tmp/other.conf"},
			error: "invalid rclone_args",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := setupTestPolicyAPI(t)
			agent := createPolicyTestAgent(t, setup.database)
			storage := createPolicyTestStorage(t, setup.database)

			w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
				"agent_id":    agent.ID,
				"storage_id":  storage.ID,
				"backup_dirs": []string{"/etc"},
				"schedule":    "0 3 * * *",
				"retention":   map[string]any{"keep_last": 3},
				"rclone_args": tt.args,
			})

			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			body := parseJSON(t, w)
			assert.Contains(t, body["error"], tt.error)
		})
	}
}

func TestCreatePolicyWithoutRclone(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	created := createPolicy(t, setup.router, agent.ID, storage.ID)
	responseArgs := requireMap(t, created["rclone_args"])
	assert.Empty(t, responseArgs)

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", created["id"]).Error)
	assert.Empty(t, stored.RcloneArgs)

	payload, err := policyPushPayload(setup.database, stored, storage)
	require.NoError(t, err)
	assert.NotNil(t, payload.Storage.RcloneArgs)
	assert.Empty(t, payload.Storage.RcloneArgs)
}

func TestCreatePolicyPublishesEvent(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	received := make(chan events.Event, 1)
	setup.bus.Subscribe(events.PolicyChanged, func(event events.Event) {
		received <- event
	})

	created := createPolicy(t, setup.router, agent.ID, storage.ID)

	select {
	case event := <-received:
		assert.Equal(t, events.PolicyChanged, event.Type)
		payload := requireMap(t, event.Payload)
		assert.Equal(t, agent.ID, payload["agent_id"])
		assert.Contains(t, []string{"create", "created"}, payload["action"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for policy changed event")
	}

	assert.NotEmpty(t, created["id"])
}

func TestCreatePolicyWithProvidedRepoPathAndPassword(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":        agent.ID,
		"storage_id":      storage.ID,
		"repo_path":       "custom/repo",
		"restic_password": "provided-secret",
		"backup_dirs":     []string{"/etc"},
		"schedule":        "0 3 * * *",
		"retention":       map[string]any{"keep_last": 3},
	})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, "custom/repo", body["repo_path"])
	assert.NotContains(t, body, "restic_password")

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", body["id"]).Error)
	assert.NotContains(t, stored.ResticPassword, "provided-secret")
}

func TestCreatePolicyWithHooks(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":    agent.ID,
		"storage_id":  storage.ID,
		"backup_dirs": []string{"/srv/app/data", "/srv/app/docker-compose.yml"},
		"schedule":    "0 3 * * *",
		"retention":   map[string]any{"keep_last": 3},
		"pre_backup_hook": map[string]any{
			"command":         "docker exec db pg_dump -U app app >/srv/app/backup/db.sql",
			"timeout_seconds": 180,
		},
		"post_backup_hook": map[string]any{
			"command":         "docker compose start app",
			"timeout_seconds": 30,
		},
	})

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	body := parseJSON(t, w)
	pre := requireMap(t, body["pre_backup_hook"])
	assert.Equal(t, "docker exec db pg_dump -U app app >/srv/app/backup/db.sql", pre["command"])
	assert.Equal(t, float64(180), pre["timeout_seconds"])
	post := requireMap(t, body["post_backup_hook"])
	assert.Equal(t, "docker compose start app", post["command"])
	assert.Equal(t, float64(30), post["timeout_seconds"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", body["id"]).Error)
	assert.JSONEq(t, `{"command":"docker exec db pg_dump -U app app >/srv/app/backup/db.sql","timeout_seconds":180}`, stored.PreBackupHook)
	assert.JSONEq(t, `{"command":"docker compose start app","timeout_seconds":30}`, stored.PostBackupHook)

	payload, err := policyPushPayload(setup.database, stored, storage)
	require.NoError(t, err)
	if assert.NotNil(t, payload.PreBackupHook) {
		assert.Equal(t, 180, payload.PreBackupHook.TimeoutSeconds)
	}
	if assert.NotNil(t, payload.PostBackupHook) {
		assert.Equal(t, "docker compose start app", payload.PostBackupHook.Command)
	}
}

func TestCreatePolicyRejectsInvalidHooks(t *testing.T) {
	tests := []struct {
		name string
		hook map[string]any
		err  string
	}{
		{
			name: "empty command",
			hook: map[string]any{"command": "   "},
			err:  "hook command",
		},
		{
			name: "timeout too large",
			hook: map[string]any{"command": "docker ps", "timeout_seconds": 3601},
			err:  "timeout_seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := setupTestPolicyAPI(t)
			agent := createPolicyTestAgent(t, setup.database)
			storage := createPolicyTestStorage(t, setup.database)

			w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
				"agent_id":        agent.ID,
				"storage_id":      storage.ID,
				"backup_dirs":     []string{"/etc"},
				"schedule":        "0 3 * * *",
				"retention":       map[string]any{"keep_last": 3},
				"pre_backup_hook": tt.hook,
			})

			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			body := parseJSON(t, w)
			assert.Contains(t, body["error"], tt.err)
		})
	}
}

func TestCreatePolicyEmptyPasswordStoresEncryptedEmpty(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	created := createPolicy(t, setup.router, agent.ID, storage.ID)
	assert.NotContains(t, created, "restic_password")

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", created["id"]).Error)
	decrypted, err := db.Decrypt(stored.ResticPassword, setup.database.MasterKey)
	require.NoError(t, err)
	assert.Empty(t, decrypted)
}

func TestCreatePolicyValidatesReferencedAgentAndStorage(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)

	w := postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":    "missing-agent",
		"storage_id":  storage.ID,
		"backup_dirs": []string{"/etc"},
		"schedule":    "0 3 * * *",
		"retention":   map[string]any{"keep_last": 3},
	})
	require.Equal(t, http.StatusBadRequest, w.Code)

	w = postAnyJSON(t, setup.router, "/api/policies", map[string]any{
		"agent_id":    agent.ID,
		"storage_id":  "missing-storage",
		"backup_dirs": []string{"/etc"},
		"schedule":    "0 3 * * *",
		"retention":   map[string]any{"keep_last": 3},
	})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListPoliciesOmitsResticPassword(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	createPolicy(t, setup.router, agent.ID, storage.ID)

	w := getJSON(t, setup.router, "/api/policies")

	require.Equal(t, http.StatusOK, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, true, body["ok"])
	list := requireList(t, body["data"])
	require.Len(t, list, 1)
	item := requireMap(t, list[0])
	assert.NotContains(t, item, "restic_password")
	assertJSONList(t, item["backup_dirs"], []string{"/etc"})
	retention := requireMap(t, item["retention"])
	assert.Equal(t, float64(3), retention["keep_last"])
}

func TestGetPolicyOmitsResticPassword(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)

	w := getJSON(t, setup.router, "/api/policies/"+created["id"].(string))

	require.Equal(t, http.StatusOK, w.Code)
	body := parseJSON(t, w)
	assert.Equal(t, created["id"], body["id"])
	assert.NotContains(t, body, "restic_password")
}

func TestGetPolicyNotFound(t *testing.T) {
	setup := setupTestPolicyAPI(t)

	w := getJSON(t, setup.router, "/api/policies/missing")

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdatePolicyMarksSyncedFalse(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)
	id := created["id"].(string)
	require.NoError(t, setup.database.DB.Model(&db.BackupPolicy{}).Where("id = ?", id).Update("synced", true).Error)

	w := putJSON(t, setup.router, "/api/policies/"+id, map[string]any{
		"schedule":         "0 4 * * *",
		"backup_dirs":      []string{"/var/lib"},
		"exclude_patterns": []string{"cache"},
		"retention":        map[string]any{"keep_last": 5, "keep_daily": 2},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, false, body["synced"])
	assert.Equal(t, "0 4 * * *", body["schedule"])
	assert.NotContains(t, body, "restic_password")
	assertJSONList(t, body["backup_dirs"], []string{"/var/lib"})
	assertJSONList(t, body["exclude_patterns"], []string{"cache"})
	retention := requireMap(t, body["retention"])
	assert.Equal(t, float64(5), retention["keep_last"])
	assert.Equal(t, float64(2), retention["keep_daily"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", id).Error)
	assert.False(t, stored.Synced)
	assert.Equal(t, `["/var/lib"]`, stored.BackupDirs)
	assert.Equal(t, `["cache"]`, stored.ExcludePatterns)
	assert.JSONEq(t, `{"keep_daily":2,"keep_last":5}`, stored.Retention)
}

func TestUpdatePolicyRclone(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)
	id := created["id"].(string)

	w := putJSON(t, setup.router, "/api/policies/"+id, map[string]any{
		"rclone_args": map[string]string{
			"retries":           "3",
			"low-level-retries": "10",
			"timeout":           "30s",
		},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	responseArgs := requireMap(t, body["rclone_args"])
	assert.Equal(t, "3", responseArgs["retries"])
	assert.Equal(t, "10", responseArgs["low-level-retries"])
	assert.Equal(t, "30s", responseArgs["timeout"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", id).Error)
	assert.JSONEq(t, `{"retries":"3","low-level-retries":"10","timeout":"30s"}`, stored.RcloneArgs)

	w = putJSON(t, setup.router, "/api/policies/"+id, map[string]any{
		"rclone_args": map[string]string{},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body = parseJSON(t, w)
	responseArgs = requireMap(t, body["rclone_args"])
	assert.Empty(t, responseArgs)
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", id).Error)
	assert.JSONEq(t, `{}`, stored.RcloneArgs)
}

func TestUpdatePolicyTimeoutHours(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)
	id := created["id"].(string)

	w := putJSON(t, setup.router, "/api/policies/"+id, map[string]any{
		"timeout_hours": 9,
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Equal(t, float64(9), body["timeout_hours"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", id).Error)
	assert.Equal(t, 9, stored.TimeoutHours)

	w = getJSON(t, setup.router, "/api/policies/"+id)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body = parseJSON(t, w)
	assert.Equal(t, float64(9), body["timeout_hours"])
}

func TestUpdatePolicyHooks(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)
	id := created["id"].(string)

	w := putJSON(t, setup.router, "/api/policies/"+id, map[string]any{
		"pre_backup_hook": map[string]any{
			"command":         "docker compose stop app",
			"timeout_seconds": 45,
		},
		"post_backup_hook": map[string]any{
			"command":         "docker compose start app",
			"timeout_seconds": 45,
		},
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := parseJSON(t, w)
	pre := requireMap(t, body["pre_backup_hook"])
	assert.Equal(t, "docker compose stop app", pre["command"])

	var stored db.BackupPolicy
	require.NoError(t, setup.database.DB.First(&stored, "id = ?", id).Error)
	assert.JSONEq(t, `{"command":"docker compose stop app","timeout_seconds":45}`, stored.PreBackupHook)
	assert.JSONEq(t, `{"command":"docker compose start app","timeout_seconds":45}`, stored.PostBackupHook)
}

func TestUpdatePolicyRejectsInvalidTimeoutHours(t *testing.T) {
	for _, timeoutHours := range []int{0, -1, 73} {
		t.Run(strconv.Itoa(timeoutHours), func(t *testing.T) {
			setup := setupTestPolicyAPI(t)
			agent := createPolicyTestAgent(t, setup.database)
			storage := createPolicyTestStorage(t, setup.database)
			created := createPolicy(t, setup.router, agent.ID, storage.ID)
			id := created["id"].(string)

			w := putJSON(t, setup.router, "/api/policies/"+id, map[string]any{"timeout_hours": timeoutHours})

			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			body := parseJSON(t, w)
			assert.Contains(t, body["error"], "timeout_hours")
		})
	}
}

func TestUpdatePolicyRejectsInvalidRcloneArgs(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)
	id := created["id"].(string)

	w := putJSON(t, setup.router, "/api/policies/"+id, map[string]any{
		"rclone_args": map[string]string{"retries": "-1"},
	})

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	body := parseJSON(t, w)
	assert.Contains(t, body["error"], "invalid rclone_args")
}

func TestUpdatePolicyPublishesEvent(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)

	received := make(chan events.Event, 1)
	setup.bus.Subscribe(events.PolicyChanged, func(event events.Event) {
		received <- event
	})

	w := putJSON(t, setup.router, "/api/policies/"+created["id"].(string), map[string]any{
		"schedule": "0 5 * * *",
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	select {
	case event := <-received:
		assert.Equal(t, events.PolicyChanged, event.Type)
		payload := requireMap(t, event.Payload)
		assert.Equal(t, agent.ID, payload["agent_id"])
		assert.Equal(t, "updated", payload["action"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for policy changed event")
	}
}

func TestUpdatePolicyValidatesNewStorage(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)

	w := putJSON(t, setup.router, "/api/policies/"+created["id"].(string), map[string]any{
		"storage_id": "missing-storage",
	})

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdatePolicyNotFound(t *testing.T) {
	setup := setupTestPolicyAPI(t)

	w := putJSON(t, setup.router, "/api/policies/missing", map[string]any{"schedule": "0 1 * * *"})

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeletePolicy(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)
	id := created["id"].(string)

	w := deleteJSON(t, setup.router, "/api/policies/"+id)

	require.Equal(t, http.StatusNoContent, w.Code)

	w = getJSON(t, setup.router, "/api/policies/"+id)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeletePolicyPublishesEvent(t *testing.T) {
	setup := setupTestPolicyAPI(t)
	agent := createPolicyTestAgent(t, setup.database)
	storage := createPolicyTestStorage(t, setup.database)
	created := createPolicy(t, setup.router, agent.ID, storage.ID)
	id := created["id"].(string)

	received := make(chan events.Event, 1)
	setup.bus.Subscribe(events.PolicyChanged, func(event events.Event) {
		received <- event
	})

	w := deleteJSON(t, setup.router, "/api/policies/"+id)

	require.Equal(t, http.StatusNoContent, w.Code)

	select {
	case event := <-received:
		assert.Equal(t, events.PolicyChanged, event.Type)
		payload := requireMap(t, event.Payload)
		assert.Equal(t, agent.ID, payload["agent_id"])
		assert.Contains(t, []string{"delete", "deleted"}, payload["action"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for policy changed event")
	}
}

func TestDeletePolicyNotFound(t *testing.T) {
	setup := setupTestPolicyAPI(t)

	w := deleteJSON(t, setup.router, "/api/policies/missing")

	require.Equal(t, http.StatusNotFound, w.Code)
}

func createPolicyTestAgent(t *testing.T, database *db.Database) db.Agent {
	t.Helper()

	return createNamedPolicyTestAgent(t, database, "Tokyo-1")
}

func createNamedPolicyTestAgent(t *testing.T, database *db.Database, name string) db.Agent {
	t.Helper()

	agent := db.Agent{
		Name:   name,
		Status: "online",
	}
	require.NoError(t, database.DB.Create(&agent).Error)
	return agent
}

func setPolicyAgentTags(t *testing.T, database *db.Database, agentID string, tags []string) {
	t.Helper()
	raw, err := json.Marshal(tags)
	require.NoError(t, err)
	require.NoError(t, database.DB.Model(&db.Agent{}).Where("id = ?", agentID).Update("tags", string(raw)).Error)
}

func markPolicyAgentCapabilities(t *testing.T, database *db.Database, agentID string, capabilities []string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"capabilities": capabilities})
	require.NoError(t, err)
	require.NoError(t, database.DB.Model(&db.Agent{}).Where("id = ?", agentID).Update("system_info", string(data)).Error)
}

func createPolicyTestStorage(t *testing.T, database *db.Database) db.StorageConfig {
	t.Helper()

	encrypted, err := db.Encrypt(`{"provider":"Cloudflare"}`, database.MasterKey)
	require.NoError(t, err)

	storage := db.StorageConfig{
		Name:         "Test Storage",
		RcloneType:   "s3",
		RcloneConfig: encrypted,
	}
	require.NoError(t, database.DB.Create(&storage).Error)
	return storage
}

func createPolicy(t *testing.T, router http.Handler, agentID string, storageID string) map[string]any {
	t.Helper()

	w := postAnyJSON(t, router, "/api/policies", map[string]any{
		"agent_id":    agentID,
		"storage_id":  storageID,
		"backup_dirs": []string{"/etc"},
		"schedule":    "0 3 * * *",
		"retention":   map[string]any{"keep_last": 3},
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	return parseJSON(t, w)
}

func assertJSONList(t *testing.T, value any, expected []string) {
	t.Helper()

	raw, ok := value.([]any)
	require.True(t, ok, "expected list, got %T", value)
	require.Len(t, raw, len(expected))
	for i, expectedValue := range expected {
		assert.Equal(t, expectedValue, raw[i])
	}
}
