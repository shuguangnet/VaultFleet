package agent

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"vaultfleet/internal/agent/executor"
	"vaultfleet/internal/agent/filebrowse"
	"vaultfleet/internal/agent/policy"
	"vaultfleet/internal/agent/scheduler"
	"vaultfleet/pkg/protocol"
)

type SendFunc func(protocol.Message) error

type BrowseFunc func(fsRoot string, scanPath string, maxDepth int) ([]protocol.DirEntry, error)

type BackupRunnerFunc func(context.Context, executor.ExecutorConfig) executor.TaskResult

type policyScheduler interface {
	Validate(schedule string) error
	UpdateSchedule(agentID string, schedule string, fn func()) error
	RemoveJob(agentID string)
}

type HandlerConfig struct {
	PolicyStore  *policy.Store
	SendFunc     SendFunc
	BrowseFunc   BrowseFunc
	ConfigDir    string
	AgentID      string
	Scheduler    policyScheduler
	BackupRunner BackupRunnerFunc
}

type Handler struct {
	policyStore   *policy.Store
	send          SendFunc
	browse        BrowseFunc
	configDir     string
	agentID       string
	scheduler     policyScheduler
	backupRunner  BackupRunnerFunc
	backupMu      sync.Mutex
	backupRunning bool
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
	policyScheduler := config.Scheduler
	if policyScheduler == nil {
		defaultScheduler := scheduler.New()
		if err := defaultScheduler.Start(); err != nil {
			log.Printf("start scheduler failed: %v", err)
		}
		policyScheduler = defaultScheduler
	}
	return &Handler{
		policyStore:  config.PolicyStore,
		send:         config.SendFunc,
		browse:       browse,
		configDir:    configDir,
		agentID:      config.AgentID,
		scheduler:    policyScheduler,
		backupRunner: runner,
	}
}

func (h *Handler) Handle(msg protocol.Message) {
	switch msg.Type {
	case protocol.TypePolicyPush:
		h.handlePolicyPush(msg)
	case protocol.TypeBackupNow:
		h.handleBackupNow(msg)
	case protocol.TypeDirBrowseReq:
		h.handleDirBrowseReq(msg)
	}
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
			h.runBackupForPolicy(pushedPolicy.AgentID, pushedPolicy)
		}); err != nil {
			log.Printf("update backup schedule failed: %v", err)
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
		Type:   pushedPolicy.Storage.RcloneType,
		Params: pushedPolicy.Storage.RcloneConfig,
	}); err != nil {
		staged.cleanup()
		return nil, err
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
		h.sendTaskResult(h.failedTaskResult(h.agentID, "parse backup_now: "+err.Error(), time.Now()))
		return
	}

	agentID := backupNow.AgentID
	if agentID == "" {
		agentID = h.agentID
	}
	if h.policyStore == nil {
		h.sendTaskResult(h.failedTaskResult(agentID, "policy store not configured", time.Now()))
		return
	}

	policyPayload, err := h.policyStore.LoadPolicy()
	if err != nil {
		log.Printf("load policy failed: %v", err)
		h.sendTaskResult(h.failedTaskResult(agentID, "load policy: "+err.Error(), time.Now()))
		return
	}
	if agentID == "" {
		agentID = policyPayload.AgentID
	}
	h.runBackupForPolicy(agentID, policyPayload)
}

func (h *Handler) runBackupForPolicy(agentID string, policyPayload *protocol.PolicyPushPayload) {
	if policyPayload == nil {
		return
	}
	if agentID == "" {
		agentID = policyPayload.AgentID
	}
	if !h.beginBackup() {
		h.sendTaskResult(h.failedTaskResult(agentID, "backup already running", time.Now()))
		return
	}
	defer h.endBackup()

	startedAt := time.Now()
	result := h.backupRunner(context.Background(), executor.ExecutorConfig{
		ConfigDir:  h.configDir,
		RepoPath:   policyPayload.Storage.RepoPath,
		BackupDirs: append([]string(nil), policyPayload.BackupDirs...),
		Excludes:   append([]string(nil), policyPayload.ExcludePatterns...),
		Retention:  toExecutorRetention(policyPayload.Retention),
	})
	h.sendTaskResult(result.ToProtocol(agentID, startedAt))
}

func (h *Handler) beginBackup() bool {
	h.backupMu.Lock()
	defer h.backupMu.Unlock()
	if h.backupRunning {
		return false
	}
	h.backupRunning = true
	return true
}

func (h *Handler) endBackup() {
	h.backupMu.Lock()
	defer h.backupMu.Unlock()
	h.backupRunning = false
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
	msg, err := protocol.NewMessage(protocol.TypeTaskResult, payload)
	if err != nil {
		log.Printf("create task result failed: %v", err)
		return
	}
	if err := h.sendMessage(*msg); err != nil {
		log.Printf("send task result failed: %v", err)
		h.persistPendingResult(payload)
	}
}

func (h *Handler) sendMessage(msg protocol.Message) error {
	if h.send == nil {
		return nil
	}
	return h.send(msg)
}

func (h *Handler) persistPendingResult(result protocol.TaskResultPayload) {
	if h.policyStore == nil {
		return
	}
	results, err := h.policyStore.LoadPendingResults()
	if err != nil {
		log.Printf("load pending results failed: %v", err)
		results = nil
	}
	results = append(results, result)
	if err := h.policyStore.SavePendingResults(results); err != nil {
		log.Printf("save pending result failed: %v", err)
	}
}

func (h *Handler) failedTaskResult(agentID string, errorText string, startedAt time.Time) protocol.TaskResultPayload {
	return executor.TaskResult{
		Type:       "backup",
		Status:     "failed",
		DurationMs: 0,
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

func runBackup(ctx context.Context, cfg executor.ExecutorConfig) executor.TaskResult {
	return executor.NewExecutor(cfg).RunBackupJob(ctx)
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
