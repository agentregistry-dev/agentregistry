package kagent

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/secret"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

type adapter struct {
	newClient          clientFactory
	tokenSourceFactory TokenSourceFactory
	secretResolver     secret.Resolver
	findDeployment     DeploymentFinderFunc
}

type Option func(*adapter)

// TokenSourceFactory builds a token source for one runtime operation.
type TokenSourceFactory func(context.Context, *v1alpha1.Runtime) (TokenSource, error)

// DeploymentFinderFunc locates a Deployment for a Kagent workload dependency.
type DeploymentFinderFunc func(
	context.Context,
	v1alpha1.ResourceRef,
	v1alpha1.ResourceRef,
) (*v1alpha1.Deployment, bool, error)

// WithTokenSourceFactory resolves authentication separately for each Runtime.
func WithTokenSourceFactory(factory TokenSourceFactory) Option {
	return func(a *adapter) { a.tokenSourceFactory = factory }
}

// WithSecretResolver resolves auth.secretRef independently for each Runtime.
func WithSecretResolver(resolver secret.Resolver) Option {
	return func(a *adapter) { a.secretResolver = resolver }
}

// WithDeploymentFinder configures lookup of source-backed MCPServer Deployments.
func WithDeploymentFinder(finder DeploymentFinderFunc) Option {
	return func(a *adapter) { a.findDeployment = finder }
}

func New(options ...Option) types.DeploymentAdapter {
	return newAdapter(nil, options...)
}

func newAdapter(factory clientFactory, options ...Option) *adapter {
	a := &adapter{newClient: factory}
	for _, option := range options {
		option(a)
	}
	return a
}

var _ types.DeploymentAdapter = (*adapter)(nil)
var _ types.DeploymentDiscoverySource = (*adapter)(nil)
var _ types.DeploymentDesiredFingerprinter = (*adapter)(nil)

func (a *adapter) Type() string { return RuntimeType }

func (a *adapter) SupportedTargetKinds() []string {
	return []string{v1alpha1.KindAgent, v1alpha1.KindMCPServer}
}

type agentMCPFingerprint struct {
	Config []mcpRuntimeConfig `json:"config,omitempty"`
	Error  string             `json:"error,omitempty"`
}

// DesiredFingerprint includes MCP endpoint wiring derived from Deployment status.
func (a *adapter) DesiredFingerprint(
	ctx context.Context,
	input types.ApplyInput,
) (string, error) {
	options := types.ApplyFingerprintOptions{AdapterType: RuntimeType}
	agent, ok := input.Target.(*v1alpha1.Agent)
	if !ok || input.Deployment == nil {
		return types.DefaultApplyFingerprint(ctx, input, options)
	}
	hasMCPDependencies := len(agent.Spec.MCPServers) > 0 ||
		len(input.Deployment.Spec.DeploymentRefs) > 0
	if !hasMCPDependencies {
		return types.DefaultApplyFingerprint(ctx, input, options)
	}

	config, err := resolveAgentMCPConfig(ctx, input, agent, a.findDeployment)
	fingerprint := agentMCPFingerprint{Config: config}
	if err != nil {
		if errors.Is(err, ErrDependencyNotReady) {
			return "", err
		}
		if !errors.Is(err, errInvalidDependency) &&
			!errors.Is(err, errUnsupported) {
			return "", err
		}
		fingerprint.Error = err.Error()
	}
	options.Extra = fingerprint
	return types.DefaultApplyFingerprint(ctx, input, options)
}

func (a *adapter) Apply(ctx context.Context, input types.ApplyInput) (*types.ApplyResult, error) {
	now := time.Now().UTC()
	runtimeConfig, err := decodeRuntimeConfig(input.Runtime.Spec.Config)
	if err != nil {
		return FailedApplyResult(input.Target, "InvalidRuntimeConfig", err.Error(), now), nil
	}
	deploymentConfig, err := decodeDeployConfig(
		input.Deployment.Spec.RuntimeConfig,
		input.Deployment.Spec.TargetRef.Kind,
	)
	if err != nil {
		return FailedApplyResult(input.Target, "InvalidDeployConfig", err.Error(), now), nil
	}
	switch input.Target.(type) {
	case *v1alpha1.Agent:
		return a.applyAgent(ctx, input, runtimeConfig, now)
	case *v1alpha1.MCPServer:
		return a.applyMCPServer(ctx, input, runtimeConfig, deploymentConfig, now)
	default:
		return FailedApplyResult(
			input.Target,
			"UnsupportedTarget",
			fmt.Sprintf("target type %T is not supported", input.Target),
			now,
		), nil
	}
}

func (a *adapter) applyAgent(
	ctx context.Context,
	input types.ApplyInput,
	runtimeConfig runtimeConfig,
	now time.Time,
) (*types.ApplyResult, error) {
	agent, err := buildBYOAgent(
		ctx,
		input,
		runtimeConfig,
		a.findDeployment,
	)
	if err != nil {
		return translationFailure(input.Target, err, now)
	}
	agent.Labels = mergeLabels(agent.Labels, runtimeConfig.Deployment.Labels)
	agent.Spec.BYO.Deployment.Labels = mergeLabels(
		agent.Spec.BYO.Deployment.Labels,
		runtimeConfig.Deployment.Labels,
	)
	client, err := a.clientFor(ctx, input.Runtime, runtimeConfig)
	if err != nil {
		return nil, fmt.Errorf("build kagent client: %w", err)
	}
	if err := client.ensureAgent(ctx, agent); err != nil {
		return nil, err
	}
	return successfulApplyResult(agent.Name, agent.Namespace, nil, now)
}

func (a *adapter) applyMCPServer(
	ctx context.Context,
	input types.ApplyInput,
	runtimeConfig runtimeConfig,
	deploymentConfig deployConfig,
	now time.Time,
) (*types.ApplyResult, error) {
	server, err := buildToolServer(input, runtimeConfig, deploymentConfig)
	if err != nil {
		return translationFailure(input.Target, err, now)
	}
	if server.MCP != nil {
		server.MCP.Labels = mergeLabels(server.MCP.Labels, runtimeConfig.Deployment.Labels)
		server.MCP.Spec.Deployment.Labels = mergeLabels(
			server.MCP.Spec.Deployment.Labels,
			runtimeConfig.Deployment.Labels,
		)
	}
	client, err := a.clientFor(ctx, input.Runtime, runtimeConfig)
	if err != nil {
		return nil, fmt.Errorf("build kagent client: %w", err)
	}
	if err := client.ensureToolServer(ctx, server); err != nil {
		return nil, err
	}
	endpoint := &v1alpha1.Condition{
		Type:               mcpServerURLCondition,
		Status:             v1alpha1.ConditionTrue,
		Reason:             "RuntimeReady",
		Message:            toolServerEndpoint(server),
		LastTransitionTime: now,
	}
	return successfulApplyResult(server.Name(), server.Namespace(), endpoint, now)
}

func mergeLabels(existing, additional map[string]string) map[string]string {
	if len(additional) == 0 {
		return existing
	}
	merged := maps.Clone(existing)
	if merged == nil {
		merged = make(map[string]string, len(additional))
	}
	maps.Copy(merged, additional)
	return merged
}

func toolServerEndpoint(server *toolServerSpec) string {
	if server.Remote != nil {
		return server.Remote.Spec.URL
	}
	port := uint16(3000)
	if server.MCP.Spec.Deployment.Port > 0 {
		port = server.MCP.Spec.Deployment.Port
	}
	path := "/mcp"
	if server.MCP.Spec.HTTPTransport != nil && server.MCP.Spec.HTTPTransport.TargetPath != "" {
		path = server.MCP.Spec.HTTPTransport.TargetPath
	}
	return fmt.Sprintf(
		"http://%s.%s.svc.cluster.local:%d%s",
		server.Name(),
		server.Namespace(),
		port,
		path,
	)
}

func (a *adapter) Remove(
	ctx context.Context,
	input types.RemoveInput,
) (*types.RemoveResult, error) {
	runtimeConfig, err := decodeRuntimeConnectionConfig(input.Runtime.Spec.Config)
	if err != nil {
		return nil, fmt.Errorf("kagent remove: %w", err)
	}
	client, err := a.clientFor(ctx, input.Runtime, runtimeConfig)
	if err != nil {
		return nil, fmt.Errorf("build kagent client: %w", err)
	}

	name := deploymentRuntimeID(input.Deployment)
	if name == "" {
		name = WorkloadName(input.Deployment.Spec.TargetRef.Name)
	}
	namespace := deploymentRuntimeNamespace(input.Deployment)
	if namespace == "" {
		namespace = targetNamespace(runtimeConfig)
	}

	switch input.Deployment.Spec.TargetRef.Kind {
	case v1alpha1.KindAgent:
		err = client.deleteAgent(ctx, namespace, name)
	case v1alpha1.KindMCPServer:
		err = client.deleteToolServer(ctx, namespace, name)
	default:
		return nil, fmt.Errorf(
			"kagent remove: unsupported target kind %q",
			input.Deployment.Spec.TargetRef.Kind,
		)
	}
	if err != nil && !errors.Is(err, errNotFound) {
		return nil, fmt.Errorf("kagent remove %s/%s: %w", namespace, name, err)
	}

	now := time.Now().UTC()
	conditions := removedConditions(now)
	if input.Deployment.Spec.TargetRef.Kind == v1alpha1.KindMCPServer {
		conditions = append(conditions, v1alpha1.Condition{
			Type:               mcpServerURLCondition,
			Status:             v1alpha1.ConditionFalse,
			Reason:             "Removed",
			Message:            "Kagent MCP server removed",
			LastTransitionTime: now,
		})
	}
	return &types.RemoveResult{Conditions: conditions}, nil
}

func (a *adapter) Logs(_ context.Context, in types.LogsInput) (<-chan types.LogLine, error) {
	if in.Deployment == nil {
		return nil, fmt.Errorf("kagent logs: deployment is required")
	}
	ch := make(chan types.LogLine, 1)
	if ready := in.Deployment.Status.GetCondition("Ready"); ready != nil &&
		ready.Status == v1alpha1.ConditionFalse && ready.Reason == "Failed" && ready.Message != "" {
		ch <- types.LogLine{Timestamp: ready.LastTransitionTime, Stream: "stdout", Line: ready.Message}
	}
	close(ch)
	return ch, nil
}

func (a *adapter) clientFor(
	ctx context.Context,
	runtime *v1alpha1.Runtime,
	config runtimeConfig,
) (kagentClient, error) {
	if a.newClient != nil {
		return a.newClient(config)
	}
	tokenSource, err := a.tokenSourceFor(ctx, runtime, config.Auth)
	if err != nil {
		return nil, err
	}
	return newRESTClient(config, tokenSource)
}

func (a *adapter) tokenSourceFor(
	ctx context.Context,
	runtime *v1alpha1.Runtime,
	auth authConfig,
) (TokenSource, error) {
	if a.tokenSourceFactory != nil {
		return a.tokenSourceFactory(ctx, runtime)
	}
	if auth.SecretRef == nil {
		return nil, nil
	}
	if a.secretResolver == nil {
		return nil, fmt.Errorf("kagent runtime auth.secretRef requires a secret resolver")
	}
	if runtime == nil {
		return nil, fmt.Errorf("kagent runtime is required to resolve auth.secretRef")
	}
	ref := *auth.SecretRef
	if ref.Namespace == "" {
		ref.Namespace = runtime.Metadata.NamespaceOrDefault()
	}
	value, err := a.secretResolver.Resolve(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve kagent runtime credential: %w", err)
	}
	return staticToken(string(value.Reveal())), nil
}
