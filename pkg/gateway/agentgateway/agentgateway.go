// Package agentgateway provides a gateway.Engine backed by agent-gateway.yaml.
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

type engine struct {
	dir  string
	port uint16
}

// NewEngine constructs an agentgateway-backed gateway.Engine.
func NewEngine(dir string, port uint16) gateway.Engine {
	return &engine{dir: dir, port: port}
}

var _ gateway.Engine = (*engine)(nil)

// RenderYAML translates a desired gateway.Config into deterministic YAML.
func RenderYAML(ctx context.Context, desired gateway.Config) ([]byte, error) {
	cfg, err := renderConfig(ctx, desired)
	if err != nil {
		return nil, err
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal agent gateway config: %w", err)
	}
	return out, nil
}

func renderConfig(_ context.Context, desired gateway.Config) (*agentGatewayConfig, error) {
	backends := make(map[string]gateway.Backend, len(desired.Backends))
	for _, b := range desired.Backends {
		backends[b.Name] = b
	}

	routes, err := renderRoutes(desired.Routes, backends)
	if err != nil {
		return nil, err
	}

	cfg := &agentGatewayConfig{
		Config:   struct{}{},
		Backends: renderBackends(desired.Backends),
	}

	listenersByPort := make(map[int][]localListener)
	var ports []int
	for _, l := range desired.Listeners {
		if _, ok := listenersByPort[l.Port]; !ok {
			ports = append(ports, l.Port)
		}
		listenersByPort[l.Port] = append(listenersByPort[l.Port], localListener{
			Name:          l.Name,
			GatewayName:   desired.ClassName,
			Protocol:      localListenerProtocol(l.Protocol),
			TLS:           renderTLS(l.TLS),
			AllowedRoutes: renderAllowedRoutes(l.AllowedRoutes),
			Policies:      renderPolicySpec(l.Policies),
			Routes:        routes,
		})
	}

	slices.Sort(ports)
	for _, port := range ports {
		listeners := listenersByPort[port]
		slices.SortFunc(listeners, func(a, b localListener) int {
			return cmp.Compare(a.Name, b.Name)
		})
		cfg.Binds = append(cfg.Binds, localBind{
			Port:      uint16(port),
			Listeners: listeners,
		})
	}

	return cfg, nil
}

func renderRoutes(routes []gateway.Route, backends map[string]gateway.Backend) ([]localRoute, error) {
	if len(routes) == 0 {
		return nil, nil
	}
	out := make([]localRoute, 0, len(routes))
	for _, r := range routes {
		routeBackends, err := renderBackendRefs(r.BackendRefs, backends)
		if err != nil {
			return nil, fmt.Errorf("route %q: %w", r.Name, err)
		}
		if r.MCP != nil {
			routeBackends = []routeBackend{{
				Weight: 100,
				MCP:    renderMCPBackend(r.MCP),
			}}
		}
		out = append(out, localRoute{
			RouteName: r.Name,
			Hostnames: r.Hostnames,
			Matches: []routeMatch{{
				Path: pathMatch{PathPrefix: r.PathPrefix},
			}},
			Policies: renderPolicySpec(r.Policies),
			Backends: routeBackends,
		})
	}
	slices.SortFunc(out, func(a, b localRoute) int {
		return cmp.Compare(a.RouteName, b.RouteName)
	})
	return out, nil
}

func renderBackendRefs(refs []gateway.BackendRef, backends map[string]gateway.Backend) ([]routeBackend, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]routeBackend, 0, len(refs))
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
			out = append(out, routeBackend{Weight: weight, Backend: backendRef(ref.Name)})
			continue
		}
		out = append(out, routeBackend{Weight: weight, Host: b.URL})
	}
	return out, nil
}

func renderBackends(backends []gateway.Backend) []localBackend {
	out := make([]localBackend, 0, len(backends))
	for _, b := range backends {
		switch {
		case b.MCP != nil:
			out = append(out, localBackend{Name: b.Name, MCP: renderMCPBackend(b.MCP)})
		case len(b.Extensions) > 0:
			out = append(out, localBackend{Name: b.Name, Extra: b.Extensions})
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.SortFunc(out, func(a, b localBackend) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}

func backendRef(name string) string {
	return "/" + name
}

func renderMCPBackend(mcp *gateway.MCPBackend) *mcpBackend {
	if mcp == nil {
		return nil
	}
	targets := make([]mcpTarget, 0, len(mcp.Targets))
	for _, t := range mcp.Targets {
		targets = append(targets, mcpTarget{
			Name:    t.Name,
			SSE:     renderSSETargetSpec(t.SSE),
			Stdio:   renderStdioTargetSpec(t.Stdio),
			MCP:     renderMCPTargetSpecField(t.MCP),
			OpenAPI: renderOpenAPITargetSpec(t.OpenAPI),
		})
	}
	slices.SortFunc(targets, func(a, b mcpTarget) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return &mcpBackend{Targets: targets}
}

func renderSSETargetSpec(s *gateway.SSETargetSpec) *sseTargetSpec {
	if s == nil {
		return nil
	}
	return &sseTargetSpec{
		Scheme: s.Scheme,
		Host:   s.Host,
		Port:   s.Port,
		Path:   s.Path,
	}
}

func renderStdioTargetSpec(s *gateway.StdioTargetSpec) *stdioTargetSpec {
	if s == nil {
		return nil
	}
	return &stdioTargetSpec{
		Cmd:  s.Cmd,
		Args: s.Args,
		Env:  s.Env,
	}
}

func renderMCPTargetSpecField(s *gateway.MCPTargetSpec) *mcpTargetSpec {
	if s == nil {
		return nil
	}
	out := &mcpTargetSpec{Host: s.Host, Path: s.Path}
	if s.Backend != "" {
		out.Backend = backendRef(s.Backend)
	}
	return out
}

func renderOpenAPITargetSpec(s *gateway.OpenAPITargetSpec) *openAPITargetSpec {
	if s == nil {
		return nil
	}
	return &openAPITargetSpec{
		Host:   s.Host,
		Port:   s.Port,
		Schema: s.Schema,
	}
}

func renderTLS(tls *gateway.TLSConfig) *localTLSServerConfig {
	if tls == nil {
		return nil
	}
	out := &localTLSServerConfig{
		Mode:    tls.Mode,
		Options: tls.Options,
	}
	if len(tls.CertificateRefs) > 0 {
		refs := make([]localObjectReference, 0, len(tls.CertificateRefs))
		for _, ref := range tls.CertificateRefs {
			refs = append(refs, localObjectReference{
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

func renderAllowedRoutes(ar *gateway.AllowedRoutes) *localAllowedRoutes {
	if ar == nil {
		return nil
	}
	return &localAllowedRoutes{
		Namespaces: renderAllowedRouteNamespaces(ar.Namespaces),
		Kinds:      ar.Kinds,
	}
}

func renderAllowedRouteNamespaces(ns *gateway.AllowedRouteNamespaces) *localAllowedRouteNamespaces {
	if ns == nil {
		return nil
	}
	return &localAllowedRouteNamespaces{From: ns.From}
}

func renderPolicySpec(spec gateway.PolicySpec) *filterOrPolicy {
	if spec == (gateway.PolicySpec{}) {
		return nil
	}
	fp := &filterOrPolicy{}
	contributed := false
	if a := spec.MCPAuthorization; a != nil {
		fp.MCPAuthorization = &mcpAuthorization{Rules: authzRules(a)}
		contributed = true
	}
	if a := spec.TrafficAuthorization; a != nil {
		fp.TrafficAuthorization = &trafficAuthorization{Rules: authzRules(a)}
		contributed = true
	}
	if fc := spec.FrontendConnect; fc != nil {
		native := &frontendConnect{Enabled: fc.Enabled}
		if fc.Authorization != nil {
			native.Rules = authzRules(fc.Authorization)
		}
		fp.FrontendConnect = native
		contributed = true
	}
	if c := spec.CORS; c != nil {
		fp.CORS = &cors{
			AllowOrigins:  c.AllowOrigins,
			AllowMethods:  c.AllowMethods,
			AllowHeaders:  c.AllowHeaders,
			ExposeHeaders: c.ExposeHeaders,
		}
		contributed = true
	}
	if spec.A2A != nil {
		fp.A2A = &a2aPolicy{}
		contributed = true
	}
	if u := spec.URLRewrite; u != nil {
		fp.URLRewrite = &urlRewrite{Path: &pathRedirect{Prefix: u.PathPrefix}}
		contributed = true
	}
	if j := spec.JWTAuth; j != nil {
		fp.JWTAuth = renderJWTAuth(j)
		contributed = true
	}
	if tr := spec.Transformation; tr != nil {
		fp.Transformations = renderTransformation(tr)
		contributed = true
	}
	if !contributed {
		return nil
	}
	return fp
}

func renderJWTAuth(j *gateway.JWTAuthPolicy) *listenerJWTAuth {
	providers := make([]jwtProvider, 0, len(j.Providers))
	for _, p := range j.Providers {
		providers = append(providers, jwtProvider{
			Issuer:    p.Issuer,
			Audiences: p.Audiences,
			JWKS:      jwksSource{URL: p.JWKS.URL, File: p.JWKS.File},
		})
	}
	return &listenerJWTAuth{Mode: j.Mode, Providers: providers}
}

func renderTransformation(t *gateway.TransformationPolicy) *transformationPolicy {
	return &transformationPolicy{
		Request: &transformStage{Metadata: t.RequestMetadata},
	}
}

func authzRules(a *gateway.AuthzPolicy) any {
	if a.Rules == nil {
		return []string{}
	}
	return a.Rules
}

// Apply merges desired routes and targets into agent-gateway.yaml.
func (e *engine) Apply(ctx context.Context, _ gateway.Target, desired gateway.Config) error {
	if len(desired.Listeners) == 0 && len(desired.Routes) == 0 {
		return nil
	}
	rendered, err := renderConfig(ctx, desired)
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

// Remove strips routes, targets, and backends associated with target.Name.
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

func loadConfig(dir string, port uint16) (*agentGatewayConfig, error) {
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

func writeConfig(dir string, cfg *agentGatewayConfig, port uint16) error {
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

func defaultConfig(port uint16) *agentGatewayConfig {
	return &agentGatewayConfig{
		Config: struct{}{},
		Binds: []localBind{{
			Port: port,
			Listeners: []localListener{{
				Name:     "default",
				Protocol: localListenerProtocolHTTP,
				Routes:   []localRoute{},
			}},
		}},
	}
}

func ensureDefaults(cfg *agentGatewayConfig, port uint16) {
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
		cfg.Binds[0].Listeners = []localListener{{
			Name:     "default",
			Protocol: localListenerProtocolHTTP,
			Routes:   []localRoute{},
		}}
		return
	}
	if cfg.Binds[0].Listeners[0].Protocol == "" {
		cfg.Binds[0].Listeners[0].Protocol = localListenerProtocolHTTP
	}
}

func extractNonMCPRoutes(config *agentGatewayConfig) []localRoute {
	if config == nil || len(config.Binds) == 0 || len(config.Binds[0].Listeners) == 0 {
		return nil
	}
	var routes []localRoute
	for _, route := range config.Binds[0].Listeners[0].Routes {
		if route.RouteName == gateway.MCPRouteName {
			continue
		}
		routes = append(routes, route)
	}
	return routes
}

func extractMCPRouteTargets(config *agentGatewayConfig) []mcpTarget {
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
		return append([]mcpTarget{}, route.Backends[0].MCP.Targets...)
	}
	return nil
}

func mergeConfig(
	existing *agentGatewayConfig,
	incomingTargets []mcpTarget,
	incomingRoutes []localRoute,
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

	var existingTargets []mcpTarget
	var otherRoutes []localRoute
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

	slices.SortFunc(existingTargets, func(a, b mcpTarget) int {
		return cmp.Compare(a.Name, b.Name)
	})
	slices.SortFunc(otherRoutes, func(a, b localRoute) int {
		return cmp.Compare(a.RouteName, b.RouteName)
	})

	routes := make([]localRoute, 0, len(otherRoutes)+1)
	if len(existingTargets) > 0 {
		routes = append(routes, localRoute{
			RouteName: gateway.MCPRouteName,
			Matches: []routeMatch{{
				Path: pathMatch{PathPrefix: "/mcp"},
			}},
			Backends: []routeBackend{{
				Weight: 100,
				MCP:    &mcpBackend{Targets: existingTargets},
			}},
		})
	}
	routes = append(routes, otherRoutes...)
	l.Routes = routes
}

func mergeBackends(existing, incoming []localBackend) []localBackend {
	if len(existing) == 0 && len(incoming) == 0 {
		return nil
	}
	incomingSet := make(map[string]struct{}, len(incoming))
	for _, b := range incoming {
		incomingSet[b.Name] = struct{}{}
	}
	merged := make([]localBackend, 0, len(existing)+len(incoming))
	for _, b := range existing {
		if _, replace := incomingSet[b.Name]; replace {
			continue
		}
		merged = append(merged, b)
	}
	merged = append(merged, incoming...)
	slices.SortFunc(merged, func(a, b localBackend) int {
		return cmp.Compare(a.Name, b.Name)
	})
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func filterBackendsByDeploymentID(backends []localBackend, deploymentID string) []localBackend {
	if len(backends) == 0 {
		return nil
	}
	out := make([]localBackend, 0, len(backends))
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

func filterRoutesByDeploymentID(cfg *agentGatewayConfig, deploymentID string) {
	l := listener(cfg)
	if l == nil {
		return
	}

	filteredRoutes := make([]localRoute, 0, len(l.Routes))
	for _, route := range l.Routes {
		filteredRoute, keep := filterRouteByDeploymentID(route, deploymentID)
		if keep {
			filteredRoutes = append(filteredRoutes, filteredRoute)
		}
	}
	l.Routes = filteredRoutes
}

func listener(cfg *agentGatewayConfig) *localListener {
	if cfg == nil || len(cfg.Binds) == 0 || len(cfg.Binds[0].Listeners) == 0 {
		return nil
	}
	return &cfg.Binds[0].Listeners[0]
}

func filterRouteByDeploymentID(route localRoute, deploymentID string) (localRoute, bool) {
	if route.RouteName == gateway.MCPRouteName {
		return filterMCPRouteTargets(route, deploymentID)
	}
	return route, !matchesDeploymentID(route.RouteName, deploymentID)
}

func filterMCPRouteTargets(route localRoute, deploymentID string) (localRoute, bool) {
	if len(route.Backends) == 0 || route.Backends[0].MCP == nil {
		return route, false
	}

	filteredTargets := make([]mcpTarget, 0, len(route.Backends[0].MCP.Targets))
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
