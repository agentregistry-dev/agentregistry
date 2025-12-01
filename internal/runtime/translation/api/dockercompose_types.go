package api

import (
	"github.com/compose-spec/compose-go/v2/types"
)

type DockerComposeConfig = types.Project

type LocalRuntimeConfig struct {
	DockerCompose *DockerComposeConfig
	AgentGateway  *AgentGatewayConfig
}
