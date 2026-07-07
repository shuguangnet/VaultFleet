package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Message type constants identify WebSocket payload kinds exchanged by master and agents.
const (
	TypeHeartbeat            = "heartbeat"
	TypeDirBrowseReq         = "dir_browse_req"
	TypeDirBrowseResp        = "dir_browse_resp"
	TypeDockerDiscoveryReq   = "docker_discovery_req"
	TypeDockerDiscoveryResp  = "docker_discovery_resp"
	TypePolicyPush           = "policy_push"
	TypePolicyAck            = "policy_ack"
	TypeBackupNow            = "backup_now"
	TypeBackupVerifyReq      = "backup_verify_req"
	TypeTaskResult           = "task_result"
	TypeRestoreReq           = "restore_req"
	TypeSelectiveRestoreReq  = "selective_restore_req"
	TypeRestoreProgress      = "restore_progress"
	TypeRestorePreflightReq  = "restore_preflight_req"
	TypeRestorePreflightResp = "restore_preflight_resp"
	TypeSnapshotListReq      = "snapshot_list_req"
	TypeSnapshotListResp     = "snapshot_list_resp"
	TypeSnapshotBrowseReq    = "snapshot_browse_req"
	TypeSnapshotBrowseResp   = "snapshot_browse_resp"
	TypeCollectLogsReq       = "collect_logs_req"
	TypeCollectLogsResp      = "collect_logs_resp"
	TypeDirSizeReq           = "dir_size_req"
	TypeDirSizeResp          = "dir_size_resp"
	TypeVersionInfo          = "version_info"
	TypeUpdateAgent          = "update_agent"
	TypeUpdateAgentResp      = "update_agent_resp"
	TypeBackupProgress       = "backup_progress"
	TypeTaskLog              = "task_log"
	TypeCancelTask           = "cancel_task"
)

const (
	CapabilitySnapshotBrowse            = "snapshot_browse"
	CapabilityRestoreIncludePaths       = "restore_include_paths"
	CapabilityRestorePreflight          = "restore_preflight"
	CapabilityPolicyPlaintextRclonePass = "policy_plaintext_rclone_pass"
	CapabilityArchiveBackup             = "archive_backup"
	CapabilityDockerWorkloadBackups     = "docker_workload_backups"
	CapabilityDockerContainerRestore    = "docker_container_restore"
	CapabilityTypedBackupSources        = "typed_backup_sources"
	CapabilityBackupVerification        = "backup_verification"
	CapabilityLiveTaskLogs              = "live_task_logs"
)

// DefaultAgentCapabilities returns the feature set reported by current agents.
func DefaultAgentCapabilities() []string {
	return []string{
		CapabilitySnapshotBrowse,
		CapabilityRestoreIncludePaths,
		CapabilityRestorePreflight,
		CapabilityPolicyPlaintextRclonePass,
		CapabilityArchiveBackup,
		CapabilityTypedBackupSources,
		CapabilityBackupVerification,
		CapabilityLiveTaskLogs,
	}
}

const (
	BackupModeSnapshot = "snapshot"
	BackupModeArchive  = "archive"
	ArchiveFormatZip   = "zip"
	ArchiveFormatTarGz = "tar.gz"
)

const (
	BackupSourceTypePath            = "path"
	BackupSourceTypeDockerContainer = "docker_container"
)

const (
	RestoreModeFiles           = "files"
	RestoreModeDockerContainer = "docker_container"
)

// Message is the shared WebSocket envelope used by master and agents.
type Message struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload"`
}

// NewMessage wraps a typed payload in a Message and assigns a random 16-byte hex ID.
func NewMessage(msgType string, payload interface{}) (*Message, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("generate message id: %w", err)
	}

	return &Message{
		Type:    msgType,
		ID:      hex.EncodeToString(idBytes),
		Payload: json.RawMessage(data),
	}, nil
}

// ParsePayload unmarshals a message payload into the requested payload type.
func ParsePayload[T any](msg *Message) (*T, error) {
	var payload T
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// HeartbeatPayload reports agent health and installed backup tool versions.
type HeartbeatPayload struct {
	CPUPercent    float64  `json:"cpu_percent"`
	MemoryPercent float64  `json:"memory_percent"`
	DiskPercent   float64  `json:"disk_percent"`
	ResticVersion string   `json:"restic_version"`
	RcloneVersion string   `json:"rclone_version"`
	AgentVersion  string   `json:"agent_version,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`
	Uptime        int64    `json:"uptime"`
}

// DirBrowseRespPayload returns directory entries for a browse request.
type DirBrowseRespPayload struct {
	Path    string     `json:"path"`
	Entries []DirEntry `json:"entries"`
	Error   string     `json:"error,omitempty"`
}

// DirEntry describes one file-system entry returned by directory browsing.
type DirEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// PolicyAckPayload acknowledges whether an agent accepted a pushed policy.
type PolicyAckPayload struct {
	AgentID string `json:"agent_id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// TaskResultPayload reports completion metadata for backup, restore, or maintenance work.
type TaskResultPayload struct {
	AgentID             string                    `json:"agent_id"`
	TaskType            string                    `json:"task_type"`
	Status              string                    `json:"status"`
	SnapshotID          string                    `json:"snapshot_id,omitempty"`
	BackupMode          string                    `json:"backup_mode,omitempty"`
	ArchiveFormat       string                    `json:"archive_format,omitempty"`
	ArtifactPath        string                    `json:"artifact_path,omitempty"`
	ArtifactName        string                    `json:"artifact_name,omitempty"`
	ArtifactSize        int64                     `json:"artifact_size,omitempty"`
	ArtifactContentType string                    `json:"artifact_content_type,omitempty"`
	DurationMs          int64                     `json:"duration_ms"`
	RepoSize            int64                     `json:"repo_size"`
	ErrorLog            string                    `json:"error_log,omitempty"`
	StartedAt           time.Time                 `json:"started_at"`
	FinishedAt          time.Time                 `json:"finished_at"`
	Snapshots           []SnapshotInfo            `json:"snapshots,omitempty"`
	Docker              *DockerBackupMetadata     `json:"docker,omitempty"`
	Verification        *BackupVerificationResult `json:"verification,omitempty"`
}

const (
	VerificationStatusPassed = "passed"
	VerificationStatusFailed = "failed"

	VerificationCheckStatusPassed  = "passed"
	VerificationCheckStatusFailed  = "failed"
	VerificationCheckStatusSkipped = "skipped"

	VerificationSeverityInfo    = "info"
	VerificationSeverityWarning = "warning"
	VerificationSeverityError   = "error"
)

// BackupVerificationSettings controls scheduled and manual recoverability checks.
type BackupVerificationSettings struct {
	Enabled              bool   `json:"enabled"`
	Schedule             string `json:"schedule,omitempty"`
	SampleCount          int    `json:"sample_count,omitempty"`
	SampleRestoreEnabled bool   `json:"sample_restore_enabled,omitempty"`
	TimeoutMinutes       int    `json:"timeout_minutes,omitempty"`
}

// BackupVerifyReqPayload requests a recoverability verification for a policy repository.
type BackupVerifyReqPayload struct {
	AgentID      string                      `json:"agent_id"`
	Policy       *PolicyPushPayload          `json:"policy,omitempty"`
	Verification *BackupVerificationSettings `json:"verification,omitempty"`
}

// BackupVerificationResult reports structured recoverability check results.
type BackupVerificationResult struct {
	Status     string                    `json:"status"`
	SnapshotID string                    `json:"snapshot_id,omitempty"`
	Checks     []BackupVerificationCheck `json:"checks"`
	Error      string                    `json:"error,omitempty"`
}

// BackupVerificationCheck is one recoverability verification finding.
type BackupVerificationCheck struct {
	Code       string `json:"code"`
	Status     string `json:"status"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	Detail     string `json:"detail,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// PolicyHook defines an optional host-side command executed before or after backup.
type PolicyHook struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// BackupProgressPayload reports incremental backup progress from an agent.
type BackupProgressPayload struct {
	AgentID     string  `json:"agent_id"`
	Phase       string  `json:"phase"`
	PercentDone float64 `json:"percent_done"`
	TotalFiles  int64   `json:"total_files"`
	FilesDone   int64   `json:"files_done"`
	TotalBytes  int64   `json:"total_bytes"`
	BytesDone   int64   `json:"bytes_done"`
	BytesPerSec int64   `json:"bytes_per_sec"`
	CurrentFile string  `json:"current_file"`
}

// TaskLogPayload carries one redacted log line for a task identified by message ID.
type TaskLogPayload struct {
	AgentID   string    `json:"agent_id"`
	MessageID string    `json:"message_id"`
	TaskType  string    `json:"task_type"`
	Sequence  int64     `json:"sequence"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Phase     string    `json:"phase"`
	Stream    string    `json:"stream"`
	Line      string    `json:"line"`
	Truncated bool      `json:"truncated,omitempty"`
}

// CancelTaskPayload requests cancellation of a running agent task by message ID.
type CancelTaskPayload struct {
	AgentID   string `json:"agent_id"`
	MessageID string `json:"message_id"`
}

// RestoreProgressPayload reports incremental restore progress from an agent.
type RestoreProgressPayload struct {
	AgentID       string  `json:"agent_id"`
	SnapshotID    string  `json:"snapshot_id"`
	FilesRestored int64   `json:"files_restored"`
	BytesRestored int64   `json:"bytes_restored"`
	Percent       float64 `json:"percent"`
}

// SnapshotListRespPayload returns snapshots known to an agent repository.
type SnapshotListRespPayload struct {
	AgentID   string         `json:"agent_id"`
	Snapshots []SnapshotInfo `json:"snapshots"`
	Error     string         `json:"error,omitempty"`
}

// SnapshotInfo describes one restic snapshot.
type SnapshotInfo struct {
	ID    string    `json:"id"`
	Time  time.Time `json:"time"`
	Paths []string  `json:"paths"`
	Size  int64     `json:"size"`
}

// DirBrowseReqPayload requests a bounded directory listing from an agent.
type DirBrowseReqPayload struct {
	Path  string `json:"path"`
	Depth int    `json:"depth"`
}

type DockerDiscoveryReqPayload struct{}

type DockerDiscoveryRespPayload struct {
	Available  bool              `json:"available"`
	Error      string            `json:"error,omitempty"`
	Containers []DockerContainer `json:"containers"`
}

type DockerContainer struct {
	ID         string            `json:"id"`
	Names      []string          `json:"names"`
	Image      string            `json:"image"`
	State      string            `json:"state"`
	Labels     map[string]string `json:"labels,omitempty"`
	Compose    DockerComposeInfo `json:"compose,omitempty"`
	Mounts     []DockerMount     `json:"mounts"`
	Selectable bool              `json:"selectable"`
	Warnings   []string          `json:"warnings,omitempty"`
}

type DockerComposeInfo struct {
	Project     string   `json:"project,omitempty"`
	Service     string   `json:"service,omitempty"`
	WorkingDir  string   `json:"working_dir,omitempty"`
	ConfigFiles []string `json:"config_files,omitempty"`
}

type DockerMount struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination"`
	RW          bool   `json:"rw"`
}

type BackupSource struct {
	Type            string                       `json:"type"`
	Path            string                       `json:"path,omitempty"`
	DockerContainer *DockerContainerBackupSource `json:"docker_container,omitempty"`
}

type DockerContainerBackupSource struct {
	ContainerID         string            `json:"container_id,omitempty"`
	Name                string            `json:"name,omitempty"`
	Image               string            `json:"image,omitempty"`
	Labels              map[string]string `json:"labels,omitempty"`
	ComposeProject      string            `json:"compose_project,omitempty"`
	ComposeService      string            `json:"compose_service,omitempty"`
	ComposeWorkingDir   string            `json:"compose_working_dir,omitempty"`
	ComposeConfigFiles  []string          `json:"compose_config_files,omitempty"`
	IncludeBindMounts   bool              `json:"include_bind_mounts"`
	IncludeVolumes      bool              `json:"include_volumes"`
	IncludeComposeFiles bool              `json:"include_compose_files"`
}

type DockerBackupMetadata struct {
	Sources  []DockerResolvedSource `json:"sources,omitempty"`
	Warnings []string               `json:"warnings,omitempty"`
}

type DockerResolvedSource struct {
	Selection     DockerContainerBackupSource `json:"selection"`
	ContainerID   string                      `json:"container_id,omitempty"`
	Name          string                      `json:"name,omitempty"`
	Image         string                      `json:"image,omitempty"`
	Labels        map[string]string           `json:"labels,omitempty"`
	Compose       DockerComposeInfo           `json:"compose,omitempty"`
	Mounts        []DockerMount               `json:"mounts,omitempty"`
	Env           []string                    `json:"env,omitempty"`
	Cmd           []string                    `json:"cmd,omitempty"`
	Entrypoint    []string                    `json:"entrypoint,omitempty"`
	WorkingDir    string                      `json:"working_dir,omitempty"`
	User          string                      `json:"user,omitempty"`
	Ports         []DockerPortBinding         `json:"ports,omitempty"`
	RestartPolicy string                      `json:"restart_policy,omitempty"`
	NetworkMode   string                      `json:"network_mode,omitempty"`
	State         string                      `json:"state,omitempty"`
	ResolvedPaths []string                    `json:"resolved_paths,omitempty"`
	Warnings      []string                    `json:"warnings,omitempty"`
}

type DockerPortBinding struct {
	ContainerPort string `json:"container_port"`
	Protocol      string `json:"protocol,omitempty"`
	HostIP        string `json:"host_ip,omitempty"`
	HostPort      string `json:"host_port,omitempty"`
}

type DirSizeReqPayload struct {
	Path string `json:"path"`
}

type DirSizeRespPayload struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Error string `json:"error,omitempty"`
}

// PolicyPushPayload contains the full backup policy sent from master to agent.
type PolicyPushPayload struct {
	AgentID         string                      `json:"agent_id"`
	Storage         StorageConfig               `json:"storage"`
	ResticPassword  string                      `json:"restic_password"`
	PlainBackup     bool                        `json:"plain_backup,omitempty"`
	BackupMode      string                      `json:"backup_mode,omitempty"`
	ArchiveFormat   string                      `json:"archive_format,omitempty"`
	BackupDirs      []string                    `json:"backup_dirs"`
	BackupSources   []BackupSource              `json:"backup_sources,omitempty"`
	ExcludePatterns []string                    `json:"exclude_patterns"`
	PreBackupHook   *PolicyHook                 `json:"pre_backup_hook,omitempty"`
	PostBackupHook  *PolicyHook                 `json:"post_backup_hook,omitempty"`
	Schedule        string                      `json:"schedule"`
	Retention       RetentionPolicy             `json:"retention"`
	Verification    *BackupVerificationSettings `json:"verification,omitempty"`
}

// StorageConfig contains rclone and repository settings for a backup policy.
type StorageConfig struct {
	RcloneType         string            `json:"rclone_type"`
	RcloneConfig       map[string]string `json:"rclone_config"`
	RepoPath           string            `json:"repo_path"`
	RcloneArgs         map[string]string `json:"rclone_args,omitempty"`
	RclonePassObscured bool              `json:"rclone_pass_obscured,omitempty"`
}

// RetentionPolicy maps directly to restic forget retention options.
type RetentionPolicy struct {
	KeepLast    int `json:"keep_last"`
	KeepDaily   int `json:"keep_daily"`
	KeepWeekly  int `json:"keep_weekly"`
	KeepMonthly int `json:"keep_monthly"`
}

// BackupNowPayload requests an immediate backup run for an agent.
type BackupNowPayload struct {
	AgentID string             `json:"agent_id"`
	Policy  *PolicyPushPayload `json:"policy,omitempty"`
}

// RestoreReqPayload requests a snapshot restore to a target path.
type RestoreReqPayload struct {
	SnapshotID   string                `json:"snapshot_id"`
	Target       string                `json:"target"`
	IncludePaths []string              `json:"include_paths,omitempty"`
	RestoreMode  string                `json:"restore_mode,omitempty"`
	Docker       *DockerRestoreRequest `json:"docker,omitempty"`
}

type DockerRestoreRequest struct {
	Sources []DockerResolvedSource `json:"sources,omitempty"`
}

const (
	RestorePreflightStatusPassed = "passed"
	RestorePreflightStatusFailed = "failed"

	RestorePreflightSeverityInfo    = "info"
	RestorePreflightSeverityWarning = "warning"
	RestorePreflightSeverityError   = "error"
)

// RestorePreflightReqPayload asks an agent to validate local restore readiness.
type RestorePreflightReqPayload struct {
	AgentID      string                `json:"agent_id,omitempty"`
	SnapshotID   string                `json:"snapshot_id"`
	Target       string                `json:"target,omitempty"`
	IncludePaths []string              `json:"include_paths,omitempty"`
	RestoreMode  string                `json:"restore_mode,omitempty"`
	Docker       *DockerRestoreRequest `json:"docker,omitempty"`
}

// RestorePreflightRespPayload reports structured readiness checks for a restore plan.
type RestorePreflightRespPayload struct {
	AgentID    string                  `json:"agent_id,omitempty"`
	SnapshotID string                  `json:"snapshot_id"`
	Status     string                  `json:"status"`
	Checks     []RestorePreflightCheck `json:"checks"`
	Error      string                  `json:"error,omitempty"`
}

// RestorePreflightCheck is one preflight finding.
type RestorePreflightCheck struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Detail   string `json:"detail,omitempty"`
}

// SnapshotListReqPayload requests repository snapshots from an agent.
type SnapshotListReqPayload struct {
	AgentID string `json:"agent_id"`
}

// SnapshotBrowseReqPayload requests entries contained in one repository snapshot.
type SnapshotBrowseReqPayload struct {
	SnapshotID string `json:"snapshot_id"`
	Path       string `json:"path,omitempty"`
}

// SnapshotBrowseRespPayload returns file entries contained in one snapshot.
type SnapshotBrowseRespPayload struct {
	SnapshotID string              `json:"snapshot_id"`
	Entries    []SnapshotFileEntry `json:"entries"`
	Error      string              `json:"error,omitempty"`
}

// SnapshotFileEntry describes one file or directory inside a snapshot.
type SnapshotFileEntry struct {
	Path  string `json:"path"`
	Type  string `json:"type"`
	Size  int64  `json:"size"`
	Mtime string `json:"mtime"`
}

// CollectLogsReqPayload requests recent logs from an agent.
type CollectLogsReqPayload struct {
	MaxBytes int `json:"max_bytes"`
}

// CollectLogsRespPayload returns collected log text from an agent.
type CollectLogsRespPayload struct {
	Logs  string `json:"logs"`
	Error string `json:"error,omitempty"`
}

type VersionInfoPayload struct {
	Version    string `json:"version"`
	GitHubRepo string `json:"github_repo"`
}

type UpdateAgentPayload struct {
	Version    string `json:"version"`
	GitHubRepo string `json:"github_repo"`
}

type UpdateAgentRespPayload struct {
	Accepted   bool   `json:"accepted"`
	Version    string `json:"version,omitempty"`
	GitHubRepo string `json:"github_repo,omitempty"`
	Error      string `json:"error,omitempty"`
}
