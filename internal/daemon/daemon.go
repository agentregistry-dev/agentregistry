package daemon

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/version"
)

//go:embed docker-compose.yml
var dockerComposeYaml string

func Start() error {
	fmt.Println("Starting agentregistry daemon...")
	// Pipe the docker-compose.yml via stdin to docker compose
	cmd := exec.Command("docker", "compose", "-p", "agentregistry", "-f", "-", "up", "-d", "--wait")
	cmd.Stdin = strings.NewReader(dockerComposeYaml)
	cmd.Env = append(os.Environ(), fmt.Sprintf("VERSION=%s", version.Version), fmt.Sprintf("DOCKER_REGISTRY=%s", version.DockerRegistry))
	if byt, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("failed to start docker compose: %v, output: %s", err, string(byt))
		return fmt.Errorf("failed to start docker compose: %w", err)
	}

	fmt.Println("✓ Agentregistry daemon started successfully")

	_, err := client.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	fmt.Println("✓ API connected successfully")
	return nil
}

func IsRunning() bool {
	cmd := exec.Command("docker", "compose", "-p", "agentregistry", "-f", "-", "ps")
	cmd.Stdin = strings.NewReader(dockerComposeYaml)
	cmd.Env = append(os.Environ(), fmt.Sprintf("VERSION=%s", version.Version), fmt.Sprintf("DOCKER_REGISTRY=%s", version.DockerRegistry))
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
