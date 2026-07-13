package protocol

import (
	"encoding/json"
	"testing"
	"time"
	"unicode"

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
		Capabilities:  []string{CapabilitySnapshotBrowse, CapabilityRestoreIncludePaths},
		Uptime:        86400,
	}

	msg, err := NewMessage(TypeHeartbeat, hb)
	require.NoError(t, err)
	assert.Equal(t, TypeHeartbeat, msg.Type)
	assertHexMessageID(t, msg.ID)

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
	assert.Equal(t, []string{CapabilitySnapshotBrowse, CapabilityRestoreIncludePaths}, parsed.Capabilities)
}

func TestNewMessage_GeneratesUniqueHexIDs(t *testing.T) {
	const count = 16
	seen := make(map[string]bool, count)

	for i := 0; i < count; i++ {
		msg, err := NewMessage(TypeHeartbeat, HeartbeatPayload{})
		require.NoError(t, err)
		assertHexMessageID(t, msg.ID)
		assert.False(t, seen[msg.ID], "duplicate message ID: %s", msg.ID)
		seen[msg.ID] = true
	}
}

func TestNewMessageIDGeneratesUniqueHexIDs(t *testing.T) {
	first, err := NewMessageID()
	require.NoError(t, err)
	second, err := NewMessageID()
	require.NoError(t, err)

	assertHexMessageID(t, first)
	assertHexMessageID(t, second)
	assert.NotEqual(t, first, second)
}

func TestDefaultAgentCapabilitiesIncludesCurrentFeatureSet(t *testing.T) {
	capabilities := DefaultAgentCapabilities()

	assert.Contains(t, capabilities, CapabilitySnapshotBrowse)
	assert.Contains(t, capabilities, CapabilityRestoreIncludePaths)
	assert.Contains(t, capabilities, CapabilityRestorePreflight)
	assert.Contains(t, capabilities, CapabilityPolicyPlaintextRclonePass)
	assert.Contains(t, capabilities, CapabilityArchiveBackup)
	assert.Contains(t, capabilities, CapabilityBackupVerification)
	assert.Contains(t, capabilities, CapabilityLiveTaskLogs)
}

func TestTaskLogPayloadRoundTrip(t *testing.T) {
	ts := time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)
	payload := TaskLogPayload{
		AgentID:   "agent-1",
		MessageID: "msg-1",
		TaskType:  "backup",
		Sequence:  7,
		Timestamp: ts,
		Level:     "info",
		Phase:     "backup",
		Stream:    "stdout",
		Line:      "uploaded /srv/app.db",
		Truncated: true,
	}

	msg, parsed := roundTripPayload[TaskLogPayload](t, TypeTaskLog, payload)

	assert.Equal(t, TypeTaskLog, msg.Type)
	assert.Equal(t, payload.AgentID, parsed.AgentID)
	assert.Equal(t, payload.MessageID, parsed.MessageID)
	assert.Equal(t, payload.TaskType, parsed.TaskType)
	assert.Equal(t, payload.Sequence, parsed.Sequence)
	assert.Equal(t, payload.Timestamp, parsed.Timestamp)
	assert.Equal(t, payload.Level, parsed.Level)
	assert.Equal(t, payload.Phase, parsed.Phase)
	assert.Equal(t, payload.Stream, parsed.Stream)
	assert.Equal(t, payload.Line, parsed.Line)
	assert.True(t, parsed.Truncated)
}

func TestPolicyPushPayload(t *testing.T) {
	policy := PolicyPushPayload{
		PolicyName: "系统配置",
		AgentID:    "agent-001",
		Storage: StorageConfig{
			RcloneType:         "s3",
			RclonePassObscured: true,
			RcloneConfig: map[string]string{
				"provider":          "Cloudflare",
				"access_key_id":     "AKID",
				"secret_access_key": "SECRET",
				"endpoint":          "https://xxx.r2.cloudflarestorage.com",
				"bucket":            "backups",
			},
			RepoPath: "vaultfleet/agent-001",
		},
		ResticPassword: "secure-password",
		BackupDirs:     []string{"/etc", "/home", "/opt/myapp/data"},
		BackupSources: []BackupSource{
			{Type: BackupSourceTypePath, Path: "/etc"},
			{
				Type: BackupSourceTypeDockerContainer,
				DockerContainer: &DockerContainerBackupSource{
					ContainerID:         "container-1",
					Name:                "postgres",
					ComposeProject:      "app",
					ComposeService:      "db",
					IncludeBindMounts:   true,
					IncludeVolumes:      true,
					IncludeComposeFiles: true,
				},
			},
			{
				Type: BackupSourceTypeDatabase,
				Database: &DatabaseBackupSource{
					Engine:             DatabaseEnginePostgreSQL,
					ExecutionMode:      DatabaseExecutionDocker,
					Username:           "postgres",
					Password:           "db-secret",
					Database:           "app",
					Compress:           true,
					OutputName:         "app.sql.gz",
					DumpTimeoutSeconds: 600,
					DockerContainer: &DockerContainerBackupSource{
						Name: "postgres",
					},
				},
			},
		},
		ExcludePatterns: []string{"*.log", "*.tmp", "node_modules"},
		PreBackupHook:   &PolicyHook{Command: "docker exec db pg_dumpall >/backup/db.sql", TimeoutSeconds: 120},
		PostBackupHook:  &PolicyHook{Command: "docker compose start app", TimeoutSeconds: 30},
		Schedule:        "0 3 * * *",
		Retention: RetentionPolicy{
			KeepLast:    3,
			KeepDaily:   7,
			KeepWeekly:  4,
			KeepMonthly: 6,
		},
		Verification: &BackupVerificationSettings{
			Enabled:              true,
			Schedule:             "0 4 * * *",
			SampleCount:          5,
			SampleRestoreEnabled: true,
			TimeoutMinutes:       30,
		},
	}

	_, parsed := roundTripPayload[PolicyPushPayload](t, TypePolicyPush, policy)
	assert.Equal(t, "agent-001", parsed.AgentID)
	assert.Equal(t, "系统配置", parsed.PolicyName)
	assert.Equal(t, "s3", parsed.Storage.RcloneType)
	assert.Equal(t, "Cloudflare", parsed.Storage.RcloneConfig["provider"])
	assert.Equal(t, "AKID", parsed.Storage.RcloneConfig["access_key_id"])
	assert.Equal(t, "SECRET", parsed.Storage.RcloneConfig["secret_access_key"])
	assert.Equal(t, "https://xxx.r2.cloudflarestorage.com", parsed.Storage.RcloneConfig["endpoint"])
	assert.Equal(t, "backups", parsed.Storage.RcloneConfig["bucket"])
	assert.True(t, parsed.Storage.RclonePassObscured)
	assert.Equal(t, "vaultfleet/agent-001", parsed.Storage.RepoPath)
	assert.Equal(t, "secure-password", parsed.ResticPassword)
	assert.Equal(t, []string{"/etc", "/home", "/opt/myapp/data"}, parsed.BackupDirs)
	require.Len(t, parsed.BackupSources, 3)
	assert.Equal(t, BackupSourceTypePath, parsed.BackupSources[0].Type)
	assert.Equal(t, "/etc", parsed.BackupSources[0].Path)
	require.NotNil(t, parsed.BackupSources[1].DockerContainer)
	assert.Equal(t, "container-1", parsed.BackupSources[1].DockerContainer.ContainerID)
	assert.Equal(t, "app", parsed.BackupSources[1].DockerContainer.ComposeProject)
	require.NotNil(t, parsed.BackupSources[2].Database)
	assert.Equal(t, DatabaseEnginePostgreSQL, parsed.BackupSources[2].Database.Engine)
	assert.Equal(t, DatabaseExecutionDocker, parsed.BackupSources[2].Database.ExecutionMode)
	assert.Equal(t, "db-secret", parsed.BackupSources[2].Database.Password)
	assert.Equal(t, "app", parsed.BackupSources[2].Database.Database)
	assert.True(t, parsed.BackupSources[2].Database.Compress)
	require.NotNil(t, parsed.BackupSources[2].Database.DockerContainer)
	assert.Equal(t, "postgres", parsed.BackupSources[2].Database.DockerContainer.Name)
	assert.Equal(t, []string{"*.log", "*.tmp", "node_modules"}, parsed.ExcludePatterns)
	require.NotNil(t, parsed.Verification)
	assert.True(t, parsed.Verification.Enabled)
	assert.Equal(t, "0 4 * * *", parsed.Verification.Schedule)
	assert.Equal(t, 5, parsed.Verification.SampleCount)
	if assert.NotNil(t, parsed.PreBackupHook) {
		assert.Equal(t, "docker exec db pg_dumpall >/backup/db.sql", parsed.PreBackupHook.Command)
		assert.Equal(t, 120, parsed.PreBackupHook.TimeoutSeconds)
	}
	if assert.NotNil(t, parsed.PostBackupHook) {
		assert.Equal(t, "docker compose start app", parsed.PostBackupHook.Command)
		assert.Equal(t, 30, parsed.PostBackupHook.TimeoutSeconds)
	}
	assert.Equal(t, "0 3 * * *", parsed.Schedule)
	assert.Equal(t, 3, parsed.Retention.KeepLast)
	assert.Equal(t, 7, parsed.Retention.KeepDaily)
	assert.Equal(t, 4, parsed.Retention.KeepWeekly)
	assert.Equal(t, 6, parsed.Retention.KeepMonthly)
}

func TestDatabaseBackupMetadataRoundTrip(t *testing.T) {
	result := TaskResultPayload{
		AgentID:    "agent-1",
		TaskType:   "backup",
		Status:     "success",
		DurationMs: 25,
		Database: &DatabaseBackupMetadata{
			Dumps: []DatabaseDumpMetadata{
				{
					Engine:        DatabaseEngineMySQL,
					ExecutionMode: DatabaseExecutionHost,
					Database:      "app",
					OutputPath:    "/var/lib/vaultfleet/database-dumps/app.sql.gz",
					OutputName:    "app.sql.gz",
					Size:          1024,
					Compressed:    true,
				},
			},
		},
	}

	_, parsed := roundTripPayload[TaskResultPayload](t, TypeTaskResult, result)
	require.NotNil(t, parsed.Database)
	require.Len(t, parsed.Database.Dumps, 1)
	assert.Equal(t, DatabaseEngineMySQL, parsed.Database.Dumps[0].Engine)
	assert.Equal(t, DatabaseExecutionHost, parsed.Database.Dumps[0].ExecutionMode)
	assert.Equal(t, "app.sql.gz", parsed.Database.Dumps[0].OutputName)
	assert.Equal(t, int64(1024), parsed.Database.Dumps[0].Size)
	assert.True(t, parsed.Database.Dumps[0].Compressed)
}

func TestDatabaseDiscoveryRoundTrip(t *testing.T) {
	req := DatabaseDiscoveryReqPayload{
		Source: DatabaseBackupSource{
			Engine:        DatabaseEnginePostgreSQL,
			ExecutionMode: DatabaseExecutionDocker,
			Username:      "postgres",
			Password:      "secret",
			DockerContainer: &DockerContainerBackupSource{
				Name: "db",
			},
		},
	}
	_, parsedReq := roundTripPayload[DatabaseDiscoveryReqPayload](t, TypeDatabaseDiscoveryReq, req)
	assert.Equal(t, DatabaseEnginePostgreSQL, parsedReq.Source.Engine)
	assert.Equal(t, "db", parsedReq.Source.DockerContainer.Name)

	resp := DatabaseDiscoveryRespPayload{
		Available: true,
		Databases: []string{"app", "analytics"},
	}
	_, parsedResp := roundTripPayload[DatabaseDiscoveryRespPayload](t, TypeDatabaseDiscoveryResp, resp)
	assert.True(t, parsedResp.Available)
	assert.Equal(t, []string{"app", "analytics"}, parsedResp.Databases)
}

func TestBackupVerifyRoundTrip(t *testing.T) {
	req := BackupVerifyReqPayload{
		AgentID: "agent-1",
		Policy: &PolicyPushPayload{
			AgentID:        "agent-1",
			ResticPassword: "secret",
			BackupMode:     BackupModeSnapshot,
		},
		Verification: &BackupVerificationSettings{
			Enabled:              true,
			SampleCount:          3,
			SampleRestoreEnabled: true,
			TimeoutMinutes:       15,
		},
	}

	_, parsedReq := roundTripPayload[BackupVerifyReqPayload](t, TypeBackupVerifyReq, req)
	assert.Equal(t, "agent-1", parsedReq.AgentID)
	require.NotNil(t, parsedReq.Policy)
	assert.Equal(t, BackupModeSnapshot, parsedReq.Policy.BackupMode)
	require.NotNil(t, parsedReq.Verification)
	assert.Equal(t, 3, parsedReq.Verification.SampleCount)

	result := TaskResultPayload{
		AgentID:    "agent-1",
		TaskType:   "verify",
		Status:     "success",
		SnapshotID: "snap-1",
		Verification: &BackupVerificationResult{
			Status:     VerificationStatusPassed,
			SnapshotID: "snap-1",
			Checks: []BackupVerificationCheck{{
				Code:       "restic_check",
				Status:     VerificationCheckStatusPassed,
				Severity:   VerificationSeverityInfo,
				Message:    "repository check passed",
				DurationMs: 12,
			}},
		},
	}

	_, parsedResult := roundTripPayload[TaskResultPayload](t, TypeTaskResult, result)
	assert.Equal(t, "verify", parsedResult.TaskType)
	require.NotNil(t, parsedResult.Verification)
	assert.Equal(t, VerificationStatusPassed, parsedResult.Verification.Status)
	require.Len(t, parsedResult.Verification.Checks, 1)
	assert.Equal(t, "restic_check", parsedResult.Verification.Checks[0].Code)
}

func TestStorageConfigRcloneArgsOmitsWhenNil(t *testing.T) {
	storage := StorageConfig{
		RcloneType:   "s3",
		RcloneConfig: map[string]string{"provider": "Cloudflare"},
		RepoPath:     "vaultfleet/agent-001",
	}

	data, err := json.Marshal(storage)
	require.NoError(t, err)

	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	assert.NotContains(t, fields, "rclone_args")
}

func TestStorageConfigRcloneArgsIncludesWhenSet(t *testing.T) {
	storage := StorageConfig{
		RcloneType:   "s3",
		RcloneConfig: map[string]string{"provider": "Cloudflare"},
		RepoPath:     "vaultfleet/agent-001",
		RcloneArgs: map[string]string{
			"transfers": "8",
			"s3-acl":    "private",
		},
	}

	data, err := json.Marshal(storage)
	require.NoError(t, err)

	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	assert.Equal(t, map[string]any{
		"transfers": "8",
		"s3-acl":    "private",
	}, fields["rclone_args"])
}

func TestPolicyAckPayload(t *testing.T) {
	ack := PolicyAckPayload{
		AgentID: "agent-001",
		Success: false,
		Error:   "invalid schedule",
	}

	_, parsed := roundTripPayload[PolicyAckPayload](t, TypePolicyAck, ack)
	assert.Equal(t, "agent-001", parsed.AgentID)
	assert.False(t, parsed.Success)
	assert.Equal(t, "invalid schedule", parsed.Error)
}

func TestPolicyReconcilePayload(t *testing.T) {
	payload := PolicyReconcilePayload{
		AgentID:   "agent-001",
		PolicyIDs: []string{"policy-a", "policy-b"},
	}

	_, parsed := roundTripPayload[PolicyReconcilePayload](t, TypePolicyReconcile, payload)
	assert.Equal(t, payload, *parsed)
}

func TestBackupNowPayload(t *testing.T) {
	backupNow := BackupNowPayload{AgentID: "agent-002"}

	_, parsed := roundTripPayload[BackupNowPayload](t, TypeBackupNow, backupNow)
	assert.Equal(t, "agent-002", parsed.AgentID)
}

func TestBackupProgressPayloadMarshalsExpectedKeys(t *testing.T) {
	progress := BackupProgressPayload{
		AgentID:     "agent-002",
		Phase:       "uploading",
		PercentDone: 42.5,
		TotalFiles:  100,
		FilesDone:   42,
		TotalBytes:  104857600,
		BytesDone:   44564480,
		BytesPerSec: 524288,
		CurrentFile: "/var/lib/app/data.db",
	}

	msg, parsed := roundTripPayload[BackupProgressPayload](t, TypeBackupProgress, progress)
	assert.Equal(t, TypeBackupProgress, msg.Type)
	assert.Equal(t, progress, *parsed)

	data, err := json.Marshal(progress)
	require.NoError(t, err)

	var fields map[string]any
	require.NoError(t, json.Unmarshal(data, &fields))
	assert.Contains(t, fields, "agent_id")
	assert.Contains(t, fields, "phase")
	assert.Contains(t, fields, "percent_done")
	assert.Contains(t, fields, "total_files")
	assert.Contains(t, fields, "files_done")
	assert.Contains(t, fields, "total_bytes")
	assert.Contains(t, fields, "bytes_done")
	assert.Contains(t, fields, "bytes_per_sec")
	assert.Contains(t, fields, "current_file")
}

func TestCancelTaskPayloadMarshal(t *testing.T) {
	payload := CancelTaskPayload{
		AgentID:   "agent-1",
		MessageID: "msg-abc123",
	}

	msg, parsed := roundTripPayload[CancelTaskPayload](t, TypeCancelTask, payload)
	assert.Equal(t, TypeCancelTask, msg.Type)
	assert.Equal(t, payload, *parsed)
}

func TestTaskResultPayload(t *testing.T) {
	finishedAt := time.Date(2026, 5, 18, 9, 30, 45, 0, time.UTC)
	startedAt := finishedAt.Add(-45 * time.Second)
	result := TaskResultPayload{
		AgentID:    "agent-002",
		TaskType:   "backup",
		Status:     "success",
		SnapshotID: "abc123def456",
		DurationMs: 45000,
		RepoSize:   1073741824,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Snapshots: []SnapshotInfo{
			{ID: "abc123def456", Time: finishedAt, Paths: []string{"/etc"}, Size: 4096},
		},
	}

	_, parsed := roundTripPayload[TaskResultPayload](t, TypeTaskResult, result)
	assert.Equal(t, "agent-002", parsed.AgentID)
	assert.Equal(t, "backup", parsed.TaskType)
	assert.Equal(t, "success", parsed.Status)
	assert.Equal(t, "abc123def456", parsed.SnapshotID)
	assert.Equal(t, int64(45000), parsed.DurationMs)
	assert.Equal(t, int64(1073741824), parsed.RepoSize)
	assert.True(t, parsed.StartedAt.Equal(startedAt))
	assert.True(t, parsed.FinishedAt.Equal(finishedAt))
	require.Len(t, parsed.Snapshots, 1)
	assert.Equal(t, "abc123def456", parsed.Snapshots[0].ID)
	assert.True(t, parsed.Snapshots[0].Time.Equal(finishedAt))
	assert.Equal(t, []string{"/etc"}, parsed.Snapshots[0].Paths)
	assert.Equal(t, int64(4096), parsed.Snapshots[0].Size)
}

func TestTaskResultPayloadWithDockerMetadata(t *testing.T) {
	result := TaskResultPayload{
		AgentID:    "agent-002",
		TaskType:   "backup",
		Status:     "success",
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
		Docker: &DockerBackupMetadata{
			Sources: []DockerResolvedSource{
				{
					Selection: DockerContainerBackupSource{
						ContainerID:       "container-1",
						Name:              "postgres",
						IncludeBindMounts: true,
						IncludeVolumes:    true,
					},
					ContainerID:   "container-1",
					Name:          "postgres",
					Image:         "postgres:16",
					Compose:       DockerComposeInfo{Project: "app", Service: "db", WorkingDir: "/srv/app", ConfigFiles: []string{"compose.yml"}},
					Mounts:        []DockerMount{{Type: "volume", Name: "db-data", Source: "/var/lib/docker/volumes/db/_data", Destination: "/var/lib/postgresql/data", RW: true}},
					Env:           []string{"POSTGRES_DB=app"},
					Cmd:           []string{"postgres"},
					Ports:         []DockerPortBinding{{ContainerPort: "5432", HostPort: "15432"}},
					State:         "running",
					ResolvedPaths: []string{"/var/lib/docker/volumes/db/_data"},
				},
			},
			Warnings: []string{"compose file not found"},
		},
	}

	_, parsed := roundTripPayload[TaskResultPayload](t, TypeTaskResult, result)
	require.NotNil(t, parsed.Docker)
	require.Len(t, parsed.Docker.Sources, 1)
	assert.Equal(t, "container-1", parsed.Docker.Sources[0].ContainerID)
	assert.Equal(t, []string{"/var/lib/docker/volumes/db/_data"}, parsed.Docker.Sources[0].ResolvedPaths)
	require.Len(t, parsed.Docker.Sources[0].Mounts, 1)
	assert.Equal(t, "/var/lib/postgresql/data", parsed.Docker.Sources[0].Mounts[0].Destination)
	assert.Equal(t, "app", parsed.Docker.Sources[0].Compose.Project)
	assert.Equal(t, []string{"POSTGRES_DB=app"}, parsed.Docker.Sources[0].Env)
	assert.Equal(t, "15432", parsed.Docker.Sources[0].Ports[0].HostPort)
	assert.Equal(t, []string{"compose file not found"}, parsed.Docker.Warnings)
}

func TestTaskResultPayloadWithBackupContentManifest(t *testing.T) {
	generatedAt := time.Date(2026, 7, 8, 2, 30, 0, 0, time.UTC)
	result := TaskResultPayload{
		AgentID:    "agent-002",
		TaskType:   "backup",
		Status:     "success",
		StartedAt:  generatedAt,
		FinishedAt: generatedAt.Add(time.Minute),
		Manifest: &BackupContentManifest{
			Version:       BackupContentManifestVersion,
			GeneratedAt:   generatedAt,
			BackupMode:    BackupModeArchive,
			ArchiveFormat: ArchiveFormatZip,
			Agent:         ManifestAgent{ID: "agent-002", Version: "1.0.0", Hostname: "node-1"},
			Policy:        ManifestPolicy{BackupMode: BackupModeArchive, ArchiveFormat: ArchiveFormatZip, StorageType: "s3", Repository: "tenant/node-1"},
			Sources: ManifestSources{
				Paths:     []ManifestPathSource{{Path: "/srv/site", Kind: "path"}},
				Docker:    []ManifestDockerSource{{Name: "web", Image: "nginx", ComposeProject: "site", ComposeService: "web"}},
				Databases: []ManifestDatabaseDump{{Engine: DatabaseEngineMySQL, Database: "app", OutputName: "mysql-app.sql.gz", Compressed: true}},
			},
			ExcludePatterns: []string{"*.log"},
			Artifact:        &ManifestArtifact{Name: "backup.zip", Path: "artifacts/backup.zip", Format: ArchiveFormatZip, ContentType: "application/zip", Size: 2048},
			Warnings:        []ManifestWarning{{Code: "docker_warning", Message: "compose file missing", Source: "docker"}},
		},
	}

	_, parsed := roundTripPayload[TaskResultPayload](t, TypeTaskResult, result)
	require.NotNil(t, parsed.Manifest)
	assert.Equal(t, BackupContentManifestVersion, parsed.Manifest.Version)
	assert.True(t, parsed.Manifest.GeneratedAt.Equal(generatedAt))
	assert.Equal(t, "node-1", parsed.Manifest.Agent.Hostname)
	assert.Equal(t, "tenant/node-1", parsed.Manifest.Policy.Repository)
	require.Len(t, parsed.Manifest.Sources.Paths, 1)
	assert.Equal(t, "/srv/site", parsed.Manifest.Sources.Paths[0].Path)
	require.Len(t, parsed.Manifest.Sources.Docker, 1)
	assert.Equal(t, "web", parsed.Manifest.Sources.Docker[0].Name)
	require.Len(t, parsed.Manifest.Sources.Databases, 1)
	assert.Equal(t, "mysql-app.sql.gz", parsed.Manifest.Sources.Databases[0].OutputName)
	require.NotNil(t, parsed.Manifest.Artifact)
	assert.Equal(t, int64(2048), parsed.Manifest.Artifact.Size)
}

func TestDirBrowseRoundTrip(t *testing.T) {
	reqPayload := DirBrowseReqPayload{Path: "/home", Depth: 2}

	_, parsedReq := roundTripPayload[DirBrowseReqPayload](t, TypeDirBrowseReq, reqPayload)
	assert.Equal(t, "/home", parsedReq.Path)
	assert.Equal(t, 2, parsedReq.Depth)

	resp := DirBrowseRespPayload{
		Path: "/home",
		Entries: []DirEntry{
			{Path: "/home/user1", Type: "dir", Size: 1048576},
			{Path: "/home/user2", Type: "dir", Size: 2097152},
		},
	}

	_, parsedResp := roundTripPayload[DirBrowseRespPayload](t, TypeDirBrowseResp, resp)
	require.Len(t, parsedResp.Entries, 2)
	assert.Equal(t, "/home", parsedResp.Path)
	assert.Equal(t, "/home/user1", parsedResp.Entries[0].Path)
	assert.Equal(t, "dir", parsedResp.Entries[0].Type)
	assert.Equal(t, int64(1048576), parsedResp.Entries[0].Size)
	assert.Equal(t, "/home/user2", parsedResp.Entries[1].Path)
	assert.Equal(t, "dir", parsedResp.Entries[1].Type)
	assert.Equal(t, int64(2097152), parsedResp.Entries[1].Size)
}

func TestDockerDiscoveryRoundTrip(t *testing.T) {
	req, parsedReq := roundTripPayload[DockerDiscoveryReqPayload](t, TypeDockerDiscoveryReq, DockerDiscoveryReqPayload{})
	assert.Equal(t, TypeDockerDiscoveryReq, req.Type)
	assert.Equal(t, DockerDiscoveryReqPayload{}, *parsedReq)

	respPayload := DockerDiscoveryRespPayload{
		Available: true,
		Containers: []DockerContainer{
			{
				ID:    "container-1",
				Names: []string{"postgres"},
				Image: "postgres:16",
				State: "running",
				Labels: map[string]string{
					"com.docker.compose.project": "app",
				},
				Compose: DockerComposeInfo{
					Project:     "app",
					Service:     "db",
					WorkingDir:  "/srv/app",
					ConfigFiles: []string{"compose.yml"},
				},
				Mounts: []DockerMount{
					{Type: "volume", Name: "db-data", Source: "/var/lib/docker/volumes/db-data/_data", Destination: "/var/lib/postgresql/data", RW: true},
					{Type: "bind", Source: "/srv/app/config", Destination: "/config", RW: false},
				},
				Selectable: true,
				Warnings:   []string{"container is stopped"},
			},
		},
	}

	_, parsedResp := roundTripPayload[DockerDiscoveryRespPayload](t, TypeDockerDiscoveryResp, respPayload)
	assert.True(t, parsedResp.Available)
	require.Len(t, parsedResp.Containers, 1)
	assert.Equal(t, "container-1", parsedResp.Containers[0].ID)
	assert.Equal(t, "app", parsedResp.Containers[0].Compose.Project)
	assert.Equal(t, "db-data", parsedResp.Containers[0].Mounts[0].Name)
}

func TestSnapshotListRoundTrip(t *testing.T) {
	firstSnapshotAt := time.Date(2026, 5, 18, 8, 0, 0, 0, time.UTC)
	secondSnapshotAt := firstSnapshotAt.Add(-24 * time.Hour)
	resp := SnapshotListRespPayload{
		AgentID: "agent-003",
		Snapshots: []SnapshotInfo{
			{ID: "snap1", Time: firstSnapshotAt, Paths: []string{"/etc", "/home"}, Size: 500000},
			{ID: "snap2", Time: secondSnapshotAt, Paths: []string{"/etc"}, Size: 300000},
		},
	}

	_, parsed := roundTripPayload[SnapshotListRespPayload](t, TypeSnapshotListResp, resp)
	require.Len(t, parsed.Snapshots, 2)
	assert.Equal(t, "agent-003", parsed.AgentID)
	assert.Equal(t, "snap1", parsed.Snapshots[0].ID)
	assert.True(t, parsed.Snapshots[0].Time.Equal(firstSnapshotAt))
	assert.Equal(t, []string{"/etc", "/home"}, parsed.Snapshots[0].Paths)
	assert.Equal(t, int64(500000), parsed.Snapshots[0].Size)
	assert.Equal(t, "snap2", parsed.Snapshots[1].ID)
	assert.True(t, parsed.Snapshots[1].Time.Equal(secondSnapshotAt))
	assert.Equal(t, []string{"/etc"}, parsed.Snapshots[1].Paths)
	assert.Equal(t, int64(300000), parsed.Snapshots[1].Size)
}

func TestSnapshotListReqPayload(t *testing.T) {
	req := SnapshotListReqPayload{AgentID: "agent-003"}

	_, parsed := roundTripPayload[SnapshotListReqPayload](t, TypeSnapshotListReq, req)
	assert.Equal(t, "agent-003", parsed.AgentID)
}

func TestCollectLogsPayloadRoundTrip(t *testing.T) {
	req, parsedReq := roundTripPayload[CollectLogsReqPayload](t, TypeCollectLogsReq, CollectLogsReqPayload{MaxBytes: 1024})
	assert.Equal(t, TypeCollectLogsReq, req.Type)
	assert.Equal(t, 1024, parsedReq.MaxBytes)

	respPayload := CollectLogsRespPayload{
		Logs:  "line1\nline2\n",
		Error: "partial collection failed",
	}
	resp, parsedResp := roundTripPayload[CollectLogsRespPayload](t, TypeCollectLogsResp, respPayload)
	assert.Equal(t, TypeCollectLogsResp, resp.Type)
	assert.Equal(t, respPayload, *parsedResp)
}

func TestRestorePayloads(t *testing.T) {
	reqPayload := RestoreReqPayload{
		SnapshotID:   "abc123",
		Target:       "/restore/20260518",
		IncludePaths: []string{"/etc/hosts", "/var/log/app.log"},
		RestoreMode:  RestoreModeDockerContainer,
		Docker: &DockerRestoreRequest{
			Sources: []DockerResolvedSource{{ContainerID: "container-1", ResolvedPaths: []string{"/srv/app"}}},
		},
	}

	_, parsedReq := roundTripPayload[RestoreReqPayload](t, TypeRestoreReq, reqPayload)
	assert.Equal(t, "abc123", parsedReq.SnapshotID)
	assert.Equal(t, "/restore/20260518", parsedReq.Target)
	assert.Equal(t, []string{"/etc/hosts", "/var/log/app.log"}, parsedReq.IncludePaths)
	assert.Equal(t, RestoreModeDockerContainer, parsedReq.RestoreMode)
	require.NotNil(t, parsedReq.Docker)
	assert.Equal(t, "container-1", parsedReq.Docker.Sources[0].ContainerID)

	_, parsedSelectiveReq := roundTripPayload[RestoreReqPayload](t, TypeSelectiveRestoreReq, reqPayload)
	assert.Equal(t, reqPayload, *parsedSelectiveReq)

	progress := RestoreProgressPayload{
		AgentID:       "agent-001",
		SnapshotID:    "abc123",
		FilesRestored: 1500,
		BytesRestored: 104857600,
		Percent:       75.5,
	}

	_, parsedProgress := roundTripPayload[RestoreProgressPayload](t, TypeRestoreProgress, progress)
	assert.Equal(t, "agent-001", parsedProgress.AgentID)
	assert.Equal(t, "abc123", parsedProgress.SnapshotID)
	assert.Equal(t, int64(1500), parsedProgress.FilesRestored)
	assert.Equal(t, int64(104857600), parsedProgress.BytesRestored)
	assert.InDelta(t, 75.5, parsedProgress.Percent, 0.01)
}

func TestRestorePreflightPayloads(t *testing.T) {
	reqPayload := RestorePreflightReqPayload{
		AgentID:      "agent-001",
		SnapshotID:   "abc123",
		Target:       "/restore/target",
		IncludePaths: []string{"/etc/hosts"},
		RestoreMode:  RestoreModeDockerContainer,
		Docker: &DockerRestoreRequest{
			Sources: []DockerResolvedSource{{ContainerID: "container-1", ResolvedPaths: []string{"/srv/app"}}},
		},
	}

	_, parsedReq := roundTripPayload[RestorePreflightReqPayload](t, TypeRestorePreflightReq, reqPayload)
	assert.Equal(t, reqPayload, *parsedReq)

	respPayload := RestorePreflightRespPayload{
		AgentID:    "agent-001",
		SnapshotID: "abc123",
		Status:     RestorePreflightStatusFailed,
		Checks: []RestorePreflightCheck{
			{Code: "target_path_writable", Severity: RestorePreflightSeverityError, Message: "target path is not writable", Detail: "permission denied"},
			{Code: "docker_available", Severity: RestorePreflightSeverityInfo, Message: "Docker is available"},
		},
	}

	_, parsedResp := roundTripPayload[RestorePreflightRespPayload](t, TypeRestorePreflightResp, respPayload)
	assert.Equal(t, respPayload, *parsedResp)
}

func TestDirSizeRoundTrip(t *testing.T) {
	reqPayload := DirSizeReqPayload{Path: "/home/data"}

	_, parsedReq := roundTripPayload[DirSizeReqPayload](t, TypeDirSizeReq, reqPayload)
	assert.Equal(t, "/home/data", parsedReq.Path)

	respPayload := DirSizeRespPayload{
		Path: "/home/data",
		Size: 1073741824,
	}

	_, parsedResp := roundTripPayload[DirSizeRespPayload](t, TypeDirSizeResp, respPayload)
	assert.Equal(t, "/home/data", parsedResp.Path)
	assert.Equal(t, int64(1073741824), parsedResp.Size)
	assert.Empty(t, parsedResp.Error)
}

func TestVersionInfoRoundTrip(t *testing.T) {
	payload := VersionInfoPayload{
		Version:    "v0.5.0",
		GitHubRepo: "shuguangnet/VaultFleet",
	}

	_, parsed := roundTripPayload[VersionInfoPayload](t, TypeVersionInfo, payload)
	assert.Equal(t, "v0.5.0", parsed.Version)
	assert.Equal(t, "shuguangnet/VaultFleet", parsed.GitHubRepo)
}

func TestUpdateAgentRoundTrip(t *testing.T) {
	payload := UpdateAgentPayload{
		Version:    "v0.5.0",
		GitHubRepo: "shuguangnet/VaultFleet",
	}

	_, parsed := roundTripPayload[UpdateAgentPayload](t, TypeUpdateAgent, payload)
	assert.Equal(t, "v0.5.0", parsed.Version)
	assert.Equal(t, "shuguangnet/VaultFleet", parsed.GitHubRepo)
}

func TestUpdateAgentRespRoundTrip(t *testing.T) {
	payload := UpdateAgentRespPayload{
		Accepted:   true,
		Version:    "v0.5.0",
		GitHubRepo: "shuguangnet/VaultFleet",
	}

	_, parsed := roundTripPayload[UpdateAgentRespPayload](t, TypeUpdateAgentResp, payload)
	assert.True(t, parsed.Accepted)
	assert.Equal(t, "v0.5.0", parsed.Version)
	assert.Equal(t, "shuguangnet/VaultFleet", parsed.GitHubRepo)
}

func TestAllMessageTypeConstants(t *testing.T) {
	types := []string{
		TypeHeartbeat,
		TypeDirBrowseReq,
		TypeDirBrowseResp,
		TypeDockerDiscoveryReq,
		TypeDockerDiscoveryResp,
		TypeDatabaseDiscoveryReq,
		TypeDatabaseDiscoveryResp,
		TypePolicyPush,
		TypePolicyAck,
		TypeBackupNow,
		TypeTaskResult,
		TypeRestoreReq,
		TypeSelectiveRestoreReq,
		TypeRestoreProgress,
		TypeRestorePreflightReq,
		TypeRestorePreflightResp,
		TypeSnapshotListReq,
		TypeSnapshotListResp,
		TypeSnapshotBrowseReq,
		TypeSnapshotBrowseResp,
		TypeCollectLogsReq,
		TypeCollectLogsResp,
		TypeDirSizeReq,
		TypeDirSizeResp,
		TypeVersionInfo,
		TypeUpdateAgent,
		TypeUpdateAgentResp,
		TypeBackupProgress,
		TypeCancelTask,
	}
	expected := []string{
		"heartbeat",
		"dir_browse_req",
		"dir_browse_resp",
		"docker_discovery_req",
		"docker_discovery_resp",
		"database_discovery_req",
		"database_discovery_resp",
		"policy_push",
		"policy_ack",
		"backup_now",
		"task_result",
		"restore_req",
		"selective_restore_req",
		"restore_progress",
		"restore_preflight_req",
		"restore_preflight_resp",
		"snapshot_list_req",
		"snapshot_list_resp",
		"snapshot_browse_req",
		"snapshot_browse_resp",
		"collect_logs_req",
		"collect_logs_resp",
		"dir_size_req",
		"dir_size_resp",
		"version_info",
		"update_agent",
		"update_agent_resp",
		"backup_progress",
		"cancel_task",
	}

	assert.Equal(t, expected, types)
	assert.Len(t, types, 29)
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
		Payload: json.RawMessage(`{invalid json`),
	}
	_, err := ParsePayload[HeartbeatPayload](msg)
	assert.Error(t, err)
}

func roundTripPayload[T any](t *testing.T, msgType string, payload interface{}) (*Message, *T) {
	t.Helper()

	msg, err := NewMessage(msgType, payload)
	require.NoError(t, err)

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	require.Equal(t, msgType, decoded.Type)
	require.Equal(t, msg.ID, decoded.ID)
	assertHexMessageID(t, decoded.ID)

	parsed, err := ParsePayload[T](&decoded)
	require.NoError(t, err)

	return &decoded, parsed
}

func assertHexMessageID(t *testing.T, id string) {
	t.Helper()

	require.Len(t, id, 32)
	for _, r := range id {
		assert.Truef(t, unicode.Is(unicode.ASCII_Hex_Digit, r), "message ID contains non-hex character: %q", r)
	}
}
