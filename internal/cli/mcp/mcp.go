package mcp

import (
	"github.com/spf13/cobra"
)

var verbose bool

var McpCmd = &cobra.Command{
	Use: "mcp",
}

func init() {
	McpCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
}
