package utils

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/daemon"
)

// EnsureRegistryConnection checks for Docker Compose, starts the daemon if needed, and initializes the API client.
func EnsureRegistryConnection() (*client.Client, error) {
	// Check if docker compose is available
	if !daemon.IsDockerComposeAvailable() {
		fmt.Println("Docker compose is not available. Please install docker compose and try again.")
		fmt.Println("See https://docs.docker.com/compose/install/ for installation instructions.")
		fmt.Println("agent registry uses docker compose to start the server and the agent gateway.")
		return nil, fmt.Errorf("docker compose is not available")
	}
	if !daemon.IsRunning() {
		if err := daemon.Start(); err != nil {
			return nil, fmt.Errorf("failed to start daemon: %w", err)
		}
	}
	// Check if local registry is running
	c, err := client.NewClientFromEnv()
	if err != nil {
		return nil, fmt.Errorf("API client not initialized: %w", err)
	}
	return c, nil
}
