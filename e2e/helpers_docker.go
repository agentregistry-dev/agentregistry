//go:build e2e

// Docker-related test helpers for image and compose cleanup.

package e2e

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// CleanupDockerImage registers a t.Cleanup that removes a Docker image.
func CleanupDockerImage(t *testing.T, image string) {
	t.Helper()
	t.Cleanup(func() {
		t.Logf("Cleaning up Docker image: %s", image)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", image)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("Warning: failed to remove image %s: %v\n%s", image, err, string(out))
		}
	})
}

// CleanupDockerCompose registers a t.Cleanup that runs docker compose down in the given directory.
func CleanupDockerCompose(t *testing.T, projectDir string) {
	t.Helper()
	t.Cleanup(func() {
		t.Logf("Cleaning up Docker Compose in: %s", projectDir)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", "compose", "down", "--volumes", "--remove-orphans")
		cmd.Dir = projectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("Warning: docker compose down failed in %s: %v\n%s", projectDir, err, string(out))
		}
	})
}

// DockerImageExists checks if a Docker image exists locally.
func DockerImageExists(t *testing.T, image string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", image)
	err := cmd.Run()
	return err == nil
}
