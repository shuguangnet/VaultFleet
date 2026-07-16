package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agent "vaultfleet/internal/agent"
	agentdocker "vaultfleet/internal/agent/docker"
	"vaultfleet/internal/agent/executor"
	"vaultfleet/internal/agent/policy"
	"vaultfleet/internal/master/api"
	"vaultfleet/internal/master/commands"
	"vaultfleet/internal/master/db"
	"vaultfleet/pkg/protocol"
)

func TestIntegrationMultiContainerRestoreContinuesAfterFailure(t *testing.T) {
	var started []string
	results := agentdocker.RestoreBatch(context.Background(), protocol.DockerRestoreRequest{Sources: []protocol.DockerResolvedSource{
		{ContainerID: "broken-id", Name: "broken"},
		{ContainerID: "healthy-id", Name: "healthy", Image: "example/healthy:latest"},
	}}, func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := name + " " + strings.Join(args, " ")
		if strings.HasPrefix(call, "docker start ") {
			container := strings.TrimPrefix(call, "docker start ")
			started = append(started, container)
			if container == "healthy" {
				return nil, nil
			}
		}
		return []byte("not found"), errors.New("not found")
	}, nil)

	require.Len(t, results, 2)
	assert.Equal(t, []string{"broken", "healthy"}, started)
	assert.Equal(t, protocol.RestoreItemStatusFailed, results[0].Status)
	assert.True(t, results[0].Retryable)
	assert.Equal(t, protocol.RestoreItemStatusSuccess, results[1].Status)
}

func TestIntegrationMultiContainerRestorePersistsAndBuildsRetryPlan(t *testing.T) {
	database, err := db.New(t.TempDir())
	require.NoError(t, err)
	commandService := commands.NewService(database, nil)
	store := policy.NewStore(t.TempDir())
	require.NoError(t, store.SavePolicy(&protocol.PolicyPushPayload{AgentID: "target-agent", Storage: protocol.StorageConfig{RepoPath: "test/repo"}}))
	restoreRequest := protocol.RestoreReqPayload{
		SnapshotID: "snap-disposable", SourceAgentID: "source-agent", Target: "/", RestoreMode: protocol.RestoreModeDockerContainer,
		Docker: &protocol.DockerRestoreRequest{Sources: []protocol.DockerResolvedSource{
			{ContainerID: "broken-id", Name: "broken", ResolvedPaths: []string{"/tmp/disposable-shared", "/tmp/disposable-broken"}},
			{ContainerID: "healthy-id", Name: "healthy", ResolvedPaths: []string{"/tmp/disposable-shared", "/tmp/disposable-healthy"}},
		}},
	}
	message, err := protocol.NewMessage(protocol.TypeSelectiveRestoreReq, restoreRequest)
	require.NoError(t, err)
	command, err := commandService.CreateCommand(context.Background(), commands.CreateCommandInput{
		AgentID: "target-agent", Type: protocol.TypeSelectiveRestoreReq, Message: *message, TaskType: "restore", TaskState: commands.TaskStatusRunning, SnapshotID: restoreRequest.SnapshotID,
	})
	require.NoError(t, err)
	require.NoError(t, database.DB.Model(&db.AgentCommand{}).Where("id = ?", command.ID).Update("status", commands.CommandStatusRunning).Error)

	progressProcessor := api.NewRestoreProgressProcessor(database)
	resultProcessor := api.NewTaskResultProcessor(database, commandService)
	completed := make(chan protocol.TaskResultPayload, 1)
	handler := agent.NewHandler(agent.HandlerConfig{
		PolicyStore: store, ConfigDir: t.TempDir(), AgentID: "target-agent",
		RestoreRunner: func(context.Context, executor.ExecutorConfig, string, string, []string) error { return nil },
		DockerRestoreBatchRunner: func(_ context.Context, request protocol.DockerRestoreRequest, progress agentdocker.RestoreProgressFunc) []protocol.RestoreItemResult {
			progress(request.Sources[0], 0, 0)
			progress(request.Sources[0], 1, 1)
			progress(request.Sources[1], 1, 1)
			progress(request.Sources[1], 2, 1)
			return []protocol.RestoreItemResult{
				{SourceID: "broken-id", SourceName: "broken", Status: protocol.RestoreItemStatusFailed, Error: "disposable conflict", Retryable: true},
				{SourceID: "healthy-id", SourceName: "healthy", Status: protocol.RestoreItemStatusSuccess},
			}
		},
		SendFunc: func(sent protocol.Message) error {
			switch sent.Type {
			case protocol.TypeRestoreProgress:
				return progressProcessor("target-agent", sent)
			case protocol.TypeTaskResult:
				payload, parseErr := protocol.ParsePayload[protocol.TaskResultPayload](&sent)
				if parseErr != nil {
					return parseErr
				}
				if processErr := resultProcessor("target-agent", sent); processErr != nil {
					return processErr
				}
				completed <- *payload
			}
			return nil
		},
	})

	handler.Handle(*message)
	select {
	case result := <-completed:
		assert.Equal(t, message.ID, command.MessageID)
		assert.Equal(t, commands.TaskStatusPartialSuccess, result.Status)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for disposable multi-container restore")
	}

	var history db.TaskHistory
	require.NoError(t, database.DB.First(&history, "command_id = ?", command.ID).Error)
	assert.Equal(t, commands.TaskStatusPartialSuccess, history.Status)
	assert.NotEmpty(t, history.RestoreProgress)
	assert.Contains(t, history.RestoreItems, "broken-id")

	gin.SetMode(gin.TestMode)
	router := gin.New()
	api.RegisterTaskRoutes(router.Group("/api"), api.NewTaskHandler(database, nil))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/tasks/"+history.ID+"/retry-failed", bytes.NewReader([]byte(`{}`)))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var response struct {
		Data struct {
			DockerSourceIDs []string `json:"docker_source_ids"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, []string{"broken-id"}, response.Data.DockerSourceIDs)
	t.Logf("message_id=%s task_status=%s retry_source_ids=%v", message.ID, history.Status, response.Data.DockerSourceIDs)
}
