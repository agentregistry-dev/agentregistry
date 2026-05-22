package inspector

import (
	"context"
	"os/exec"
	"testing"
)

func TestLaunch_BuildsExpectedArgv(t *testing.T) {
	var gotName string
	var gotArgs []string
	fakeFactory := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = args
		// Return a cmd we never actually start; the fake starter swallows it.
		return exec.CommandContext(ctx, "echo")
	}

	_, err := launchWith(context.Background(), "http://localhost:3000/mcp", fakeFactory, fakeStarter)
	if err != nil {
		t.Fatalf("launchWith returned error: %v", err)
	}
	if gotName != "npx" {
		t.Errorf("expected npx, got %q", gotName)
	}
	want := []string{"-y", "@modelcontextprotocol/inspector", "--server-url", "http://localhost:3000/mcp"}
	if len(gotArgs) != len(want) {
		t.Fatalf("argv length: got %d want %d (%v)", len(gotArgs), len(want), gotArgs)
	}
	for i := range want {
		if gotArgs[i] != want[i] {
			t.Errorf("argv[%d]: got %q want %q", i, gotArgs[i], want[i])
		}
	}
}

func TestLaunch_ReturnsErrorWhenStartFails(t *testing.T) {
	fakeFactory := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "/nonexistent/path")
	}
	failingStarter := func(_ *exec.Cmd) error {
		return exec.ErrNotFound
	}

	cmd, err := launchWith(context.Background(), "http://localhost:3000/mcp", fakeFactory, failingStarter)
	if err == nil {
		t.Fatalf("expected error, got cmd=%v err=nil", cmd)
	}
	if cmd != nil {
		t.Errorf("expected nil cmd on error, got %v", cmd)
	}
}

// fakeStarter returns nil so Launch treats it as a successful start.
func fakeStarter(_ *exec.Cmd) error { return nil }
