package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vaultfleet/pkg/protocol"
)

const (
	DefaultSocketPath = "/var/run/docker.sock"
	requestTimeout    = 10 * time.Second
)

type Client struct {
	socketPath string
	httpClient *http.Client
}

type ContainerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
	Mounts []Mount           `json:"Mounts"`
}

type ContainerInspect struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Image  string `json:"Image"`
	Config struct {
		Image      string            `json:"Image"`
		Labels     map[string]string `json:"Labels"`
		Env        []string          `json:"Env"`
		Cmd        []string          `json:"Cmd"`
		Entrypoint []string          `json:"Entrypoint"`
		WorkingDir string            `json:"WorkingDir"`
		User       string            `json:"User"`
	} `json:"Config"`
	HostConfig struct {
		PortBindings map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"PortBindings"`
		RestartPolicy struct {
			Name string `json:"Name"`
		} `json:"RestartPolicy"`
		NetworkMode string `json:"NetworkMode"`
	} `json:"HostConfig"`
	State struct {
		Status string `json:"Status"`
	} `json:"State"`
	Mounts []Mount `json:"Mounts"`
}

type Mount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

type API interface {
	Ping(ctx context.Context) error
	ListContainers(ctx context.Context) ([]ContainerSummary, error)
	InspectContainer(ctx context.Context, id string) (ContainerInspect, error)
}

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func NewClient(socketPath string) *Client {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		socketPath: socketPath,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   requestTimeout,
		},
	}
}

func (c *Client) Ping(ctx context.Context) error {
	var body string
	if err := c.get(ctx, "/_ping", &body); err != nil {
		return err
	}
	if strings.TrimSpace(body) != "OK" {
		return fmt.Errorf("unexpected docker ping response: %q", body)
	}
	return nil
}

func (c *Client) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	var containers []ContainerSummary
	if err := c.get(ctx, "/containers/json?all=1", &containers); err != nil {
		return nil, err
	}
	return containers, nil
}

func (c *Client) InspectContainer(ctx context.Context, id string) (ContainerInspect, error) {
	var inspect ContainerInspect
	if strings.TrimSpace(id) == "" {
		return inspect, errors.New("container id is required")
	}
	if err := c.get(ctx, "/containers/"+url.PathEscape(id)+"/json", &inspect); err != nil {
		return inspect, err
	}
	return inspect, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("docker socket unavailable at %s: %w", c.socketPath, err)
		}
		return fmt.Errorf("docker api request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("docker api request %s failed: status %d", path, resp.StatusCode)
	}
	if text, ok := out.(*string); ok {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		*text = string(data)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode docker api response %s: %w", path, err)
	}
	return nil
}

func Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	client := NewClient("")
	if err := client.Ping(ctx); err != nil {
		return false
	}
	_, err := client.ListContainers(ctx)
	return err == nil
}

func Discover(ctx context.Context, client API) protocol.DockerDiscoveryRespPayload {
	if client == nil {
		client = NewClient("")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		return protocol.DockerDiscoveryRespPayload{Available: false, Error: err.Error()}
	}
	containers, err := client.ListContainers(ctx)
	if err != nil {
		return protocol.DockerDiscoveryRespPayload{Available: false, Error: err.Error()}
	}

	result := protocol.DockerDiscoveryRespPayload{
		Available:  true,
		Containers: make([]protocol.DockerContainer, 0, len(containers)),
	}
	for _, container := range containers {
		labels := copyStringMap(container.Labels)
		mounts := toProtocolMounts(container.Mounts)
		compose := composeInfo(labels)
		warnings := containerWarnings(container.State, mounts, compose)
		result.Containers = append(result.Containers, protocol.DockerContainer{
			ID:         container.ID,
			Names:      cleanContainerNames(container.Names),
			Image:      container.Image,
			State:      container.State,
			Labels:     labels,
			Compose:    compose,
			Mounts:     mounts,
			Selectable: hasPersistentData(mounts, compose),
			Warnings:   warnings,
		})
	}
	sort.Slice(result.Containers, func(i, j int) bool {
		return firstName(result.Containers[i].Names) < firstName(result.Containers[j].Names)
	})
	return result
}

func Resolve(ctx context.Context, client API, sources []protocol.BackupSource) ([]string, *protocol.DockerBackupMetadata, error) {
	if client == nil {
		client = NewClient("")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	containers, err := client.ListContainers(ctx)
	if err != nil {
		return nil, nil, err
	}

	var paths []string
	metadata := &protocol.DockerBackupMetadata{}
	for _, source := range sources {
		if source.Type != protocol.BackupSourceTypeDockerContainer || source.DockerContainer == nil {
			continue
		}
		match, err := matchContainer(containers, *source.DockerContainer)
		if err != nil {
			return nil, nil, err
		}
		inspect, err := client.InspectContainer(ctx, match.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("inspect docker container %s: %w", match.ID, err)
		}
		resolved, resolvedMetadata, err := resolveInspect(inspect, *source.DockerContainer)
		if err != nil {
			return nil, nil, err
		}
		paths = append(paths, resolved...)
		metadata.Sources = append(metadata.Sources, resolvedMetadata)
		metadata.Warnings = append(metadata.Warnings, resolvedMetadata.Warnings...)
	}
	paths = uniqueStrings(paths)
	if len(metadata.Sources) == 0 {
		return paths, nil, nil
	}
	if len(paths) == 0 {
		return nil, nil, errors.New("docker backup sources resolved no backup paths")
	}
	return paths, metadata, nil
}

func matchContainer(containers []ContainerSummary, source protocol.DockerContainerBackupSource) (ContainerSummary, error) {
	if source.ContainerID != "" {
		for _, container := range containers {
			if container.ID == source.ContainerID || strings.HasPrefix(container.ID, source.ContainerID) {
				return container, nil
			}
		}
	}
	if source.ComposeProject != "" && source.ComposeService != "" {
		var matches []ContainerSummary
		for _, container := range containers {
			if container.Labels["com.docker.compose.project"] == source.ComposeProject &&
				container.Labels["com.docker.compose.service"] == source.ComposeService {
				matches = append(matches, container)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return ContainerSummary{}, fmt.Errorf("docker container selection %s/%s is ambiguous", source.ComposeProject, source.ComposeService)
		}
	}
	if source.Name != "" {
		var matches []ContainerSummary
		for _, container := range containers {
			for _, name := range cleanContainerNames(container.Names) {
				if name == source.Name {
					matches = append(matches, container)
					break
				}
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return ContainerSummary{}, fmt.Errorf("docker container name %q is ambiguous", source.Name)
		}
	}
	return ContainerSummary{}, fmt.Errorf("docker container selection not found: %s", sourceDescription(source))
}

func resolveInspect(inspect ContainerInspect, source protocol.DockerContainerBackupSource) ([]string, protocol.DockerResolvedSource, error) {
	labels := inspect.Config.Labels
	compose := composeInfo(labels)
	if source.ComposeWorkingDir == "" {
		source.ComposeWorkingDir = compose.WorkingDir
	}
	if len(source.ComposeConfigFiles) == 0 {
		source.ComposeConfigFiles = compose.ConfigFiles
	}

	var paths []string
	var warnings []string
	for _, mount := range inspect.Mounts {
		include := (mount.Type == "bind" && source.IncludeBindMounts) ||
			((mount.Type == "volume" || mount.Type == "") && source.IncludeVolumes)
		if !include {
			continue
		}
		if strings.TrimSpace(mount.Source) == "" {
			return nil, protocol.DockerResolvedSource{}, fmt.Errorf("docker mount %s has no source path", mount.Destination)
		}
		if err := assertReadablePath(mount.Source); err != nil {
			return nil, protocol.DockerResolvedSource{}, fmt.Errorf("docker mount %s at %s is unreadable: %w", mount.Destination, mount.Source, err)
		}
		paths = append(paths, mount.Source)
	}
	if source.IncludeComposeFiles {
		for _, configFile := range source.ComposeConfigFiles {
			path := configFile
			if !filepath.IsAbs(path) && source.ComposeWorkingDir != "" {
				path = filepath.Join(source.ComposeWorkingDir, path)
			}
			if strings.TrimSpace(path) == "" {
				continue
			}
			if err := assertReadablePath(path); err != nil {
				warnings = append(warnings, fmt.Sprintf("compose file %s is not readable: %v", path, err))
				continue
			}
			paths = append(paths, path)
		}
	}
	paths = uniqueStrings(paths)
	if len(paths) == 0 {
		return nil, protocol.DockerResolvedSource{}, fmt.Errorf("docker container %s resolved no backup paths", strings.Trim(inspect.Name, "/"))
	}
	image := inspect.Config.Image
	if image == "" {
		image = inspect.Image
	}
	return paths, protocol.DockerResolvedSource{
		Selection:     source,
		ContainerID:   inspect.ID,
		Name:          strings.Trim(inspect.Name, "/"),
		Image:         image,
		Labels:        copyStringMap(labels),
		Compose:       compose,
		Mounts:        selectedMounts(inspect.Mounts, source),
		Env:           append([]string(nil), inspect.Config.Env...),
		Cmd:           append([]string(nil), inspect.Config.Cmd...),
		Entrypoint:    append([]string(nil), inspect.Config.Entrypoint...),
		WorkingDir:    inspect.Config.WorkingDir,
		User:          inspect.Config.User,
		Ports:         portBindings(inspect.HostConfig.PortBindings),
		RestartPolicy: inspect.HostConfig.RestartPolicy.Name,
		NetworkMode:   inspect.HostConfig.NetworkMode,
		State:         inspect.State.Status,
		ResolvedPaths: paths,
		Warnings:      warnings,
	}, nil
}

func Restore(ctx context.Context, request protocol.DockerRestoreRequest, runner CommandRunner) error {
	if len(request.Sources) == 0 {
		return errors.New("docker restore has no sources")
	}
	if runner == nil {
		runner = runCommand
	}
	for _, source := range request.Sources {
		if err := restoreSource(ctx, source, runner); err != nil {
			return err
		}
	}
	return nil
}

func PreflightRestore(ctx context.Context, client API, request protocol.DockerRestoreRequest) []protocol.RestorePreflightCheck {
	var checks []protocol.RestorePreflightCheck
	if len(request.Sources) == 0 {
		return append(checks, preflightCheck("docker_metadata", protocol.RestorePreflightSeverityError, "docker restore metadata has no sources", ""))
	}
	if client == nil {
		client = NewClient("")
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		return append(checks, preflightCheck("docker_available", protocol.RestorePreflightSeverityError, "Docker Engine is not available", err.Error()))
	}
	checks = append(checks, preflightCheck("docker_available", protocol.RestorePreflightSeverityInfo, "Docker Engine is available", ""))

	containers, err := client.ListContainers(ctx)
	if err != nil {
		return append(checks, preflightCheck("docker_list_containers", protocol.RestorePreflightSeverityError, "cannot list Docker containers", err.Error()))
	}

	for _, source := range request.Sources {
		checks = append(checks, preflightSource(containers, source)...)
	}
	return checks
}

func preflightSource(containers []ContainerSummary, source protocol.DockerResolvedSource) []protocol.RestorePreflightCheck {
	var checks []protocol.RestorePreflightCheck
	sourceName := sourceDescriptionFromResolved(source)
	if strings.TrimSpace(source.Image) == "" && !hasComposeRestoreMetadata(source) {
		checks = append(checks, preflightCheck("docker_image_metadata", protocol.RestorePreflightSeverityWarning, "Docker image metadata is missing", sourceName))
	}
	if conflict := containerConflict(containers, source); conflict != "" {
		checks = append(checks, preflightCheck("docker_container_conflict", protocol.RestorePreflightSeverityWarning, "target host already has a matching Docker container or Compose service", conflict))
	}
	if len(source.ResolvedPaths) == 0 {
		checks = append(checks, preflightCheck("docker_restore_paths", protocol.RestorePreflightSeverityError, "Docker source has no restore paths", sourceName))
		return checks
	}
	for _, restorePath := range source.ResolvedPaths {
		checks = append(checks, preflightRestorePath(restorePath)...)
	}
	return checks
}

func hasComposeRestoreMetadata(source protocol.DockerResolvedSource) bool {
	compose := source.Compose
	if compose.Service == "" {
		compose.Service = source.Selection.ComposeService
	}
	if len(compose.ConfigFiles) == 0 {
		compose.ConfigFiles = source.Selection.ComposeConfigFiles
	}
	return compose.Service != "" && len(compose.ConfigFiles) > 0
}

func containerConflict(containers []ContainerSummary, source protocol.DockerResolvedSource) string {
	name := strings.TrimSpace(source.Name)
	if name == "" {
		name = strings.TrimSpace(source.Selection.Name)
	}
	composeProject := strings.TrimSpace(source.Compose.Project)
	if composeProject == "" {
		composeProject = strings.TrimSpace(source.Selection.ComposeProject)
	}
	composeService := strings.TrimSpace(source.Compose.Service)
	if composeService == "" {
		composeService = strings.TrimSpace(source.Selection.ComposeService)
	}
	for _, container := range containers {
		if name != "" {
			for _, existingName := range cleanContainerNames(container.Names) {
				if existingName == name {
					return "container name " + name
				}
			}
		}
		if composeProject != "" && composeService != "" &&
			container.Labels["com.docker.compose.project"] == composeProject &&
			container.Labels["com.docker.compose.service"] == composeService {
			return "compose service " + composeProject + "/" + composeService
		}
	}
	return ""
}

func preflightRestorePath(path string) []protocol.RestorePreflightCheck {
	path = strings.TrimSpace(path)
	if path == "" {
		return []protocol.RestorePreflightCheck{preflightCheck("docker_restore_path_empty", protocol.RestorePreflightSeverityWarning, "Docker restore path is empty", "")}
	}
	if !filepath.IsAbs(path) {
		return []protocol.RestorePreflightCheck{preflightCheck("docker_restore_path_absolute", protocol.RestorePreflightSeverityError, "Docker restore path must be absolute", path)}
	}
	info, err := os.Stat(path)
	if err == nil {
		checks := []protocol.RestorePreflightCheck{preflightCheck("docker_restore_path_exists", protocol.RestorePreflightSeverityWarning, "Docker restore path already exists and may be overwritten", path)}
		if info.IsDir() {
			if err := probeWritableDir(path); err != nil {
				checks = append(checks, preflightCheck("docker_restore_path_writable", protocol.RestorePreflightSeverityError, "Docker restore path is not writable", err.Error()))
			}
			return checks
		}
		if err := probeWritableDir(filepath.Dir(path)); err != nil {
			checks = append(checks, preflightCheck("docker_restore_path_writable", protocol.RestorePreflightSeverityError, "Docker restore path parent is not writable", err.Error()))
		}
		return checks
	}
	if !os.IsNotExist(err) {
		return []protocol.RestorePreflightCheck{preflightCheck("docker_restore_path_stat", protocol.RestorePreflightSeverityError, "cannot inspect Docker restore path", err.Error())}
	}
	parent := nearestExistingParent(path)
	if parent == "" {
		return []protocol.RestorePreflightCheck{preflightCheck("docker_restore_path_parent", protocol.RestorePreflightSeverityError, "Docker restore path has no existing writable parent", path)}
	}
	if err := probeWritableDir(parent); err != nil {
		return []protocol.RestorePreflightCheck{preflightCheck("docker_restore_path_parent", protocol.RestorePreflightSeverityError, "Docker restore path parent is not writable", err.Error())}
	}
	return []protocol.RestorePreflightCheck{preflightCheck("docker_restore_path_missing", protocol.RestorePreflightSeverityWarning, "Docker restore path does not exist and will be created during restore", path)}
}

func nearestExistingParent(path string) string {
	current := filepath.Clean(path)
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		if info, err := os.Stat(parent); err == nil && info.IsDir() {
			return parent
		}
		current = parent
	}
}

func probeWritableDir(dir string) error {
	probe, err := os.CreateTemp(dir, ".vaultfleet-docker-restore-preflight-*")
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

func preflightCheck(code string, severity string, message string, detail string) protocol.RestorePreflightCheck {
	return protocol.RestorePreflightCheck{
		Code:     code,
		Severity: severity,
		Message:  message,
		Detail:   detail,
	}
}

func restoreSource(ctx context.Context, source protocol.DockerResolvedSource, runner CommandRunner) error {
	if args, configFiles, ok := composeRestoreArgs(source); ok {
		if len(unusableComposeConfigFiles(configFiles)) == 0 {
			if output, err := runner(ctx, "docker", args...); err != nil {
				return commandError("restore docker compose service", output, err)
			}
			return nil
		}
	}

	name := strings.TrimSpace(source.Name)
	if name == "" {
		name = strings.TrimSpace(source.ContainerID)
	}
	if name != "" {
		if _, err := runner(ctx, "docker", "start", name); err == nil {
			return nil
		}
	}

	image := strings.TrimSpace(source.Image)
	if image == "" {
		return fmt.Errorf("docker container %s has no image metadata for restore", sourceDescriptionFromResolved(source))
	}
	args := []string{"run", "-d"}
	if name != "" {
		args = append(args, "--name", name)
	}
	for _, mount := range source.Mounts {
		if strings.TrimSpace(mount.Source) == "" || strings.TrimSpace(mount.Destination) == "" {
			continue
		}
		value := mount.Source + ":" + mount.Destination
		if !mount.RW {
			value += ":ro"
		}
		args = append(args, "-v", value)
	}
	for _, env := range source.Env {
		env = strings.TrimSpace(env)
		if env != "" {
			args = append(args, "-e", env)
		}
	}
	labelKeys := make([]string, 0, len(source.Labels))
	for key := range source.Labels {
		labelKeys = append(labelKeys, key)
	}
	sort.Strings(labelKeys)
	for _, key := range labelKeys {
		value := source.Labels[key]
		key = strings.TrimSpace(key)
		if key != "" {
			args = append(args, "--label", key+"="+value)
		}
	}
	for _, port := range source.Ports {
		value := dockerRunPortArg(port)
		if value != "" {
			args = append(args, "-p", value)
		}
	}
	if restart := strings.TrimSpace(source.RestartPolicy); restart != "" && restart != "no" {
		args = append(args, "--restart", restart)
	}
	if network := strings.TrimSpace(source.NetworkMode); network != "" && network != "default" {
		args = append(args, "--network", network)
	}
	if workdir := strings.TrimSpace(source.WorkingDir); workdir != "" {
		args = append(args, "-w", workdir)
	}
	if user := strings.TrimSpace(source.User); user != "" {
		args = append(args, "-u", user)
	}
	if len(source.Entrypoint) > 0 && strings.TrimSpace(source.Entrypoint[0]) != "" {
		args = append(args, "--entrypoint", source.Entrypoint[0])
	}
	args = append(args, image)
	args = append(args, source.Cmd...)
	if output, err := runner(ctx, "docker", args...); err != nil {
		return commandError("restore docker container", output, err)
	}
	return nil
}

func composeRestoreArgs(source protocol.DockerResolvedSource) ([]string, []string, bool) {
	compose := source.Compose
	if compose.WorkingDir == "" {
		compose.WorkingDir = source.Selection.ComposeWorkingDir
	}
	if len(compose.ConfigFiles) == 0 {
		compose.ConfigFiles = source.Selection.ComposeConfigFiles
	}
	service := strings.TrimSpace(compose.Service)
	if service == "" {
		service = strings.TrimSpace(source.Selection.ComposeService)
	}
	if len(compose.ConfigFiles) == 0 || service == "" {
		return nil, nil, false
	}

	args := []string{"compose"}
	configFiles := make([]string, 0, len(compose.ConfigFiles))
	for _, configFile := range compose.ConfigFiles {
		configFile = strings.TrimSpace(configFile)
		if configFile == "" {
			continue
		}
		if !filepath.IsAbs(configFile) && compose.WorkingDir != "" {
			configFile = filepath.Join(compose.WorkingDir, configFile)
		}
		args = append(args, "-f", configFile)
		configFiles = append(configFiles, configFile)
	}
	if len(args) == 1 {
		return nil, nil, false
	}
	args = append(args, "up", "-d", service)
	return args, configFiles, true
}

func unusableComposeConfigFiles(configFiles []string) []string {
	var unusable []string
	for _, configFile := range configFiles {
		configFile = strings.TrimSpace(configFile)
		if configFile == "" {
			continue
		}
		info, err := os.Stat(configFile)
		if err != nil {
			unusable = append(unusable, configFile)
			continue
		}
		if info.IsDir() {
			unusable = append(unusable, configFile)
		}
	}
	return unusable
}

func selectedMounts(mounts []Mount, source protocol.DockerContainerBackupSource) []protocol.DockerMount {
	var selected []protocol.DockerMount
	for _, mount := range mounts {
		include := (mount.Type == "bind" && source.IncludeBindMounts) ||
			((mount.Type == "volume" || mount.Type == "") && source.IncludeVolumes)
		if !include {
			continue
		}
		selected = append(selected, protocol.DockerMount{
			Type:        mount.Type,
			Name:        mount.Name,
			Source:      mount.Source,
			Destination: mount.Destination,
			RW:          mount.RW,
		})
	}
	return selected
}

func portBindings(bindings map[string][]struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}) []protocol.DockerPortBinding {
	var result []protocol.DockerPortBinding
	for containerPort, hosts := range bindings {
		port, proto := splitPortProtocol(containerPort)
		if len(hosts) == 0 {
			result = append(result, protocol.DockerPortBinding{ContainerPort: port, Protocol: proto})
			continue
		}
		for _, host := range hosts {
			result = append(result, protocol.DockerPortBinding{
				ContainerPort: port,
				Protocol:      proto,
				HostIP:        host.HostIP,
				HostPort:      host.HostPort,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ContainerPort+result[i].HostPort < result[j].ContainerPort+result[j].HostPort
	})
	return result
}

func splitPortProtocol(value string) (string, string) {
	parts := strings.SplitN(value, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return value, ""
}

func dockerRunPortArg(port protocol.DockerPortBinding) string {
	container := strings.TrimSpace(port.ContainerPort)
	if container == "" {
		return ""
	}
	if protocol := strings.TrimSpace(port.Protocol); protocol != "" && protocol != "tcp" {
		container += "/" + protocol
	}
	host := strings.TrimSpace(port.HostPort)
	if host == "" {
		return container
	}
	if hostIP := strings.TrimSpace(port.HostIP); hostIP != "" {
		return hostIP + ":" + host + ":" + container
	}
	return host + ":" + container
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func commandError(action string, output []byte, err error) error {
	trimmed := strings.TrimSpace(string(output))
	if trimmed != "" {
		return fmt.Errorf("%s: %w: %s", action, err, trimmed)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func sourceDescriptionFromResolved(source protocol.DockerResolvedSource) string {
	switch {
	case source.Name != "":
		return source.Name
	case source.ContainerID != "":
		return source.ContainerID
	case source.Selection.ComposeProject != "" && source.Selection.ComposeService != "":
		return source.Selection.ComposeProject + "/" + source.Selection.ComposeService
	default:
		return "unnamed"
	}
}

func assertReadablePath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		return file.Close()
	}
	entries, err := os.ReadDir(path)
	if err != nil && len(entries) == 0 {
		return err
	}
	return nil
}

func toProtocolMounts(mounts []Mount) []protocol.DockerMount {
	result := make([]protocol.DockerMount, 0, len(mounts))
	for _, mount := range mounts {
		if mount.Type != "bind" && mount.Type != "volume" {
			continue
		}
		result = append(result, protocol.DockerMount{
			Type:        mount.Type,
			Name:        mount.Name,
			Source:      mount.Source,
			Destination: mount.Destination,
			RW:          mount.RW,
		})
	}
	return result
}

func composeInfo(labels map[string]string) protocol.DockerComposeInfo {
	if labels == nil {
		return protocol.DockerComposeInfo{}
	}
	configFiles := splitComposeFiles(labels["com.docker.compose.project.config_files"])
	return protocol.DockerComposeInfo{
		Project:     labels["com.docker.compose.project"],
		Service:     labels["com.docker.compose.service"],
		WorkingDir:  labels["com.docker.compose.project.working_dir"],
		ConfigFiles: configFiles,
	}
}

func splitComposeFiles(value string) []string {
	fields := strings.Split(value, ",")
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			result = append(result, field)
		}
	}
	return result
}

func containerWarnings(state string, mounts []protocol.DockerMount, compose protocol.DockerComposeInfo) []string {
	var warnings []string
	if state != "" && state != "running" {
		warnings = append(warnings, "container is not running")
	}
	if !hasPersistentData(mounts, compose) {
		warnings = append(warnings, "container has no persistent mounts or compose files")
	}
	return warnings
}

func hasPersistentData(mounts []protocol.DockerMount, compose protocol.DockerComposeInfo) bool {
	return len(mounts) > 0 || (compose.WorkingDir != "" && len(compose.ConfigFiles) > 0)
}

func cleanContainerNames(names []string) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.Trim(strings.TrimSpace(name), "/")
		if name != "" {
			result = append(result, name)
		}
	}
	return uniqueStrings(result)
}

func firstName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
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

func sourceDescription(source protocol.DockerContainerBackupSource) string {
	switch {
	case source.ContainerID != "":
		return source.ContainerID
	case source.ComposeProject != "" && source.ComposeService != "":
		return source.ComposeProject + "/" + source.ComposeService
	case source.Name != "":
		return source.Name
	default:
		return "unnamed"
	}
}
