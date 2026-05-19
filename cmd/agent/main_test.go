package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
