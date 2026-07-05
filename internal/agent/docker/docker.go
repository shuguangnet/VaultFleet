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
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
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
		State:         inspect.State.Status,
		ResolvedPaths: paths,
		Warnings:      warnings,
	}, nil
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
