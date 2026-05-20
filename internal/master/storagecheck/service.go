package storagecheck

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Runner interface {
	Run(ctx context.Context, configPath string, args ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, configPath string, args ...string) error {
	commandArgs := append([]string{"--config", configPath}, args...)
	cmd := exec.CommandContext(ctx, "rclone", commandArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) == 0 {
			return err
		}
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

type Service struct {
	Runner  Runner
	Now     func() time.Time
	Timeout time.Duration
}

type Request struct {
	RcloneType   string
	RcloneConfig map[string]string
}

type Result struct {
	OK        bool      `json:"ok"`
	LatencyMs int64     `json:"latency_ms"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

func NewService(runner Runner) *Service {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Service{
		Runner:  runner,
		Now:     time.Now,
		Timeout: 15 * time.Second,
	}
}

func (s *Service) Test(ctx context.Context, request Request) Result {
	now := s.now()
	start := now
	result := Result{CheckedAt: now}

	tempDir, err := os.MkdirTemp("", "vaultfleet-rclone-*")
	if err != nil {
		result.Error = s.redactError(err, request)
		result.LatencyMs = s.latencySince(start)
		return result
	}
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "rclone.conf")
	configContents, err := rcloneConfigContents(request)
	if err != nil {
		result.Error = s.redactError(err, request)
		result.LatencyMs = s.latencySince(start)
		return result
	}

	if err := os.WriteFile(configPath, []byte(configContents), 0o600); err != nil {
		result.Error = s.redactError(err, request)
		result.LatencyMs = s.latencySince(start)
		return result
	}

	runCtx, cancel := context.WithTimeout(ctx, s.timeout())
	defer cancel()

	if err := s.runner().Run(runCtx, configPath, "lsd", "vaultfleet:"); err != nil {
		result.Error = s.redactError(err, request)
		result.LatencyMs = s.latencySince(start)
		return result
	}

	result.OK = true
	result.LatencyMs = s.latencySince(start)
	return result
}

func (s *Service) runner() Runner {
	if s.Runner != nil {
		return s.Runner
	}
	return ExecRunner{}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) timeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return 15 * time.Second
}

func (s *Service) latencySince(start time.Time) int64 {
	return s.now().Sub(start).Milliseconds()
}

func rcloneConfigContents(request Request) (string, error) {
	var builder strings.Builder
	builder.WriteString("[vaultfleet]\n")
	builder.WriteString("type = ")
	builder.WriteString(request.RcloneType)
	builder.WriteString("\n")

	keys := make([]string, 0, len(request.RcloneConfig))
	for key := range request.RcloneConfig {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value, err := rcloneConfigValue(request.RcloneType, key, request.RcloneConfig[key])
		if err != nil {
			return "", err
		}
		builder.WriteString(key)
		builder.WriteString(" = ")
		builder.WriteString(value)
		builder.WriteString("\n")
	}
	return builder.String(), nil
}

func rcloneConfigValue(configType string, key string, value string) (string, error) {
	if configType == "webdav" && key == "pass" && value != "" {
		return obscureRcloneValue(value)
	}
	return value, nil
}

func (s *Service) redactError(err error, request Request) string {
	message := err.Error()
	for key, value := range request.RcloneConfig {
		if value == "" || !isSecretKey(key) {
			continue
		}
		message = strings.ReplaceAll(message, value, "[redacted]")
	}
	return message
}

func isSecretKey(key string) bool {
	normalized := strings.ToLower(key)
	return normalized == "pass" ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "access_key") ||
		strings.Contains(normalized, "api_key") ||
		strings.Contains(normalized, "private_key") ||
		strings.Contains(normalized, "key_pem") ||
		strings.HasSuffix(normalized, "_key")
}

var rcloneObscureKey = []byte{
	0x9c, 0x93, 0x5b, 0x48, 0x73, 0x0a, 0x55, 0x4d,
	0x6b, 0xfd, 0x7c, 0x63, 0xc8, 0x86, 0xa9, 0x2b,
	0xd3, 0x90, 0x19, 0x8e, 0xb8, 0x12, 0x8a, 0xfb,
	0xf4, 0xde, 0x16, 0x2b, 0x8b, 0x95, 0xf6, 0x38,
}

func obscureRcloneValue(value string) (string, error) {
	plaintext := []byte(value)
	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", fmt.Errorf("generate rclone obscure iv: %w", err)
	}
	if err := cryptRcloneValue(ciphertext[aes.BlockSize:], plaintext, iv); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func cryptRcloneValue(out []byte, in []byte, iv []byte) error {
	block, err := aes.NewCipher(rcloneObscureKey)
	if err != nil {
		return fmt.Errorf("create rclone obscure cipher: %w", err)
	}
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(out, in)
	return nil
}
