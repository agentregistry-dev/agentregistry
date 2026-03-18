package daemon

import (
	"bytes"
	"errors"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// mockDaemonManager for unit tests.
type mockDaemonManager struct {
	running     bool
	startCalled bool
	startErr    error
	stopCalled  bool
	stopErr     error
	purgeCalled bool
	purgeErr    error
}

func (m *mockDaemonManager) IsRunning() bool { return m.running }
func (m *mockDaemonManager) Start() error    { m.startCalled = true; return m.startErr }
func (m *mockDaemonManager) Stop() error     { m.stopCalled = true; return m.stopErr }
func (m *mockDaemonManager) Purge() error    { m.purgeCalled = true; return m.purgeErr }

var _ types.DaemonManager = (*mockDaemonManager)(nil)

func TestStartCmd(t *testing.T) {
	tests := []struct {
		name            string
		dm              *mockDaemonManager
		wantErr         bool
		wantStartCalled bool
	}{
		{
			name:            "starts successfully",
			dm:              &mockDaemonManager{},
			wantStartCalled: true,
		},
		{
			name:            "start fails",
			dm:              &mockDaemonManager{startErr: errors.New("docker compose not available")},
			wantErr:         true,
			wantStartCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := New(tt.dm)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"start"})

			err := cmd.Execute()
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.dm.startCalled != tt.wantStartCalled {
				t.Errorf("startCalled = %v, want %v", tt.dm.startCalled, tt.wantStartCalled)
			}
		})
	}
}

func TestStopCmd(t *testing.T) {
	tests := []struct {
		name            string
		dm              *mockDaemonManager
		purge           bool
		wantErr         bool
		wantStopCalled  bool
		wantPurgeCalled bool
	}{
		{
			name:           "stop successfully",
			dm:             &mockDaemonManager{},
			wantStopCalled: true,
		},
		{
			name:           "stop fails",
			dm:             &mockDaemonManager{stopErr: errors.New("stop failed")},
			wantErr:        true,
			wantStopCalled: true,
		},
		{
			name:            "purge successfully",
			dm:              &mockDaemonManager{},
			purge:           true,
			wantPurgeCalled: true,
		},
		{
			name:            "purge fails",
			dm:              &mockDaemonManager{purgeErr: errors.New("purge failed")},
			purge:           true,
			wantErr:         true,
			wantPurgeCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := New(tt.dm)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			args := []string{"stop"}
			if tt.purge {
				args = append(args, "--purge")
			}
			cmd.SetArgs(args)

			err := cmd.Execute()
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.dm.stopCalled != tt.wantStopCalled {
				t.Errorf("stopCalled = %v, want %v", tt.dm.stopCalled, tt.wantStopCalled)
			}
			if tt.dm.purgeCalled != tt.wantPurgeCalled {
				t.Errorf("purgeCalled = %v, want %v", tt.dm.purgeCalled, tt.wantPurgeCalled)
			}
		})
	}
}

func TestStatusCmd(t *testing.T) {
	tests := []struct {
		name    string
		dm      *mockDaemonManager
		wantErr bool
	}{
		{
			name: "running",
			dm:   &mockDaemonManager{running: true},
		},
		{
			name: "not running",
			dm:   &mockDaemonManager{running: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := New(tt.dm)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"status"})

			err := cmd.Execute()
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
