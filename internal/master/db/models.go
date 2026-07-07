package db

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type User struct {
	ID           string     `gorm:"type:text;primaryKey" json:"id"`
	Username     string     `gorm:"type:text;uniqueIndex;not null" json:"username"`
	PasswordHash string     `gorm:"type:text;not null" json:"-"`
	Role         string     `gorm:"type:text;default:admin;index" json:"role"`
	DisabledAt   *time.Time `json:"disabled_at,omitempty"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = uuid.NewString()
	}
	if u.Role == "" {
		u.Role = "admin"
	}
	return nil
}

type APIToken struct {
	ID          string     `gorm:"type:text;primaryKey" json:"id"`
	Name        string     `gorm:"type:text;not null" json:"name"`
	TokenPrefix string     `gorm:"type:text;uniqueIndex;not null" json:"token_prefix"`
	SecretHash  string     `gorm:"type:text;not null" json:"-"`
	OwnerUserID string     `gorm:"type:text;index;not null" json:"owner_user_id"`
	Role        string     `gorm:"type:text;index;not null" json:"role"`
	Scopes      string     `gorm:"type:text" json:"scopes"`
	ExpiresAt   *time.Time `gorm:"index" json:"expires_at,omitempty"`
	RevokedAt   *time.Time `gorm:"index" json:"revoked_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (t *APIToken) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	return nil
}

type AuditEvent struct {
	ID         string    `gorm:"type:text;primaryKey" json:"id"`
	ActorType  string    `gorm:"type:text;index" json:"actor_type"`
	ActorID    string    `gorm:"type:text;index" json:"actor_id"`
	ActorName  string    `gorm:"type:text" json:"actor_name"`
	ActorRole  string    `gorm:"type:text;index" json:"actor_role"`
	TokenID    string    `gorm:"type:text;index" json:"token_id,omitempty"`
	Action     string    `gorm:"type:text;index;not null" json:"action"`
	TargetType string    `gorm:"type:text;index" json:"target_type,omitempty"`
	TargetID   string    `gorm:"type:text;index" json:"target_id,omitempty"`
	Result     string    `gorm:"type:text;index;not null" json:"result"`
	Message    string    `gorm:"type:text" json:"message,omitempty"`
	IPAddress  string    `gorm:"type:text" json:"ip_address,omitempty"`
	UserAgent  string    `gorm:"type:text" json:"user_agent,omitempty"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

func (e *AuditEvent) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	return nil
}

type Agent struct {
	ID          string     `gorm:"type:text;primaryKey" json:"id"`
	Name        string     `gorm:"type:text;not null" json:"name"`
	EnrollToken string     `gorm:"type:text;uniqueIndex:idx_agents_enroll_token_nonempty,where:enroll_token <> ''" json:"enroll_token,omitempty"`
	AgentToken  string     `gorm:"type:text;uniqueIndex:idx_agents_agent_token_nonempty,where:agent_token <> ''" json:"-"`
	Status      string     `gorm:"type:text;default:offline" json:"status"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
	SystemInfo  string     `gorm:"type:text" json:"system_info"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (a *Agent) BeforeCreate(tx *gorm.DB) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
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
		s.ID = uuid.NewString()
	}
	return nil
}

type BackupPolicy struct {
	ID              string    `gorm:"type:text;primaryKey" json:"id"`
	AgentID         string    `gorm:"type:text;index;not null" json:"agent_id"`
	StorageID       string    `gorm:"type:text;not null" json:"storage_id"`
	BackupMode      string    `gorm:"type:text;default:snapshot" json:"backup_mode"`
	ArchiveFormat   string    `gorm:"type:text" json:"archive_format"`
	RepoPath        string    `gorm:"type:text" json:"repo_path"`
	ResticPassword  string    `gorm:"type:text" json:"-"`
	BackupDirs      string    `gorm:"type:text" json:"backup_dirs"`
	BackupSources   string    `gorm:"type:text" json:"backup_sources"`
	ExcludePatterns string    `gorm:"type:text" json:"exclude_patterns"`
	PreBackupHook   string    `gorm:"type:text" json:"pre_backup_hook"`
	PostBackupHook  string    `gorm:"type:text" json:"post_backup_hook"`
	Schedule        string    `gorm:"type:text" json:"schedule"`
	Retention       string    `gorm:"type:text" json:"retention"`
	RcloneArgs      string    `gorm:"type:text" json:"rclone_args"`
	TimeoutHours    int       `gorm:"default:6" json:"timeout_hours"`
	Verification    string    `gorm:"type:text" json:"verification"`
	Synced          bool      `gorm:"default:false" json:"synced"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (b *BackupPolicy) BeforeCreate(tx *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	return nil
}

type AgentCommand struct {
	ID              string     `gorm:"type:text;primaryKey" json:"id"`
	AgentID         string     `gorm:"type:text;index;not null" json:"agent_id"`
	Type            string     `gorm:"type:text;index;not null" json:"type"`
	Status          string     `gorm:"type:text;index;not null" json:"status"`
	MessageID       string     `gorm:"type:text;uniqueIndex;not null" json:"message_id"`
	Payload         string     `gorm:"type:text" json:"-"`
	Result          string     `gorm:"type:text" json:"result,omitempty"`
	ErrorMessage    string     `gorm:"type:text" json:"error_message,omitempty"`
	Attempts        int        `json:"attempts"`
	PolicyID        string     `gorm:"type:text;index" json:"policy_id,omitempty"`
	PolicyUpdatedAt *time.Time `gorm:"index" json:"policy_updated_at,omitempty"`
	StorageID       string     `gorm:"type:text;index" json:"storage_id,omitempty"`
	DeadlineAt      *time.Time `json:"deadline_at"`
	DispatchedAt    *time.Time `json:"dispatched_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (c *AgentCommand) BeforeCreate(tx *gorm.DB) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	return nil
}

type TaskHistory struct {
	ID                  string     `gorm:"type:text;primaryKey" json:"id"`
	AgentID             string     `gorm:"type:text;index;not null" json:"agent_id"`
	Type                string     `gorm:"type:text;not null" json:"type"`
	Status              string     `gorm:"type:text;not null" json:"status"`
	SnapshotID          string     `gorm:"type:text" json:"snapshot_id"`
	ArtifactPath        string     `gorm:"type:text" json:"artifact_path"`
	ArtifactName        string     `gorm:"type:text" json:"artifact_name"`
	ArtifactSize        int64      `json:"artifact_size"`
	ArtifactContentType string     `gorm:"type:text" json:"artifact_content_type"`
	BackupMode          string     `gorm:"type:text" json:"backup_mode"`
	ArchiveFormat       string     `gorm:"type:text" json:"archive_format"`
	MessageID           string     `gorm:"type:text;index" json:"message_id,omitempty"`
	CommandID           string     `gorm:"type:text;index" json:"command_id,omitempty"`
	PolicyID            string     `gorm:"type:text;index" json:"policy_id,omitempty"`
	StorageID           string     `gorm:"type:text;index" json:"storage_id,omitempty"`
	Docker              string     `gorm:"type:text" json:"docker,omitempty"`
	Verification        string     `gorm:"type:text" json:"verification,omitempty"`
	StartedAt           *time.Time `json:"started_at"`
	FinishedAt          *time.Time `json:"finished_at"`
	DurationMs          int64      `json:"duration_ms"`
	RepoSize            int64      `json:"repo_size"`
	ErrorLog            string     `gorm:"type:text" json:"error_log"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func (th *TaskHistory) BeforeCreate(tx *gorm.DB) error {
	if th.ID == "" {
		th.ID = uuid.NewString()
	}
	return nil
}

type Snapshot struct {
	ID         string    `gorm:"type:text;primaryKey" json:"id"`
	AgentID    string    `gorm:"type:text;uniqueIndex:idx_snapshots_agent_snapshot;not null" json:"agent_id"`
	SnapshotID string    `gorm:"type:text;uniqueIndex:idx_snapshots_agent_snapshot;not null" json:"snapshot_id"`
	Timestamp  time.Time `json:"timestamp"`
	Paths      string    `gorm:"type:text" json:"paths"`
	Size       int64     `json:"size"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Snapshot) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	return nil
}

type NotificationConfig struct {
	ID        string    `gorm:"type:text;primaryKey" json:"id"`
	Name      string    `gorm:"type:text" json:"name"`
	Type      string    `gorm:"type:text;not null" json:"type"`
	Config    string    `gorm:"type:text" json:"config"`
	Events    string    `gorm:"type:text" json:"events"`
	CreatedAt time.Time `json:"created_at"`
}

func (n *NotificationConfig) BeforeCreate(tx *gorm.DB) error {
	if n.ID == "" {
		n.ID = uuid.NewString()
	}
	return nil
}
