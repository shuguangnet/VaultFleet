package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"vaultfleet/pkg/protocol"
)

type BuildInput struct {
	AgentID       string
	AgentVersion  string
	GeneratedAt   time.Time
	Policy        *protocol.PolicyPushPayload
	BackupMode    string
	ArchiveFormat string
	BackupDirs    []string
	Excludes      []string
	Docker        *protocol.DockerBackupMetadata
	Database      *protocol.DatabaseBackupMetadata
	ContextName   string
	SourceType    string
	Warnings      []protocol.ManifestWarning
}

func Build(input BuildInput) *protocol.BackupContentManifest {
	generatedAt := input.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}
	hostname, _ := os.Hostname()
	backupMode := strings.TrimSpace(input.BackupMode)
	if backupMode == "" && input.Policy != nil {
		backupMode = input.Policy.BackupMode
	}
	if backupMode == "" {
		backupMode = protocol.BackupModeSnapshot
	}
	archiveFormat := strings.TrimSpace(input.ArchiveFormat)
	if archiveFormat == "" && input.Policy != nil {
		archiveFormat = input.Policy.ArchiveFormat
	}

	manifest := &protocol.BackupContentManifest{
		Version:       protocol.BackupContentManifestVersion,
		GeneratedAt:   generatedAt.UTC(),
		BackupMode:    backupMode,
		ArchiveFormat: archiveFormat,
		Agent: protocol.ManifestAgent{
			ID:       firstNonEmpty(input.AgentID, policyAgentID(input.Policy)),
			Version:  strings.TrimSpace(input.AgentVersion),
			Hostname: hostname,
		},
		Policy:          buildPolicySummary(input.Policy, backupMode, archiveFormat),
		Sources:         protocol.ManifestSources{},
		ExcludePatterns: cleanedStrings(input.Excludes),
		Warnings:        append([]protocol.ManifestWarning(nil), input.Warnings...),
		ContextName:     strings.TrimSpace(input.ContextName),
		SiteName:        strings.TrimSpace(input.ContextName),
		SourceType:      strings.TrimSpace(input.SourceType),
	}

	databasePaths := databaseOutputPaths(input.Database)
	manifest.Sources.Paths = buildPathSources(input.BackupDirs, databasePaths)
	manifest.Sources.Docker = buildDockerSources(input.Docker)
	manifest.Sources.Databases = buildDatabaseDumps(input.Database)
	manifest.Warnings = append(manifest.Warnings, dockerWarnings(input.Docker)...)
	manifest.Warnings = append(manifest.Warnings, databaseWarnings(input.Database)...)
	return manifest
}

func buildPolicySummary(policy *protocol.PolicyPushPayload, backupMode string, archiveFormat string) protocol.ManifestPolicy {
	if policy == nil {
		return protocol.ManifestPolicy{BackupMode: backupMode, ArchiveFormat: archiveFormat}
	}
	return protocol.ManifestPolicy{
		BackupMode:    backupMode,
		ArchiveFormat: archiveFormat,
		StorageType:   strings.TrimSpace(policy.Storage.RcloneType),
		Repository:    strings.TrimSpace(policy.Storage.RepoPath),
	}
}

func buildPathSources(paths []string, databasePaths map[string]struct{}) []protocol.ManifestPathSource {
	cleaned := cleanedStrings(paths)
	result := make([]protocol.ManifestPathSource, 0, len(cleaned))
	for _, path := range cleaned {
		source := protocol.ManifestPathSource{
			Path: path,
			Kind: "path",
		}
		if _, ok := databasePaths[path]; ok {
			source.Kind = "database_dump"
			source.Origin = "database"
		}
		if filepath.Base(path) == protocol.BackupContentManifestName {
			source.Kind = "manifest"
			source.Origin = "vaultfleet"
		}
		result = append(result, source)
	}
	return result
}

func buildDockerSources(metadata *protocol.DockerBackupMetadata) []protocol.ManifestDockerSource {
	if metadata == nil {
		return nil
	}
	result := make([]protocol.ManifestDockerSource, 0, len(metadata.Sources))
	for _, source := range metadata.Sources {
		warnings := make([]protocol.ManifestWarning, 0, len(source.Warnings))
		for _, warning := range source.Warnings {
			warnings = append(warnings, protocol.ManifestWarning{Code: "docker_source_warning", Message: warning, Source: firstNonEmpty(source.Name, source.ContainerID)})
		}
		result = append(result, protocol.ManifestDockerSource{
			ContainerID:        source.ContainerID,
			Name:               source.Name,
			Image:              source.Image,
			ComposeProject:     source.Compose.Project,
			ComposeService:     source.Compose.Service,
			ComposeWorkingDir:  source.Compose.WorkingDir,
			ComposeConfigFiles: append([]string(nil), source.Compose.ConfigFiles...),
			Mounts:             append([]protocol.DockerMount(nil), source.Mounts...),
			ResolvedPaths:      append([]string(nil), source.ResolvedPaths...),
			Warnings:           warnings,
		})
	}
	return result
}

func buildDatabaseDumps(metadata *protocol.DatabaseBackupMetadata) []protocol.ManifestDatabaseDump {
	if metadata == nil {
		return nil
	}
	result := make([]protocol.ManifestDatabaseDump, 0, len(metadata.Dumps))
	for _, dump := range metadata.Dumps {
		warnings := make([]protocol.ManifestWarning, 0, len(dump.Warnings))
		for _, warning := range dump.Warnings {
			warnings = append(warnings, protocol.ManifestWarning{Code: "database_dump_warning", Message: warning, Source: dump.OutputName})
		}
		result = append(result, protocol.ManifestDatabaseDump{
			Engine:        dump.Engine,
			ExecutionMode: dump.ExecutionMode,
			Database:      dump.Database,
			AllDatabases:  dump.AllDatabases,
			ContainerName: dump.ContainerName,
			OutputName:    dump.OutputName,
			Size:          dump.Size,
			Compressed:    dump.Compressed,
			Warnings:      warnings,
		})
	}
	return result
}

func databaseOutputPaths(metadata *protocol.DatabaseBackupMetadata) map[string]struct{} {
	paths := map[string]struct{}{}
	if metadata == nil {
		return paths
	}
	for _, dump := range metadata.Dumps {
		if path := strings.TrimSpace(dump.OutputPath); path != "" {
			paths[path] = struct{}{}
		}
	}
	return paths
}

func dockerWarnings(metadata *protocol.DockerBackupMetadata) []protocol.ManifestWarning {
	if metadata == nil {
		return nil
	}
	warnings := make([]protocol.ManifestWarning, 0, len(metadata.Warnings))
	for _, warning := range metadata.Warnings {
		warnings = append(warnings, protocol.ManifestWarning{Code: "docker_warning", Message: warning, Source: "docker"})
	}
	return warnings
}

func databaseWarnings(metadata *protocol.DatabaseBackupMetadata) []protocol.ManifestWarning {
	if metadata == nil {
		return nil
	}
	warnings := make([]protocol.ManifestWarning, 0, len(metadata.Warnings))
	for _, warning := range metadata.Warnings {
		warnings = append(warnings, protocol.ManifestWarning{Code: "database_warning", Message: warning, Source: "database"})
	}
	return warnings
}

func cleanedStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
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

func policyAgentID(policy *protocol.PolicyPushPayload) string {
	if policy == nil {
		return ""
	}
	return policy.AgentID
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
