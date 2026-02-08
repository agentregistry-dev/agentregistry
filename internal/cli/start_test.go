package cli

import (
	"testing"
)

func TestStartCmd_Config(t *testing.T) {
	if StartCmd.Use != "start" {
		t.Errorf("expected Use to be 'start', got %q", StartCmd.Use)
	}
	if StartCmd.Short == "" {
		t.Error("expected Short description to be non-empty")
	}
	if StartCmd.RunE == nil {
		t.Error("expected RunE to be set")
	}
	if StartCmd.PersistentPreRunE == nil {
		t.Error("expected PersistentPreRunE override to be set")
	}
	// The PersistentPreRunE override should return nil (bypass auto-start).
	if err := StartCmd.PersistentPreRunE(nil, nil); err != nil {
		t.Errorf("expected PersistentPreRunE to return nil, got %v", err)
	}
}
