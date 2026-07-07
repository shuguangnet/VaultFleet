package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
)

type fakeVerificationRestic struct {
	snapshots      []SnapshotInfo
	entries        []SnapshotFileEntry
	checkErr       error
	restoreErr     error
	checked        bool
	listedSnapshot string
}

func (f *fakeVerificationRestic) ListSnapshots(context.Context) ([]SnapshotInfo, error) {
	return f.snapshots, nil
}

func (f *fakeVerificationRestic) CheckRepository(context.Context) error {
	f.checked = true
	return f.checkErr
}

func (f *fakeVerificationRestic) LsSnapshot(_ context.Context, snapshotID string, _ ...string) ([]SnapshotFileEntry, error) {
	f.listedSnapshot = snapshotID
	return f.entries, nil
}

func (f *fakeVerificationRestic) RestoreSnapshot(_ context.Context, _ string, targetPath string, includePaths []string) error {
	if f.restoreErr != nil {
		return f.restoreErr
	}
	for _, includePath := range includePaths {
		restoredPath := filepath.Join(targetPath, filepath.Clean(includePath))
		if filepath.IsAbs(includePath) {
			restoredPath = filepath.Join(targetPath, includePath[1:])
		}
		if err := os.MkdirAll(filepath.Dir(restoredPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(restoredPath, []byte("ok"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func TestRunVerificationJobSelectsLatestSnapshotAndRunsReadChecks(t *testing.T) {
	older := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	newer := older.Add(time.Hour)
	restic := &fakeVerificationRestic{
		snapshots: []SnapshotInfo{
			{ID: "snap-old", Time: older},
			{ID: "snap-new", Time: newer},
		},
		entries: []SnapshotFileEntry{
			{Path: "/srv/app/config.yml", Type: "file", Size: 12},
		},
	}

	result := RunVerificationJob(context.Background(), restic, VerificationConfig{SampleCount: 1})

	assert.Equal(t, "verify", result.Type)
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, "snap-new", result.SnapshotID)
	require.NotNil(t, result.Verification)
	assert.Equal(t, protocol.VerificationStatusPassed, result.Verification.Status)
	assert.Equal(t, "snap-new", result.Verification.SnapshotID)
	assert.True(t, restic.checked)
	assert.Equal(t, "snap-new", restic.listedSnapshot)
	assert.Contains(t, verificationCheckCodes(result.Verification.Checks), "restic_check")
	assert.Contains(t, verificationCheckCodes(result.Verification.Checks), "sample_restore")
}

func TestRunVerificationJobReportsEmptyRepository(t *testing.T) {
	result := RunVerificationJob(context.Background(), &fakeVerificationRestic{}, VerificationConfig{})

	assert.Equal(t, "failed", result.Status)
	require.NotNil(t, result.Verification)
	assert.Equal(t, protocol.VerificationStatusFailed, result.Verification.Status)
	assert.Contains(t, verificationCheckCodes(result.Verification.Checks), "snapshot_available")
}

func TestRunVerificationJobReportsResticCheckFailure(t *testing.T) {
	restic := &fakeVerificationRestic{
		snapshots: []SnapshotInfo{{ID: "snap-1", Time: time.Now()}},
		entries:   []SnapshotFileEntry{{Path: "/srv/app/config.yml", Type: "file", Size: 12}},
		checkErr:  errors.New("repository damaged"),
	}

	result := RunVerificationJob(context.Background(), restic, VerificationConfig{SampleCount: 1})

	assert.Equal(t, "failed", result.Status)
	require.NotNil(t, result.Verification)
	assert.Equal(t, protocol.VerificationStatusFailed, result.Verification.Status)
	assert.Contains(t, result.ErrorLog, "repository damaged")
}

func TestRunVerificationJobCanRestoreSampleFile(t *testing.T) {
	restic := &fakeVerificationRestic{
		snapshots: []SnapshotInfo{{ID: "snap-1", Time: time.Now()}},
		entries:   []SnapshotFileEntry{{Path: "/srv/app/config.yml", Type: "file", Size: 12}},
	}

	result := RunVerificationJob(context.Background(), restic, VerificationConfig{
		SampleCount:          1,
		SampleRestoreEnabled: true,
		TempDir:              t.TempDir(),
	})

	assert.Equal(t, "success", result.Status)
	require.NotNil(t, result.Verification)
	assert.Equal(t, protocol.VerificationStatusPassed, result.Verification.Status)
	assert.Contains(t, verificationCheckCodes(result.Verification.Checks), "cleanup")
}

func verificationCheckCodes(checks []protocol.BackupVerificationCheck) []string {
	codes := make([]string, 0, len(checks))
	for _, check := range checks {
		codes = append(codes, check.Code)
	}
	return codes
}
