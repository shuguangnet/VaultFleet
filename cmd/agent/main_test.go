package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vaultfleet/pkg/protocol"
)

func TestEnrollReturnsCmdAgentConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"data": map[string]string{
				"agent_id":    "agent-1",
				"agent_token": "ak_test",
			},
		}))
	}))
	t.Cleanup(server.Close)

	cfg, err := enroll(server.URL, "ek_test", filepath.Join(t.TempDir(), "agent.yaml"))

	require.NoError(t, err)
	assert.Equal(t, &AgentConfig{
		Server:     server.URL,
		AgentID:    "agent-1",
		AgentToken: "ak_test",
	}, cfg)
}

func TestRunAgentEnrollOnlyEnrollsAndExits(t *testing.T) {
	var enrolled bool
	var clientStarted bool
	configPath := filepath.Join(t.TempDir(), "agent.yaml")

	err := runAgent(context.Background(), []string{
		"--enroll-only",
		"--server", "https://master.example.com",
		"--token", "ek_test",
		"--config", configPath,
	}, agentRuntime{
		loadConfig: func(string) (*AgentConfig, error) {
			return nil, errors.New("config should not be loaded for enroll-only")
		},
		enroll: func(server, token, path string) (*AgentConfig, error) {
			enrolled = true
			assert.Equal(t, "https://master.example.com", server)
			assert.Equal(t, "ek_test", token)
			assert.Equal(t, configPath, path)
			return &AgentConfig{Server: server, AgentID: "agent-1", AgentToken: "ak_test"}, nil
		},
		runClient: func(context.Context, *AgentConfig) error {
			clientStarted = true
			return nil
		},
	})

	require.NoError(t, err)
	assert.True(t, enrolled)
	assert.False(t, clientStarted)
}

func TestRunAgentEnrollOnlyRequiresServerAndToken(t *testing.T) {
	err := runAgent(context.Background(), []string{"--enroll-only"}, agentRuntime{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--server and --token")
}

func TestRunAgentHelpReturnsNil(t *testing.T) {
	err := runAgent(context.Background(), []string{"--help"}, agentRuntime{})

	require.NoError(t, err)
}

func TestAgentConfigAutoUpdateDefault(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("server: https://master\nagent_id: a1\nagent_token: tok\n"), 0600))

	cfg, err := loadConfig(configPath)
	require.NoError(t, err)
	assert.Nil(t, cfg.AutoUpdate)
	assert.Empty(t, cfg.GitHubProxy)
}

func TestAgentConfigAutoUpdateDisabled(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "agent.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("server: https://master\nagent_id: a1\nagent_token: tok\nauto_update: false\ngithub_proxy: https://proxy.example.com\ngithub_repo: shuguangnet/VaultFleet\n"), 0600))

	cfg, err := loadConfig(configPath)
	require.NoError(t, err)
	require.NotNil(t, cfg.AutoUpdate)
	assert.False(t, *cfg.AutoUpdate)
	assert.Equal(t, "https://proxy.example.com", cfg.GitHubProxy)
	assert.Equal(t, "shuguangnet/VaultFleet", cfg.GitHubRepo)
}

func TestCollectAgentCapabilitiesIncludesDockerRestoreWhenDockerAvailable(t *testing.T) {
	capabilities := collectAgentCapabilities(true)

	assert.Contains(t, capabilities, protocol.CapabilityDockerWorkloadBackups)
	assert.Contains(t, capabilities, protocol.CapabilityDockerContainerRestore)
}

func TestCollectAgentCapabilitiesSkipsDockerCapabilitiesWhenDockerUnavailable(t *testing.T) {
	capabilities := collectAgentCapabilities(false)

	assert.NotContains(t, capabilities, protocol.CapabilityDockerWorkloadBackups)
	assert.NotContains(t, capabilities, protocol.CapabilityDockerContainerRestore)
}
