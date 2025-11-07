package cli

import (
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent"
)

func init() {
	rootCmd.AddCommand(agent.NewAgentCmd(APIClient))
}
