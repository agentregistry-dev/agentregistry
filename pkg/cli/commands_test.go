package cli

import (
	"fmt"
	"slices"
	"testing"

	"github.com/spf13/cobra"
)

// TestCommandTree verifies the CLI command hierarchy is correct.
func TestCommandTree(t *testing.T) {
	root := Root()

	expectedTopLevel := []string{
		"agent",
		"configure",
		"embeddings",
		"export",
		"import",
		"mcp",
		"prompt",
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

	// Verify subcommand counts for parent commands
	expectedSubcmdCounts := map[string]int{
		// init, build, run, add-skill, add-prompt, add-mcp, publish, delete, deploy, list, show
		"agent": 11,
		// init, build, add-tool, publish, delete, deploy, list, run, show
		"mcp": 9,
		// init, list, publish, delete, pull, show
		"skill": 6,
		// list, publish, delete, show
		"prompt": 4,
		// generate
		"embeddings": 1,
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

// TestHiddenCommands verifies that import and export are hidden.
func TestHiddenCommands(t *testing.T) {
	root := Root()

	tests := []struct {
		name       string
		wantHidden bool
	}{
		{"import", true},
		{"export", true},
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
			}
			if cmd.Hidden != tt.wantHidden {
				t.Errorf("command %q Hidden = %v, want %v", tt.name, cmd.Hidden, tt.wantHidden)
			}
		})
	}
}

// TestAgentInitFlags verifies flag registration on agent init.
func TestAgentInitFlags(t *testing.T) {
	root := Root()
	agentCmd := findSubcommand(root, "agent")
	if agentCmd == nil {
		t.Fatal("agent command not found")
	}
	initCmd := findSubcommand(agentCmd, "init")
	if initCmd == nil {
		t.Fatal("agent init command not found")
	}

	tests := []struct {
		flag     string
		defValue string
	}{
		{"instruction-file", ""},
		{"model-provider", "Gemini"},
		{"model-name", "gemini-2.0-flash"},
		{"description", ""},
		{"telemetry", ""},
		{"image", ""},
	}

	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			f := initCmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("flag --%s not found on agent init", tt.flag)
			}
			if f.DefValue != tt.defValue {
				t.Errorf("flag --%s default = %q, want %q", tt.flag, f.DefValue, tt.defValue)
			}
		})
	}
}

// TestSkillPublishFlags verifies flag registration on skill publish.
func TestSkillPublishFlags(t *testing.T) {
	root := Root()
	skillCmd := findSubcommand(root, "skill")
	if skillCmd == nil {
		t.Fatal("skill command not found")
	}
	publishCmd := findSubcommand(skillCmd, "publish")
	if publishCmd == nil {
		t.Fatal("skill publish command not found")
	}

	tests := []struct {
		flag     string
		defValue string
		required bool
	}{
		{"docker-url", "", true},
		{"push", "false", false},
		{"dry-run", "false", false},
		{"tag", "latest", false},
		{"platform", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.flag, func(t *testing.T) {
			f := publishCmd.Flags().Lookup(tt.flag)
			if f == nil {
				t.Fatalf("flag --%s not found on skill publish", tt.flag)
			}
			if f.DefValue != tt.defValue {
				t.Errorf("flag --%s default = %q, want %q", tt.flag, f.DefValue, tt.defValue)
			}
			if tt.required {
				if _, ok := f.Annotations[cobra.BashCompOneRequiredFlag]; !ok {
					t.Errorf("flag --%s should be marked as required", tt.flag)
				}
			}
		})
	}
}

// TestRequiredFlags verifies that commands with required flags have them marked.
func TestRequiredFlags(t *testing.T) {
	root := Root()

	tests := []struct {
		parent   string
		command  string
		required []string
	}{
		{"skill", "publish", []string{"docker-url"}},
		{"skill", "delete", []string{"version"}},
		{"mcp", "publish", []string{"package-id", "type"}},
		{"agent", "delete", []string{"version"}},
	}

	for _, tt := range tests {
		t.Run(tt.parent+"/"+tt.command, func(t *testing.T) {
			parentCmd := findSubcommand(root, tt.parent)
			if parentCmd == nil {
				t.Fatalf("parent command %q not found", tt.parent)
			}
			cmd := findSubcommand(parentCmd, tt.command)
			if cmd == nil {
				t.Fatalf("command %q not found under %q", tt.command, tt.parent)
			}
			for _, flagName := range tt.required {
				f := cmd.Flags().Lookup(flagName)
				if f == nil {
					t.Errorf("flag --%s not found", flagName)
					continue
				}
				if _, ok := f.Annotations[cobra.BashCompOneRequiredFlag]; !ok {
					t.Errorf("flag --%s should be marked as required", flagName)
				}
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

// TestArgsValidators verifies that commands enforce correct argument counts.
func TestArgsValidators(t *testing.T) {
	root := Root()

	tests := []struct {
		parent  string
		command string
		args    int
		wantErr bool
	}{
		// Parent commands accept arbitrary args
		{"", "agent", 0, false},
		{"", "mcp", 0, false},
		{"", "skill", 0, false},
		// agent init requires exactly 3 args
		{"agent", "init", 3, false},
		{"agent", "init", 0, true},
		{"agent", "init", 2, true},
		// Commands requiring exactly 1 arg
		{"agent", "show", 1, false},
		{"agent", "show", 0, true},
		{"agent", "delete", 1, false},
		{"agent", "delete", 0, true},
		{"agent", "build", 1, false},
		{"agent", "build", 0, true},
		{"skill", "publish", 1, false},
		{"skill", "publish", 0, true},
		{"mcp", "show", 1, false},
		{"mcp", "show", 0, true},
	}

	for _, tt := range tests {
		name := tt.command
		if tt.parent != "" {
			name = tt.parent + "/" + tt.command
		}
		t.Run(name+"/"+argsDesc(tt.args, tt.wantErr), func(t *testing.T) {
			var cmd *cobra.Command
			if tt.parent == "" {
				cmd = findSubcommand(root, tt.command)
			} else {
				parentCmd := findSubcommand(root, tt.parent)
				if parentCmd == nil {
					t.Fatalf("parent command %q not found", tt.parent)
				}
				cmd = findSubcommand(parentCmd, tt.command)
			}
			if cmd == nil {
				t.Fatalf("command %q not found", tt.command)
			}
			if cmd.Args == nil {
				if tt.wantErr {
					t.Errorf("command %q has no Args validator but expected error with %d args", name, tt.args)
				}
				return
			}
			args := make([]string, tt.args)
			for i := range args {
				args[i] = "test"
			}
			err := cmd.Args(cmd, args)
			if (err != nil) != tt.wantErr {
				t.Errorf("command %q Args(%d args) error = %v, wantErr %v", name, tt.args, err, tt.wantErr)
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

// argsDesc returns a short description for test naming.
func argsDesc(n int, wantErr bool) string {
	if wantErr {
		return fmt.Sprintf("rejects_%d_args", n)
	}
	return fmt.Sprintf("accepts_%d_args", n)
}
