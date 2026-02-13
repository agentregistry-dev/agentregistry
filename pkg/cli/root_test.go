package cli

import (
	"sort"
	"testing"

	"github.com/spf13/cobra"
)

// TestAllCommandsRegistered ensures every expected CLI command and subcommand
// is registered on the root cobra command tree. If a new command is added to
// the source but not to the expected list (or vice versa), this test fails.
func TestAllCommandsRegistered(t *testing.T) {
	root := Root()

	// Top-level commands registered on rootCmd.
	// Hidden commands (import, export) are included — they are still registered.
	expectedTopLevel := []string{
		"agent",
		"configure",
		"embeddings",
		"export",
		"import",
		"mcp",
		"skill",
		"version",
	}

	gotTopLevel := commandNames(root)
	assertEqualSorted(t, "root", expectedTopLevel, gotTopLevel)

	// Subcommands for each parent that has them.
	expectedSubcommands := map[string][]string{
		"agent": {
			"add-mcp",
			"add-skill",
			"build",
			"delete",
			"deploy",
			"init",
			"list",
			"publish",
			"remove",
			"run",
			"show",
			"unpublish",
		},
		"mcp": {
			"add-tool",
			"build",
			"delete",
			"deploy",
			"init",
			"list",
			"publish",
			"remove",
			"run",
			"show",
			"unpublish",
		},
		"skill": {
			"delete",
			"init",
			"list",
			"publish",
			"pull",
			"remove",
			"show",
			"unpublish",
		},
		"embeddings": {
			"generate",
		},
	}

	for _, cmd := range root.Commands() {
		expected, ok := expectedSubcommands[cmd.Name()]
		if !ok {
			// Commands like "version", "import", "export", "configure" have
			// no subcommands to verify.
			continue
		}
		got := commandNames(cmd)
		assertEqualSorted(t, cmd.Name(), expected, got)
	}
}

// commandNames returns the sorted names of a command's direct children.
func commandNames(cmd *cobra.Command) []string {
	children := cmd.Commands()
	names := make([]string, 0, len(children))
	for _, c := range children {
		names = append(names, c.Name())
	}
	sort.Strings(names)
	return names
}

// assertEqualSorted compares two string slices after sorting.
func assertEqualSorted(t *testing.T, context string, expected, got []string) {
	t.Helper()

	sortedExpected := make([]string, len(expected))
	copy(sortedExpected, expected)
	sort.Strings(sortedExpected)

	sortedGot := make([]string, len(got))
	copy(sortedGot, got)
	sort.Strings(sortedGot)

	if len(sortedExpected) != len(sortedGot) {
		t.Errorf("[%s] command count mismatch: expected %d, got %d\n  expected: %v\n  got:      %v",
			context, len(sortedExpected), len(sortedGot), sortedExpected, sortedGot)
		return
	}

	for i := range sortedExpected {
		if sortedExpected[i] != sortedGot[i] {
			t.Errorf("[%s] command mismatch at index %d: expected %q, got %q\n  expected: %v\n  got:      %v",
				context, i, sortedExpected[i], sortedGot[i], sortedExpected, sortedGot)
			return
		}
	}
}
