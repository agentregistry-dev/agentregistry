package cli

import (
	"github.com/agentregistry-dev/agentregistry/internal/cli/configure"
	"github.com/agentregistry-dev/agentregistry/pkg/cli"
)

func init() {
	cli.Root().AddCommand(configure.NewConfigureCmd())
}
