package common

import (
	"context"
	"fmt"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/client"
)

const (
	defaultWaitTimeout  = 5 * time.Minute
	defaultPollInterval = 2 * time.Second
)

// WaitForDeploymentReady polls a deployment until it reaches a terminal state
// (deployed or failed). Returns an error if the deployment fails or the timeout
// is exceeded.
func WaitForDeploymentReady(c *client.Client, deploymentID string) error {
	deadline := time.Now().Add(defaultWaitTimeout)

	for {
		dep, err := FindDeploymentByIDPrefix(context.Background(), c, deploymentID)
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
			if dep.Error != "" {
				return fmt.Errorf("deployment failed: %s", dep.Error)
			}
			return fmt.Errorf("deployment failed")
		case "undeployed", "terminating":
			return fmt.Errorf("deployment is %s", dep.Status)
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for deployment to become ready (current status: %s)", dep.Status)
		}

		time.Sleep(defaultPollInterval)
	}
}
