package common

import (
	"fmt"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/client"
)

var (
	defaultWaitTimeout  = 5 * time.Minute
	defaultPollInterval = 2 * time.Second
)

// WaitForDeploymentReady polls a deployment until it reaches a terminal state
// (deployed or failed). Returns an error if the deployment fails or the timeout
// is exceeded.
func WaitForDeploymentReady(c *client.Client, deploymentID string) error {
	deadline := time.Now().Add(defaultWaitTimeout)

	for {
		dep, err := c.GetDeploymentByID(deploymentID)
		if err != nil {
			return fmt.Errorf("polling deployment status: %w", err)
		}
		if dep == nil {
			return fmt.Errorf("deployment %s not found", deploymentID)
		}

		switch dep.Status {
		case "deployed":
			return nil
		case "failed":
			return fmt.Errorf("deployment failed%s", formatDeploymentWaitError(dep.Error))
		case "cancelled":
			return fmt.Errorf("deployment was cancelled%s", formatDeploymentWaitError(dep.Error))
		}

		if time.Now().After(deadline) {
			return fmt.Errorf(
				"timed out waiting for deployment to become ready (current status: %s%s)",
				dep.Status,
				formatDeploymentWaitError(dep.Error),
			)
		}

		time.Sleep(defaultPollInterval)
	}
}

func formatDeploymentWaitError(errorText string) string {
	trimmed := strings.TrimSpace(errorText)
	if trimmed == "" {
		return ""
	}
	return ": " + trimmed
}
