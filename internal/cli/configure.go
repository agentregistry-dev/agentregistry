package cli

import (
	"github.com/agentregistry-dev/agentregistry/internal/cli/configure"
)

func init() {
	rootCmd.AddCommand(configure.NewConfigureCmd())
}
