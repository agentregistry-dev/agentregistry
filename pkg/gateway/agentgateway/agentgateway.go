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
	"sort"
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
	backendURLs := make(map[string]string, len(desired.Backends))
	for _, b := range desired.Backends {
		backendURLs[b.Name] = b.URL
	}

	policies := make(map[string]gateway.Policy, len(desired.Policies))
	for _, p := range desired.Policies {
		policies[p.Name] = p
	}

	routes, err := renderRoutes(desired.Routes, backendURLs, policies)
	if err != nil {
		return nil, err
	}

	cfg := &AgentGatewayConfig{
		Config: struct{}{},
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

	sort.Ints(ports)
	for _, port := range ports {
		listeners := listenersByPort[port]
		sort.Slice(listeners, func(i, j int) bool {
			return listeners[i].Name < listeners[j].Name
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
func renderRoutes(routes []gateway.Route, backendURLs map[string]string, policies map[string]gateway.Policy) ([]LocalRoute, error) {
	if len(routes) == 0 {
		return nil, nil
	}
	out := make([]LocalRoute, 0, len(routes))
	for _, r := range routes {
		backends, err := renderBackendRefs(r.BackendRefs, backendURLs)
		if err != nil {
			return nil, fmt.Errorf("route %q: %w", r.Name, err)
		}
		if r.MCP != nil {
			backends = []RouteBackend{{
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
			Backends: backends,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RouteName < out[j].RouteName
	})
	return out, nil
}

// renderBackendRefs resolves each BackendRef to its backend URL, defaulting the
// weight to 100 when unset. Input order is preserved. It errors when a ref
// names a backend that Config.Backends never declared, instead of silently
// rendering an empty host.
func renderBackendRefs(refs []gateway.BackendRef, backendURLs map[string]string) ([]RouteBackend, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]RouteBackend, 0, len(refs))
	for _, ref := range refs {
		url, ok := backendURLs[ref.Name]
		if !ok {
			return nil, fmt.Errorf("backend ref %q: no matching backend declared", ref.Name)
		}
		weight := ref.Weight
		if weight == 0 {
			weight = 100
		}
		out = append(out, RouteBackend{
			Weight: weight,
			Host:   url,
		})
	}
	return out, nil
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
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].Name < targets[j].Name
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

// renderMCPTargetSpecField maps a desired MCPTargetSpec into its native
// form. It returns nil when s is nil.
func renderMCPTargetSpecField(s *gateway.MCPTargetSpec) *MCPTargetSpec {
	if s == nil {
		return nil
	}
	return &MCPTargetSpec{Host: s.Host}
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
			fp.MCPAuthorization = &MCPAuthorization{
				Rules: renderAuthzRules(a.Action, a.MatchExpressions),
			}
			contributed = true
		}
		if a := p.Spec.TrafficAuthorization; a != nil {
			if fp.TrafficAuthorization != nil {
				return nil, fmt.Errorf("policy %q: traffic authorization already set by another policy", ref.Name)
			}
			fp.TrafficAuthorization = &TrafficAuthorization{
				Rules: renderAuthzRules(a.Action, a.MatchExpressions),
			}
			contributed = true
		}
		if fc := p.Spec.FrontendConnect; fc != nil {
			if fp.FrontendConnect != nil {
				return nil, fmt.Errorf("policy %q: frontend connect already set by another policy", ref.Name)
			}
			native := &FrontendConnect{Enabled: fc.Enabled}
			if fc.Authorization != nil {
				native.Rules = renderAuthzRules(fc.Authorization.Action, fc.Authorization.MatchExpressions)
			}
			fp.FrontendConnect = native
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
	}
	if !contributed {
		return nil, nil
	}
	return fp, nil
}

// renderAuthzRules builds the deterministic native rules payload for an
// authorization policy from its action and CEL match expressions.
func renderAuthzRules(action string, matchExpressions []string) any {
	return map[string]any{
		"action":           action,
		"matchExpressions": matchExpressions,
	}
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
