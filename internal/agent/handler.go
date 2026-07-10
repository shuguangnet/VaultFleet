package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	agentdatabase "vaultfleet/internal/agent/database"
	agentdocker "vaultfleet/internal/agent/docker"
	"vaultfleet/internal/agent/executor"
	"vaultfleet/internal/agent/filebrowse"
	agentmanifest "vaultfleet/internal/agent/manifest"
	"vaultfleet/internal/agent/policy"
	"vaultfleet/internal/agent/scheduler"
	"vaultfleet/internal/artifactnaming"
	"vaultfleet/pkg/protocol"
)

const (
	maxSnapshotBrowseResponseBytes = 900 * 1024
	backupProgressThrottleInterval = 5 * time.Second
)

type SendFunc func(protocol.Message) error

type BrowseFunc func(fsRoot string, scanPath string, maxDepth int) ([]protocol.DirEntry, error)

type DirSizeFunc func(fsRoot string, path string) (int64, error)

type AgentUpdater interface {
	Update(targetVersion, githubRepo string) error
}

type BackupRunnerFunc func(context.Context, executor.ExecutorConfig) executor.TaskResult
type BackupRunnerWithProgressFunc func(context.Context, executor.ExecutorConfig, executor.ProgressCallback) executor.TaskResult
type DatabasePrepareFunc func(context.Context, agentdatabase.Config) (agentdatabase.Result, func(), error)
type DatabaseListFunc func(context.Context, agentdatabase.ListConfig) ([]string, error)
type RestoreRunnerFunc func(context.Context, executor.ExecutorConfig, string, string, []string) error
type DockerRestoreRunnerFunc func(context.Context, protocol.DockerRestoreRequest) error
type SnapshotListRunnerFunc func(context.Context, executor.ExecutorConfig) ([]executor.SnapshotInfo, error)
type SnapshotBrowseRunnerFunc func(context.Context, executor.ExecutorConfig, string, string) ([]executor.SnapshotFileEntry, error)
type BackupVerificationRunnerFunc func(context.Context, executor.ExecutorConfig, protocol.BackupVerificationSettings) executor.TaskResult

type policyScheduler interface {
	Validate(schedule string) error
	UpdateSchedule(agentID string, schedule string, fn func()) error
	RemoveJob(agentID string)
}

type HandlerConfig struct {
	PolicyStore              *policy.Store
	SendFunc                 SendFunc
	BrowseFunc               BrowseFunc
	ConfigDir                string
	AgentID                  string
	LogFile                  string
	Scheduler                policyScheduler
	BackupRunner             BackupRunnerFunc
	BackupRunnerWithProgress BackupRunnerWithProgressFunc
	DatabasePrepare          DatabasePrepareFunc
	DatabaseList             DatabaseListFunc
	RestoreRunner            RestoreRunnerFunc
	DockerRestoreRunner      DockerRestoreRunnerFunc
	SnapshotListRunner       SnapshotListRunnerFunc
	SnapshotBrowseRunner     SnapshotBrowseRunnerFunc
	BackupVerificationRunner BackupVerificationRunnerFunc
	DirSizeFunc              DirSizeFunc
	AgentVersion             string
	Updater                  AgentUpdater
	DockerAPI                agentdocker.API
}

type Handler struct {
	policyStore              *policy.Store
	send                     SendFunc
	browse                   BrowseFunc
	configDir                string
	agentID                  string
	logFile                  string
	scheduler                policyScheduler
	backupRunner             BackupRunnerFunc
	backupRunnerWithProgress BackupRunnerWithProgressFunc
	databasePrepare          DatabasePrepareFunc
	databaseList             DatabaseListFunc
	restoreRunner            RestoreRunnerFunc
	dockerRestoreRunner      DockerRestoreRunnerFunc
	snapshotListRunner       SnapshotListRunnerFunc
	snapshotBrowseRunner     SnapshotBrowseRunnerFunc
	backupVerificationRunner BackupVerificationRunnerFunc
	snapshotCache            *snapshotCache
	dirSizeFunc              DirSizeFunc
	agentVersion             string
	updater                  AgentUpdater
	dockerAPI                agentdocker.API
	tasks                    *taskManager
	pendingResultsMu         sync.Mutex
}

func NewHandler(config HandlerConfig) *Handler {
	browse := config.BrowseFunc
	if browse == nil {
		browse = filebrowse.Browse
	}
	configDir := config.ConfigDir
	if configDir == "" {
		configDir = policy.DefaultDir
	}
	runner := config.BackupRunner
	if runner == nil {
		runner = runBackup
	}
	progressRunner := config.BackupRunnerWithProgress
	if progressRunner == nil {
		if config.BackupRunner != nil {
			progressRunner = func(ctx context.Context, cfg executor.ExecutorConfig, _ executor.ProgressCallback) executor.TaskResult {
				return runner(ctx, cfg)
			}
		} else {
			progressRunner = runBackupWithProgress
		}
	}
	restoreRunner := config.RestoreRunner
	if restoreRunner == nil {
		restoreRunner = runRestore
	}
	dockerRestoreRunner := config.DockerRestoreRunner
	if dockerRestoreRunner == nil {
		dockerRestoreRunner = runDockerRestore
	}
	snapshotListRunner := config.SnapshotListRunner
	if snapshotListRunner == nil {
		snapshotListRunner = runSnapshotList
	}
	snapshotBrowseRunner := config.SnapshotBrowseRunner
	if snapshotBrowseRunner == nil {
		snapshotBrowseRunner = runSnapshotBrowse
	}
	backupVerificationRunner := config.BackupVerificationRunner
	if backupVerificationRunner == nil {
		backupVerificationRunner = runBackupVerification
	}
	databasePrepare := config.DatabasePrepare
	if databasePrepare == nil {
		databasePrepare = agentdatabase.Prepare
	}
	databaseList := config.DatabaseList
	if databaseList == nil {
		databaseList = agentdatabase.List
	}
	dirSizeFunc := config.DirSizeFunc
	if dirSizeFunc == nil {
		dirSizeFunc = filebrowse.CalculateDirSize
	}
	policyScheduler := config.Scheduler
	if policyScheduler == nil {
		defaultScheduler := scheduler.New()
		if err := defaultScheduler.Start(); err != nil {
			log.Printf("start scheduler failed: %v", err)
		}
		policyScheduler = defaultScheduler
	}
	handler := &Handler{
		policyStore:              config.PolicyStore,
		send:                     config.SendFunc,
		browse:                   browse,
		configDir:                configDir,
		agentID:                  config.AgentID,
		logFile:                  config.LogFile,
		scheduler:                policyScheduler,
		backupRunner:             runner,
		backupRunnerWithProgress: progressRunner,
		databasePrepare:          databasePrepare,
		databaseList:             databaseList,
		restoreRunner:            restoreRunner,
		dockerRestoreRunner:      dockerRestoreRunner,
		snapshotListRunner:       snapshotListRunner,
		snapshotBrowseRunner:     snapshotBrowseRunner,
		backupVerificationRunner: backupVerificationRunner,
		snapshotCache:            newSnapshotCache(configDir),
		dirSizeFunc:              dirSizeFunc,
		agentVersion:             config.AgentVersion,
		updater:                  config.Updater,
		dockerAPI:                config.DockerAPI,
		tasks:                    newTaskManager(),
	}
	handler.restoreSavedPolicySchedule()
	return handler
}

func (h *Handler) Handle(msg protocol.Message) {
	switch msg.Type {
	case protocol.TypePolicyPush:
		h.handlePolicyPush(msg)
	case protocol.TypeBackupNow:
		h.handleBackupNow(msg)
	case protocol.TypeBackupVerifyReq:
		h.handleBackupVerifyReq(msg)
	case protocol.TypeDirBrowseReq:
		h.handleDirBrowseReq(msg)
	case protocol.TypeDockerDiscoveryReq:
		h.handleDockerDiscoveryReq(msg)
	case protocol.TypeDatabaseDiscoveryReq:
		h.handleDatabaseDiscoveryReq(msg)
	case protocol.TypeDirSizeReq:
		h.handleDirSizeReq(msg)
	case protocol.TypeRestoreReq, protocol.TypeSelectiveRestoreReq:
		h.handleRestoreReq(msg)
	case protocol.TypeRestorePreflightReq:
		h.handleRestorePreflightReq(msg)
	case protocol.TypeSnapshotListReq:
		h.handleSnapshotListReq(msg)
	case protocol.TypeSnapshotBrowseReq:
		h.handleSnapshotBrowseReq(msg)
	case protocol.TypeCollectLogsReq:
		h.handleCollectLogsReq(msg)
	case protocol.TypeVersionInfo:
		h.handleVersionInfo(msg)
	case protocol.TypeCancelTask:
		h.handleCancelTask(msg)
	case protocol.TypeUpdateAgent:
		h.handleUpdateAgent(msg)
	}
}

func (h *Handler) restoreSavedPolicySchedule() {
	if h.policyStore == nil || h.scheduler == nil {
		return
	}
	savedPolicy, err := h.policyStore.LoadPolicy()
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("load saved policy schedule failed: %v", err)
		}
		return
	}
	if savedPolicy.Schedule == "" {
		h.scheduler.RemoveJob(savedPolicy.AgentID)
	} else if err := h.scheduler.UpdateSchedule(savedPolicy.AgentID, savedPolicy.Schedule, func() {
		startErr := h.tasks.Start("", taskTypeBackup, func(ctx context.Context) {
			h.runBackupForPolicy(ctx, "", savedPolicy.AgentID, savedPolicy)
		})
		if startErr != nil {
			log.Printf("scheduled backup skipped: %v", startErr)
		}
	}); err != nil {
		log.Printf("restore saved policy schedule failed: %v", err)
	}
	h.updateVerificationSchedule(savedPolicy)
}

func (h *Handler) updateVerificationSchedule(policyPayload *protocol.PolicyPushPayload) error {
	if h.scheduler == nil || policyPayload == nil {
		return nil
	}
	jobID := verificationScheduleJobID(policyPayload.AgentID)
	if policyPayload.Verification == nil || !policyPayload.Verification.Enabled || strings.TrimSpace(policyPayload.Verification.Schedule) == "" {
		h.scheduler.RemoveJob(jobID)
		return nil
	}
	settings := *policyPayload.Verification
	return h.scheduler.UpdateSchedule(jobID, settings.Schedule, func() {
		startErr := h.tasks.Start("", taskTypeVerify, func(ctx context.Context) {
			h.runBackupVerificationForPolicy(ctx, "", policyPayload.AgentID, policyPayload, settings)
		})
		if startErr != nil {
			log.Printf("scheduled backup verification skipped: %v", startErr)
		}
	})
}

func verificationScheduleJobID(agentID string) string {
	return agentID + ":verify"
}

func (h *Handler) handlePolicyPush(msg protocol.Message) {
	pushedPolicy, err := protocol.ParsePayload[protocol.PolicyPushPayload](&msg)
	if err != nil {
		log.Printf("parse policy push failed: %v", err)
		h.sendPolicyAck(msg.ID, "", false, err.Error())
		return
	}
	if h.policyStore == nil {
		h.sendPolicyAck(msg.ID, pushedPolicy.AgentID, false, "policy store not configured")
		return
	}

	if h.scheduler != nil && pushedPolicy.Schedule != "" {
		if err := h.scheduler.Validate(pushedPolicy.Schedule); err != nil {
			log.Printf("validate backup schedule failed: %v", err)
			h.sendPolicyAck(msg.ID, pushedPolicy.AgentID, false, err.Error())
			return
		}
	}
	if h.scheduler != nil && pushedPolicy.Verification != nil && pushedPolicy.Verification.Enabled && pushedPolicy.Verification.Schedule != "" {
		if err := h.scheduler.Validate(pushedPolicy.Verification.Schedule); err != nil {
			log.Printf("validate backup verification schedule failed: %v", err)
			h.sendPolicyAck(msg.ID, pushedPolicy.AgentID, false, err.Error())
			return
		}
	}

	rollbackState, err := h.snapshotPolicyState()
	if err != nil {
		log.Printf("snapshot policy state failed: %v", err)
		h.sendPolicyAck(msg.ID, pushedPolicy.AgentID, false, err.Error())
		return
	}
	defer rollbackState.cleanup()

	stagedFiles, err := h.stagePolicyFiles(pushedPolicy)
	if err != nil {
		log.Printf("stage policy config failed: %v", err)
		h.sendPolicyAck(msg.ID, pushedPolicy.AgentID, false, err.Error())
		return
	}
	defer stagedFiles.cleanup()

	if err := stagedFiles.commit(h.configDir); err != nil {
		log.Printf("commit policy config failed: %v", err)
		rollbackState.restoreConfig()
		h.sendPolicyAck(msg.ID, pushedPolicy.AgentID, false, err.Error())
		return
	}

	if err := h.policyStore.SavePolicy(pushedPolicy); err != nil {
		log.Printf("save policy failed: %v", err)
		rollbackState.restoreConfig()
		rollbackState.restorePolicy()
		h.sendPolicyAck(msg.ID, pushedPolicy.AgentID, false, err.Error())
		return
	}

	if h.scheduler != nil {
		if pushedPolicy.Schedule == "" {
			h.scheduler.RemoveJob(pushedPolicy.AgentID)
		} else if err := h.scheduler.UpdateSchedule(pushedPolicy.AgentID, pushedPolicy.Schedule, func() {
			startErr := h.tasks.Start("", taskTypeBackup, func(ctx context.Context) {
				h.runBackupForPolicy(ctx, "", pushedPolicy.AgentID, pushedPolicy)
			})
			if startErr != nil {
				log.Printf("scheduled backup skipped: %v", startErr)
			}
		}); err != nil {
			log.Printf("update backup schedule failed: %v", err)
			rollbackState.restore()
			h.sendPolicyAck(msg.ID, pushedPolicy.AgentID, false, err.Error())
			return
		}
		if err := h.updateVerificationSchedule(pushedPolicy); err != nil {
			log.Printf("update backup verification schedule failed: %v", err)
			rollbackState.restore()
			h.sendPolicyAck(msg.ID, pushedPolicy.AgentID, false, err.Error())
			return
		}
	}
	h.sendPolicyAck(msg.ID, pushedPolicy.AgentID, true, "")
}

type stagedPolicyFiles struct {
	rclonePath   string
	passwordPath string
}

type policyRollbackState struct {
	policyStore *policy.Store
	oldPolicy   *protocol.PolicyPushPayload
	rclone      fileSnapshot
	password    fileSnapshot
}

type fileSnapshot struct {
	target  string
	backup  string
	existed bool
}

func (h *Handler) snapshotPolicyState() (*policyRollbackState, error) {
	state := &policyRollbackState{policyStore: h.policyStore}
	oldPolicy, err := h.policyStore.LoadPolicy()
	if err == nil {
		state.oldPolicy = oldPolicy
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	rcloneSnapshot, err := snapshotFile(filepath.Join(h.configDir, "rclone.conf"), h.configDir, ".rclone.conf.rollback.*")
	if err != nil {
		state.cleanup()
		return nil, err
	}
	state.rclone = rcloneSnapshot

	passwordSnapshot, err := snapshotFile(filepath.Join(h.configDir, ".restic-password"), h.configDir, ".restic-password.rollback.*")
	if err != nil {
		state.cleanup()
		return nil, err
	}
	state.password = passwordSnapshot
	return state, nil
}

func (h *Handler) stagePolicyFiles(pushedPolicy *protocol.PolicyPushPayload) (*stagedPolicyFiles, error) {
	if err := os.MkdirAll(h.configDir, 0o700); err != nil {
		return nil, err
	}

	rclonePath, err := createSecureTempPath(h.configDir, "rclone.conf.*")
	if err != nil {
		return nil, err
	}
	staged := &stagedPolicyFiles{rclonePath: rclonePath}
	if err := executor.WriteRcloneConf(rclonePath, executor.RcloneConfig{
		Type:         pushedPolicy.Storage.RcloneType,
		Params:       pushedPolicy.Storage.RcloneConfig,
		PassObscured: pushedPolicy.Storage.RclonePassObscured,
	}); err != nil {
		staged.cleanup()
		return nil, err
	}

	if pushedPolicy.PlainBackup {
		staged.passwordPath = filepath.Join(h.configDir, ".restic-password")
		return staged, nil
	}

	passwordPath, err := writeSecureTempFile(h.configDir, ".restic-password.*", []byte(pushedPolicy.ResticPassword))
	if err != nil {
		staged.cleanup()
		return nil, err
	}
	staged.passwordPath = passwordPath

	return staged, nil
}

func snapshotFile(target string, dir string, pattern string) (fileSnapshot, error) {
	snapshot := fileSnapshot{target: target}
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		return snapshot, nil
	}
	if err != nil {
		return snapshot, err
	}
	if info.IsDir() {
		return snapshot, &os.PathError{Op: "snapshot", Path: target, Err: os.ErrInvalid}
	}

	data, err := os.ReadFile(target)
	if err != nil {
		return snapshot, err
	}
	backupPath, err := writeSecureTempFile(dir, pattern, data)
	if err != nil {
		return snapshot, err
	}
	snapshot.backup = backupPath
	snapshot.existed = true
	return snapshot, nil
}

func (s *stagedPolicyFiles) commit(configDir string) error {
	rcloneTarget := filepath.Join(configDir, "rclone.conf")
	passwordTarget := filepath.Join(configDir, ".restic-password")
	if err := validateReplaceTarget(rcloneTarget); err != nil {
		return err
	}
	if err := validateReplaceTarget(passwordTarget); err != nil {
		return err
	}

	if err := os.Rename(s.rclonePath, rcloneTarget); err != nil {
		return err
	}
	s.rclonePath = ""
	if s.passwordPath == passwordTarget {
		if err := os.Remove(passwordTarget); err != nil && !os.IsNotExist(err) {
			return err
		}
		s.passwordPath = ""
		return nil
	}
	if err := os.Rename(s.passwordPath, passwordTarget); err != nil {
		return err
	}
	s.passwordPath = ""
	return nil
}

func (s *stagedPolicyFiles) cleanup() {
	if s.rclonePath != "" {
		if err := os.Remove(s.rclonePath); err != nil && !os.IsNotExist(err) {
			log.Printf("remove staged rclone config failed: %v", err)
		}
	}
	if s.passwordPath != "" {
		if err := os.Remove(s.passwordPath); err != nil && !os.IsNotExist(err) {
			log.Printf("remove staged restic password failed: %v", err)
		}
	}
}

func (s *policyRollbackState) restore() {
	s.restoreConfig()
	s.restorePolicy()
}

func (s *policyRollbackState) restoreConfig() {
	s.rclone.restore()
	s.password.restore()
}

func (s *policyRollbackState) restorePolicy() {
	if s.oldPolicy == nil {
		if err := s.policyStore.DeletePolicy(); err != nil {
			log.Printf("remove new policy failed: %v", err)
		}
		return
	}
	if err := s.policyStore.SavePolicy(s.oldPolicy); err != nil {
		log.Printf("restore previous policy failed: %v", err)
	}
}

func (s *policyRollbackState) cleanup() {
	s.rclone.cleanup()
	s.password.cleanup()
}

func (s *fileSnapshot) restore() {
	if s.target == "" {
		return
	}
	if !s.existed {
		if err := os.Remove(s.target); err != nil && !os.IsNotExist(err) {
			log.Printf("remove new policy config file failed: %v", err)
		}
		return
	}
	if s.backup == "" {
		return
	}
	if err := os.Rename(s.backup, s.target); err != nil {
		log.Printf("restore previous policy config file failed: %v", err)
		return
	}
	s.backup = ""
}

func (s *fileSnapshot) cleanup() {
	if s.backup == "" {
		return
	}
	if err := os.Remove(s.backup); err != nil && !os.IsNotExist(err) {
		log.Printf("remove policy config rollback file failed: %v", err)
	}
	s.backup = ""
}

func createSecureTempPath(dir string, pattern string) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func writeSecureTempFile(dir string, pattern string, data []byte) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path := file.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(path)
		}
	}()

	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	remove = false
	return path, nil
}

func validateReplaceTarget(path string) error {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return &os.PathError{Op: "replace", Path: path, Err: os.ErrExist}
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (h *Handler) handleBackupNow(msg protocol.Message) {
	backupNow, err := protocol.ParsePayload[protocol.BackupNowPayload](&msg)
	if err != nil {
		log.Printf("parse backup_now failed: %v", err)
		h.sendTaskResultWithID(msg.ID, h.failedTaskResult(h.agentID, "parse backup_now: "+err.Error(), time.Now()))
		return
	}

	agentID := backupNow.AgentID
	if agentID == "" {
		agentID = h.agentID
	}
	if h.policyStore == nil {
		h.sendTaskResultWithID(msg.ID, h.failedTaskResult(agentID, "policy store not configured", time.Now()))
		return
	}

	policyPayload := backupNow.Policy
	if policyPayload == nil {
		policyPayload, err = h.policyStore.LoadPolicy()
		if err != nil {
			log.Printf("load policy failed: %v", err)
			h.sendTaskResultWithID(msg.ID, h.failedTaskResult(agentID, "load policy: "+err.Error(), time.Now()))
			return
		}
	}
	if agentID == "" {
		agentID = policyPayload.AgentID
	}
	startErr := h.tasks.Start(msg.ID, taskTypeBackup, func(ctx context.Context) {
		h.runBackupForPolicy(ctx, msg.ID, agentID, policyPayload)
	})
	if startErr != nil {
		h.sendTaskResultWithID(msg.ID, h.failedPolicyTaskResult(agentID, startErr.Error(), time.Now(), policyPayload))
	}
}

func (h *Handler) handleBackupVerifyReq(msg protocol.Message) {
	verifyReq, err := protocol.ParsePayload[protocol.BackupVerifyReqPayload](&msg)
	if err != nil {
		log.Printf("parse backup_verify_req failed: %v", err)
		h.sendTaskResultWithID(msg.ID, h.failedTypedTaskResult(h.agentID, "verify", "", "parse backup_verify_req: "+err.Error(), time.Now()))
		return
	}

	agentID := verifyReq.AgentID
	if agentID == "" {
		agentID = h.agentID
	}
	if h.policyStore == nil {
		h.sendTaskResultWithID(msg.ID, h.failedTypedTaskResult(agentID, "verify", "", "policy store not configured", time.Now()))
		return
	}

	policyPayload := verifyReq.Policy
	if policyPayload == nil {
		policyPayload, err = h.policyStore.LoadPolicy()
		if err != nil {
			log.Printf("load policy failed: %v", err)
			h.sendTaskResultWithID(msg.ID, h.failedTypedTaskResult(agentID, "verify", "", "load policy: "+err.Error(), time.Now()))
			return
		}
	}
	settings := protocol.BackupVerificationSettings{}
	if verifyReq.Verification != nil {
		settings = *verifyReq.Verification
	} else if policyPayload.Verification != nil {
		settings = *policyPayload.Verification
	}
	if agentID == "" {
		agentID = policyPayload.AgentID
	}
	startErr := h.tasks.Start(msg.ID, taskTypeVerify, func(ctx context.Context) {
		h.runBackupVerificationForPolicy(ctx, msg.ID, agentID, policyPayload, settings)
	})
	if startErr != nil {
		h.sendTaskResultWithID(msg.ID, h.failedTypedTaskResult(agentID, "verify", "", startErr.Error(), time.Now()))
	}
}

func (h *Handler) runBackupVerificationForPolicy(ctx context.Context, messageID string, agentID string, policyPayload *protocol.PolicyPushPayload, settings protocol.BackupVerificationSettings) {
	if policyPayload == nil {
		return
	}
	if agentID == "" {
		agentID = policyPayload.AgentID
	}
	taskLogs := newTaskLogEmitter(agentID, messageID, taskTypeVerify, h.send)

	startedAt := time.Now()
	if strings.TrimSpace(policyPayload.BackupMode) == protocol.BackupModeArchive {
		taskLogs.Error("init", "archive backup verification is unsupported")
		h.sendTaskResultWithID(messageID, h.failedTypedTaskResult(agentID, "verify", "", "archive backup verification is unsupported", startedAt))
		return
	}
	taskLogs.Info("init", "preparing backup verification")
	if err := h.ensureRcloneConf(policyPayload); err != nil {
		taskLogs.Error("init", "prepare rclone config failed: "+err.Error())
		h.sendTaskResultWithID(messageID, h.failedTypedTaskResult(agentID, "verify", "", "prepare rclone config: "+err.Error(), startedAt))
		return
	}

	runCtx := ctx
	cancel := func() {}
	if settings.TimeoutMinutes > 0 {
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(settings.TimeoutMinutes)*time.Minute)
	}
	defer cancel()

	taskLogs.Info("verify", "running backup recoverability verification")
	cfg := executorConfigForPolicy(h.configDir, policyPayload)
	cfg.TaskLog = taskLogs.Emit
	result := h.backupVerificationRunner(runCtx, cfg, settings)
	if runCtx.Err() == context.DeadlineExceeded {
		result.Status = "failed"
		result.ErrorLog = "backup verification timed out"
		if result.Verification == nil {
			result.Verification = &protocol.BackupVerificationResult{}
		}
		result.Verification.Status = protocol.VerificationStatusFailed
		result.Verification.Error = result.ErrorLog
	} else if runCtx.Err() == context.Canceled {
		result.Status = "cancelled"
		if result.ErrorLog == "" {
			result.ErrorLog = runCtx.Err().Error()
		}
	}
	if result.Status == "success" {
		taskLogs.Info("complete", "backup verification completed successfully")
	} else {
		taskLogs.Error("complete", "backup verification finished with status "+result.Status+": "+result.ErrorLog)
	}
	h.sendTaskResultWithID(messageID, result.ToProtocol(agentID, startedAt))
}

func (h *Handler) runBackupForPolicy(ctx context.Context, messageID string, agentID string, policyPayload *protocol.PolicyPushPayload) {
	if policyPayload == nil {
		return
	}
	if agentID == "" {
		agentID = policyPayload.AgentID
	}
	taskLogs := newTaskLogEmitter(agentID, messageID, taskTypeBackup, h.send)

	startedAt := time.Now()
	taskLogs.Info("init", "preparing backup configuration")
	if err := h.ensureRcloneConf(policyPayload); err != nil {
		log.Printf("prepare rclone config failed: %v", err)
		taskLogs.Error("init", "prepare rclone config failed: "+err.Error())
		h.sendTaskResultWithID(messageID, h.failedPolicyTaskResult(agentID, "prepare rclone config: "+err.Error(), startedAt, policyPayload))
		return
	}
	taskLogs.Info("sources", "resolving backup sources")
	resolvedPolicy, dockerMetadata, err := h.resolvePolicyBackupSources(ctx, policyPayload)
	if err != nil {
		log.Printf("resolve backup sources failed: %v", err)
		taskLogs.Error("sources", "resolve backup sources failed: "+err.Error())
		h.sendTaskResultWithID(messageID, h.failedPolicyTaskResult(agentID, "resolve backup sources: "+err.Error(), startedAt, policyPayload))
		return
	}
	if dockerMetadata != nil {
		taskLogs.Info("sources", "resolved Docker backup metadata")
	}
	cfg := executorConfigForPolicy(h.configDir, resolvedPolicy)
	cfg.TaskLog = taskLogs.Emit
	if err := runPolicyHookWithLogs(ctx, h.configDir, cfg.PreBackupHook, "pre_backup_hook", taskLogs); err != nil {
		log.Printf("pre-backup hook failed: %v", err)
		taskLogs.Error("pre_backup_hook", "pre-backup hook failed: "+err.Error())
		h.sendTaskResultWithID(messageID, h.failedPolicyTaskResult(agentID, "pre_backup_hook: "+err.Error(), startedAt, resolvedPolicy))
		return
	}
	databaseResult, databaseCleanup, err := h.prepareDatabaseBackupSources(ctx, resolvedPolicy.BackupSources, taskLogs)
	if databaseCleanup != nil {
		defer databaseCleanup()
	}
	if err != nil {
		log.Printf("prepare database backup sources failed: %v", err)
		taskLogs.Error("database-dump", "prepare database backup sources failed: "+err.Error())
		h.sendTaskResultWithID(messageID, h.failedPolicyTaskResult(agentID, "database_dump: "+err.Error(), startedAt, resolvedPolicy))
		return
	}
	if len(databaseResult.Paths) > 0 {
		cfg.BackupDirs = appendUniqueStrings(cfg.BackupDirs, databaseResult.Paths...)
		taskLogs.Info("database-dump", "prepared database dump files")
	}
	if strings.EqualFold(strings.TrimSpace(cfg.BackupMode), protocol.BackupModeArchive) {
		cfg.ArtifactNamingContext = executor.ArtifactNamingContext{
			AgentID:       resolvedPolicy.AgentID,
			PolicyID:      resolvedPolicy.PolicyID,
			PolicyName:    resolvedPolicy.PolicyName,
			BackupSources: append([]protocol.BackupSource(nil), resolvedPolicy.BackupSources...),
			Docker:        dockerMetadata,
			Database:      databaseResult.Metadata,
		}
	}
	manifestDoc, manifestCleanup, err := h.prepareBackupManifest(startedAt, agentID, resolvedPolicy, cfg, dockerMetadata, databaseResult.Metadata)
	if manifestCleanup != nil {
		defer manifestCleanup()
	}
	if err != nil {
		log.Printf("prepare backup manifest failed: %v", err)
		taskLogs.Error("manifest", "prepare backup manifest failed: "+err.Error())
		h.sendTaskResultWithID(messageID, h.failedPolicyTaskResult(agentID, "manifest: "+err.Error(), startedAt, resolvedPolicy))
		return
	}
	if manifestDoc != nil {
		cfg = applyManifestToExecutorConfig(cfg, manifestDoc)
		taskLogs.Info("manifest", "prepared backup content manifest")
	}
	taskLogs.Info("backup", "starting backup")
	result := h.backupRunnerWithProgress(ctx, cfg, h.backupProgressCallback(messageID, agentID, taskLogs))
	if result.PolicyName == "" && policyPayload != nil {
		result.PolicyName = policyPayload.PolicyName
	}
	if dockerMetadata != nil {
		result.Docker = dockerMetadata
	}
	if databaseResult.Metadata != nil && len(databaseResult.Metadata.Dumps) > 0 {
		result.Database = databaseResult.Metadata
	}
	if manifestDoc != nil {
		attachArtifactToManifest(manifestDoc, result)
		if len(result.ManifestWarnings) > 0 {
			manifestDoc.Warnings = append(manifestDoc.Warnings, result.ManifestWarnings...)
		}
		if result.ArtifactNaming != nil {
			manifestDoc.ArtifactNaming = result.ArtifactNaming
			manifestDoc.ContextName = result.ArtifactNaming.ContextName
			manifestDoc.SiteName = result.ArtifactNaming.SiteName
			manifestDoc.SourceType = result.ArtifactNaming.SourceType
		}
		result.Manifest = manifestDoc
	}
	if ctx.Err() == context.Canceled {
		result.Status = "cancelled"
		if result.ErrorLog == "" {
			result.ErrorLog = ctx.Err().Error()
		}
	} else if result.Status == "success" && cfg.PostBackupHook != nil {
		if err := runPolicyHookWithLogs(ctx, h.configDir, cfg.PostBackupHook, "post_backup_hook", taskLogs); err != nil {
			log.Printf("post-backup hook failed: %v", err)
			taskLogs.Error("post_backup_hook", "post-backup hook failed: "+err.Error())
			result.Status = "failed"
			result.ErrorLog = appendHookError(result.ErrorLog, "post_backup_hook: "+err.Error())
		}
	}
	if result.Status == "success" {
		taskLogs.Info("complete", "backup completed successfully")
	} else {
		taskLogs.Error("complete", "backup finished with status "+result.Status+": "+result.ErrorLog)
	}
	h.sendTaskResultWithID(messageID, result.ToProtocol(agentID, startedAt))

	if result.Status == "success" && len(result.Snapshots) > 0 {
		go h.warmSnapshotCache(context.Background(), cfg, result.Snapshots)
	}
}

func (h *Handler) prepareBackupManifest(startedAt time.Time, agentID string, policyPayload *protocol.PolicyPushPayload, cfg executor.ExecutorConfig, dockerMetadata *protocol.DockerBackupMetadata, databaseMetadata *protocol.DatabaseBackupMetadata) (*protocol.BackupContentManifest, func(), error) {
	stageDir, err := os.MkdirTemp(h.configDir, "backup-manifest-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(stageDir) }
	manifestPath := filepath.Join(stageDir, protocol.BackupContentManifestName)
	backupDirs := append([]string(nil), cfg.BackupDirs...)
	if !strings.EqualFold(strings.TrimSpace(cfg.BackupMode), protocol.BackupModeArchive) {
		backupDirs = appendUniqueStrings(backupDirs, manifestPath)
	}
	manifestDoc := agentmanifest.Build(agentmanifest.BuildInput{
		AgentID:       agentID,
		AgentVersion:  h.agentVersion,
		GeneratedAt:   startedAt,
		Policy:        policyPayload,
		BackupMode:    cfg.BackupMode,
		ArchiveFormat: cfg.ArchiveFormat,
		BackupDirs:    backupDirs,
		Excludes:      cfg.Excludes,
		Docker:        dockerMetadata,
		Database:      databaseMetadata,
		ContextName:   backupContextName(policyPayload, dockerMetadata, databaseMetadata),
		SourceType:    artifactnaming.InferSourceType(policyPayload.BackupSources, dockerMetadata, databaseMetadata),
	})
	raw, err := json.MarshalIndent(manifestDoc, "", "  ")
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		cleanup()
		return nil, nil, err
	}
	manifestDoc.Artifact = &protocol.ManifestArtifact{
		Name: protocol.BackupContentManifestName,
		Path: manifestPath,
	}
	return manifestDoc, cleanup, nil
}

func applyManifestToExecutorConfig(cfg executor.ExecutorConfig, manifestDoc *protocol.BackupContentManifest) executor.ExecutorConfig {
	if manifestDoc == nil || manifestDoc.Artifact == nil || manifestDoc.Artifact.Path == "" {
		return cfg
	}
	if strings.EqualFold(strings.TrimSpace(cfg.BackupMode), protocol.BackupModeArchive) {
		archiveManifest := *manifestDoc
		archiveManifest.Artifact = nil
		raw, err := json.MarshalIndent(archiveManifest, "", "  ")
		if err == nil {
			raw = append(raw, '\n')
			cfg.ExtraArchiveFiles = append(cfg.ExtraArchiveFiles, executor.ArchiveExtraFile{
				Name: protocol.BackupContentManifestName,
				Data: raw,
			})
		}
		return cfg
	}
	cfg.BackupDirs = appendUniqueStrings(cfg.BackupDirs, manifestDoc.Artifact.Path)
	return cfg
}

func attachArtifactToManifest(manifestDoc *protocol.BackupContentManifest, result executor.TaskResult) {
	if manifestDoc == nil {
		return
	}
	if strings.EqualFold(strings.TrimSpace(result.BackupMode), protocol.BackupModeArchive) {
		manifestDoc.Artifact = &protocol.ManifestArtifact{
			Name:        result.ArtifactName,
			Path:        result.ArtifactPath,
			Format:      result.ArchiveFormat,
			ContentType: result.ArtifactContentType,
			Size:        result.ArtifactSize,
		}
		return
	}
	if manifestDoc.Artifact == nil {
		manifestDoc.Artifact = &protocol.ManifestArtifact{Name: protocol.BackupContentManifestName}
	}
	manifestDoc.Artifact.Format = protocol.BackupModeSnapshot
}

func (h *Handler) resolvePolicyBackupSources(ctx context.Context, policyPayload *protocol.PolicyPushPayload) (*protocol.PolicyPushPayload, *protocol.DockerBackupMetadata, error) {
	if policyPayload == nil {
		return nil, nil, errors.New("policy payload is nil")
	}
	resolved := *policyPayload
	resolved.BackupDirs = append([]string(nil), policyPayload.BackupDirs...)
	if len(policyPayload.BackupSources) == 0 {
		return &resolved, nil, nil
	}
	dockerPaths, metadata, err := agentdocker.Resolve(ctx, h.dockerAPI, policyPayload.BackupSources)
	if err != nil {
		return nil, nil, err
	}
	resolved.BackupDirs = appendUniqueStrings(resolved.BackupDirs, dockerPaths...)
	return &resolved, metadata, nil
}

func (h *Handler) prepareDatabaseBackupSources(ctx context.Context, sources []protocol.BackupSource, taskLogs *taskLogEmitter) (agentdatabase.Result, func(), error) {
	if h.databasePrepare == nil {
		return agentdatabase.Result{}, func() {}, nil
	}
	return h.databasePrepare(ctx, agentdatabase.Config{
		ConfigDir: h.configDir,
		Sources:   sources,
		TaskLog: func(level string, phase string, stream string, line string) {
			if taskLogs != nil {
				taskLogs.Emit(level, phase, stream, line)
			}
		},
	})
}

func (h *Handler) warmSnapshotCache(ctx context.Context, cfg executor.ExecutorConfig, snapshots []executor.SnapshotInfo) {
	if h.snapshotCache == nil {
		return
	}

	liveSnapshotIDs := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.ID != "" {
			liveSnapshotIDs = append(liveSnapshotIDs, snapshot.ID)
		}
	}
	if err := h.snapshotCache.Sync(liveSnapshotIDs); err != nil {
		log.Printf("sync snapshot cache failed: %v", err)
	}

	for _, snapshot := range snapshots {
		if snapshot.ID == "" || h.snapshotCache.Has(snapshot.ID) {
			continue
		}
		select {
		case <-ctx.Done():
			return
		default:
		}

		entries, err := h.snapshotBrowseRunner(ctx, cfg, snapshot.ID, "")
		if err != nil {
			log.Printf("warm snapshot cache %s failed: %v", snapshot.ID, err)
			continue
		}
		if err := h.snapshotCache.Put(snapshot.ID, entries); err != nil {
			log.Printf("write snapshot cache %s failed: %v", snapshot.ID, err)
		}
	}
}

func (h *Handler) backupProgressCallback(messageID string, agentID string, logs *taskLogEmitter) executor.ProgressCallback {
	var mu sync.Mutex
	var lastPhase string
	var lastSentAt time.Time
	var lastBytesDone int64
	var lastBytesAt time.Time
	var sentMeasuredProgress bool

	return func(phase string, progress *executor.BackupProgress) {
		now := time.Now()
		mu.Lock()
		if phase != "" && phase != lastPhase {
			logs.Info(phase, "entered "+phase+" phase")
		}
		hasMeasuredProgress := progress != nil
		if phase == lastPhase && !lastSentAt.IsZero() && now.Sub(lastSentAt) < backupProgressThrottleInterval && (!hasMeasuredProgress || sentMeasuredProgress) {
			mu.Unlock()
			return
		}

		payload := protocol.BackupProgressPayload{
			AgentID: agentID,
			Phase:   phase,
		}
		if progress != nil {
			payload.PercentDone = progress.PercentDone
			payload.TotalFiles = progress.TotalFiles
			payload.FilesDone = progress.FilesDone
			payload.TotalBytes = progress.TotalBytes
			payload.BytesDone = progress.BytesDone
			payload.CurrentFile = progress.CurrentFile
			if !lastBytesAt.IsZero() {
				bytesDelta := progress.BytesDone - lastBytesDone
				timeDelta := now.Sub(lastBytesAt)
				if bytesDelta > 0 && timeDelta > 0 {
					payload.BytesPerSec = int64(float64(bytesDelta) / timeDelta.Seconds())
				}
			}
		}

		h.sendBackupProgress(messageID, payload)
		lastPhase = phase
		lastSentAt = now
		if progress != nil {
			lastBytesDone = progress.BytesDone
			lastBytesAt = now
			sentMeasuredProgress = true
		}
		mu.Unlock()
	}
}

func (h *Handler) sendBackupProgress(messageID string, payload protocol.BackupProgressPayload) {
	msg, err := protocol.NewMessage(protocol.TypeBackupProgress, payload)
	if err != nil {
		log.Printf("create backup progress failed: %v", err)
		return
	}
	if messageID != "" {
		msg.ID = messageID
	}
	if err := h.sendMessage(*msg); err != nil {
		log.Printf("send backup progress failed: %v", err)
	}
}

func (h *Handler) sendPolicyAck(messageID string, agentID string, success bool, errorText string) {
	payload := protocol.PolicyAckPayload{
		AgentID: agentID,
		Success: success,
		Error:   errorText,
	}
	msg, err := protocol.NewMessage(protocol.TypePolicyAck, payload)
	if err != nil {
		log.Printf("create policy ack failed: %v", err)
		return
	}
	msg.ID = messageID
	h.sendMessage(*msg)
}

func (h *Handler) sendTaskResult(payload protocol.TaskResultPayload) {
	h.sendTaskResultWithID("", payload)
}

func (h *Handler) sendTaskResultWithID(messageID string, payload protocol.TaskResultPayload) {
	msg, err := protocol.NewMessage(protocol.TypeTaskResult, payload)
	if err != nil {
		log.Printf("create task result failed: %v", err)
		return
	}
	if messageID != "" {
		msg.ID = messageID
	}
	if err := h.sendMessage(*msg); err != nil {
		log.Printf("send task result failed: %v", err)
		h.persistPendingResult(messageID, payload)
	}
}

func (h *Handler) FlushPendingResults() {
	if h.policyStore == nil {
		return
	}
	h.pendingResultsMu.Lock()
	defer h.pendingResultsMu.Unlock()

	results, err := h.policyStore.LoadPendingResults()
	if err != nil {
		log.Printf("load pending results failed: %v", err)
		return
	}
	if len(results) == 0 {
		return
	}

	remaining := make([]policy.PendingTaskResult, 0)
	for _, result := range results {
		msg, err := protocol.NewMessage(protocol.TypeTaskResult, result.Payload)
		if err != nil {
			log.Printf("create pending task result failed: %v", err)
			remaining = append(remaining, result)
			continue
		}
		if result.MessageID != "" {
			msg.ID = result.MessageID
		}
		if err := h.sendMessage(*msg); err != nil {
			log.Printf("send pending task result failed: %v", err)
			remaining = append(remaining, result)
		}
	}

	if len(remaining) == 0 {
		if err := h.policyStore.ClearPendingResults(); err != nil {
			log.Printf("clear pending results failed: %v", err)
		}
		return
	}
	if err := h.policyStore.SavePendingResults(remaining); err != nil {
		log.Printf("save remaining pending results failed: %v", err)
	}
}

func (h *Handler) sendMessage(msg protocol.Message) error {
	if h.send == nil {
		return nil
	}
	return h.send(msg)
}

func (h *Handler) persistPendingResult(messageID string, result protocol.TaskResultPayload) {
	if h.policyStore == nil {
		return
	}
	h.pendingResultsMu.Lock()
	defer h.pendingResultsMu.Unlock()

	results, err := h.policyStore.LoadPendingResults()
	if err != nil {
		log.Printf("load pending results failed: %v", err)
		results = nil
	}
	results = append(results, policy.PendingTaskResult{MessageID: messageID, Payload: result})
	if err := h.policyStore.SavePendingResults(results); err != nil {
		log.Printf("save pending result failed: %v", err)
	}
}

func (h *Handler) failedTaskResult(agentID string, errorText string, startedAt time.Time) protocol.TaskResultPayload {
	return h.failedTypedTaskResult(agentID, "backup", "", errorText, startedAt)
}

func (h *Handler) failedPolicyTaskResult(agentID string, errorText string, startedAt time.Time, policyPayload *protocol.PolicyPushPayload) protocol.TaskResultPayload {
	result := h.failedTaskResult(agentID, errorText, startedAt)
	if policyPayload != nil {
		result.PolicyName = policyPayload.PolicyName
	}
	return result
}

func (h *Handler) failedTypedTaskResult(agentID string, taskType string, snapshotID string, errorText string, startedAt time.Time) protocol.TaskResultPayload {
	return executor.TaskResult{
		Type:       taskType,
		Status:     "failed",
		DurationMs: 0,
		SnapshotID: snapshotID,
		ErrorLog:   errorText,
	}.ToProtocol(agentID, startedAt)
}

func toExecutorRetention(retention protocol.RetentionPolicy) executor.RetentionPolicy {
	return executor.RetentionPolicy{
		KeepLast:    retention.KeepLast,
		KeepDaily:   retention.KeepDaily,
		KeepWeekly:  retention.KeepWeekly,
		KeepMonthly: retention.KeepMonthly,
	}
}

func executorConfigForPolicy(configDir string, policyPayload *protocol.PolicyPushPayload) executor.ExecutorConfig {
	return executor.ExecutorConfig{
		ConfigDir:                configDir,
		RepoPath:                 policyPayload.Storage.RepoPath,
		BackupDirs:               append([]string(nil), policyPayload.BackupDirs...),
		Excludes:                 append([]string(nil), policyPayload.ExcludePatterns...),
		Retention:                toExecutorRetention(policyPayload.Retention),
		RcloneArgs:               copyStringMap(policyPayload.Storage.RcloneArgs),
		PlainBackup:              policyPayload.PlainBackup,
		BackupMode:               policyPayload.BackupMode,
		ArchiveFormat:            policyPayload.ArchiveFormat,
		ArtifactContextName:      policyPayload.ArtifactContextName,
		ArchiveRemoteDirTemplate: policyPayload.ArchiveRemoteDirTemplate,
		ArchiveNameTemplate:      policyPayload.ArchiveNameTemplate,
		PreBackupHook:            clonePolicyHook(policyPayload.PreBackupHook),
		PostBackupHook:           clonePolicyHook(policyPayload.PostBackupHook),
	}
}

func backupContextName(policyPayload *protocol.PolicyPushPayload, dockerMetadata *protocol.DockerBackupMetadata, databaseMetadata *protocol.DatabaseBackupMetadata) string {
	if policyPayload == nil {
		return ""
	}
	if strings.TrimSpace(policyPayload.ArtifactContextName) != "" {
		return strings.TrimSpace(policyPayload.ArtifactContextName)
	}
	return artifactnaming.InferContextName(artifactnaming.Context{
		AgentID:       policyPayload.AgentID,
		PolicyID:      policyPayload.PolicyID,
		ContextName:   policyPayload.ArtifactContextName,
		ArchiveFormat: policyPayload.ArchiveFormat,
		Sources:       policyPayload.BackupSources,
		Docker:        dockerMetadata,
		Database:      databaseMetadata,
	})
}

func clonePolicyHook(hook *protocol.PolicyHook) *protocol.PolicyHook {
	if hook == nil {
		return nil
	}
	cloned := *hook
	cloned.Command = strings.TrimSpace(cloned.Command)
	return &cloned
}

func appendHookError(existing string, hookError string) string {
	hookError = strings.TrimSpace(hookError)
	if hookError == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
		return hookError
	}
	return existing + "\n" + hookError
}

func runPolicyHook(ctx context.Context, configDir string, hook *protocol.PolicyHook) error {
	return runPolicyHookWithLogs(ctx, configDir, hook, "hook", nil)
}

func runPolicyHookWithLogs(ctx context.Context, configDir string, hook *protocol.PolicyHook, phase string, logs *taskLogEmitter) error {
	if hook == nil {
		return nil
	}
	command := strings.TrimSpace(hook.Command)
	if command == "" {
		return errors.New("hook command is empty")
	}
	if phase == "" {
		phase = "hook"
	}
	logs.Info(phase, "running hook")

	runCtx := ctx
	var cancel context.CancelFunc
	if hook.TimeoutSeconds > 0 {
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(hook.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	if configDir != "" {
		cmd.Dir = configDir
	}
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		if err == nil {
			logs.Stdout(phase, string(output))
		} else {
			logs.Stderr(phase, string(output))
		}
	}
	if err == nil {
		logs.Info(phase, "hook completed")
		return nil
	}
	trimmed := strings.TrimSpace(string(output))
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		logs.Error(phase, "hook timed out")
		if trimmed != "" {
			return errors.New("timeout: " + trimmed)
		}
		return errors.New("timeout")
	}
	logs.Error(phase, "hook failed")
	if trimmed != "" {
		return errors.New(trimmed)
	}
	return err
}

func (h *Handler) ensureRcloneConf(policyPayload *protocol.PolicyPushPayload) error {
	if policyPayload == nil {
		return errors.New("policy payload is nil")
	}
	if h.configDir == "" {
		return errors.New("config dir not configured")
	}
	if err := os.MkdirAll(h.configDir, 0o700); err != nil {
		return err
	}
	return executor.WriteRcloneConf(filepath.Join(h.configDir, "rclone.conf"), executor.RcloneConfig{
		Type:         policyPayload.Storage.RcloneType,
		Params:       policyPayload.Storage.RcloneConfig,
		PassObscured: policyPayload.Storage.RclonePassObscured,
	})
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func appendUniqueStrings(values []string, more ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(more))
	result := make([]string, 0, len(values)+len(more))
	for _, value := range append(values, more...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func runBackup(ctx context.Context, cfg executor.ExecutorConfig) executor.TaskResult {
	if strings.EqualFold(strings.TrimSpace(cfg.BackupMode), protocol.BackupModeArchive) {
		return executor.RunArchiveJob(ctx, cfg)
	}
	return executor.NewExecutor(cfg).RunBackupJob(ctx)
}

func runBackupWithProgress(ctx context.Context, cfg executor.ExecutorConfig, progressFn executor.ProgressCallback) executor.TaskResult {
	if strings.EqualFold(strings.TrimSpace(cfg.BackupMode), protocol.BackupModeArchive) {
		return executor.RunArchiveJob(ctx, cfg)
	}
	return executor.NewExecutor(cfg).RunBackupJobWithProgress(ctx, progressFn)
}

func runRestore(ctx context.Context, cfg executor.ExecutorConfig, snapshotID string, target string, includePaths []string) error {
	passwordFile := filepath.Join(cfg.ConfigDir, ".restic-password")
	usePlain := cfg.PlainBackup || !executor.HasPasswordFile(passwordFile)
	if usePlain {
		runner := executor.PlainRunner{
			RcloneConfPath:  filepath.Join(cfg.ConfigDir, "rclone.conf"),
			RepoPath:        cfg.RepoPath,
			RcloneExtraArgs: copyStringMap(cfg.RcloneArgs),
		}
		return runner.RestoreSnapshot(ctx, snapshotID, target, includePaths)
	}
	runner := executor.ResticRunner{
		RcloneConfPath:  filepath.Join(cfg.ConfigDir, "rclone.conf"),
		PasswordFile:    passwordFile,
		RepoPath:        cfg.RepoPath,
		RcloneExtraArgs: copyStringMap(cfg.RcloneArgs),
	}
	return runner.RestoreSnapshot(ctx, snapshotID, target, includePaths)
}

func runDockerRestore(ctx context.Context, request protocol.DockerRestoreRequest) error {
	return agentdocker.Restore(ctx, request, nil)
}

func runSnapshotList(ctx context.Context, cfg executor.ExecutorConfig) ([]executor.SnapshotInfo, error) {
	passwordFile := filepath.Join(cfg.ConfigDir, ".restic-password")
	if !executor.HasPasswordFile(passwordFile) {
		runner := executor.PlainRunner{
			RcloneConfPath:  filepath.Join(cfg.ConfigDir, "rclone.conf"),
			RepoPath:        cfg.RepoPath,
			RcloneExtraArgs: copyStringMap(cfg.RcloneArgs),
		}
		return runner.ListSnapshots(ctx)
	}
	runner := executor.ResticRunner{
		RcloneConfPath:  filepath.Join(cfg.ConfigDir, "rclone.conf"),
		PasswordFile:    passwordFile,
		RepoPath:        cfg.RepoPath,
		RcloneExtraArgs: copyStringMap(cfg.RcloneArgs),
	}
	return runner.ListSnapshots(ctx)
}

func runSnapshotBrowse(ctx context.Context, cfg executor.ExecutorConfig, snapshotID string, path string) ([]executor.SnapshotFileEntry, error) {
	passwordFile := filepath.Join(cfg.ConfigDir, ".restic-password")
	if !executor.HasPasswordFile(passwordFile) {
		runner := executor.PlainRunner{
			RcloneConfPath:  filepath.Join(cfg.ConfigDir, "rclone.conf"),
			RepoPath:        cfg.RepoPath,
			RcloneExtraArgs: copyStringMap(cfg.RcloneArgs),
		}
		return runner.LsSnapshot(ctx, snapshotID, path)
	}
	runner := executor.ResticRunner{
		RcloneConfPath:  filepath.Join(cfg.ConfigDir, "rclone.conf"),
		PasswordFile:    passwordFile,
		RepoPath:        cfg.RepoPath,
		RcloneExtraArgs: copyStringMap(cfg.RcloneArgs),
	}
	if path != "" {
		return runner.LsSnapshot(ctx, snapshotID, path)
	}
	return runner.LsSnapshot(ctx, snapshotID)
}

func runBackupVerification(ctx context.Context, cfg executor.ExecutorConfig, settings protocol.BackupVerificationSettings) executor.TaskResult {
	start := time.Now()
	passwordFile := filepath.Join(cfg.ConfigDir, ".restic-password")
	if cfg.PlainBackup || !executor.HasPasswordFile(passwordFile) {
		return executor.TaskResult{
			Type:       "verify",
			Status:     "failed",
			DurationMs: time.Since(start).Milliseconds(),
			ErrorLog:   "restic backup verification requires an encrypted restic repository",
			Verification: &protocol.BackupVerificationResult{
				Status: protocol.VerificationStatusFailed,
				Error:  "restic backup verification requires an encrypted restic repository",
				Checks: []protocol.BackupVerificationCheck{{
					Code:     "repository_type",
					Status:   protocol.VerificationCheckStatusFailed,
					Severity: protocol.VerificationSeverityError,
					Message:  "repository is not a restic snapshot repository",
				}},
			},
		}
	}
	runner := executor.ResticRunner{
		RcloneConfPath:  filepath.Join(cfg.ConfigDir, "rclone.conf"),
		PasswordFile:    passwordFile,
		RepoPath:        cfg.RepoPath,
		RcloneExtraArgs: copyStringMap(cfg.RcloneArgs),
	}
	return executor.RunVerificationJob(ctx, runner, executor.VerificationConfig{
		SampleCount:          settings.SampleCount,
		SampleRestoreEnabled: settings.SampleRestoreEnabled,
		TempDir:              filepath.Join(cfg.ConfigDir, "verify-tmp"),
	})
}

func (h *Handler) handleRestoreReq(msg protocol.Message) {
	req, err := protocol.ParsePayload[protocol.RestoreReqPayload](&msg)
	if err != nil {
		log.Printf("parse restore request failed: %v", err)
		h.sendTaskResultWithID(msg.ID, h.failedTypedTaskResult(h.agentID, "restore", "", "parse restore: "+err.Error(), time.Now()))
		return
	}
	if h.policyStore == nil {
		h.sendTaskResultWithID(msg.ID, h.failedTypedTaskResult(h.agentID, "restore", req.SnapshotID, "policy store not configured", time.Now()))
		return
	}

	policyPayload, err := h.policyStore.LoadPolicy()
	if err != nil {
		if os.IsNotExist(err) {
			h.sendTaskResultWithID(msg.ID, h.failedTypedTaskResult(h.agentID, "restore", req.SnapshotID, "no backup policy configured for this agent", time.Now()))
		} else {
			log.Printf("load policy failed: %v", err)
			h.sendTaskResultWithID(msg.ID, h.failedTypedTaskResult(h.agentID, "restore", req.SnapshotID, "load policy: "+err.Error(), time.Now()))
		}
		return
	}
	agentID := policyPayload.AgentID
	if agentID == "" {
		agentID = h.agentID
	}

	startErr := h.tasks.Start(msg.ID, taskTypeRestore, func(ctx context.Context) {
		startedAt := time.Now()
		if err := h.ensureRcloneConf(policyPayload); err != nil {
			h.sendTaskResultWithID(msg.ID, h.failedTypedTaskResult(agentID, "restore", req.SnapshotID, "prepare rclone config: "+err.Error(), startedAt))
			return
		}
		h.sendRestoreProgress(msg.ID, agentID, req.SnapshotID)
		target := req.Target
		includePaths := append([]string(nil), req.IncludePaths...)
		var restoreErr error
		if req.Docker != nil {
			target = "/"
			dockerPaths := dockerRestorePaths(*req.Docker)
			if len(dockerPaths) == 0 {
				restoreErr = errors.New("docker restore has no restore paths")
			} else {
				includePaths = appendUniqueStrings(includePaths, dockerPaths...)
			}
		}
		if restoreErr == nil {
			restoreErr = h.restoreRunner(ctx, executorConfigForPolicy(h.configDir, policyPayload), req.SnapshotID, target, includePaths)
		}
		if restoreErr == nil && req.Docker != nil {
			restoreErr = h.dockerRestoreRunner(ctx, *req.Docker)
		}
		finishedAt := time.Now()
		result := protocol.TaskResultPayload{
			AgentID:    agentID,
			TaskType:   "restore",
			Status:     "success",
			SnapshotID: req.SnapshotID,
			DurationMs: finishedAt.Sub(startedAt).Milliseconds(),
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
		}
		if restoreErr != nil {
			result.Status = "failed"
			result.ErrorLog = restoreErr.Error()
		}
		if ctx.Err() == context.Canceled {
			result.Status = "cancelled"
			if result.ErrorLog == "" {
				result.ErrorLog = ctx.Err().Error()
			}
		}
		h.sendTaskResultWithID(msg.ID, result)
	})
	if startErr != nil {
		h.sendTaskResultWithID(msg.ID, h.failedTypedTaskResult(agentID, "restore", req.SnapshotID, startErr.Error(), time.Now()))
	}
}

func (h *Handler) handleRestorePreflightReq(msg protocol.Message) {
	req, err := protocol.ParsePayload[protocol.RestorePreflightReqPayload](&msg)
	if err != nil {
		log.Printf("parse restore preflight request failed: %v", err)
		h.sendRestorePreflightResp(msg.ID, protocol.RestorePreflightRespPayload{
			AgentID: h.agentID,
			Status:  protocol.RestorePreflightStatusFailed,
			Checks: []protocol.RestorePreflightCheck{
				restorePreflightCheck("invalid_request", protocol.RestorePreflightSeverityError, "invalid restore preflight request", err.Error()),
			},
			Error: err.Error(),
		})
		return
	}
	agentID := req.AgentID
	if agentID == "" {
		agentID = h.agentID
	}

	checks := h.restorePreflightChecks(context.Background(), *req)
	payload := protocol.RestorePreflightRespPayload{
		AgentID:    agentID,
		SnapshotID: req.SnapshotID,
		Status:     restorePreflightStatus(checks),
		Checks:     checks,
	}
	h.sendRestorePreflightResp(msg.ID, payload)
}

func (h *Handler) restorePreflightChecks(ctx context.Context, req protocol.RestorePreflightReqPayload) []protocol.RestorePreflightCheck {
	checks := []protocol.RestorePreflightCheck{
		restorePreflightCheck("agent_reachable", protocol.RestorePreflightSeverityInfo, "target agent received restore preflight request", ""),
	}
	restoreMode := strings.TrimSpace(req.RestoreMode)
	if restoreMode == "" {
		restoreMode = protocol.RestoreModeFiles
	}
	if restoreMode == protocol.RestoreModeDockerContainer || req.Docker != nil {
		if req.Docker == nil {
			checks = append(checks, restorePreflightCheck("docker_metadata", protocol.RestorePreflightSeverityError, "docker restore metadata is missing", ""))
			return checks
		}
		checks = append(checks, agentdocker.PreflightRestore(ctx, h.dockerAPI, *req.Docker)...)
		return checks
	}
	checks = append(checks, fileRestorePreflightChecks(req.Target, req.IncludePaths)...)
	return checks
}

func fileRestorePreflightChecks(target string, includePaths []string) []protocol.RestorePreflightCheck {
	checks := []protocol.RestorePreflightCheck{
		restorePreflightCheck("restore_mode", protocol.RestorePreflightSeverityInfo, "file restore mode selected", ""),
	}
	if len(includePaths) > 0 {
		checks = append(checks, restorePreflightCheck("include_paths", protocol.RestorePreflightSeverityInfo, "selected snapshot paths will be restored", strings.Join(includePaths, "\n")))
	}
	if err := ensureRestoreTargetWritable(target); err != nil {
		checks = append(checks, restorePreflightCheck("target_path_writable", protocol.RestorePreflightSeverityError, "target path is not writable", err.Error()))
		return checks
	}
	checks = append(checks, restorePreflightCheck("target_path_writable", protocol.RestorePreflightSeverityInfo, "target path is writable", target))
	return checks
}

func ensureRestoreTargetWritable(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return errors.New("target path is required")
	}
	cleaned := filepath.Clean(target)
	if !filepath.IsAbs(cleaned) {
		return errors.New("target path must be absolute")
	}
	if info, err := os.Stat(cleaned); err == nil && !info.IsDir() {
		return errors.New("target path exists and is not a directory")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(cleaned, 0o755); err != nil {
		return err
	}
	probe, err := os.CreateTemp(cleaned, ".vaultfleet-restore-preflight-*")
	if err != nil {
		return err
	}
	probePath := probe.Name()
	if _, err := probe.Write([]byte("ok")); err != nil {
		_ = probe.Close()
		_ = os.Remove(probePath)
		return err
	}
	if err := probe.Close(); err != nil {
		_ = os.Remove(probePath)
		return err
	}
	return os.Remove(probePath)
}

func restorePreflightStatus(checks []protocol.RestorePreflightCheck) string {
	for _, check := range checks {
		if check.Severity == protocol.RestorePreflightSeverityError {
			return protocol.RestorePreflightStatusFailed
		}
	}
	return protocol.RestorePreflightStatusPassed
}

func restorePreflightCheck(code string, severity string, message string, detail string) protocol.RestorePreflightCheck {
	return protocol.RestorePreflightCheck{
		Code:     code,
		Severity: severity,
		Message:  message,
		Detail:   detail,
	}
}

func (h *Handler) sendRestorePreflightResp(messageID string, payload protocol.RestorePreflightRespPayload) {
	resp, err := protocol.NewMessage(protocol.TypeRestorePreflightResp, payload)
	if err != nil {
		log.Printf("create restore preflight response failed: %v", err)
		return
	}
	resp.ID = messageID
	if err := h.sendMessage(*resp); err != nil {
		log.Printf("send restore preflight response failed: %v", err)
	}
}

func dockerRestorePaths(request protocol.DockerRestoreRequest) []string {
	var paths []string
	for _, source := range request.Sources {
		paths = append(paths, source.ResolvedPaths...)
	}
	return appendUniqueStrings(nil, paths...)
}

func (h *Handler) sendRestoreProgress(messageID string, agentID string, snapshotID string) {
	payload := protocol.RestoreProgressPayload{
		AgentID:    agentID,
		SnapshotID: snapshotID,
		Percent:    0,
	}
	msg, err := protocol.NewMessage(protocol.TypeRestoreProgress, payload)
	if err != nil {
		log.Printf("create restore progress failed: %v", err)
		return
	}
	msg.ID = messageID
	if err := h.sendMessage(*msg); err != nil {
		log.Printf("send restore progress failed: %v", err)
	}
}

func (h *Handler) handleSnapshotListReq(msg protocol.Message) {
	req, err := protocol.ParsePayload[protocol.SnapshotListReqPayload](&msg)
	agentID := h.agentID
	if err != nil {
		log.Printf("parse snapshot list request failed: %v", err)
		h.sendSnapshotListResp(msg.ID, agentID, nil, "parse snapshot list: "+err.Error())
		return
	}
	if req.AgentID != "" {
		agentID = req.AgentID
	}
	if h.policyStore == nil {
		h.sendSnapshotListResp(msg.ID, agentID, nil, "policy store not configured")
		return
	}

	policyPayload, err := h.policyStore.LoadPolicy()
	if err != nil {
		if os.IsNotExist(err) {
			h.sendSnapshotListResp(msg.ID, agentID, nil, "no backup policy configured for this agent")
		} else {
			log.Printf("load policy failed: %v", err)
			h.sendSnapshotListResp(msg.ID, agentID, nil, "load policy: "+err.Error())
		}
		return
	}
	if agentID == "" {
		agentID = policyPayload.AgentID
	}

	finalAgentID := agentID
	_ = h.tasks.Start(msg.ID, taskTypeQuery, func(ctx context.Context) {
		if err := h.ensureRcloneConf(policyPayload); err != nil {
			h.sendSnapshotListResp(msg.ID, finalAgentID, nil, "prepare rclone config: "+err.Error())
			return
		}
		snapshots, err := h.snapshotListRunner(ctx, executorConfigForPolicy(h.configDir, policyPayload))
		if err != nil {
			h.sendSnapshotListResp(msg.ID, finalAgentID, nil, err.Error())
			return
		}
		h.sendSnapshotListResp(msg.ID, finalAgentID, snapshotsToProtocol(snapshots), "")
	})
}

func (h *Handler) sendSnapshotListResp(messageID string, agentID string, snapshots []protocol.SnapshotInfo, errorText string) {
	payload := protocol.SnapshotListRespPayload{
		AgentID:   agentID,
		Snapshots: snapshots,
		Error:     errorText,
	}
	msg, err := protocol.NewMessage(protocol.TypeSnapshotListResp, payload)
	if err != nil {
		log.Printf("create snapshot list response failed: %v", err)
		return
	}
	msg.ID = messageID
	if err := h.sendMessage(*msg); err != nil {
		log.Printf("send snapshot list response failed: %v", err)
	}
}

func (h *Handler) snapshotEntriesForBrowse(ctx context.Context, cfg executor.ExecutorConfig, snapshotID string) ([]executor.SnapshotFileEntry, error) {
	if h.snapshotCache != nil {
		entries, ok, err := h.snapshotCache.Get(snapshotID)
		if err != nil {
			log.Printf("read snapshot cache %s failed: %v", snapshotID, err)
		} else if ok {
			return entries, nil
		}
	}

	entries, err := h.snapshotBrowseRunner(ctx, cfg, snapshotID, "")
	if err != nil {
		return nil, err
	}

	if h.snapshotCache != nil {
		if err := h.snapshotCache.Put(snapshotID, entries); err != nil {
			log.Printf("write snapshot cache %s failed: %v", snapshotID, err)
		}
	}
	return entries, nil
}

func (h *Handler) handleSnapshotBrowseReq(msg protocol.Message) {
	req, err := protocol.ParsePayload[protocol.SnapshotBrowseReqPayload](&msg)
	if err != nil {
		log.Printf("parse snapshot browse request failed: %v", err)
		h.sendSnapshotBrowseResp(msg.ID, "", nil, "parse snapshot browse: "+err.Error())
		return
	}
	if h.policyStore == nil {
		h.sendSnapshotBrowseResp(msg.ID, req.SnapshotID, nil, "policy store not configured")
		return
	}

	policyPayload, err := h.policyStore.LoadPolicy()
	if err != nil {
		if os.IsNotExist(err) {
			h.sendSnapshotBrowseResp(msg.ID, req.SnapshotID, nil, "no backup policy configured for this agent")
		} else {
			log.Printf("load policy failed: %v", err)
			h.sendSnapshotBrowseResp(msg.ID, req.SnapshotID, nil, "load policy: "+err.Error())
		}
		return
	}

	snapshotID := req.SnapshotID
	path := req.Path
	cfg := executorConfigForPolicy(h.configDir, policyPayload)
	_ = h.tasks.Start(msg.ID, taskTypeQuery, func(ctx context.Context) {
		if err := h.ensureRcloneConf(policyPayload); err != nil {
			h.sendSnapshotBrowseResp(msg.ID, snapshotID, nil, "prepare rclone config: "+err.Error())
			return
		}
		entries, err := h.snapshotEntriesForBrowse(ctx, cfg, snapshotID)
		if err != nil {
			h.sendSnapshotBrowseResp(msg.ID, snapshotID, nil, err.Error())
			return
		}

		if path != "" {
			entries = filterDirectChildren(entries, path)
		} else {
			entries = filterTopLevelEntries(entries)
		}

		protoEntries := make([]protocol.SnapshotFileEntry, len(entries))
		for i, entry := range entries {
			protoEntries[i] = protocol.SnapshotFileEntry{
				Path:  entry.Path,
				Type:  entry.Type,
				Size:  entry.Size,
				Mtime: entry.Mtime,
			}
		}
		h.sendSnapshotBrowseResp(msg.ID, snapshotID, protoEntries, "")
	})
}

func (h *Handler) sendSnapshotBrowseResp(messageID string, snapshotID string, entries []protocol.SnapshotFileEntry, errorText string) {
	payload := protocol.SnapshotBrowseRespPayload{
		SnapshotID: snapshotID,
		Entries:    entries,
		Error:      errorText,
	}
	if errorText == "" && len(entries) > 0 {
		raw, err := json.Marshal(payload)
		if err != nil {
			payload.Entries = nil
			payload.Error = "encode snapshot browse response: " + err.Error()
		} else if len(raw) > maxSnapshotBrowseResponseBytes {
			payload.Entries = nil
			payload.Error = "snapshot browse response too large; narrow the snapshot contents before browsing"
		}
	}
	msg, err := protocol.NewMessage(protocol.TypeSnapshotBrowseResp, payload)
	if err != nil {
		log.Printf("create snapshot browse response failed: %v", err)
		return
	}
	msg.ID = messageID
	if err := h.sendMessage(*msg); err != nil {
		log.Printf("send snapshot browse response failed: %v", err)
	}
}

func (h *Handler) handleCancelTask(msg protocol.Message) {
	payload, err := protocol.ParsePayload[protocol.CancelTaskPayload](&msg)
	if err != nil {
		log.Printf("parse cancel_task failed: %v", err)
		return
	}
	if payload.MessageID == "" {
		log.Printf("ignore cancel_task with empty message_id")
		return
	}
	if payload.AgentID != "" && h.agentID != "" && payload.AgentID != h.agentID {
		log.Printf("ignore cancel_task for agent %s on agent %s", payload.AgentID, h.agentID)
		return
	}
	if h.tasks.Cancel(payload.MessageID) {
		log.Printf("cancelled task %s", payload.MessageID)
	}
}

func filterDirectChildren(entries []executor.SnapshotFileEntry, parentPath string) []executor.SnapshotFileEntry {
	prefix := strings.TrimSuffix(parentPath, "/") + "/"
	var result []executor.SnapshotFileEntry
	for _, e := range entries {
		if !strings.HasPrefix(e.Path, prefix) {
			continue
		}
		rest := e.Path[len(prefix):]
		if rest == "" || strings.Contains(rest, "/") {
			continue
		}
		result = append(result, e)
	}
	return result
}

func filterTopLevelEntries(entries []executor.SnapshotFileEntry) []executor.SnapshotFileEntry {
	var result []executor.SnapshotFileEntry
	for _, e := range entries {
		path := strings.TrimPrefix(e.Path, "/")
		if path == "" || strings.Contains(path, "/") {
			continue
		}
		result = append(result, e)
	}
	return result
}

func (h *Handler) handleCollectLogsReq(msg protocol.Message) {
	req, err := protocol.ParsePayload[protocol.CollectLogsReqPayload](&msg)
	maxBytes := 5 * 1024 * 1024
	if err == nil && req.MaxBytes > 0 && req.MaxBytes < maxBytes {
		maxBytes = req.MaxBytes
	}

	logs := ""
	errorText := ""
	if h.logFile != "" {
		logs, err = collectLogsFromFile(h.logFile, maxBytes)
	} else {
		logs, err = collectLogs(defaultLogFile, maxBytes)
	}
	if err != nil {
		errorText = err.Error()
	}
	resp, err := protocol.NewMessage(protocol.TypeCollectLogsResp, protocol.CollectLogsRespPayload{
		Logs:  logs,
		Error: errorText,
	})
	if err != nil {
		log.Printf("create collect_logs response failed: %v", err)
		return
	}
	resp.ID = msg.ID
	if err := h.sendMessage(*resp); err != nil {
		log.Printf("send collect_logs response failed: %v", err)
	}
}

func snapshotsToProtocol(snapshots []executor.SnapshotInfo) []protocol.SnapshotInfo {
	result := make([]protocol.SnapshotInfo, 0, len(snapshots))
	for _, snapshot := range snapshots {
		result = append(result, protocol.SnapshotInfo{
			ID:    snapshot.ID,
			Time:  snapshot.Time,
			Paths: append([]string(nil), snapshot.Paths...),
			Size:  snapshot.Size,
		})
	}
	return result
}

func (h *Handler) handleDirBrowseReq(msg protocol.Message) {
	req, err := protocol.ParsePayload[protocol.DirBrowseReqPayload](&msg)
	if err != nil {
		log.Printf("parse directory browse request failed: %v", err)
		return
	}

	if req.Depth <= 0 || req.Depth > 3 {
		req.Depth = 2
	}

	entries, browseErr := h.browse("/", req.Path, req.Depth)
	payload := protocol.DirBrowseRespPayload{
		Path:    req.Path,
		Entries: entries,
	}
	if browseErr != nil {
		payload.Error = browseErr.Error()
		payload.Entries = nil
	}

	resp, err := protocol.NewMessage(protocol.TypeDirBrowseResp, payload)
	if err != nil {
		log.Printf("create directory browse response failed: %v", err)
		return
	}
	resp.ID = msg.ID

	if h.send == nil {
		return
	}
	if err := h.send(*resp); err != nil {
		log.Printf("send directory browse response failed: %v", err)
	}
}

func (h *Handler) handleDockerDiscoveryReq(msg protocol.Message) {
	payload := agentdocker.Discover(context.Background(), h.dockerAPI)
	resp, err := protocol.NewMessage(protocol.TypeDockerDiscoveryResp, payload)
	if err != nil {
		log.Printf("create docker discovery response failed: %v", err)
		return
	}
	resp.ID = msg.ID
	if err := h.sendMessage(*resp); err != nil {
		log.Printf("send docker discovery response failed: %v", err)
	}
}

func (h *Handler) handleDatabaseDiscoveryReq(msg protocol.Message) {
	req, err := protocol.ParsePayload[protocol.DatabaseDiscoveryReqPayload](&msg)
	if err != nil {
		log.Printf("parse database discovery request failed: %v", err)
		return
	}

	timeout := 30 * time.Second
	if req.Source.DumpTimeoutSeconds > 0 {
		timeout = time.Duration(req.Source.DumpTimeoutSeconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	databases, listErr := h.databaseList(ctx, agentdatabase.ListConfig{
		ConfigDir: h.configDir,
		Source:    req.Source,
	})
	payload := protocol.DatabaseDiscoveryRespPayload{
		Available: listErr == nil,
		Databases: databases,
	}
	if listErr != nil {
		payload.Error = listErr.Error()
		payload.Databases = nil
	}

	resp, err := protocol.NewMessage(protocol.TypeDatabaseDiscoveryResp, payload)
	if err != nil {
		log.Printf("create database discovery response failed: %v", err)
		return
	}
	resp.ID = msg.ID
	if err := h.sendMessage(*resp); err != nil {
		log.Printf("send database discovery response failed: %v", err)
	}
}

func (h *Handler) handleDirSizeReq(msg protocol.Message) {
	req, err := protocol.ParsePayload[protocol.DirSizeReqPayload](&msg)
	if err != nil {
		log.Printf("parse directory size request failed: %v", err)
		return
	}

	size, sizeErr := h.dirSizeFunc("/", req.Path)
	payload := protocol.DirSizeRespPayload{
		Path: req.Path,
		Size: size,
	}
	if sizeErr != nil {
		payload.Error = sizeErr.Error()
	}

	resp, err := protocol.NewMessage(protocol.TypeDirSizeResp, payload)
	if err != nil {
		log.Printf("create directory size response failed: %v", err)
		return
	}
	resp.ID = msg.ID

	if h.send == nil {
		return
	}
	if err := h.send(*resp); err != nil {
		log.Printf("send directory size response failed: %v", err)
	}
}

func (h *Handler) handleVersionInfo(msg protocol.Message) {
	if h.updater == nil {
		return
	}
	info, err := protocol.ParsePayload[protocol.VersionInfoPayload](&msg)
	if err != nil {
		log.Printf("parse version info failed: %v", err)
		return
	}
	if info.Version == h.agentVersion {
		return
	}
	log.Printf("master version %s differs from agent version %s, starting update", info.Version, h.agentVersion)
	go func() {
		if err := h.updater.Update(info.Version, info.GitHubRepo); err != nil {
			log.Printf("self-update to %s failed: %v", info.Version, err)
		}
	}()
}

func (h *Handler) handleUpdateAgent(msg protocol.Message) {
	info, err := protocol.ParsePayload[protocol.UpdateAgentPayload](&msg)
	if err != nil {
		log.Printf("parse update agent failed: %v", err)
		h.sendUpdateAgentResp(msg.ID, false, "", "", err.Error())
		return
	}
	if h.updater == nil {
		h.sendUpdateAgentResp(msg.ID, false, info.Version, info.GitHubRepo, "agent self-update is disabled")
		return
	}
	h.sendUpdateAgentResp(msg.ID, true, info.Version, info.GitHubRepo, "")
	log.Printf("master requested update to %s", info.Version)
	go func() {
		if err := h.updater.Update(info.Version, info.GitHubRepo); err != nil {
			log.Printf("self-update to %s failed: %v", info.Version, err)
		}
	}()
}

func (h *Handler) sendUpdateAgentResp(messageID string, accepted bool, version string, githubRepo string, errorText string) {
	resp, err := protocol.NewMessage(protocol.TypeUpdateAgentResp, protocol.UpdateAgentRespPayload{
		Accepted:   accepted,
		Version:    version,
		GitHubRepo: githubRepo,
		Error:      errorText,
	})
	if err != nil {
		log.Printf("create update agent response failed: %v", err)
		return
	}
	resp.ID = messageID
	if err := h.sendMessage(*resp); err != nil {
		log.Printf("send update agent response failed: %v", err)
	}
}
