package artifactnaming

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"vaultfleet/pkg/protocol"
)

const (
	LegacyRemoteDirTemplate        = "artifacts"
	LegacyArchiveNameTemplate      = "backup-{{datetime}}.{{ext}}"
	RecommendedRemoteDirTemplate   = "archives/{{agent_name}}/{{context_name}}/{{date}}"
	RecommendedArchiveNameTemplate = "{{context_name}}_{{agent_name}}_{{datetime}}.{{ext}}"

	SourceTypePath     = "path"
	SourceTypeDocker   = "docker"
	SourceTypeDatabase = "database"
	SourceTypeMixed    = "mixed"
)

var tokenPattern = regexp.MustCompile(`{{\s*([a-zA-Z0-9_]+)\s*}}`)

var supportedVariables = map[string]struct{}{
	"date":            {},
	"time":            {},
	"datetime":        {},
	"agent_id":        {},
	"agent_name":      {},
	"policy_id":       {},
	"policy_name":     {},
	"context_name":    {},
	"site_name":       {},
	"source_type":     {},
	"container_name":  {},
	"compose_project": {},
	"compose_service": {},
	"database_engine": {},
	"database_name":   {},
	"format":          {},
	"ext":             {},
}

type Context struct {
	AgentID       string
	AgentName     string
	PolicyID      string
	PolicyName    string
	ContextName   string
	ArchiveFormat string
	Now           time.Time
	Sources       []protocol.BackupSource
	Docker        *protocol.DockerBackupMetadata
	Database      *protocol.DatabaseBackupMetadata
}

type RenderInput struct {
	Context                Context
	RemoteDirTemplate      string
	NameTemplate           string
	UseRecommendedDefaults bool
}

func Render(input RenderInput) (protocol.ArtifactNamingMetadata, error) {
	now := input.Context.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	format := normalizeArchiveFormat(input.Context.ArchiveFormat)
	remoteTemplate, nameTemplate, legacy := effectiveTemplates(input.RemoteDirTemplate, input.NameTemplate, input.UseRecommendedDefaults)
	if err := validateTemplateSyntax(remoteTemplate, true); err != nil {
		return protocol.ArtifactNamingMetadata{}, err
	}
	if err := validateTemplateSyntax(nameTemplate, false); err != nil {
		return protocol.ArtifactNamingMetadata{}, err
	}

	sourceType := inferSourceType(input.Context.Sources, input.Context.Docker, input.Context.Database)
	contextName := firstNonEmpty(input.Context.ContextName, inferContextName(input.Context, sourceType), "backup")
	values := renderVariables(input.Context, now, format, sourceType, contextName)
	remoteDir := renderTemplate(remoteTemplate, values)
	artifactName := renderTemplate(nameTemplate, values)

	if err := validateRenderedRemoteDir(remoteDir); err != nil {
		return protocol.ArtifactNamingMetadata{}, err
	}
	if err := validateRenderedFilename(artifactName); err != nil {
		return protocol.ArtifactNamingMetadata{}, err
	}
	artifactPath := filepath.ToSlash(filepath.Join(remoteDir, artifactName))
	if err := validateRenderedPath(artifactPath); err != nil {
		return protocol.ArtifactNamingMetadata{}, err
	}

	warnings := collisionWarnings(nameTemplate)
	return protocol.ArtifactNamingMetadata{
		ContextName:       values["context_name"],
		SiteName:          values["site_name"],
		SourceType:        sourceType,
		RemoteDir:         remoteDir,
		ArtifactName:      artifactName,
		ArtifactPath:      artifactPath,
		RemoteDirTemplate: remoteTemplate,
		NameTemplate:      nameTemplate,
		Variables:         values,
		Warnings:          warnings,
		Legacy:            legacy,
	}, nil
}

func RecommendedPreview(input RenderInput) (protocol.ArtifactNamingMetadata, error) {
	input.UseRecommendedDefaults = true
	return Render(input)
}

func effectiveTemplates(remoteTemplate string, nameTemplate string, recommended bool) (string, string, bool) {
	remoteTemplate = strings.TrimSpace(remoteTemplate)
	nameTemplate = strings.TrimSpace(nameTemplate)
	if recommended {
		if remoteTemplate == "" {
			remoteTemplate = RecommendedRemoteDirTemplate
		}
		if nameTemplate == "" {
			nameTemplate = RecommendedArchiveNameTemplate
		}
		return remoteTemplate, nameTemplate, false
	}
	if remoteTemplate == "" && nameTemplate == "" {
		return LegacyRemoteDirTemplate, LegacyArchiveNameTemplate, true
	}
	if remoteTemplate == "" {
		remoteTemplate = RecommendedRemoteDirTemplate
	}
	if nameTemplate == "" {
		nameTemplate = RecommendedArchiveNameTemplate
	}
	return remoteTemplate, nameTemplate, false
}

func validateTemplateSyntax(template string, allowPathSeparators bool) error {
	if strings.TrimSpace(template) == "" {
		return fmt.Errorf("artifact naming template is required")
	}
	if hasControl(template) {
		return fmt.Errorf("artifact naming template contains control characters")
	}
	if !allowPathSeparators && strings.ContainsAny(template, `/\`) {
		return fmt.Errorf("archive filename template cannot contain path separators")
	}
	matches := tokenPattern.FindAllStringSubmatch(template, -1)
	for _, match := range matches {
		token := match[1]
		if _, ok := supportedVariables[token]; !ok {
			return fmt.Errorf("unsupported artifact naming variable %q", token)
		}
	}
	if strings.Contains(template, "{{") || strings.Contains(template, "}}") {
		stripped := tokenPattern.ReplaceAllString(template, "")
		if strings.Contains(stripped, "{{") || strings.Contains(stripped, "}}") {
			return fmt.Errorf("invalid artifact naming template token")
		}
	}
	return nil
}

func renderVariables(ctx Context, now time.Time, format string, sourceType string, contextName string) map[string]string {
	docker := dockerValues(ctx)
	databaseEngine, databaseName := databaseValues(ctx)
	values := map[string]string{
		"date":            now.UTC().Format("2006-01-02"),
		"time":            now.UTC().Format("150405"),
		"datetime":        now.UTC().Format("20060102-150405"),
		"agent_id":        sanitizeValue(ctx.AgentID),
		"agent_name":      sanitizeValue(firstNonEmpty(ctx.AgentName, ctx.AgentID, "agent")),
		"policy_id":       sanitizeValue(ctx.PolicyID),
		"policy_name":     sanitizeValue(ctx.PolicyName),
		"context_name":    sanitizeValue(contextName),
		"site_name":       sanitizeValue(contextName),
		"source_type":     sanitizeValue(sourceType),
		"container_name":  sanitizeValue(docker.containerName),
		"compose_project": sanitizeValue(docker.composeProject),
		"compose_service": sanitizeValue(docker.composeService),
		"database_engine": sanitizeValue(databaseEngine),
		"database_name":   sanitizeValue(databaseName),
		"format":          sanitizeValue(format),
		"ext":             sanitizeValue(format),
	}
	return values
}

func renderTemplate(template string, values map[string]string) string {
	return tokenPattern.ReplaceAllStringFunc(template, func(token string) string {
		matches := tokenPattern.FindStringSubmatch(token)
		if len(matches) != 2 {
			return ""
		}
		return values[matches[1]]
	})
}

func InferContextName(ctx Context) string {
	return inferContextName(ctx, inferSourceType(ctx.Sources, ctx.Docker, ctx.Database))
}

func InferSourceType(sources []protocol.BackupSource, docker *protocol.DockerBackupMetadata, database *protocol.DatabaseBackupMetadata) string {
	return inferSourceType(sources, docker, database)
}

func inferContextName(ctx Context, sourceType string) string {
	switch sourceType {
	case SourceTypeDocker:
		if docker := dockerValues(ctx); docker.composeProject != "" {
			return docker.composeProject
		} else if docker.containerName != "" {
			return docker.containerName
		}
	case SourceTypeDatabase:
		engine, name := databaseValues(ctx)
		if engine != "" && name != "" {
			return engine + "-" + name
		}
		if engine != "" {
			return engine + "-database"
		}
	case SourceTypePath:
		if len(ctx.Sources) == 1 {
			path := strings.TrimSpace(ctx.Sources[0].Path)
			if path != "" {
				base := filepath.Base(filepath.Clean(path))
				if base != "." && base != string(filepath.Separator) {
					return base
				}
			}
		}
	}
	return firstNonEmpty(ctx.PolicyName, ctx.PolicyID, ctx.AgentName, ctx.AgentID)
}

func inferSourceType(sources []protocol.BackupSource, docker *protocol.DockerBackupMetadata, database *protocol.DatabaseBackupMetadata) string {
	seen := map[string]struct{}{}
	for _, source := range sources {
		switch source.Type {
		case protocol.BackupSourceTypePath:
			seen[SourceTypePath] = struct{}{}
		case protocol.BackupSourceTypeDockerContainer:
			seen[SourceTypeDocker] = struct{}{}
		case protocol.BackupSourceTypeDatabase:
			seen[SourceTypeDatabase] = struct{}{}
		}
	}
	if len(seen) == 0 {
		if docker != nil && len(docker.Sources) > 0 {
			seen[SourceTypeDocker] = struct{}{}
		}
		if database != nil && len(database.Dumps) > 0 {
			seen[SourceTypeDatabase] = struct{}{}
		}
	}
	if len(seen) == 1 {
		for sourceType := range seen {
			return sourceType
		}
	}
	if len(seen) > 1 {
		return SourceTypeMixed
	}
	return SourceTypePath
}

type dockerContextValues struct {
	containerName  string
	composeProject string
	composeService string
}

func dockerValues(ctx Context) dockerContextValues {
	for _, source := range ctx.Sources {
		if source.Type != protocol.BackupSourceTypeDockerContainer || source.DockerContainer == nil {
			continue
		}
		return dockerContextValues{
			containerName:  firstNonEmpty(source.DockerContainer.Name, source.DockerContainer.ContainerID),
			composeProject: source.DockerContainer.ComposeProject,
			composeService: source.DockerContainer.ComposeService,
		}
	}
	if ctx.Docker != nil {
		for _, source := range ctx.Docker.Sources {
			return dockerContextValues{
				containerName:  firstNonEmpty(source.Name, source.ContainerID),
				composeProject: source.Compose.Project,
				composeService: source.Compose.Service,
			}
		}
	}
	for _, source := range ctx.Sources {
		if source.Type == protocol.BackupSourceTypeDatabase && source.Database != nil && source.Database.DockerContainer != nil {
			return dockerContextValues{
				containerName:  firstNonEmpty(source.Database.DockerContainer.Name, source.Database.DockerContainer.ContainerID),
				composeProject: source.Database.DockerContainer.ComposeProject,
				composeService: source.Database.DockerContainer.ComposeService,
			}
		}
	}
	return dockerContextValues{}
}

func databaseValues(ctx Context) (string, string) {
	for _, source := range ctx.Sources {
		if source.Type != protocol.BackupSourceTypeDatabase || source.Database == nil {
			continue
		}
		name := strings.TrimSpace(source.Database.Database)
		if source.Database.AllDatabases {
			name = "all-databases"
		}
		return source.Database.Engine, name
	}
	if ctx.Database != nil && len(ctx.Database.Dumps) > 0 {
		first := ctx.Database.Dumps[0]
		name := strings.TrimSpace(first.Database)
		if first.AllDatabases || name == "" && len(ctx.Database.Dumps) > 1 {
			name = "all-databases"
		}
		return first.Engine, name
	}
	return "", ""
}

func validateRenderedRemoteDir(path string) error {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return fmt.Errorf("archive remote directory cannot be empty")
	}
	return validateRenderedPath(path)
}

func validateRenderedFilename(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("archive filename cannot be empty")
	}
	if hasControl(name) {
		return fmt.Errorf("archive filename contains control characters")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("archive filename cannot contain path separators")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("archive filename cannot be %q", name)
	}
	return nil
}

func validateRenderedPath(path string) error {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return fmt.Errorf("artifact path cannot be empty")
	}
	if strings.HasPrefix(path, "/") || filepath.IsAbs(path) {
		return fmt.Errorf("artifact path cannot be absolute")
	}
	if hasControl(path) {
		return fmt.Errorf("artifact path contains control characters")
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("artifact path contains unsafe segment %q", segment)
		}
	}
	return nil
}

func collisionWarnings(template string) []protocol.ArtifactNamingWarning {
	for _, token := range []string{"{{datetime}}", "{{date}}", "{{time}}"} {
		if strings.Contains(template, token) {
			return nil
		}
	}
	return []protocol.ArtifactNamingWarning{{
		Code:    "possible_collision",
		Message: "Archive filename template has no time-varying token; future backups may collide or overwrite prior artifacts.",
		Source:  "archive_name_template",
	}}
}

func sanitizeValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		ok := r >= 'a' && r <= 'z' ||
			r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' ||
			r == '.' || r == '-' || r == '_'
		if ok {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	result := strings.Trim(b.String(), "._-")
	if result == "" {
		return "value"
	}
	return result
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 32 || r == 127 {
			return true
		}
	}
	return false
}

func normalizeArchiveFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case protocol.ArchiveFormatZip:
		return protocol.ArchiveFormatZip
	default:
		return protocol.ArchiveFormatTarGz
	}
}

func SupportedVariables() []string {
	result := make([]string, 0, len(supportedVariables))
	for name := range supportedVariables {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
