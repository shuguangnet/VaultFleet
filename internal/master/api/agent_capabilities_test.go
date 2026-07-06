package api

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"vaultfleet/pkg/protocol"
)

func TestSystemInfoHasCompatibleDockerRestoreCapability(t *testing.T) {
	raw := `{"version":"v0.5.22","capabilities":["docker_workload_backups","typed_backup_sources"]}`

	assert.True(t, systemInfoHasCapability(raw, protocol.CapabilityDockerContainerRestore))
}

func TestSystemInfoDoesNotInferDockerRestoreForOlderAgents(t *testing.T) {
	raw := `{"version":"v0.5.20","capabilities":["docker_workload_backups","typed_backup_sources"]}`

	assert.False(t, systemInfoHasCapability(raw, protocol.CapabilityDockerContainerRestore))
}
