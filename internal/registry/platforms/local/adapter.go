package local

// localDeploymentAdapter serves Deployments onto a local docker-compose
// runtime. The v1alpha1 surface (Apply / Remove / Logs / Discover /
// SupportedTargetKinds / Platform) lives in v1alpha1_adapter.go; this
// file holds only the struct + constructor shared across that surface
// and the supporting platform helpers in deployment_adapter_local_platform.go.
type localDeploymentAdapter struct {
	platformDir      string
	agentGatewayPort uint16
}

var (
	runLocalComposeUp   = ComposeUpLocalPlatform
	runLocalComposeDown = ComposeDownLocalPlatform
)

// NewLocalDeploymentAdapter constructs an adapter pinned to a runtime
// directory (docker-compose.yaml + agent-gateway.yaml live here) and the
// port the agentgateway service binds.
func NewLocalDeploymentAdapter(platformDir string, agentGatewayPort uint16) *localDeploymentAdapter {
	return &localDeploymentAdapter{
		platformDir:      platformDir,
		agentGatewayPort: agentGatewayPort,
	}
}

func (a *localDeploymentAdapter) Platform() string { return "local" }
