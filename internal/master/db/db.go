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

	return &Database{
		DB:        gormDB,
		DataDir:   dataDir,
		MasterKey: masterKey,
	}, nil
}
