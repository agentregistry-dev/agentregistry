package main

import (
	"os"

	"github.com/agentregistry-dev/agentregistry/pkg/cli"
	"github.com/agentregistry-dev/agentregistry/pkg/cli/config"
)

func main() {
	// We should auto-approve pushed/published resources by default
	config.SetAutoApprove(true)

	if err := cli.Root().Execute(); err != nil {
		os.Exit(1)
	}
}
