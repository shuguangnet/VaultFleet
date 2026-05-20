package storagecheck

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	err        error
	calls      int
	configPath string
	args       []string
	onRun      func(t *testing.T, configPath string)
	t          *testing.T
}

func (r *fakeRunner) Run(_ context.Context, configPath string, args ...string) error {
	r.calls++
	r.configPath = configPath
	r.args = append([]string(nil), args...)
	if r.onRun != nil {
		r.onRun(r.t, configPath)
	}
	return r.err
}

func TestServiceRunsRcloneWithTempConfigAndRedactsSecrets(t *testing.T) {
	runner := &fakeRunner{err: errors.New("failed with SECRET456 and token-123")}
	service := NewService(runner)

	result := service.Test(context.Background(), Request{
		RcloneType: "s3",
		RcloneConfig: map[string]string{
			"provider":          "Cloudflare",
			"access_key_id":     "AKID123",
			"secret_access_key": "SECRET456",
			"token":             "token-123",
		},
	})

	assert.False(t, result.OK)
	assert.Contains(t, result.Error, "[redacted]")
	assert.NotContains(t, result.Error, "SECRET456")
	assert.NotContains(t, result.Error, "token-123")
	require.Equal(t, 1, runner.calls)
	assert.True(t, slices.Contains(runner.args, "lsd"), runner.args)
	assert.True(t, slices.Contains(runner.args, "vaultfleet:"), runner.args)
	assertTempConfigRemoved(t, runner.configPath)
}

func TestServiceRedactsSensitiveKeyPatternsFromRunnerError(t *testing.T) {
	runner := &fakeRunner{err: errors.New("api-key-value private-key-value key-pem-value")}
	service := NewService(runner)

	result := service.Test(context.Background(), Request{
		RcloneType: "s3",
		RcloneConfig: map[string]string{
			"api_key":     "api-key-value",
			"private_key": "private-key-value",
			"key_pem":     "key-pem-value",
		},
	})

	assert.False(t, result.OK)
	assert.Contains(t, result.Error, "[redacted]")
	assert.NotContains(t, result.Error, "api-key-value")
	assert.NotContains(t, result.Error, "private-key-value")
	assert.NotContains(t, result.Error, "key-pem-value")
}

func TestServiceObscuresWebDAVPasswordInTempConfig(t *testing.T) {
	runner := &fakeRunner{
		t: t,
		onRun: func(t *testing.T, configPath string) {
			t.Helper()

			info, err := os.Stat(configPath)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

			contents, err := os.ReadFile(configPath)
			require.NoError(t, err)

			config := string(contents)
			assert.NotContains(t, config, "clear-webdav-pass")
			passValue := requireConfigValue(t, config, "pass")
			assert.NotEmpty(t, passValue)
			assert.NotEqual(t, "clear-webdav-pass", passValue)
		},
	}
	service := NewService(runner)

	result := service.Test(context.Background(), Request{
		RcloneType: "webdav",
		RcloneConfig: map[string]string{
			"url":  "https://example.test/webdav",
			"user": "vaultfleet",
			"pass": "clear-webdav-pass",
		},
	})

	assert.True(t, result.OK)
}

func TestServiceReportsSuccessfulConnection(t *testing.T) {
	runner := &fakeRunner{}
	service := NewService(runner)

	result := service.Test(context.Background(), Request{
		RcloneType:   "s3",
		RcloneConfig: map[string]string{"provider": "Cloudflare"},
	})

	assert.True(t, result.OK)
	assert.GreaterOrEqual(t, result.LatencyMs, int64(0))
	assert.False(t, result.CheckedAt.IsZero())
}

func assertTempConfigRemoved(t *testing.T, configPath string) {
	t.Helper()

	require.NotEmpty(t, configPath)
	assert.True(t, strings.HasSuffix(configPath, "rclone.conf"), configPath)
	_, err := os.Stat(configPath)
	require.True(t, errors.Is(err, os.ErrNotExist), "expected temp config to be removed, got %v", err)
}

func requireConfigValue(t *testing.T, config string, key string) string {
	t.Helper()

	prefix := key + " = "
	for _, line := range strings.Split(config, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("missing config key %q in:\n%s", key, config)
	return ""
}
