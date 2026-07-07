package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite"
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

	if err := dedupeLegacySnapshots(gormDB); err != nil {
		return nil, fmt.Errorf("dedupe snapshots: %w", err)
	}

	if err := gormDB.AutoMigrate(
		&User{},
		&APIToken{},
		&AuditEvent{},
		&Agent{},
		&StorageConfig{},
		&BackupPolicy{},
		&AgentCommand{},
		&TaskHistory{},
		&Snapshot{},
		&NotificationConfig{},
	); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}
	if err := ensureDockerBackupColumns(gormDB); err != nil {
		return nil, fmt.Errorf("ensure docker backup columns: %w", err)
	}
	if err := ensureBackupVerificationColumns(gormDB); err != nil {
		return nil, fmt.Errorf("ensure backup verification columns: %w", err)
	}
	if err := ensureIdentityAccessColumns(gormDB); err != nil {
		return nil, fmt.Errorf("ensure identity access columns: %w", err)
	}
	if err := ensureAgentTagsColumn(gormDB); err != nil {
		return nil, fmt.Errorf("ensure agent tags column: %w", err)
	}
	if err := backfillUserRoles(gormDB); err != nil {
		return nil, fmt.Errorf("backfill user roles: %w", err)
	}
	if err := migrateTaskHistoryArtifacts(gormDB); err != nil {
		return nil, fmt.Errorf("migrate task history artifacts: %w", err)
	}
	if err := ensureTaskHistoryIndexes(gormDB); err != nil {
		return nil, fmt.Errorf("ensure task history indexes: %w", err)
	}

	masterKey, err := InitMasterKey(dataDir)
	if err != nil {
		return nil, fmt.Errorf("init master key: %w", err)
	}

	return &Database{
		DB:        gormDB,
		DataDir:   dataDir,
		MasterKey: masterKey,
	}, nil
}

func ensureIdentityAccessColumns(gormDB *gorm.DB) error {
	if gormDB.Migrator().HasTable(&User{}) {
		for _, column := range []string{"Role", "DisabledAt", "LastLoginAt"} {
			if !gormDB.Migrator().HasColumn(&User{}, column) {
				if err := gormDB.Migrator().AddColumn(&User{}, column); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func ensureAgentTagsColumn(gormDB *gorm.DB) error {
	if gormDB.Migrator().HasTable(&Agent{}) && !gormDB.Migrator().HasColumn(&Agent{}, "Tags") {
		if err := gormDB.Migrator().AddColumn(&Agent{}, "Tags"); err != nil {
			return err
		}
	}
	return nil
}

func backfillUserRoles(gormDB *gorm.DB) error {
	if !gormDB.Migrator().HasTable(&User{}) {
		return nil
	}
	return gormDB.Model(&User{}).
		Where("role = '' OR role IS NULL").
		Update("role", "admin").Error
}

func ensureBackupVerificationColumns(gormDB *gorm.DB) error {
	if gormDB.Migrator().HasTable(&BackupPolicy{}) && !gormDB.Migrator().HasColumn(&BackupPolicy{}, "Verification") {
		if err := gormDB.Migrator().AddColumn(&BackupPolicy{}, "Verification"); err != nil {
			return err
		}
	}
	if gormDB.Migrator().HasTable(&TaskHistory{}) && !gormDB.Migrator().HasColumn(&TaskHistory{}, "Verification") {
		if err := gormDB.Migrator().AddColumn(&TaskHistory{}, "Verification"); err != nil {
			return err
		}
	}
	return nil
}

func ensureDockerBackupColumns(gormDB *gorm.DB) error {
	if gormDB.Migrator().HasTable(&BackupPolicy{}) && !gormDB.Migrator().HasColumn(&BackupPolicy{}, "BackupSources") {
		if err := gormDB.Migrator().AddColumn(&BackupPolicy{}, "BackupSources"); err != nil {
			return err
		}
	}
	if gormDB.Migrator().HasTable(&TaskHistory{}) && !gormDB.Migrator().HasColumn(&TaskHistory{}, "Docker") {
		if err := gormDB.Migrator().AddColumn(&TaskHistory{}, "Docker"); err != nil {
			return err
		}
	}
	return nil
}

func dedupeLegacySnapshots(gormDB *gorm.DB) error {
	if !gormDB.Migrator().HasTable(&Snapshot{}) {
		return nil
	}

	return gormDB.Exec(`
		DELETE FROM snapshots
		WHERE rowid NOT IN (
			SELECT rowid
			FROM (
				SELECT
					rowid,
					ROW_NUMBER() OVER (
						PARTITION BY agent_id, snapshot_id
						ORDER BY datetime(timestamp) DESC, datetime(created_at) DESC, rowid DESC
					) AS rn
				FROM snapshots
			)
			WHERE rn = 1
		)
	`).Error
}

func migrateTaskHistoryArtifacts(gormDB *gorm.DB) error {
	if !gormDB.Migrator().HasTable(&TaskHistory{}) {
		return nil
	}

	return gormDB.Transaction(func(tx *gorm.DB) error {
		rows, err := tx.Model(&TaskHistory{}).
			Select("id", "artifact_path").
			Where("artifact_path LIKE ?", "/etc/vaultfleet/artifacts/%").
			Rows()
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var id string
			var artifactPath string
			if err := rows.Scan(&id, &artifactPath); err != nil {
				return err
			}

			relPath := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(artifactPath)), "/etc/vaultfleet/")
			if relPath == "" || relPath == artifactPath {
				continue
			}

			if err := tx.Model(&TaskHistory{}).
				Where("id = ?", id).
				Update("artifact_path", relPath).Error; err != nil {
				return err
			}
		}

		return rows.Err()
	})
}

func ensureTaskHistoryIndexes(gormDB *gorm.DB) error {
	if !gormDB.Migrator().HasTable(&TaskHistory{}) {
		return nil
	}

	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_task_histories_type_status_created_at ON task_histories(type, status, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_task_histories_artifact_name ON task_histories(artifact_name)`,
	}
	for _, stmt := range statements {
		if err := gormDB.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}
