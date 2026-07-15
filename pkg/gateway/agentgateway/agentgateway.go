// Package agentgateway is the concrete gateway.Engine implementation backed by
// agentgateway's native config format. Render translates a desired
// gateway.Config into *AgentGatewayConfig (the agent-gateway.yaml wire format);
// engine.Apply renders the desired config and maintains it on disk as
// agent-gateway.yaml, and engine.Remove strips it — both merging or filtering
// routes by deployment id so multiple deployments can share one gateway
// instance. The native config types live alongside this engine (see types.go);
// callers outside this package depend only on gateway.Config.
package agentgateway

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/agentregistry-dev/agentregistry/pkg/gateway"
)

const agentGatewayFileName = "agent-gateway.yaml"

// engine implements gateway.Engine against agentgateway's native config
// format. Apply and Remove maintain the on-disk agent-gateway.yaml at dir,
// merging or filtering routes/targets by deployment id (gateway.Target.Name)
// so multiple deployments can share one gateway instance.
type engine struct {
	dir  string
	port uint16
}

// NewEngine constructs a gateway.Engine pinned to the directory
// agent-gateway.yaml lives in and the port the agentgateway process binds.
func NewEngine(dir string, port uint16) gateway.Engine {
	return &engine{dir: dir, port: port}
}

var _ gateway.Engine = (*engine)(nil)

// Render translates a desired gateway.Config into the native
// *AgentGatewayConfig. It is a pure function: no I/O, no engine state, and
// deterministic — binds are sorted by port, listeners within a bind by name,
// and routes by name, so equal inputs always produce equal outputs. engine.Apply
// calls it internally; it is exported so agentgateway-aware callers can render
// (e.g. to preview or diff config) without applying.
func Render(_ context.Context, desired gateway.Config) (*AgentGatewayConfig, error) {
	backends := make(map[string]gateway.Backend, len(desired.Backends))
	for _, b := range desired.Backends {
		backends[b.Name] = b
	}

	policies := make(map[string]gateway.Policy, len(desired.Policies))
	for _, p := range desired.Policies {
		policies[p.Name] = p
	}

	routes, err := renderRoutes(desired.Routes, backends, policies)
	if err != nil {
		return nil, err
	}

	cfg := &AgentGatewayConfig{
		Config:   struct{}{},
		Backends: renderBackends(desired.Backends),
	}

	listenersByPort := make(map[int][]LocalListener)
	var ports []int
	for _, l := range desired.Listeners {
		if _, ok := listenersByPort[l.Port]; !ok {
			ports = append(ports, l.Port)
		}
		listenerPolicies, err := renderPolicyRefs(l.Policies, policies)
		if err != nil {
			return nil, fmt.Errorf("listener %q: %w", l.Name, err)
		}
		listenersByPort[l.Port] = append(listenersByPort[l.Port], LocalListener{
			Name:          l.Name,
			GatewayName:   desired.ClassName,
			Protocol:      LocalListenerProtocol(l.Protocol),
			TLS:           renderTLS(l.TLS),
			AllowedRoutes: renderAllowedRoutes(l.AllowedRoutes),
			Policies:      listenerPolicies,
			Routes:        routes,
		})
	}

	slices.Sort(ports)
	for _, port := range ports {
		listeners := listenersByPort[port]
		slices.SortFunc(listeners, func(a, b LocalListener) int {
			return cmp.Compare(a.Name, b.Name)
		})
		cfg.Binds = append(cfg.Binds, LocalBind{
			Port:      uint16(port),
			Listeners: listeners,
		})
	}

	return cfg, nil
}

// renderRoutes translates desired routes into deterministically sorted native
// routes. It returns nil when there are no routes so empty configs compare
// cleanly as whole objects.
func renderRoutes(routes []gateway.Route, backends map[string]gateway.Backend, policies map[string]gateway.Policy) ([]LocalRoute, error) {
	if len(routes) == 0 {
		return nil, nil
	}
	out := make([]LocalRoute, 0, len(routes))
	for _, r := range routes {
		routeBackends, err := renderBackendRefs(r.BackendRefs, backends)
		if err != nil {
			return nil, fmt.Errorf("route %q: %w", r.Name, err)
		}
		if r.MCP != nil {
			routeBackends = []RouteBackend{{
				Weight: 100,
				MCP:    renderMCPBackend(r.MCP),
			}}
		}
		routePolicies, err := renderPolicyRefs(r.Policies, policies)
		if err != nil {
			return nil, fmt.Errorf("route %q: %w", r.Name, err)
		}
		out = append(out, LocalRoute{
			RouteName: r.Name,
			Hostnames: r.Hostnames,
			Matches: []RouteMatch{{
				Path: PathMatch{PathPrefix: r.PathPrefix},
			}},
			Policies: routePolicies,
			Backends: routeBackends,
		})
	}
	slices.SortFunc(out, func(a, b LocalRoute) int {
		return cmp.Compare(a.RouteName, b.RouteName)
	})
	return out, nil
}

// renderBackendRefs resolves each BackendRef against Config.Backends, defaulting
// the weight to 100 when unset. Plain URL backends are inlined as a host; named
// top-level backends (MCP or Raw) are emitted separately by renderBackends and
// referenced here by name. Input order is preserved. It errors when a ref names
// a backend that Config.Backends never declared, instead of silently rendering
// an empty host.
func renderBackendRefs(refs []gateway.BackendRef, backends map[string]gateway.Backend) ([]RouteBackend, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]RouteBackend, 0, len(refs))
	for _, ref := range refs {
		b, ok := backends[ref.Name]
		if !ok {
			return nil, fmt.Errorf("backend ref %q: no matching backend declared", ref.Name)
		}
		weight := ref.Weight
		if weight == 0 {
			weight = 100
		}
		if b.MCP != nil || len(b.Extensions) > 0 {
			out = append(out, RouteBackend{Weight: weight, Backend: backendRef(ref.Name)})
			continue
		}
		out = append(out, RouteBackend{Weight: weight, Host: b.URL})
	}
	return out, nil
}

// renderBackends emits the top-level native backends for every named backend
// (MCP or Raw) in the desired config; plain URL backends are not emitted here
// because renderBackendRefs inlines them into the referencing route. Backends
// are sorted by name so equal inputs render identically. Raw backends carry
// their opaque spec through under their native type key without interpretation.
func renderBackends(backends []gateway.Backend) []LocalBackend {
	out := make([]LocalBackend, 0, len(backends))
	for _, b := range backends {
		switch {
		case b.MCP != nil:
			out = append(out, LocalBackend{Name: b.Name, MCP: renderMCPBackend(b.MCP)})
		case len(b.Extensions) > 0:
			extra := make(map[string]any, len(b.Extensions))
			for _, ext := range b.Extensions {
				extra[ext.Type] = ext.Spec
			}
			out = append(out, LocalBackend{Name: b.Name, Extra: extra})
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.SortFunc(out, func(a, b LocalBackend) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}

// backendRef formats a backend name as a native backend reference. Agentgateway
// references top-level backends by a leading-slash name (e.g. "/weather").
func backendRef(name string) string {
	return "/" + name
}

// renderMCPBackend translates a desired MCPBackend into its native form,
// sorting targets by name so equal desired inputs render identically
// regardless of desired target order. It returns nil when mcp is nil.
func renderMCPBackend(mcp *gateway.MCPBackend) *MCPBackend {
	if mcp == nil {
		return nil
	}
	targets := make([]MCPTarget, 0, len(mcp.Targets))
	for _, t := range mcp.Targets {
		targets = append(targets, MCPTarget{
			Name:    t.Name,
			SSE:     renderSSETargetSpec(t.SSE),
			Stdio:   renderStdioTargetSpec(t.Stdio),
			MCP:     renderMCPTargetSpecField(t.MCP),
			OpenAPI: renderOpenAPITargetSpec(t.OpenAPI),
		})
	}
	slices.SortFunc(targets, func(a, b MCPTarget) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return &MCPBackend{Targets: targets}
}

// renderSSETargetSpec maps a desired SSETargetSpec into its native form. It
// returns nil when s is nil.
func renderSSETargetSpec(s *gateway.SSETargetSpec) *SSETargetSpec {
	if s == nil {
		return nil
	}
	return &SSETargetSpec{
		Scheme: s.Scheme,
		Host:   s.Host,
		Port:   s.Port,
		Path:   s.Path,
	}
}

// renderStdioTargetSpec maps a desired StdioTargetSpec into its native form.
// It returns nil when s is nil.
func renderStdioTargetSpec(s *gateway.StdioTargetSpec) *StdioTargetSpec {
	if s == nil {
		return nil
	}
	return &StdioTargetSpec{
		Cmd:  s.Cmd,
		Args: s.Args,
		Env:  s.Env,
	}
}

// renderMCPTargetSpecField maps a desired MCPTargetSpec into its native form.
// A target may either dial Host directly or route through a named Backend (with
// Path appended); Backend is emitted as a native backend reference. It returns
// nil when s is nil.
func renderMCPTargetSpecField(s *gateway.MCPTargetSpec) *MCPTargetSpec {
	if s == nil {
		return nil
	}
	out := &MCPTargetSpec{Host: s.Host, Path: s.Path}
	if s.Backend != "" {
		out.Backend = backendRef(s.Backend)
	}
	return out
}

// renderOpenAPITargetSpec maps a desired OpenAPITargetSpec into its native
// form. It returns nil when s is nil.
func renderOpenAPITargetSpec(s *gateway.OpenAPITargetSpec) *OpenAPITargetSpec {
	if s == nil {
		return nil
	}
	return &OpenAPITargetSpec{
		Host:   s.Host,
		Port:   s.Port,
		Schema: s.Schema,
	}
}

// renderTLS maps a desired TLSConfig into the native TLS server config,
// including certificate refs and listener options. It returns nil when tls is
// nil.
func renderTLS(tls *gateway.TLSConfig) *LocalTLSServerConfig {
	if tls == nil {
		return nil
	}
	out := &LocalTLSServerConfig{
		Mode:    tls.Mode,
		Options: tls.Options,
	}
	if len(tls.CertificateRefs) > 0 {
		refs := make([]LocalObjectReference, 0, len(tls.CertificateRefs))
		for _, ref := range tls.CertificateRefs {
			refs = append(refs, LocalObjectReference{
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
func renderAllowedRoutes(ar *gateway.AllowedRoutes) *LocalAllowedRoutes {
	if ar == nil {
		return nil
	}
	return &LocalAllowedRoutes{
		Namespaces: ar.Namespaces,
		Kinds:      ar.Kinds,
	}
}

// renderPolicyRefs merges the specs of the referenced policies into a single
// native FilterOrPolicy. Unknown references and policies that contribute
// nothing are ignored; it returns nil when no policy contributes. It errors
// when two referenced policies set the same PolicySpec field on the same
// route/listener, rather than letting the later one silently overwrite the
// earlier one.
func renderPolicyRefs(refs []gateway.PolicyRef, policies map[string]gateway.Policy) (*FilterOrPolicy, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	fp := &FilterOrPolicy{}
	contributed := false
	for _, ref := range refs {
		p, ok := policies[ref.Name]
		if !ok {
			continue
		}
		if a := p.Spec.MCPAuthorization; a != nil {
			if fp.MCPAuthorization != nil {
				return nil, fmt.Errorf("policy %q: mcp authorization already set by another policy", ref.Name)
			}
			fp.MCPAuthorization = &MCPAuthorization{Rules: authzRules(a)}
			contributed = true
		}
		if a := p.Spec.TrafficAuthorization; a != nil {
			if fp.TrafficAuthorization != nil {
				return nil, fmt.Errorf("policy %q: traffic authorization already set by another policy", ref.Name)
			}
			fp.TrafficAuthorization = &TrafficAuthorization{Rules: authzRules(a)}
			contributed = true
		}
		if fc := p.Spec.FrontendConnect; fc != nil {
			if fp.FrontendConnect != nil {
				return nil, fmt.Errorf("policy %q: frontend connect already set by another policy", ref.Name)
			}
			native := &FrontendConnect{Enabled: fc.Enabled}
			if fc.Authorization != nil {
				native.Rules = authzRules(fc.Authorization)
			}
			fp.FrontendConnect = native
			contributed = true
		}
		if c := p.Spec.CORS; c != nil {
			if fp.CORS != nil {
				return nil, fmt.Errorf("policy %q: cors already set by another policy", ref.Name)
			}
			fp.CORS = &CORS{
				AllowOrigins:  c.AllowOrigins,
				AllowMethods:  c.AllowMethods,
				AllowHeaders:  c.AllowHeaders,
				ExposeHeaders: c.ExposeHeaders,
			}
			contributed = true
		}
		if p.Spec.A2A != nil {
			if fp.A2A != nil {
				return nil, fmt.Errorf("policy %q: a2a already set by another policy", ref.Name)
			}
			fp.A2A = &A2APolicy{}
			contributed = true
		}
		if u := p.Spec.URLRewrite; u != nil {
			if fp.URLRewrite != nil {
				return nil, fmt.Errorf("policy %q: url rewrite already set by another policy", ref.Name)
			}
			fp.URLRewrite = &URLRewrite{Path: &PathRedirect{Prefix: u.PathPrefix}}
			contributed = true
		}
		if j := p.Spec.JWTAuth; j != nil {
			if fp.JWTAuth != nil {
				return nil, fmt.Errorf("policy %q: jwt auth already set by another policy", ref.Name)
			}
			fp.JWTAuth = renderJWTAuth(j)
			contributed = true
		}
		if tr := p.Spec.Transformation; tr != nil {
			if fp.Transformations != nil {
				return nil, fmt.Errorf("policy %q: transformation already set by another policy", ref.Name)
			}
			fp.Transformations = renderTransformation(tr)
			contributed = true
		}
	}
	if !contributed {
		return nil, nil
	}
	return fp, nil
}

// renderJWTAuth maps a desired JWTAuthPolicy into the native listener JWT auth
// config, preserving provider order.
func renderJWTAuth(j *gateway.JWTAuthPolicy) *ListenerJWTAuth {
	providers := make([]JWTProvider, 0, len(j.Providers))
	for _, p := range j.Providers {
		providers = append(providers, JWTProvider{
			Issuer:    p.Issuer,
			Audiences: p.Audiences,
			JWKS:      JWKSSource{URL: p.JWKS.URL, File: p.JWKS.File},
		})
	}
	return &ListenerJWTAuth{Mode: j.Mode, Providers: providers}
}

// renderTransformation maps a desired TransformationPolicy into the native
// request transformation stage.
func renderTransformation(t *gateway.TransformationPolicy) *TransformationPolicy {
	return &TransformationPolicy{
		Request: &TransformStage{Metadata: t.RequestMetadata},
	}
}

// authzRules builds the native rules payload for an authorization policy. It
// populates the RuleSet's `rules` field (the native MCPAuthorization /
// TrafficAuthorization / FrontendConnect types carry that key) with the flat
// list of CEL expression strings; a request is allowed when any rule matches.
// An empty policy renders as an empty list rather than null.
func authzRules(a *gateway.AuthzPolicy) any {
	if a.Rules == nil {
		return []string{}
	}
	return a.Rules
}

// Apply renders the desired gateway.Config into agentgateway's native format
// and merges its targets/routes into the on-disk agent-gateway.yaml, upserting
// by target/route name. A desired config with no listeners and no routes is a
// no-op, leaving the existing file untouched.
func (e *engine) Apply(ctx context.Context, _ gateway.Target, desired gateway.Config) error {
	if len(desired.Listeners) == 0 && len(desired.Routes) == 0 {
		return nil
	}
	rendered, err := Render(ctx, desired)
	if err != nil {
		return err
	}
	existing, err := loadConfig(e.dir, e.port)
	if err != nil {
		return err
	}
	incomingTargets := extractMCPRouteTargets(rendered)
	incomingRoutes := extractNonMCPRoutes(rendered)
	mergeConfig(existing, incomingTargets, incomingRoutes, e.port)
	existing.Backends = mergeBackends(existing.Backends, rendered.Backends)
	return writeConfig(e.dir, existing, e.port)
}

// Remove strips every gateway target/route whose name contains
// target.Name (the deployment id) as a "-"/"_"-delimited segment, anchored
// so that e.g. deployment id "dep-1" does not also match "dep-10".
// Idempotent: calling it again once nothing matches is a no-op.
func (e *engine) Remove(_ context.Context, target gateway.Target) error {
	deploymentID := strings.TrimSpace(target.Name)
	if deploymentID == "" {
		return fmt.Errorf("agentgateway engine: target.Name (deployment id) is required")
	}
	existing, err := loadConfig(e.dir, e.port)
	if err != nil {
		return err
	}
	filterRoutesByDeploymentID(existing, deploymentID)
	existing.Backends = filterBackendsByDeploymentID(existing.Backends, deploymentID)
	return writeConfig(e.dir, existing, e.port)
}

func loadConfig(dir string, port uint16) (*AgentGatewayConfig, error) {
	path := filepath.Join(dir, agentGatewayFileName)
	cfg := defaultConfig(port)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read agent gateway config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("unmarshal agent gateway config: %w", err)
	}
	ensureDefaults(cfg, port)
	return cfg, nil
}

func writeConfig(dir string, cfg *AgentGatewayConfig, port uint16) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	if cfg == nil {
		cfg = defaultConfig(port)
	}
	ensureDefaults(cfg, port)
	content, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal agent gateway config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, agentGatewayFileName), content, 0644); err != nil {
		return fmt.Errorf("write agent gateway config: %w", err)
	}
	return nil
}

func defaultConfig(port uint16) *AgentGatewayConfig {
	return &AgentGatewayConfig{
		Config: struct{}{},
		Binds: []LocalBind{{
			Port: port,
			Listeners: []LocalListener{{
				Name:     "default",
				Protocol: LocalListenerProtocolHTTP,
				Routes:   []LocalRoute{},
			}},
		}},
	}
}

func ensureDefaults(cfg *AgentGatewayConfig, port uint16) {
	if cfg.Config == nil {
		cfg.Config = struct{}{}
	}
	if len(cfg.Binds) == 0 {
		cfg.Binds = defaultConfig(port).Binds
		return
	}
	if cfg.Binds[0].Port == 0 {
		cfg.Binds[0].Port = port
	}
	if len(cfg.Binds[0].Listeners) == 0 {
		cfg.Binds[0].Listeners = []LocalListener{{
			Name:     "default",
			Protocol: LocalListenerProtocolHTTP,
			Routes:   []LocalRoute{},
		}}
		return
	}
	if cfg.Binds[0].Listeners[0].Protocol == "" {
		cfg.Binds[0].Listeners[0].Protocol = LocalListenerProtocolHTTP
	}
}

func extractNonMCPRoutes(config *AgentGatewayConfig) []LocalRoute {
	if config == nil || len(config.Binds) == 0 || len(config.Binds[0].Listeners) == 0 {
		return nil
	}
	var routes []LocalRoute
	for _, route := range config.Binds[0].Listeners[0].Routes {
		if route.RouteName == gateway.MCPRouteName {
			continue
		}
		routes = append(routes, route)
	}
	return routes
}

func extractMCPRouteTargets(config *AgentGatewayConfig) []MCPTarget {
	if config == nil || len(config.Binds) == 0 || len(config.Binds[0].Listeners) == 0 {
		return nil
	}
	for _, route := range config.Binds[0].Listeners[0].Routes {
		if route.RouteName != gateway.MCPRouteName {
			continue
		}
		if len(route.Backends) == 0 || route.Backends[0].MCP == nil {
			return nil
		}
		return append([]MCPTarget{}, route.Backends[0].MCP.Targets...)
	}
	return nil
}

func mergeConfig(
	existing *AgentGatewayConfig,
	incomingTargets []MCPTarget,
	incomingRoutes []LocalRoute,
	port uint16,
) {
	ensureDefaults(existing, port)
	if len(existing.Binds) == 0 || len(existing.Binds[0].Listeners) == 0 {
		return
	}

	targetSet := make(map[string]struct{}, len(incomingTargets))
	for _, target := range incomingTargets {
		targetSet[target.Name] = struct{}{}
	}
	routeSet := make(map[string]struct{}, len(incomingRoutes))
	for _, route := range incomingRoutes {
		routeSet[route.RouteName] = struct{}{}
	}

	l := &existing.Binds[0].Listeners[0]

	var existingTargets []MCPTarget
	var otherRoutes []LocalRoute
	for _, route := range l.Routes {
		if route.RouteName == gateway.MCPRouteName {
			if len(route.Backends) > 0 && route.Backends[0].MCP != nil {
				for _, target := range route.Backends[0].MCP.Targets {
					if _, shouldRemove := targetSet[target.Name]; !shouldRemove {
						existingTargets = append(existingTargets, target)
					}
				}
			}
			continue
		}
		if _, shouldRemove := routeSet[route.RouteName]; shouldRemove {
			continue
		}
		otherRoutes = append(otherRoutes, route)
	}

	existingTargets = append(existingTargets, incomingTargets...)
	otherRoutes = append(otherRoutes, incomingRoutes...)

	slices.SortFunc(existingTargets, func(a, b MCPTarget) int {
		return cmp.Compare(a.Name, b.Name)
	})
	slices.SortFunc(otherRoutes, func(a, b LocalRoute) int {
		return cmp.Compare(a.RouteName, b.RouteName)
	})

	routes := make([]LocalRoute, 0, len(otherRoutes)+1)
	if len(existingTargets) > 0 {
		routes = append(routes, LocalRoute{
			RouteName: gateway.MCPRouteName,
			Matches: []RouteMatch{{
				Path: PathMatch{PathPrefix: "/mcp"},
			}},
			Backends: []RouteBackend{{
				Weight: 100,
				MCP:    &MCPBackend{Targets: existingTargets},
			}},
		})
	}
	routes = append(routes, otherRoutes...)
	l.Routes = routes
}

// mergeBackends upserts the incoming top-level backends into existing by name:
// an incoming backend replaces any existing entry with the same name, and other
// deployments' backends are preserved. The result is sorted by name so equal
// inputs write identically. Returns nil when nothing remains so empty configs
// omit the backends key. An Apply that renders no top-level backends leaves the
// existing set untouched (deletion happens via Remove, mirroring route merge).
func mergeBackends(existing, incoming []LocalBackend) []LocalBackend {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}
	incomingSet := make(map[string]struct{}, len(incoming))
	for _, b := range incoming {
		incomingSet[b.Name] = struct{}{}
	}
	merged := make([]LocalBackend, 0, len(existing)+len(incoming))
	for _, b := range existing {
		if _, replace := incomingSet[b.Name]; replace {
			continue
		}
		merged = append(merged, b)
	}
	merged = append(merged, incoming...)
	slices.SortFunc(merged, func(a, b LocalBackend) int {
		return cmp.Compare(a.Name, b.Name)
	})
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// filterBackendsByDeploymentID drops every top-level backend whose name matches
// deploymentID as a "-"/"_"-delimited segment, mirroring route removal so a
// deployment's backends are torn down alongside its routes. Returns nil when
// nothing remains.
func filterBackendsByDeploymentID(backends []LocalBackend, deploymentID string) []LocalBackend {
	if len(backends) == 0 {
		return nil
	}
	out := make([]LocalBackend, 0, len(backends))
	for _, b := range backends {
		if matchesDeploymentID(b.Name, deploymentID) {
			continue
		}
		out = append(out, b)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func filterRoutesByDeploymentID(cfg *AgentGatewayConfig, deploymentID string) {
	l := listener(cfg)
	if l == nil {
		return
	}

	filteredRoutes := make([]LocalRoute, 0, len(l.Routes))
	for _, route := range l.Routes {
		filteredRoute, keep := filterRouteByDeploymentID(route, deploymentID)
		if keep {
			filteredRoutes = append(filteredRoutes, filteredRoute)
		}
	}
	l.Routes = filteredRoutes
}

func listener(cfg *AgentGatewayConfig) *LocalListener {
	if cfg == nil || len(cfg.Binds) == 0 || len(cfg.Binds[0].Listeners) == 0 {
		return nil
	}
	return &cfg.Binds[0].Listeners[0]
}

func filterRouteByDeploymentID(route LocalRoute, deploymentID string) (LocalRoute, bool) {
	if route.RouteName == gateway.MCPRouteName {
		return filterMCPRouteTargets(route, deploymentID)
	}
	return route, !matchesDeploymentID(route.RouteName, deploymentID)
}

func filterMCPRouteTargets(route LocalRoute, deploymentID string) (LocalRoute, bool) {
	if len(route.Backends) == 0 || route.Backends[0].MCP == nil {
		return route, false
	}

	filteredTargets := make([]MCPTarget, 0, len(route.Backends[0].MCP.Targets))
	for _, target := range route.Backends[0].MCP.Targets {
		if matchesDeploymentID(target.Name, deploymentID) {
			continue
		}
		filteredTargets = append(filteredTargets, target)
	}
	route.Backends[0].MCP.Targets = filteredTargets
	return route, len(filteredTargets) > 0
}

// matchesDeploymentID reports whether name was generated for deploymentID,
// i.e. deploymentID occurs in name as a "-"/"_"-delimited segment (or at a
// string edge). This is anchored so that deployment id "dep-1" does not also
// match names generated for "dep-10".
func matchesDeploymentID(name, deploymentID string) bool {
	if deploymentID == "" {
		return false
	}
	isDelim := func(b byte) bool { return b == '-' || b == '_' }
	for start := 0; start < len(name); {
		idx := strings.Index(name[start:], deploymentID)
		if idx == -1 {
			return false
		}
		idx += start
		end := idx + len(deploymentID)
		leftOK := idx == 0 || isDelim(name[idx-1])
		rightOK := end == len(name) || isDelim(name[end])
		if leftOK && rightOK {
			return true
		}
		start = idx + 1
	}
	return false
}
