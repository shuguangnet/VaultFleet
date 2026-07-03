package dockerops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vaultfleet/pkg/protocol"
)

const DefaultManifestPath = ".vaultfleet/docker/manifest.json"

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type Service struct {
	Runner CommandRunner
	Now    func() time.Time
}

func New() *Service {
	return &Service{Runner: runCommand, Now: time.Now}
}

func (s *Service) Discover(ctx context.Context, req protocol.DockerDiscoverReqPayload) (*protocol.DockerDiscoverRespPayload, error) {
	runner := s.runner()
	if _, err := runner(ctx, "docker", "version", "--format", "{{json .Server.Version}}"); err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}

	ids := append([]string(nil), req.Containers...)
	if len(ids) == 0 || req.All {
		args := []string{"ps", "--format", "{{.ID}}"}
		if req.All {
			args = []string{"ps", "-a", "--format", "{{.ID}}"}
		}
		out, err := runner(ctx, "docker", args...)
		if err != nil {
			return nil, fmt.Errorf("list docker containers: %w", err)
		}
		ids = splitLines(string(out))
	}
	if len(ids) == 0 {
		return &protocol.DockerDiscoverRespPayload{AgentID: req.AgentID, Containers: []protocol.DockerContainer{}}, nil
	}

	args := append([]string{"inspect"}, ids...)
	out, err := runner(ctx, "docker", args...)
	if err != nil {
		return nil, fmt.Errorf("inspect docker containers: %w", err)
	}
	containers, warnings, err := parseInspect(out)
	if err != nil {
		return nil, err
	}
	sort.Slice(containers, func(i, j int) bool { return containers[i].Name < containers[j].Name })
	return &protocol.DockerDiscoverRespPayload{AgentID: req.AgentID, Containers: containers, Warnings: warnings}, nil
}

func BuildManifest(containers []protocol.DockerContainer, now time.Time) protocol.DockerManifest {
	backupDirs := BackupDirs(containers)
	return protocol.DockerManifest{
		Version:    1,
		CreatedAt:  now,
		Containers: containers,
		BackupDirs: backupDirs,
		Plan:       RestorePlan(containers),
	}
}

func BackupDirs(containers []protocol.DockerContainer) []string {
	seen := make(map[string]bool)
	for _, container := range containers {
		for _, mount := range container.Mounts {
			if mount.Source != "" && filepath.IsAbs(mount.Source) {
				seen[mount.Source] = true
			}
		}
		if container.ComposeFile != "" && filepath.IsAbs(container.ComposeFile) {
			seen[container.ComposeFile] = true
		}
		if container.WorkingDir != "" && filepath.IsAbs(container.WorkingDir) {
			for _, name := range []string{"docker-compose.yml", "compose.yml", ".env"} {
				path := filepath.Join(container.WorkingDir, name)
				if _, err := os.Stat(path); err == nil {
					seen[path] = true
				}
			}
		}
	}
	dirs := make([]string, 0, len(seen))
	for path := range seen {
		dirs = append(dirs, path)
	}
	sort.Strings(dirs)
	return dirs
}

func RestorePlan(containers []protocol.DockerContainer) protocol.DockerRestorePlan {
	plan := protocol.DockerRestorePlan{Mode: "manual"}
	projects := make(map[string]string)
	for _, container := range containers {
		if container.Name != "" {
			plan.Containers = append(plan.Containers, container.Name)
		}
		if container.ComposeProject != "" && container.WorkingDir != "" {
			projects[container.ComposeProject] = container.WorkingDir
		}
	}
	sort.Strings(plan.Containers)
	if len(projects) == 1 {
		for _, dir := range projects {
			plan.Mode = "compose"
			plan.WorkingDir = dir
			plan.Command = "docker compose up -d"
		}
	} else if len(projects) > 1 {
		plan.Warnings = append(plan.Warnings, "multiple compose projects discovered; start containers manually after restore")
	}
	return plan
}

func WriteManifest(root string, manifest protocol.DockerManifest) (string, error) {
	path := filepath.Join(root, DefaultManifestPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func ReadManifest(root string, manifestPath string) (*protocol.DockerManifest, error) {
	if manifestPath == "" {
		manifestPath = DefaultManifestPath
	}
	if filepath.IsAbs(manifestPath) {
		return nil, errors.New("manifest path must be relative")
	}
	path := filepath.Clean(filepath.Join(root, manifestPath))
	cleanRoot := filepath.Clean(root)
	if path != cleanRoot && !strings.HasPrefix(path, cleanRoot+string(os.PathSeparator)) {
		return nil, errors.New("manifest path escapes restore target")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest protocol.DockerManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.Version == 0 || len(manifest.Containers) == 0 {
		return nil, errors.New("invalid docker manifest")
	}
	return &manifest, nil
}

func (s *Service) Precheck(ctx context.Context, manifest protocol.DockerManifest, startupCommand string) []string {
	runner := s.runner()
	var warnings []string
	if _, err := runner(ctx, "docker", "version", "--format", "{{json .Server.Version}}"); err != nil {
		warnings = append(warnings, "docker unavailable: "+err.Error())
	}
	if startupCommand == "" {
		startupCommand = manifest.Plan.Command
	}
	if strings.Contains(startupCommand, "docker compose") {
		if _, err := runner(ctx, "docker", "compose", "version"); err != nil {
			warnings = append(warnings, "docker compose unavailable: "+err.Error())
		}
	}
	return warnings
}

func (s *Service) RunStartup(ctx context.Context, dir string, command string, timeoutSeconds int) ([]byte, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, errors.New("startup command is empty")
	}
	runCtx := ctx
	var cancel context.CancelFunc
	if timeoutSeconds > 0 {
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
		defer cancel()
	}
	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return out, errors.New("startup command timeout")
	}
	return out, err
}

func (s *Service) runner() CommandRunner {
	if s != nil && s.Runner != nil {
		return s.Runner
	}
	return runCommand
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type inspectContainer struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		Image  string            `json:"Image"`
		Env    []string          `json:"Env"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Status string `json:"Status"`
	} `json:"State"`
	Mounts []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
	} `json:"NetworkSettings"`
}

func parseInspect(data []byte) ([]protocol.DockerContainer, []string, error) {
	var raw []inspectContainer
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("decode docker inspect: %w", err)
	}
	containers := make([]protocol.DockerContainer, 0, len(raw))
	var warnings []string
	for _, item := range raw {
		labels := redactMap(item.Config.Labels)
		container := protocol.DockerContainer{
			ID:             shortID(item.ID),
			Name:           strings.TrimPrefix(item.Name, "/"),
			Image:          item.Config.Image,
			Status:         item.State.Status,
			Labels:         labels,
			Env:            envMap(item.Config.Env),
			ComposeProject: item.Config.Labels["com.docker.compose.project"],
			ComposeService: item.Config.Labels["com.docker.compose.service"],
			ComposeFile:    firstComposeFile(item.Config.Labels["com.docker.compose.project.config_files"]),
			WorkingDir:     item.Config.Labels["com.docker.compose.project.working_dir"],
		}
		for _, mount := range item.Mounts {
			if mount.Source == "" {
				warnings = append(warnings, "container "+container.Name+" has mount without host source: "+mount.Destination)
			}
			container.Mounts = append(container.Mounts, protocol.DockerMount{
				Type:        mount.Type,
				Name:        mount.Name,
				Source:      mount.Source,
				Destination: mount.Destination,
				RW:          mount.RW,
			})
		}
		container.Ports = parsePorts(item.NetworkSettings.Ports)
		containers = append(containers, container)
	}
	return containers, warnings, nil
}

func envMap(values []string) map[string]string {
	result := make(map[string]string)
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok || key == "" {
			continue
		}
		if isSensitiveKey(key) {
			val = "[redacted]"
		}
		result[key] = val
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func redactMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		if isSensitiveKey(key) {
			value = "[redacted]"
		}
		result[key] = value
	}
	return result
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "password") || strings.Contains(key, "passwd") || strings.Contains(key, "secret") || strings.Contains(key, "token") || strings.Contains(key, "key")
}

func parsePorts(raw map[string][]struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}) []protocol.DockerPort {
	ports := make([]protocol.DockerPort, 0)
	for key, bindings := range raw {
		private, proto, _ := strings.Cut(key, "/")
		privatePort := atoi(private)
		if len(bindings) == 0 {
			ports = append(ports, protocol.DockerPort{PrivatePort: privatePort, Type: proto})
			continue
		}
		for _, binding := range bindings {
			ports = append(ports, protocol.DockerPort{IP: binding.HostIP, PrivatePort: privatePort, PublicPort: atoi(binding.HostPort), Type: proto})
		}
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].PrivatePort < ports[j].PrivatePort })
	return ports
}

func splitLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func firstComposeFile(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ",")
	return strings.TrimSpace(parts[0])
}

func atoi(value string) int {
	var n int
	_, _ = fmt.Sscanf(value, "%d", &n)
	return n
}
