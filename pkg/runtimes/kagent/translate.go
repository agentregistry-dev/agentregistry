package kagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

var (
	errUnsupported = errors.New("unsupported on kagent runtime")
	// ErrDependencyNotReady marks a retryable apply: a referenced MCP Deployment is not ready yet.
	ErrDependencyNotReady = errors.New("dependency not ready")
	errInvalidDependency  = errors.New("invalid dependency")
)

const (
	mcpServersConfigEnv   = "MCP_SERVERS_CONFIG"
	mcpServerURLCondition = "MCPServerURL"
)

type mcpRuntimeConfig struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

var invalidNameChars = regexp.MustCompile(`[^a-z0-9]+`)

// WorkloadName returns the Kubernetes resource name used by the Kagent
// adapter for an AgentRegistry target.
func WorkloadName(s string) string {
	name := strings.ToLower(strings.TrimSpace(s))
	name = invalidNameChars.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		return "resource"
	}
	return name
}

func targetNamespace(rcfg runtimeConfig) string {
	if rcfg.Namespace != "" {
		return rcfg.Namespace
	}
	return defaultRuntimeNamespace
}

// RuntimeNamespace returns the namespace Kagent workloads for this runtime
// config land in. It reads only the namespace key; Apply validates the rest.
func RuntimeNamespace(config map[string]any) string {
	var rcfg runtimeConfig
	if err := decodeJSONMap(config, &rcfg); err != nil {
		return defaultRuntimeNamespace
	}
	return targetNamespace(rcfg)
}

func defaultNamespace(namespace, fallback string) string {
	if strings.TrimSpace(namespace) == "" {
		return fallback
	}
	return namespace
}

func workloadEnv(in types.ApplyInput) []corev1.EnvVar {
	keys := make([]string, 0, len(in.Deployment.Spec.Env))
	for k := range in.Deployment.Spec.Env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	env := make([]corev1.EnvVar, 0, len(keys)+1)
	for _, k := range keys {
		env = append(env, corev1.EnvVar{Name: k, Value: in.Deployment.Spec.Env[k]})
	}
	return env
}

func buildBYOAgent(
	ctx context.Context,
	in types.ApplyInput,
	rcfg runtimeConfig,
	findDeployment DeploymentFinderFunc,
) (*agentPayload, error) {
	if in.Deployment.Spec.Harness != nil {
		return nil, fmt.Errorf("%w: harness agents are not yet supported", errUnsupported)
	}
	agent, ok := in.Target.(*v1alpha1.Agent)
	if !ok {
		return nil, fmt.Errorf("build kagent agent: target is %T, want *v1alpha1.Agent", in.Target)
	}
	if agent.Spec.Source == nil || agent.Spec.Source.Image == "" {
		return nil, fmt.Errorf("%w: kagent agent requires spec.source.image", errUnsupported)
	}
	if agent.Spec.Source.Protocol != nil && *agent.Spec.Source.Protocol != v1alpha1.AgentProtocolA2A {
		return nil, fmt.Errorf("%w: kagent BYO agents require protocol %q", errUnsupported, v1alpha1.AgentProtocolA2A)
	}
	workloadName := WorkloadName(agent.Metadata.Name)
	workloadNamespace := targetNamespace(rcfg)
	env, err := agentWorkloadEnv(
		ctx,
		in,
		agent,
		rcfg,
		workloadNamespace,
		findDeployment,
	)
	if err != nil {
		return nil, err
	}
	out := &agentPayload{}
	out.Name = workloadName
	out.Namespace = workloadNamespace
	out.Spec = agentPayloadSpec{
		Type:        agentTypeBYO,
		Description: workloadName,
		BYO: &byoAgentPayloadSpec{
			Deployment: &byoDeploymentPayload{
				Image:            agent.Spec.Source.Image,
				ImagePullSecrets: localObjectRefs(rcfg.ImagePullSecrets),
				NodeSelector:     rcfg.Deployment.NodeSelector,
				Tolerations:      rcfg.Deployment.Tolerations,
				Affinity:         rcfg.Deployment.Affinity,
				Env:              env,
			},
		},
	}
	return out, nil
}

func agentWorkloadEnv(
	ctx context.Context,
	in types.ApplyInput,
	agent *v1alpha1.Agent,
	rcfg runtimeConfig,
	workloadNamespace string,
	findDeployment DeploymentFinderFunc,
) ([]corev1.EnvVar, error) {
	env := workloadEnv(in)
	if !hasEnvVar(env, "HOST") {
		env = append(env, corev1.EnvVar{Name: "HOST", Value: "0.0.0.0"})
	}
	env = setEnvVar(env, "KAGENT_NAMESPACE", workloadNamespace)
	env = setEnvVar(env, "KAGENT_NAME", agent.Metadata.Name)
	env = setEnvVar(env, "KAGENT_URL", rcfg.URL)
	provider, model, err := resolveAgentModel(ctx, in)
	if err != nil {
		return nil, err
	}
	if provider != "" && model != "" {
		env = setEnvVar(env, "MODEL_PROVIDER", provider)
		env = setEnvVar(env, "MODEL_NAME", model)
	}
	return addAgentMCPEnv(ctx, in, agent, env, findDeployment)
}

func addAgentMCPEnv(
	ctx context.Context,
	in types.ApplyInput,
	agent *v1alpha1.Agent,
	env []corev1.EnvVar,
	findDeployment DeploymentFinderFunc,
) ([]corev1.EnvVar, error) {
	configs, err := resolveAgentMCPConfig(ctx, in, agent, findDeployment)
	if err != nil {
		return nil, err
	}
	if len(configs) == 0 {
		return env, nil
	}
	encoded, err := json.Marshal(configs)
	if err != nil {
		return nil, fmt.Errorf("marshal kagent agent MCP config: %w", err)
	}
	return setEnvVar(env, mcpServersConfigEnv, string(encoded)), nil
}

func resolveAgentMCPConfig(
	ctx context.Context,
	in types.ApplyInput,
	agent *v1alpha1.Agent,
	findDeployment DeploymentFinderFunc,
) ([]mcpRuntimeConfig, error) {
	if len(agent.Spec.MCPServers) == 0 && len(in.Deployment.Spec.DeploymentRefs) == 0 {
		return nil, nil
	}
	if in.Getter == nil {
		return nil, fmt.Errorf("resolve kagent agent MCP configuration: object getter is required")
	}
	configs, localServers, err := resolveDeclaredAgentMCPServers(ctx, in, agent)
	if err != nil {
		return nil, err
	}
	explicitConfigs, err := resolveExplicitMCPDeploymentRefs(ctx, in, agent, localServers)
	if err != nil {
		return nil, err
	}
	configs = append(configs, explicitConfigs...)
	implicitConfigs, err := resolveImplicitKagentMCPDeployments(
		ctx,
		in,
		localServers,
		findDeployment,
	)
	if err != nil {
		return nil, err
	}
	return append(configs, implicitConfigs...), nil
}

func resolveDeclaredAgentMCPServers(
	ctx context.Context,
	in types.ApplyInput,
	agent *v1alpha1.Agent,
) ([]mcpRuntimeConfig, map[string]v1alpha1.ResourceRef, error) {
	configs := make([]mcpRuntimeConfig, 0, len(agent.Spec.MCPServers))
	localServers := make(map[string]v1alpha1.ResourceRef)
	for i, ref := range agent.Spec.MCPServers {
		normalized := ref
		if normalized.Kind == "" {
			normalized.Kind = v1alpha1.KindMCPServer
		}
		if normalized.Kind != v1alpha1.KindMCPServer {
			return nil, nil, fmt.Errorf("resolve kagent agent MCP configuration: spec.mcpServers[%d] has unsupported kind %q", i, normalized.Kind)
		}
		if normalized.Namespace == "" {
			normalized.Namespace = agent.Metadata.NamespaceOrDefault()
		}
		obj, err := in.Getter(ctx, normalized)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve kagent agent MCP server %s/%s: %w", normalized.Namespace, normalized.Name, err)
		}
		server, ok := obj.(*v1alpha1.MCPServer)
		if !ok {
			return nil, nil, fmt.Errorf("resolve kagent agent MCP server %s/%s: got %T, want *v1alpha1.MCPServer", normalized.Namespace, normalized.Name, obj)
		}
		if server.Spec.Remote == nil || strings.TrimSpace(server.Spec.Remote.URL) == "" {
			localServers[resourceIdentityKey(normalized.Namespace, normalized.Name)] = normalized
			continue
		}
		configs = append(configs, mcpRuntimeConfig{
			Name:    normalized.Name,
			Type:    "remote",
			URL:     server.Spec.Remote.URL,
			Headers: headerMap(server.Spec.Remote.Headers),
		})
	}
	return configs, localServers, nil
}

func resolveExplicitMCPDeploymentRefs(
	ctx context.Context,
	in types.ApplyInput,
	agent *v1alpha1.Agent,
	localServers map[string]v1alpha1.ResourceRef,
) ([]mcpRuntimeConfig, error) {
	configs := make([]mcpRuntimeConfig, 0, len(in.Deployment.Spec.DeploymentRefs))
	for _, ref := range in.Deployment.Spec.DeploymentRefs {
		namespace := ref.Namespace
		if namespace == "" {
			namespace = agent.Metadata.NamespaceOrDefault()
		}
		obj, err := in.Getter(ctx, v1alpha1.ResourceRef{
			Kind:      v1alpha1.KindDeployment,
			Namespace: namespace,
			Name:      ref.Name,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve kagent agent deploymentRef %s/%s: %w", namespace, ref.Name, err)
		}
		deployment, ok := obj.(*v1alpha1.Deployment)
		if !ok {
			return nil, fmt.Errorf("resolve kagent agent deploymentRef %s/%s: got %T, want *v1alpha1.Deployment", namespace, ref.Name, obj)
		}
		if deployment.Spec.TargetRef.Kind != v1alpha1.KindMCPServer {
			return nil, fmt.Errorf("resolve kagent agent deploymentRef %s/%s: target kind %q is not %q", namespace, ref.Name, deployment.Spec.TargetRef.Kind, v1alpha1.KindMCPServer)
		}
		targetNamespace := defaultNamespace(
			deployment.Spec.TargetRef.Namespace,
			deployment.Metadata.NamespaceOrDefault(),
		)
		delete(localServers, resourceIdentityKey(
			targetNamespace,
			deployment.Spec.TargetRef.Name,
		))
		currentRuntimeName := strings.TrimSpace(in.Deployment.Spec.RuntimeRef.Name)
		refRuntimeName := strings.TrimSpace(deployment.Spec.RuntimeRef.Name)
		if currentRuntimeName != "" && refRuntimeName != "" && currentRuntimeName != refRuntimeName {
			return nil, fmt.Errorf(
				"resolve kagent agent deploymentRef %s/%s: runtime %q does not match agent runtime %q",
				namespace, ref.Name, refRuntimeName, currentRuntimeName,
			)
		}
		condition := deployment.Status.GetCondition(mcpServerURLCondition)
		if condition == nil || condition.Status != v1alpha1.ConditionTrue || strings.TrimSpace(condition.Message) == "" {
			return nil, fmt.Errorf(
				"%w: kagent agent deploymentRef %s/%s MCP server endpoint is not ready",
				ErrDependencyNotReady, namespace, ref.Name,
			)
		}
		configs = append(configs, mcpRuntimeConfig{
			Name: deployment.Metadata.Name,
			Type: "remote",
			URL:  condition.Message,
		})
	}
	return configs, nil
}

func resolveImplicitKagentMCPDeployments(
	ctx context.Context,
	in types.ApplyInput,
	localServers map[string]v1alpha1.ResourceRef,
	findDeployment DeploymentFinderFunc,
) ([]mcpRuntimeConfig, error) {
	if len(localServers) == 0 {
		return nil, nil
	}
	refs := make([]string, 0, len(localServers))
	for key := range localServers {
		refs = append(refs, key)
	}
	slices.Sort(refs)
	if findDeployment == nil {
		return nil, fmt.Errorf(
			"%w: deployment finder is required to resolve source-backed MCP servers %s",
			errInvalidDependency,
			strings.Join(refs, ", "),
		)
	}
	configs := make([]mcpRuntimeConfig, 0, len(localServers))
	runtimeRef := in.Deployment.Spec.RuntimeRef
	if runtimeRef.Kind == "" {
		runtimeRef.Kind = v1alpha1.KindRuntime
	}
	if runtimeRef.Namespace == "" {
		runtimeRef.Namespace = in.Deployment.Metadata.NamespaceOrDefault()
	}
	for _, key := range refs {
		ref := localServers[key]
		deployment, found, err := findDeployment(
			ctx,
			ref,
			runtimeRef,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"resolve kagent MCP deployment %s/%s: %w",
				ref.Namespace,
				ref.Name,
				err,
			)
		}
		if !found {
			return nil, fmt.Errorf(
				"%w: no Kagent Deployment found for source-backed MCP server %s/%s",
				ErrDependencyNotReady,
				ref.Namespace,
				ref.Name,
			)
		}
		condition := deployment.Status.GetCondition(mcpServerURLCondition)
		if condition == nil || condition.Status != v1alpha1.ConditionTrue || strings.TrimSpace(condition.Message) == "" {
			return nil, fmt.Errorf(
				"%w: Kagent MCP Deployment %s/%s endpoint is not ready",
				ErrDependencyNotReady,
				deployment.Metadata.NamespaceOrDefault(),
				deployment.Metadata.Name,
			)
		}
		configs = append(configs, mcpRuntimeConfig{
			Name: deployment.Metadata.Name,
			Type: "remote",
			URL:  condition.Message,
		})
	}
	return configs, nil
}

func resourceIdentityKey(namespace, name string) string {
	return namespace + "/" + name
}

func headerMap(headers []v1alpha1.HTTPHeader) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	result := make(map[string]string, len(headers))
	for _, header := range headers {
		result[header.Name] = header.Value
	}
	return result
}

func hasEnvVar(env []corev1.EnvVar, name string) bool {
	for _, item := range env {
		if item.Name == name {
			return true
		}
	}
	return false
}

func setEnvVar(env []corev1.EnvVar, name, value string) []corev1.EnvVar {
	for i := range env {
		if env[i].Name == name {
			env[i].Value = value
			return env
		}
	}
	return append(env, corev1.EnvVar{Name: name, Value: value})
}

func resolveAgentModel(
	ctx context.Context,
	in types.ApplyInput,
) (string, string, error) {
	modelRef := in.Deployment.Spec.ModelRef
	if modelRef == nil {
		provider, model := legacyAgentModel(in.Target)
		return provider, model, nil
	}
	if in.Getter == nil {
		return "", "", fmt.Errorf("resolve kagent agent model: object getter is required")
	}
	namespace := modelRef.Namespace
	if namespace == "" {
		namespace = in.Deployment.Metadata.NamespaceOrDefault()
	}
	obj, err := in.Getter(ctx, v1alpha1.ResourceRef{
		Kind:      v1alpha1.KindModel,
		Namespace: namespace,
		Name:      modelRef.Name,
		Tag:       modelRef.Tag,
	})
	if err != nil {
		return "", "", fmt.Errorf("resolve kagent agent model: %w", err)
	}
	model, ok := obj.(*v1alpha1.Model)
	if !ok {
		return "", "", fmt.Errorf("resolve kagent agent model: got %T, want *v1alpha1.Model", obj)
	}
	return model.Spec.Provider, model.Spec.Model, nil
}

// legacyAgentModel honors the deprecated Agent model fields while they exist.
// Both must be set; a half-set pair would clobber MODEL_NAME with an empty value.
func legacyAgentModel(target v1alpha1.Object) (string, string) {
	agent, ok := target.(*v1alpha1.Agent)
	if !ok || agent.Spec.ModelProvider == "" || agent.Spec.ModelName == "" { //nolint:staticcheck
		return "", ""
	}
	return agent.Spec.ModelProvider, agent.Spec.ModelName //nolint:staticcheck
}

// toolServerSpec holds either a remote or source-backed Kagent tool server.
// The Kagent create endpoint requires a type and its corresponding resource.
type toolServerSpec struct {
	Kind   string // "RemoteMCPServer" | "MCPServer"
	Remote *remoteMCPServerPayload
	MCP    *mcpServerPayload
}

func (s *toolServerSpec) Namespace() string {
	if s.Remote != nil {
		return s.Remote.Namespace
	}
	return s.MCP.Namespace
}

func (s *toolServerSpec) Name() string {
	if s.Remote != nil {
		return s.Remote.Name
	}
	return s.MCP.Name
}

func buildToolServer(in types.ApplyInput, rcfg runtimeConfig, dcfg deployConfig) (*toolServerSpec, error) {
	server, ok := in.Target.(*v1alpha1.MCPServer)
	if !ok {
		return nil, fmt.Errorf("build kagent tool server: target is %T, want *v1alpha1.MCPServer", in.Target)
	}
	meta := metav1.ObjectMeta{
		Name:      WorkloadName(server.Metadata.Name),
		Namespace: targetNamespace(rcfg),
	}

	switch {
	case server.Spec.Remote != nil:
		if len(dcfg.SecretRefs) > 0 {
			return nil, fmt.Errorf(
				"%w: runtimeConfig.secretRefs is not supported for remote kagent MCP server %q",
				errUnsupported,
				server.Metadata.Name,
			)
		}
		remote := &remoteMCPServerPayload{
			TypeMeta:   metav1.TypeMeta{APIVersion: kagentV1Alpha2APIVersion, Kind: "RemoteMCPServer"},
			ObjectMeta: meta,
			Spec: remoteMCPServerPayloadSpec{
				Description: meta.Name,
				Protocol:    remoteMCPProtocolStreamableHTTP,
				URL:         server.Spec.Remote.URL,
			},
		}
		return &toolServerSpec{Kind: "RemoteMCPServer", Remote: remote}, nil

	case server.Spec.Source != nil && server.Spec.Source.Package != nil:
		pkg := server.Spec.Source.Package
		image, cmd, args, err := packageRunner(pkg)
		if err != nil {
			return nil, err
		}
		env, err := packageEnv(pkg, in)
		if err != nil {
			return nil, err
		}
		deployment := mcpServerDeploymentPayload{
			Image:            image,
			Cmd:              cmd,
			Args:             args,
			Env:              env,
			SecretRefs:       localObjectRefs(dcfg.SecretRefs),
			ImagePullSecrets: localObjectRefs(rcfg.ImagePullSecrets),
			NodeSelector:     rcfg.Deployment.NodeSelector,
			Tolerations:      rcfg.Deployment.Tolerations,
			Affinity:         rcfg.Deployment.Affinity,
		}
		transportType := transportTypeStdio
		stdioTransport := &stdioTransportPayload{}
		var httpTransport *httpTransportPayload
		switch strings.ToLower(strings.TrimSpace(pkg.Transport.Type)) {
		case "stdio":
			if cmd == "" {
				return nil, fmt.Errorf("%w: kagent stdio tool servers require spec.source.package.launch.command", errUnsupported)
			}
		case "http":
			if pkg.Transport.Port == 0 {
				return nil, fmt.Errorf("%w: kagent http tool server requires a non-zero transport port", errUnsupported)
			}
			transportType = transportTypeHTTP
			stdioTransport = nil
			path := strings.TrimSpace(pkg.Transport.Path)
			if path != "" && !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			httpTransport = &httpTransportPayload{
				TargetPort: uint32(pkg.Transport.Port),
				TargetPath: path,
			}
			deployment.Port = pkg.Transport.Port
			if deployment.Env == nil {
				deployment.Env = map[string]string{}
			}
			deployment.Env["PORT"] = fmt.Sprintf("%d", pkg.Transport.Port)
		default:
			return nil, fmt.Errorf("%w: unsupported kagent MCP transport %q", errUnsupported, pkg.Transport.Type)
		}
		mcp := &mcpServerPayload{
			TypeMeta:   metav1.TypeMeta{APIVersion: kagentV1Alpha1APIVersion, Kind: "MCPServer"},
			ObjectMeta: meta,
			Spec: mcpServerPayloadSpec{
				Deployment:     deployment,
				TransportType:  transportType,
				StdioTransport: stdioTransport,
				HTTPTransport:  httpTransport,
			},
		}
		return &toolServerSpec{Kind: "MCPServer", MCP: mcp}, nil
	}
	return nil, fmt.Errorf("%w: mcp server %q has neither remote nor package source", errUnsupported, server.Metadata.Name)
}

func localObjectRefs(names []string) []corev1.LocalObjectReference {
	if len(names) == 0 {
		return nil
	}
	refs := make([]corev1.LocalObjectReference, 0, len(names))
	for _, name := range names {
		refs = append(refs, corev1.LocalObjectReference{Name: name})
	}
	return refs
}

// packageRunner resolves the image and command used to launch an MCP package.
// An explicit launch configuration owns the command and arguments verbatim.
func packageRunner(pkg *v1alpha1.MCPPackage) (image, cmd string, args []string, err error) {
	configuredOrigins := 0
	if pkg.Origin.NPM != nil {
		configuredOrigins++
	}
	if pkg.Origin.PyPI != nil {
		configuredOrigins++
	}
	if pkg.Origin.OCI != nil {
		configuredOrigins++
	}
	if configuredOrigins != 1 {
		return "", "", nil, fmt.Errorf(
			"%w: exactly one package origin must be configured", errUnsupported,
		)
	}

	switch {
	case pkg.Origin.NPM != nil:
		image = types.DefaultNPMRunnerImage
		cmd = "npx"
		ref := pkg.Origin.Identifier
		if pkg.Origin.NPM.Version != "" {
			ref += "@" + pkg.Origin.NPM.Version
		}
		args = []string{"-y", ref}
	case pkg.Origin.PyPI != nil:
		image = types.DefaultPyPIRunnerImage
		cmd = "uvx"
		ref := pkg.Origin.Identifier
		if pkg.Origin.PyPI.Version != "" {
			ref += "==" + pkg.Origin.PyPI.Version
		}
		args = []string{ref}
	case pkg.Origin.OCI != nil:
		image = pkg.Origin.Identifier
	}
	if pkg.Launch == nil {
		return image, cmd, args, nil
	}
	cmd = pkg.Launch.Command
	args = packageArguments(pkg.Launch.Args)
	return image, cmd, args, nil
}

func packageArguments(arguments []v1alpha1.MCPArgument) []string {
	args := make([]string, 0, len(arguments)*2)
	for _, arg := range arguments {
		if arg.Type == v1alpha1.MCPArgumentTypePositional && arg.Value != "" {
			args = append(args, arg.Value)
		}
	}
	for _, arg := range arguments {
		if arg.Type == v1alpha1.MCPArgumentTypeNamed {
			args = append(args, arg.Name)
			if arg.Value != "" {
				args = append(args, arg.Value)
			}
		}
	}
	return args
}

func packageEnv(pkg *v1alpha1.MCPPackage, in types.ApplyInput) (map[string]string, error) {
	env := map[string]string{}
	declared := map[string]struct{}{}
	missingRequired := []string{}
	if pkg.Launch != nil {
		for _, kv := range pkg.Launch.Env {
			declared[kv.Name] = struct{}{}
			value, overridden := in.Deployment.Spec.Env[kv.Name]
			if !overridden {
				value = kv.Value
			}
			if kv.IsRequired && value == "" {
				missingRequired = append(missingRequired, kv.Name)
			}
			if value != "" {
				env[kv.Name] = value
			}
		}
	}
	if len(missingRequired) > 0 {
		slices.Sort(missingRequired)
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missingRequired, ", "))
	}
	for name, value := range in.Deployment.Spec.Env {
		if _, found := declared[name]; !found {
			env[name] = value
		}
	}
	if len(env) == 0 {
		return nil, nil
	}
	return env, nil
}
