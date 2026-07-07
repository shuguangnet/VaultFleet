package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vaultfleet/pkg/protocol"
)

type verificationRestic interface {
	ListSnapshots(ctx context.Context) ([]SnapshotInfo, error)
	CheckRepository(ctx context.Context) error
	LsSnapshot(ctx context.Context, snapshotID string, paths ...string) ([]SnapshotFileEntry, error)
	RestoreSnapshot(ctx context.Context, snapshotID, targetPath string, includePaths []string) error
}

type VerificationConfig struct {
	SampleCount          int
	SampleRestoreEnabled bool
	TempDir              string
}

func RunVerificationJob(ctx context.Context, restic verificationRestic, cfg VerificationConfig) (result TaskResult) {
	startedAt := time.Now()
	checks := make([]protocol.BackupVerificationCheck, 0, 6)
	result = TaskResult{
		Type:   "verify",
		Status: "failed",
		Verification: &protocol.BackupVerificationResult{
			Status: protocol.VerificationStatusFailed,
		},
	}
	defer func() {
		result.DurationMs = time.Since(startedAt).Milliseconds()
		if result.Verification != nil {
			result.Verification.Checks = checks
			result.Verification.Status = verificationOverallStatus(checks)
			if result.Verification.Status == protocol.VerificationStatusPassed {
				result.Status = "success"
				result.ErrorLog = ""
			} else if result.ErrorLog == "" {
				result.ErrorLog = firstVerificationError(checks)
			}
		}
	}()

	var snapshots []SnapshotInfo
	checks = append(checks, timedVerificationCheck("snapshot_list", func() (string, error) {
		var err error
		snapshots, err = restic.ListSnapshots(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d snapshots found", len(snapshots)), nil
	}))
	if ctx.Err() != nil {
		result.ErrorLog = ctx.Err().Error()
		return result
	}
	if len(snapshots) == 0 {
		checks = append(checks, verificationCheck("snapshot_available", protocol.VerificationCheckStatusFailed, protocol.VerificationSeverityError, "no snapshots are available for verification", ""))
		return result
	}

	latest := latestSnapshot(snapshots)
	result.SnapshotID = latest.ID
	result.Verification.SnapshotID = latest.ID

	checks = append(checks, timedVerificationCheck("restic_check", func() (string, error) {
		return "", restic.CheckRepository(ctx)
	}))
	if ctx.Err() != nil {
		result.ErrorLog = ctx.Err().Error()
		return result
	}

	var entries []SnapshotFileEntry
	checks = append(checks, timedVerificationCheck("snapshot_ls", func() (string, error) {
		var err error
		entries, err = restic.LsSnapshot(ctx, latest.ID)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d entries listed", len(entries)), nil
	}))
	if ctx.Err() != nil {
		result.ErrorLog = ctx.Err().Error()
		return result
	}

	samples := sampleEntries(entries, cfg.SampleCount)
	checks = append(checks, timedVerificationCheck("sample_ls", func() (string, error) {
		if len(samples) == 0 {
			return "no sample entries available", nil
		}
		paths := make([]string, 0, len(samples))
		for _, entry := range samples {
			paths = append(paths, entry.Path)
		}
		_, err := restic.LsSnapshot(ctx, latest.ID, paths...)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d sample entries listed", len(samples)), nil
	}))
	if ctx.Err() != nil {
		result.ErrorLog = ctx.Err().Error()
		return result
	}

	if cfg.SampleRestoreEnabled {
		checks = append(checks, runSampleRestoreCheck(ctx, restic, latest.ID, entries, cfg.TempDir)...)
	} else {
		checks = append(checks, verificationCheck("sample_restore", protocol.VerificationCheckStatusSkipped, protocol.VerificationSeverityInfo, "sample restore is disabled", ""))
	}

	return result
}

func latestSnapshot(snapshots []SnapshotInfo) SnapshotInfo {
	latest := snapshots[0]
	for _, snapshot := range snapshots[1:] {
		if snapshot.Time.After(latest.Time) {
			latest = snapshot
		}
	}
	return latest
}

func sampleEntries(entries []SnapshotFileEntry, count int) []SnapshotFileEntry {
	if count <= 0 {
		count = 10
	}
	candidates := make([]SnapshotFileEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.Path) != "" {
			candidates = append(candidates, entry)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Type != candidates[j].Type {
			return candidates[i].Type == "file"
		}
		if candidates[i].Size != candidates[j].Size {
			return candidates[i].Size < candidates[j].Size
		}
		return candidates[i].Path < candidates[j].Path
	})
	if len(candidates) > count {
		candidates = candidates[:count]
	}
	return candidates
}

func runSampleRestoreCheck(ctx context.Context, restic verificationRestic, snapshotID string, entries []SnapshotFileEntry, parentTempDir string) []protocol.BackupVerificationCheck {
	sample, ok := sampleRestoreEntry(entries)
	if !ok {
		return []protocol.BackupVerificationCheck{
			verificationCheck("sample_restore", protocol.VerificationCheckStatusSkipped, protocol.VerificationSeverityWarning, "sample restore skipped because no regular file sample was found", ""),
		}
	}

	if parentTempDir == "" {
		parentTempDir = os.TempDir()
	}
	if err := os.MkdirAll(parentTempDir, 0o700); err != nil {
		return []protocol.BackupVerificationCheck{
			verificationCheck("sample_restore", protocol.VerificationCheckStatusFailed, protocol.VerificationSeverityError, "create sample restore parent directory failed", err.Error()),
		}
	}
	tempDir, err := os.MkdirTemp(parentTempDir, "vaultfleet-verify-*")
	if err != nil {
		return []protocol.BackupVerificationCheck{
			verificationCheck("sample_restore", protocol.VerificationCheckStatusFailed, protocol.VerificationSeverityError, "create sample restore directory failed", err.Error()),
		}
	}

	checks := make([]protocol.BackupVerificationCheck, 0, 2)
	checks = append(checks, timedVerificationCheck("sample_restore", func() (string, error) {
		if err := restic.RestoreSnapshot(ctx, snapshotID, tempDir, []string{sample.Path}); err != nil {
			return "", err
		}
		restoredPath := filepath.Join(tempDir, strings.TrimPrefix(filepath.Clean(sample.Path), string(os.PathSeparator)))
		if _, err := os.Stat(restoredPath); err != nil {
			return "", err
		}
		return sample.Path, nil
	}))
	cleanupStart := time.Now()
	if err := os.RemoveAll(tempDir); err != nil {
		check := verificationCheck("cleanup", protocol.VerificationCheckStatusFailed, protocol.VerificationSeverityWarning, "sample restore cleanup failed", err.Error())
		check.DurationMs = time.Since(cleanupStart).Milliseconds()
		checks = append(checks, check)
	} else {
		check := verificationCheck("cleanup", protocol.VerificationCheckStatusPassed, protocol.VerificationSeverityInfo, "sample restore directory cleaned up", tempDir)
		check.DurationMs = time.Since(cleanupStart).Milliseconds()
		checks = append(checks, check)
	}
	return checks
}

func sampleRestoreEntry(entries []SnapshotFileEntry) (SnapshotFileEntry, bool) {
	samples := sampleEntries(entries, len(entries))
	for _, entry := range samples {
		if entry.Type == "file" {
			return entry, true
		}
	}
	return SnapshotFileEntry{}, false
}

func timedVerificationCheck(code string, fn func() (string, error)) protocol.BackupVerificationCheck {
	start := time.Now()
	detail, err := fn()
	if err != nil {
		check := verificationCheck(code, protocol.VerificationCheckStatusFailed, protocol.VerificationSeverityError, verificationFailureMessage(code), err.Error())
		check.DurationMs = time.Since(start).Milliseconds()
		return check
	}
	check := verificationCheck(code, protocol.VerificationCheckStatusPassed, protocol.VerificationSeverityInfo, verificationSuccessMessage(code), detail)
	check.DurationMs = time.Since(start).Milliseconds()
	return check
}

func verificationCheck(code string, status string, severity string, message string, detail string) protocol.BackupVerificationCheck {
	return protocol.BackupVerificationCheck{
		Code:     code,
		Status:   status,
		Severity: severity,
		Message:  message,
		Detail:   detail,
	}
}

func verificationOverallStatus(checks []protocol.BackupVerificationCheck) string {
	for _, check := range checks {
		if check.Severity == protocol.VerificationSeverityError && check.Status == protocol.VerificationCheckStatusFailed {
			return protocol.VerificationStatusFailed
		}
	}
	return protocol.VerificationStatusPassed
}

func firstVerificationError(checks []protocol.BackupVerificationCheck) string {
	for _, check := range checks {
		if check.Severity == protocol.VerificationSeverityError && check.Status == protocol.VerificationCheckStatusFailed {
			if check.Detail != "" {
				return check.Message + ": " + check.Detail
			}
			return check.Message
		}
	}
	return "backup verification failed"
}

func verificationSuccessMessage(code string) string {
	switch code {
	case "snapshot_list":
		return "repository snapshots listed"
	case "restic_check":
		return "repository check passed"
	case "snapshot_ls":
		return "latest snapshot contents listed"
	case "sample_ls":
		return "sample snapshot entries listed"
	case "sample_restore":
		return "sample file restored"
	default:
		return "verification check passed"
	}
}

func verificationFailureMessage(code string) string {
	switch code {
	case "snapshot_list":
		return "list repository snapshots failed"
	case "restic_check":
		return "repository check failed"
	case "snapshot_ls":
		return "list latest snapshot contents failed"
	case "sample_ls":
		return "list sample snapshot entries failed"
	case "sample_restore":
		return "sample restore failed"
	default:
		return "verification check failed"
	}
}
