package build

import (
	"os"
	"strings"
	"testing"
)

func TestInstallScriptMatchesAcceptanceContract(t *testing.T) {
	data, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)

	if strings.Contains(script, `-z "$MASTER_URL" || -z "$ENROLL_TOKEN" || -z "$AGENT_URL" || -z "$AGENT_SHA256"`) {
		t.Fatal("agent url and sha256 must be optional for acceptance installer flow")
	}
	for _, want := range []string{
		`GITHUB_REPO="shuguangnet/VaultFleet"`,
		`GITHUB_PROXY=""`,
		`--github-proxy`,
		`--agent-url <agent-binary-url>`,
		`if [[ -z "$AGENT_URL" ]]; then`,
		`AGENT_URL="${MASTER_URL%/}/download/agent-linux-${ARCH}"`,
		`proxy_url "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/restic_${RESTIC_VERSION}_${OS}_${ARCH}.bz2"`,
		`local prefix="${GITHUB_PROXY%/}"`,
		`if [[ "$url" == "$prefix"/* ]]; then`,
		`echo "${prefix}/${url}"`,
		`if [[ ! -x "${INSTALL_DIR}/restic-real" ]]; then`,
		`if [[ -x "${INSTALL_DIR}/restic" ]]; then`,
		`mv "${INSTALL_DIR}/restic" "${INSTALL_DIR}/restic-real"`,
		`install_file "$restic_bin" "${INSTALL_DIR}/restic-real"`,
		`cat > "${INSTALL_DIR}/restic"`,
		`if [[ "\${1:-}" == "--version" ]]; then`,
		`exec "${INSTALL_DIR}/restic-real" version`,
		`if [[ ! -x "${INSTALL_DIR}/rclone" ]]; then`,
		"ensure_command bunzip2 bzip2",
		"ensure_command unzip unzip",
		"apt-get install -y --no-install-recommends",
		"apk add --no-cache",
		"command -v rc-update",
		"rc-service vaultfleet-agent restart",
		"nohup \"$INSTALL_DIR/vaultfleet-agent\" --config \"$CONFIG_DIR/agent.yaml\"",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing %q", want)
		}
	}
}

func TestDockerfileInstallsRcloneForMasterStorageChecks(t *testing.T) {
	data, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(data)

	for _, want := range []string{
		"rclone",
		"apt-get install -y --no-install-recommends ca-certificates tzdata rclone iproute2",
		"ENTRYPOINT [\"vaultfleet-entrypoint\"]",
		"host.docker.internal",
		"host-gateway",
		`if [ "${1#-}" != "$1" ]; then set -- vaultfleet-master "$@"; fi`,
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing %q", want)
		}
	}
}
