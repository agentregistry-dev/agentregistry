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

	"github.com/agentregistry-dev/agentregistry/internal/registry/gateway"
	types "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
)

// MCPRouteName is the well-known name of the single route that fans out to
// every MCP target. Render groups all desired MCP targets under a route with
// this name; Apply/Remove key their merge/filter logic off it so multiple
// deployments can share one route.
const MCPRouteName = "mcp_route"

const agentGatewayFileName = "agent-gateway.yaml"

// Engine implements gateway.Engine against agentgateway's native config
// format. Apply and Remove maintain the on-disk agent-gateway.yaml at dir,
// merging or filtering routes/targets by deployment id (gateway.Target.Name)
// so multiple deployments can share one gateway instance.
type Engine struct {
	dir  string
	port uint16
}

// NewEngine constructs an Engine pinned to the directory agent-gateway.yaml
// lives in and the port the agentgateway process binds.
func NewEngine(dir string, port uint16) gateway.Engine {
	return &Engine{dir: dir, port: port}
}

var _ gateway.Engine = (*Engine)(nil)

// Apply merges rendered targets/routes into the on-disk agent-gateway.yaml,
// upserting by target/route name. A nil rendered config is a no-op.
func (e *Engine) Apply(_ context.Context, _ gateway.Target, rendered *types.AgentGatewayConfig) error {
	if rendered == nil {
		return nil
	}
	existing, err := loadConfig(e.dir, e.port)
	if err != nil {
		return err
	}
	targetNames := extractTargetNames(rendered)
	routeNames := extractNonMCPRouteNames(rendered)
	mergeConfig(existing, rendered, targetNames, routeNames, e.port)
	return writeConfig(e.dir, existing, e.port)
}

// Remove strips every gateway target/route whose name contains the
// deployment id in target.Name. Idempotent: calling it again once nothing
// matches is a no-op.
func (e *Engine) Remove(_ context.Context, target gateway.Target) error {
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

func loadConfig(dir string, port uint16) (*types.AgentGatewayConfig, error) {
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

func writeConfig(dir string, cfg *types.AgentGatewayConfig, port uint16) error {
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

func defaultConfig(port uint16) *types.AgentGatewayConfig {
	return &types.AgentGatewayConfig{
		Config: struct{}{},
		Binds: []types.LocalBind{{
			Port: port,
			Listeners: []types.LocalListener{{
				Name:     "default",
				Protocol: types.LocalListenerProtocolHTTP,
				Routes:   []types.LocalRoute{},
			}},
		}},
	}
}

func ensureDefaults(cfg *types.AgentGatewayConfig, port uint16) {
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
		cfg.Binds[0].Listeners = []types.LocalListener{{
			Name:     "default",
			Protocol: types.LocalListenerProtocolHTTP,
			Routes:   []types.LocalRoute{},
		}}
		return
	}
	if cfg.Binds[0].Listeners[0].Protocol == "" {
		cfg.Binds[0].Listeners[0].Protocol = types.LocalListenerProtocolHTTP
	}
}

func extractTargetNames(config *types.AgentGatewayConfig) []string {
	targets := extractMCPRouteTargets(config)
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Name)
	}
	slices.Sort(names)
	return names
}

func extractNonMCPRouteNames(config *types.AgentGatewayConfig) []string {
	routes := extractNonMCPRoutes(config)
	names := make([]string, 0, len(routes))
	for _, route := range routes {
		names = append(names, route.RouteName)
	}
	slices.Sort(names)
	return names
}

func extractNonMCPRoutes(config *types.AgentGatewayConfig) []types.LocalRoute {
	if config == nil || len(config.Binds) == 0 || len(config.Binds[0].Listeners) == 0 {
		return nil
	}
	var routes []types.LocalRoute
	for _, route := range config.Binds[0].Listeners[0].Routes {
		if route.RouteName == MCPRouteName {
			continue
		}
		routes = append(routes, route)
	}
	return routes
}

func extractMCPRouteTargets(config *types.AgentGatewayConfig) []types.MCPTarget {
	if config == nil || len(config.Binds) == 0 || len(config.Binds[0].Listeners) == 0 {
		return nil
	}
	for _, route := range config.Binds[0].Listeners[0].Routes {
		if route.RouteName != MCPRouteName {
			continue
		}
		if len(route.Backends) == 0 || route.Backends[0].MCP == nil {
			return nil
		}
		return append([]types.MCPTarget{}, route.Backends[0].MCP.Targets...)
	}
	return nil
}

func mergeConfig(
	existing *types.AgentGatewayConfig,
	incoming *types.AgentGatewayConfig,
	targetNames []string,
	routeNames []string,
	port uint16,
) {
	ensureDefaults(existing, port)
	if incoming == nil || len(existing.Binds) == 0 || len(existing.Binds[0].Listeners) == 0 {
		return
	}

	l := &existing.Binds[0].Listeners[0]
	l.Routes = filterRoutesByNames(l.Routes, routeNames)

	targetSet := make(map[string]struct{}, len(targetNames))
	for _, name := range targetNames {
		targetSet[name] = struct{}{}
	}

	var existingTargets []types.MCPTarget
	var otherRoutes []types.LocalRoute
	for _, route := range l.Routes {
		if route.RouteName == MCPRouteName {
			if len(route.Backends) > 0 && route.Backends[0].MCP != nil {
				for _, target := range route.Backends[0].MCP.Targets {
					if _, shouldRemove := targetSet[target.Name]; !shouldRemove {
						existingTargets = append(existingTargets, target)
					}
				}
			}
			continue
		}
		otherRoutes = append(otherRoutes, route)
	}

	existingTargets = append(existingTargets, extractMCPRouteTargets(incoming)...)
	otherRoutes = append(otherRoutes, extractNonMCPRoutes(incoming)...)

	slices.SortFunc(existingTargets, func(a, b types.MCPTarget) int {
		return cmp.Compare(a.Name, b.Name)
	})
	slices.SortFunc(otherRoutes, func(a, b types.LocalRoute) int {
		return cmp.Compare(a.RouteName, b.RouteName)
	})

	routes := make([]types.LocalRoute, 0, len(otherRoutes)+1)
	if len(existingTargets) > 0 {
		routes = append(routes, types.LocalRoute{
			RouteName: MCPRouteName,
			Matches: []types.RouteMatch{{
				Path: types.PathMatch{PathPrefix: "/mcp"},
			}},
			Backends: []types.RouteBackend{{
				Weight: 100,
				MCP:    &types.MCPBackend{Targets: existingTargets},
			}},
		})
	}
	routes = append(routes, otherRoutes...)
	l.Routes = routes
}

func filterRoutesByNames(routes []types.LocalRoute, names []string) []types.LocalRoute {
	if len(names) == 0 {
		return routes
	}
	nameSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		nameSet[name] = struct{}{}
	}
	filtered := make([]types.LocalRoute, 0, len(routes))
	for _, route := range routes {
		if _, remove := nameSet[route.RouteName]; remove {
			continue
		}
		filtered = append(filtered, route)
	}
	return filtered
}

func filterRoutesByDeploymentID(cfg *types.AgentGatewayConfig, deploymentID string) {
	l := listener(cfg)
	if l == nil {
		return
	}

	filteredRoutes := make([]types.LocalRoute, 0, len(l.Routes))
	for _, route := range l.Routes {
		filteredRoute, keep := filterRouteByDeploymentID(route, deploymentID)
		if keep {
			filteredRoutes = append(filteredRoutes, filteredRoute)
		}
	}
	l.Routes = filteredRoutes
}

func listener(cfg *types.AgentGatewayConfig) *types.LocalListener {
	if cfg == nil || len(cfg.Binds) == 0 || len(cfg.Binds[0].Listeners) == 0 {
		return nil
	}
	return &cfg.Binds[0].Listeners[0]
}

func filterRouteByDeploymentID(route types.LocalRoute, deploymentID string) (types.LocalRoute, bool) {
	if route.RouteName == MCPRouteName {
		return filterMCPRouteTargets(route, deploymentID)
	}
	return route, !strings.Contains(route.RouteName, deploymentID)
}

func filterMCPRouteTargets(route types.LocalRoute, deploymentID string) (types.LocalRoute, bool) {
	if len(route.Backends) == 0 || route.Backends[0].MCP == nil {
		return route, false
	}

	filteredTargets := make([]types.MCPTarget, 0, len(route.Backends[0].MCP.Targets))
	for _, target := range route.Backends[0].MCP.Targets {
		if strings.Contains(target.Name, deploymentID) {
			continue
		}
		filteredTargets = append(filteredTargets, target)
	}
	route.Backends[0].MCP.Targets = filteredTargets
	return route, len(filteredTargets) > 0
}
