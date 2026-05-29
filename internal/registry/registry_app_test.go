package registry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
)

func TestDeploymentControllerConfigMapsRetentionSettings(t *testing.T) {
	cfg := &config.Config{
		ControllerEventRetention:           2 * time.Hour,
		ControllerEventKeepAfterRevision:   42,
		ControllerWorkRetention:            3 * time.Hour,
		ControllerAttemptRetention:         4 * time.Hour,
		ControllerRetentionPruneBatchLimit: 17,
	}

	got := deploymentControllerConfig(cfg)

	require.Equal(t, 2*time.Hour, got.Retention.ControlPlaneEvents)
	require.Equal(t, int64(42), got.Retention.EventKeepAfterRev)
	require.Equal(t, 3*time.Hour, got.Retention.ReconcileWork)
	require.Equal(t, 4*time.Hour, got.Retention.ReconcileAttempts)
	require.Equal(t, 17, got.Retention.BatchLimit)
}
