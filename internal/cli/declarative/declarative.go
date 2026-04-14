package declarative

import (
	"github.com/agentregistry-dev/agentregistry/internal/client"

	// Trigger init() registrations for all resource handlers.
	_ "github.com/agentregistry-dev/agentregistry/internal/cli/resource"
)

var apiClient *client.Client

// SetAPIClient sets the API client used by all declarative commands.
// Called by pkg/cli/root.go's PersistentPreRunE.
func SetAPIClient(c *client.Client) {
	apiClient = c
}
