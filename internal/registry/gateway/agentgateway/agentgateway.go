package agentgateway

import (
	"context"
	"fmt"
	"sort"

	"github.com/agentregistry-dev/agentregistry/internal/registry/gateway"
	types "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
)

// AgentGatewayEngine is the default Engine. It deterministically renders
// a desired gateway.Config into the repo's native *types.AgentGatewayConfig
// and delegates Apply/Remove to an injected Applier.
type AgentGatewayEngine struct {
	applier Applier
}

var _ Engine = (*AgentGatewayEngine)(nil)

// NewAgentGatewayEngine constructs an AgentGatewayEngine. The applier may be
// nil when only Render is needed; Apply and Remove then return an error.
func NewAgentGatewayEngine(applier Applier) *AgentGatewayEngine {
	return &AgentGatewayEngine{applier: applier}
}

// Render translates a desired gateway.Config into the native
// *types.AgentGatewayConfig. It performs no I/O and is deterministic: binds
// are sorted by port, listeners within a bind by name, and routes by name, so
// equal inputs always produce equal outputs.
func (e *AgentGatewayEngine) Render(_ context.Context, desired gateway.Config) (*types.AgentGatewayConfig, error) {
	backendURLs := make(map[string]string, len(desired.Backends))
	for _, b := range desired.Backends {
		backendURLs[b.Name] = b.URL
	}

	policies := make(map[string]gateway.Policy, len(desired.Policies))
	for _, p := range desired.Policies {
		policies[p.Name] = p
	}

	routes := renderRoutes(desired.Routes, backendURLs, policies)

	cfg := &types.AgentGatewayConfig{
		Config: struct{}{},
	}

	listenersByPort := make(map[int][]types.LocalListener)
	var ports []int
	for _, l := range desired.Listeners {
		if _, ok := listenersByPort[l.Port]; !ok {
			ports = append(ports, l.Port)
		}
		listenersByPort[l.Port] = append(listenersByPort[l.Port], types.LocalListener{
			Name:          l.Name,
			GatewayName:   desired.ClassName,
			Protocol:      types.LocalListenerProtocol(l.Protocol),
			TLS:           renderTLS(l.TLS),
			AllowedRoutes: renderAllowedRoutes(l.AllowedRoutes),
			Policies:      renderPolicyRefs(l.Policies, policies),
			Routes:        routes,
		})
	}

	sort.Ints(ports)
	for _, port := range ports {
		listeners := listenersByPort[port]
		sort.Slice(listeners, func(i, j int) bool {
			return listeners[i].Name < listeners[j].Name
		})
		cfg.Binds = append(cfg.Binds, types.LocalBind{
			Port:      uint16(port),
			Listeners: listeners,
		})
	}

	return cfg, nil
}

// Apply hands the rendered native config to the injected Applier. It returns
// an error when no applier is configured.
func (e *AgentGatewayEngine) Apply(ctx context.Context, target gateway.Target, rendered *types.AgentGatewayConfig) error {
	if e.applier == nil {
		return fmt.Errorf("agentgateway engine: no applier configured")
	}
	return e.applier.Apply(ctx, target, rendered)
}

// Remove delegates removal of previously-applied config to the injected
// Applier. It returns an error when no applier is configured.
func (e *AgentGatewayEngine) Remove(ctx context.Context, target gateway.Target) error {
	if e.applier == nil {
		return fmt.Errorf("agentgateway engine: no applier configured")
	}
	return e.applier.Remove(ctx, target)
}

// renderRoutes translates desired routes into deterministically sorted native
// routes. It returns nil when there are no routes so empty configs compare
// cleanly as whole objects.
func renderRoutes(routes []gateway.Route, backendURLs map[string]string, policies map[string]gateway.Policy) []types.LocalRoute {
	if len(routes) == 0 {
		return nil
	}
	out := make([]types.LocalRoute, 0, len(routes))
	for _, r := range routes {
		backends := renderBackendRefs(r.BackendRefs, backendURLs)
		if r.MCP != nil {
			backends = []types.RouteBackend{{
				Weight: 100,
				MCP:    renderMCPBackend(r.MCP),
			}}
		}
		out = append(out, types.LocalRoute{
			RouteName: r.Name,
			Hostnames: r.Hostnames,
			Matches: []types.RouteMatch{{
				Path: types.PathMatch{PathPrefix: r.PathPrefix},
			}},
			Policies: renderPolicyRefs(r.Policies, policies),
			Backends: backends,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RouteName < out[j].RouteName
	})
	return out
}

// renderBackendRefs resolves each BackendRef to its backend URL, defaulting the
// weight to 100 when unset. Input order is preserved.
func renderBackendRefs(refs []gateway.BackendRef, backendURLs map[string]string) []types.RouteBackend {
	if len(refs) == 0 {
		return nil
	}
	out := make([]types.RouteBackend, 0, len(refs))
	for _, ref := range refs {
		weight := ref.Weight
		if weight == 0 {
			weight = 100
		}
		out = append(out, types.RouteBackend{
			Weight: weight,
			Host:   backendURLs[ref.Name],
		})
	}
	return out
}

// renderMCPBackend translates a desired MCPBackend into its native form,
// sorting targets by name so equal desired inputs render identically
// regardless of desired target order. It returns nil when mcp is nil.
func renderMCPBackend(mcp *gateway.MCPBackend) *types.MCPBackend {
	if mcp == nil {
		return nil
	}
	targets := make([]types.MCPTarget, 0, len(mcp.Targets))
	for _, t := range mcp.Targets {
		targets = append(targets, types.MCPTarget{
			Name:    t.Name,
			SSE:     renderSSETargetSpec(t.SSE),
			Stdio:   renderStdioTargetSpec(t.Stdio),
			MCP:     renderMCPTargetSpecField(t.MCP),
			OpenAPI: renderOpenAPITargetSpec(t.OpenAPI),
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].Name < targets[j].Name
	})
	return &types.MCPBackend{Targets: targets}
}

// renderSSETargetSpec maps a desired SSETargetSpec into its native form. It
// returns nil when s is nil.
func renderSSETargetSpec(s *gateway.SSETargetSpec) *types.SSETargetSpec {
	if s == nil {
		return nil
	}
	return &types.SSETargetSpec{
		Scheme: s.Scheme,
		Host:   s.Host,
		Port:   s.Port,
		Path:   s.Path,
	}
}

// renderStdioTargetSpec maps a desired StdioTargetSpec into its native form.
// It returns nil when s is nil.
func renderStdioTargetSpec(s *gateway.StdioTargetSpec) *types.StdioTargetSpec {
	if s == nil {
		return nil
	}
	return &types.StdioTargetSpec{
		Cmd:  s.Cmd,
		Args: s.Args,
		Env:  s.Env,
	}
}

// renderMCPTargetSpecField maps a desired MCPTargetSpec into its native
// form. It returns nil when s is nil.
func renderMCPTargetSpecField(s *gateway.MCPTargetSpec) *types.MCPTargetSpec {
	if s == nil {
		return nil
	}
	return &types.MCPTargetSpec{Host: s.Host}
}

// renderOpenAPITargetSpec maps a desired OpenAPITargetSpec into its native
// form. It returns nil when s is nil.
func renderOpenAPITargetSpec(s *gateway.OpenAPITargetSpec) *types.OpenAPITargetSpec {
	if s == nil {
		return nil
	}
	return &types.OpenAPITargetSpec{
		Host:   s.Host,
		Port:   s.Port,
		Schema: s.Schema,
	}
}

// renderTLS maps a desired TLSConfig into the native TLS server config,
// including certificate refs and listener options. It returns nil when tls is
// nil.
func renderTLS(tls *gateway.TLSConfig) *types.LocalTLSServerConfig {
	if tls == nil {
		return nil
	}
	out := &types.LocalTLSServerConfig{
		Mode:    tls.Mode,
		Options: tls.Options,
	}
	if len(tls.CertificateRefs) > 0 {
		refs := make([]types.LocalObjectReference, 0, len(tls.CertificateRefs))
		for _, ref := range tls.CertificateRefs {
			refs = append(refs, types.LocalObjectReference{
				Group:     ref.Group,
				Kind:      ref.Kind,
				Name:      ref.Name,
				Namespace: ref.Namespace,
			})
		}
		out.CertificateRefs = refs
	}
	return out
}

// renderAllowedRoutes maps a desired AllowedRoutes selector into its native
// form. It returns nil when ar is nil.
func renderAllowedRoutes(ar *gateway.AllowedRoutes) *types.LocalAllowedRoutes {
	if ar == nil {
		return nil
	}
	return &types.LocalAllowedRoutes{
		Namespaces: ar.Namespaces,
		Kinds:      ar.Kinds,
	}
}

// renderPolicyRefs merges the specs of the referenced policies into a single
// native FilterOrPolicy. Unknown references and policies that contribute
// nothing are ignored; it returns nil when no policy contributes.
func renderPolicyRefs(refs []gateway.PolicyRef, policies map[string]gateway.Policy) *types.FilterOrPolicy {
	if len(refs) == 0 {
		return nil
	}
	fp := &types.FilterOrPolicy{}
	contributed := false
	for _, ref := range refs {
		p, ok := policies[ref.Name]
		if !ok {
			continue
		}
		if a := p.Spec.MCPAuthorization; a != nil {
			fp.MCPAuthorization = &types.MCPAuthorization{
				Rules: renderAuthzRules(a.Action, a.MatchExpressions),
			}
			contributed = true
		}
		if a := p.Spec.TrafficAuthorization; a != nil {
			fp.TrafficAuthorization = &types.TrafficAuthorization{
				Rules: renderAuthzRules(a.Action, a.MatchExpressions),
			}
			contributed = true
		}
		if fc := p.Spec.FrontendConnect; fc != nil {
			native := &types.FrontendConnect{Enabled: fc.Enabled}
			if fc.Authorization != nil {
				native.Rules = renderAuthzRules(fc.Authorization.Action, fc.Authorization.MatchExpressions)
			}
			fp.FrontendConnect = native
			contributed = true
		}
		if p.Spec.A2A != nil {
			fp.A2A = &types.A2APolicy{}
			contributed = true
		}
		if u := p.Spec.URLRewrite; u != nil {
			fp.URLRewrite = &types.URLRewrite{Path: &types.PathRedirect{Prefix: u.PathPrefix}}
			contributed = true
		}
	}
	if !contributed {
		return nil
	}
	return fp
}

// renderAuthzRules builds the deterministic native rules payload for an
// authorization policy from its action and CEL match expressions.
func renderAuthzRules(action string, matchExpressions []string) any {
	return map[string]any{
		"action":           action,
		"matchExpressions": matchExpressions,
	}
}
