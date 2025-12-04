package main

import (
	"os"

	"github.com/agentregistry-dev/agentregistry/pkg/cli"
)

func main() {
	if err := cli.Root().Execute(); err != nil {
		os.Exit(1)
	}
}
