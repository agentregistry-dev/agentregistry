package cli

import (
	"testing"
)

func TestStopCmd_Config(t *testing.T) {
	if StopCmd.Use != "stop" {
		t.Errorf("expected Use to be 'stop', got %q", StopCmd.Use)
	}
	if StopCmd.Short == "" {
		t.Error("expected Short description to be non-empty")
	}
	if StopCmd.RunE == nil {
		t.Error("expected RunE to be set")
	}
	if StopCmd.PersistentPreRunE == nil {
		t.Error("expected PersistentPreRunE override to be set")
	}
	// The PersistentPreRunE override should return nil (bypass auto-start).
	if err := StopCmd.PersistentPreRunE(nil, nil); err != nil {
		t.Errorf("expected PersistentPreRunE to return nil, got %v", err)
	}
}
