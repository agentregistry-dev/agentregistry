package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// buildCmd constructs a command tree so CommandPath() returns the desired path.
// e.g. buildCmd("arctl", "agent", "init") → cmd whose CommandPath() is "arctl agent init".
func buildCmd(names ...string) *cobra.Command {
	if len(names) == 0 {
		return &cobra.Command{}
	}
	root := &cobra.Command{Use: names[0]}
	cur := root
	for _, n := range names[1:] {
		child := &cobra.Command{Use: n}
		cur.AddCommand(child)
		cur = child
	}
	return cur
}

func TestIsOfflineCommand(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  bool
	}{
		{"agent init is offline", []string{"arctl", "agent", "init"}, true},
		{"mcp init is offline", []string{"arctl", "mcp", "init"}, true},
		{"mcp init go subcommand is offline", []string{"arctl", "mcp", "init", "go"}, true},
		{"mcp init python subcommand is offline", []string{"arctl", "mcp", "init", "python"}, true},
		{"agent list needs daemon", []string{"arctl", "agent", "list"}, false},
		{"mcp list needs daemon", []string{"arctl", "mcp", "list"}, false},
		{"agent publish needs daemon", []string{"arctl", "agent", "publish"}, false},
		{"root command needs daemon", []string{"arctl"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := buildCmd(tt.parts...)
			if got := isOfflineCommand(cmd); got != tt.want {
				t.Errorf("isOfflineCommand(%q) = %v, want %v", cmd.CommandPath(), got, tt.want)
			}
		})
	}
}
