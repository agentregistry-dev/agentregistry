package daemon

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func TestDefaultDaemonManager_ImplementsInterface(t *testing.T) {
	// Verify that DefaultDaemonManager implements the full DaemonManager interface,
	// including the Stop() method.
	var _ types.DaemonManager = (*DefaultDaemonManager)(nil)
}

func TestNewDaemonManager_DefaultConfig(t *testing.T) {
	dm := NewDaemonManager(nil)
	if dm.config.ProjectName != "agentregistry" {
		t.Errorf("expected default project name 'agentregistry', got %q", dm.config.ProjectName)
	}
	if dm.config.ContainerName != "agentregistry-server" {
		t.Errorf("expected default container name 'agentregistry-server', got %q", dm.config.ContainerName)
	}
}

func TestNewDaemonManager_CustomConfig(t *testing.T) {
	dm := NewDaemonManager(&types.DaemonConfig{
		ProjectName:   "custom-project",
		ContainerName: "custom-container",
	})
	if dm.config.ProjectName != "custom-project" {
		t.Errorf("expected project name 'custom-project', got %q", dm.config.ProjectName)
	}
	if dm.config.ContainerName != "custom-container" {
		t.Errorf("expected container name 'custom-container', got %q", dm.config.ContainerName)
	}
}
