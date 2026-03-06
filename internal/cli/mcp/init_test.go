package mcp

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestInitCmd_HidesRegistryFlags(t *testing.T) {
	// Simulate the root command hierarchy with persistent registry flags
	root := &cobra.Command{Use: "arctl"}
	root.PersistentFlags().String("registry-url", "", "Registry base URL")
	root.PersistentFlags().String("registry-token", "", "Registry bearer token")

	mcpCmd := &cobra.Command{Use: "mcp"}
	root.AddCommand(mcpCmd)
	mcpCmd.AddCommand(InitCmd)

	// Capture the help output
	buf := new(bytes.Buffer)
	InitCmd.SetOut(buf)
	InitCmd.SetErr(buf)
	root.SetArgs([]string{"mcp", "init", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "registry-url") {
		t.Error("--registry-url should be hidden from mcp init --help output")
	}
	if strings.Contains(output, "registry-token") {
		t.Error("--registry-token should be hidden from mcp init --help output")
	}
}
