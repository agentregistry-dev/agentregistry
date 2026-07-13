// Package gateway defines a gateway-implementation-agnostic model for
// describing desired gateway configuration — listeners, routes, backends,
// and policies. Provider-specific rendering and lifecycle management (e.g.
// for agentgateway) live in subpackages such as gateway/agentgateway.
package gateway

// Config is the desired, gateway-agnostic configuration model. Routes
// reference Backends by name (BackendRef.Name) and reference Policies by name
// (PolicyRef.Name); Policies are declared once at the top level and attached
// to listeners and routes by reference.
type Config struct {
	ClassName string
	Listeners []Listener
	Routes    []Route
	Backends  []Backend
	Policies  []Policy
}

// Listener describes a gateway listener. Protocol is a plain string mapped to
// an implementation-native protocol during render.
type Listener struct {
	Name          string
	Protocol      string
	Port          int
	TLS           *TLSConfig
	AllowedRoutes *AllowedRoutes
	Policies      []PolicyRef
}

// TLSConfig describes TLS termination for a listener.
type TLSConfig struct {
	Mode            string
	CertificateRefs []ObjectRef
	Options         map[string]string
}

// AllowedRoutes is a simplified selector describing which routes a listener
// accepts, by namespace and kind.
type AllowedRoutes struct {
	Namespaces []string
	Kinds      []string
}

// PolicyRef attaches a top-level Policy to a listener or route by name.
type PolicyRef struct {
	Name string
}

// ObjectRef references another object, e.g. a TLS certificate.
type ObjectRef struct {
	Group     string
	Kind      string
	Name      string
	Namespace string
}

// Route describes a routing rule. BackendRef.Name resolves against
// Config.Backends to obtain the backend URL. Exactly one of BackendRefs or
// MCP should be set; if both are set, MCP takes precedence during render.
type Route struct {
	Name        string
	Hostnames   []string
	PathPrefix  string
	BackendRefs []BackendRef
	MCP         *MCPBackend
	Policies    []PolicyRef
}

// MCPBackend fans a route out to multiple named MCP targets instead of a
// single weighted backend. Mutually exclusive with Route.BackendRefs.
type MCPBackend struct {
	Targets []MCPTarget
}

// MCPTarget is one named upstream inside an MCPBackend. Exactly one of SSE,
// Stdio, MCP, or OpenAPI should be set, selecting the target's transport.
type MCPTarget struct {
	Name    string
	SSE     *SSETargetSpec
	Stdio   *StdioTargetSpec
	MCP     *MCPTargetSpec
	OpenAPI *OpenAPITargetSpec
}

// SSETargetSpec targets an MCP server speaking SSE at host:port/path.
type SSETargetSpec struct {
	Scheme string
	Host   string
	Port   uint32
	Path   string
}

// StdioTargetSpec runs an MCP server as a local subprocess speaking stdio.
type StdioTargetSpec struct {
	Cmd  string
	Args []string
	Env  map[string]string
}

// MCPTargetSpec targets a pre-running MCP-over-HTTP server at Host.
type MCPTargetSpec struct {
	Host string
}

// OpenAPITargetSpec targets an OpenAPI-described HTTP backend; Schema is
// translated into MCP tools.
type OpenAPITargetSpec struct {
	Host   string
	Port   uint32
	Schema any
}

// Backend is a named routing destination.
type Backend struct {
	Name string
	URL  string
}

// BackendRef references a Backend by name with an optional weight. Weight
// defaults to 100 when zero during render.
type BackendRef struct {
	Name   string
	Weight int
}

// Policy is a named, typed policy declared once and attached by reference.
type Policy struct {
	Name string
	Type string
	Spec PolicySpec
}

// PolicySpec carries the typed policy payloads. Each field is optional and
// nil when the policy does not include that concern.
type PolicySpec struct {
	MCPAuthorization     *AuthzPolicy
	TrafficAuthorization *AuthzPolicy
	FrontendConnect      *FrontendConnectPolicy
	A2A                  *A2APolicy
	URLRewrite           *URLRewritePolicy
}

// A2APolicy marks a route as serving the A2A (agent-to-agent) protocol; it
// carries no configuration.
type A2APolicy struct{}

// URLRewritePolicy rewrites the request path before it reaches the backend.
// PathPrefix replaces the matched path with this prefix.
type URLRewritePolicy struct {
	PathPrefix string
}

// AuthzPolicy describes an authorization decision. MatchExpressions carries
// CEL expressions evaluated to select matching requests.
type AuthzPolicy struct {
	Action           string
	MatchExpressions []string
}

// FrontendConnectPolicy describes frontend CONNECT handling. Authorization is
// an optional authorization applied to CONNECT requests.
type FrontendConnectPolicy struct {
	Enabled       bool
	Authorization *AuthzPolicy
}
