package cli

import (
	"slices"
	"testing"

	"github.com/spf13/cobra"
)

// TestCommandTree verifies the CLI command hierarchy is correct.
func TestCommandTree(t *testing.T) {
	root := Root()

	// Top-level commands on the current (declarative) CLI surface.
	// Imperative CRUD subcommands (publish, list, delete, show, init, build
	// under agent/mcp/skill/prompt) were removed; init/build/apply/get/delete
	// are now top-level declarative commands.
	expectedTopLevel := []string{
		"agent",
		"apply",
		"build",
		"configure",
		"daemon",
		"delete",
		"deployments",
		"get",
		"init",
		"mcp",
		"skill",
		"version",
	}

	gotTopLevel := childNames(root)
	slices.Sort(expectedTopLevel)
	slices.Sort(gotTopLevel)

	if len(expectedTopLevel) != len(gotTopLevel) {
		t.Fatalf("top-level command count: got %d, want %d\n  got:  %v\n  want: %v",
			len(gotTopLevel), len(expectedTopLevel), gotTopLevel, expectedTopLevel)
	}
	for i := range expectedTopLevel {
		if expectedTopLevel[i] != gotTopLevel[i] {
			t.Errorf("top-level command mismatch at index %d: got %q, want %q\n  got:  %v\n  want: %v",
				i, gotTopLevel[i], expectedTopLevel[i], gotTopLevel, expectedTopLevel)
			break
		}
	}

	// Verify subcommand counts for parent commands with surviving subcommands.
	expectedSubcmdCounts := map[string]int{
		// run
		"agent": 1,
		// add-tool, run
		"mcp": 2,
		// pull
		"skill": 1,
		// create, list, show, delete
		"deployments": 4,
	}

	for _, cmd := range root.Commands() {
		expected, ok := expectedSubcmdCounts[cmd.Name()]
		if !ok {
			continue
		}
		got := len(cmd.Commands())
		if got != expected {
			t.Errorf("%s subcommand count: got %d, want %d (commands: %v)",
				cmd.Name(), got, expected, childNames(cmd))
		}
	}
}

// TestCommandsHaveRequiredMetadata verifies every command has Use and Short fields set.
func TestCommandsHaveRequiredMetadata(t *testing.T) {
	root := Root()

	var walk func(cmd *cobra.Command, path string)
	walk = func(cmd *cobra.Command, path string) {
		if cmd.Use == "" {
			t.Errorf("%s: Use field is empty", path)
		}
		if cmd.Short == "" {
			t.Errorf("%s: Short field is empty", path)
		}
		for _, child := range cmd.Commands() {
			walk(child, path+"/"+child.Name())
		}
	}

	for _, cmd := range root.Commands() {
		walk(cmd, "arctl/"+cmd.Name())
	}
}

// TestHiddenCommands verifies visibility of top-level commands.
func TestHiddenCommands(t *testing.T) {
	root := Root()

	tests := []struct {
		name       string
		wantHidden bool
	}{
		{"agent", false},
		{"mcp", false},
		{"skill", false},
		{"version", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := findSubcommand(root, tt.name)
			if cmd == nil {
				t.Fatalf("command %q not found", tt.name)
				return
			}
			if cmd.Hidden != tt.wantHidden {
				t.Errorf("command %q Hidden = %v, want %v", tt.name, cmd.Hidden, tt.wantHidden)
			}
		})
	}
}

// TestRootPersistentFlags verifies persistent flags on the root command.
func TestRootPersistentFlags(t *testing.T) {
	root := Root()

	persistentFlags := []string{"registry-url", "registry-token"}
	for _, name := range persistentFlags {
		t.Run(name, func(t *testing.T) {
			f := root.PersistentFlags().Lookup(name)
			if f == nil {
				t.Fatalf("persistent flag --%s not found on root command", name)
			}
		})
	}
}

// childNames returns sorted names of a command's direct children.
func childNames(cmd *cobra.Command) []string {
	children := cmd.Commands()
	names := make([]string, 0, len(children))
	for _, c := range children {
		names = append(names, c.Name())
	}
	slices.Sort(names)
	return names
}

// findSubcommand finds a direct child command by name.
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}
