package daemon

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/version"
)

//go:embed docker-compose.yml
var dockerComposeYaml string

func Start() error {
	// Pipe the docker-compose.yml via stdin to docker compose
	cmd := exec.Command("docker", "compose", "-p", "agentregistry", "-f", "-", "up", "-d", "--wait")
	cmd.Stdin = strings.NewReader(dockerComposeYaml)
	cmd.Env = append(os.Environ(), fmt.Sprintf("VERSION=%s", version.Version), "DOCKER_REGISTRY=localhost:5001")
	if byt, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("failed to start docker compose: %v, output: %s", err, string(byt))
		return fmt.Errorf("failed to start docker compose: %w", err)
	}
	return nil
}

func IsRunning() bool {
	cmd := exec.Command("docker", "compose", "-p", "agentregistry", "-f", "-", "ps")
	cmd.Stdin = strings.NewReader(dockerComposeYaml)
	cmd.Env = append(os.Environ(), fmt.Sprintf("VERSION=%s", version.Version), "DOCKER_REGISTRY=localhost:5001")

	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("failed to check if daemon is running: %v, output: %s", err, string(output))
		return false
	}
	return strings.Contains(string(output), "agentregistry-server")
}

func IsDockerComposeAvailable() bool {
	cmd := exec.Command("docker", "compose", "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("docker compose is not available: %v, output: %s", err, string(output))
		return false
	}
	// Return true if the commands returns 0
	return true
}
