// Package gateway defines a gateway-implementation-agnostic desired
// configuration model plus the Engine contract for applying that model to a
// Target. Concrete engines translate this small DTO into their native format.
package gateway

// MCPRouteName is the well-known name of the single route that fans out to
// every MCP target.
const MCPRouteName = "mcp_route"

// Config is the desired gateway configuration model.
type Config struct {
	ClassName string
	Listeners []Listener
	Routes    []Route
	Backends  []Backend
}

// Listener describes a gateway listener.
type Listener struct {
	Name          string
	Protocol      string
	Port          int
	TLS           *TLSConfig
	AllowedRoutes *AllowedRoutes
	Policies      PolicySpec
}

// TLSConfig describes TLS termination for a listener.
type TLSConfig struct {
	Mode            string
	CertificateRefs []ObjectRef
	Options         map[string]string
}

// AllowedRoutes describes which routes a listener accepts.
type AllowedRoutes struct {
	Namespaces *AllowedRouteNamespaces
	Kinds      []string
}

// AllowedRouteNamespaces describes namespace selection for listener route
// attachment. From matches Gateway API values such as "Same" or "All".
type AllowedRouteNamespaces struct {
	From string
}

// ObjectRef references another object, e.g. a TLS certificate.
type ObjectRef struct {
	Group     string
	Kind      string
	Name      string
	Namespace string
}

// Route describes a routing rule. BackendRef.Name resolves against
// Config.Backends. Exactly one of BackendRefs or MCP should be set; if both are
// set, MCP takes precedence during render.
type Route struct {
	Name        string
	Hostnames   []string
	PathPrefix  string
	BackendRefs []BackendRef
	MCP         *MCPBackend
	Policies    PolicySpec
	ParentRefs  []RouteParentRef
	Extensions  map[string]any
}

// RouteParentRef identifies a parent this route attaches to, e.g. a parent
// HTTPRoute for delegation or a Gateway. SectionName optionally selects a
// listener/section on the parent.
type RouteParentRef struct {
	Group       string
	Kind        string
	Namespace   string
	Name        string
	SectionName string
}

// MCPBackend fans a route out to multiple named MCP targets instead of a single
// weighted backend. Mutually exclusive with Route.BackendRefs.
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

// MCPTargetSpec targets a pre-running MCP-over-HTTP server. Set Host to reach
// the server directly at a URL, or set Backend to route through another named
// Backend with Path appended. Host and Backend are mutually exclusive.
type MCPTargetSpec struct {
	Host    string
	Backend string
	Path    string
}

// OpenAPITargetSpec targets an OpenAPI-described HTTP backend; Schema is
// translated into MCP tools.
type OpenAPITargetSpec struct {
	Host   string
	Port   uint32
	Schema any
}

// Backend is a named routing destination. Set exactly one shape:
// URL, MCP, or Extensions.
type Backend struct {
	Name       string
	URL        string
	MCP        *MCPBackend
	Extensions map[string]any
}

// BackendRef references a Backend by name with an optional weight. Weight
// defaults to 100 when zero during render.
type BackendRef struct {
	Name   string
	Weight int
}

// PolicySpec carries typed policy payloads directly on listeners and routes.
// Each field is optional and nil when the policy does not include that concern.
type PolicySpec struct {
	MCPAuthorization     *AuthzPolicy
	TrafficAuthorization *AuthzPolicy
	FrontendConnect      *FrontendConnectPolicy
	A2A                  *A2APolicy
	URLRewrite           *URLRewritePolicy
	JWTAuth              *JWTAuthPolicy
	Transformation       *TransformationPolicy
	CORS                 *CORSPolicy
}

// JWTAuthPolicy authenticates incoming JWTs against one or more providers.
type JWTAuthPolicy struct {
	Mode      string
	Providers []JWTProvider
}

// JWTProvider describes one JWT issuer trusted by a JWTAuthPolicy.
type JWTProvider struct {
	Issuer    string
	Audiences []string
	JWKS      JWKSSource
}

// JWKSSource locates the JSON Web Key Set used to verify JWT signatures.
// Exactly one of URL or File should be set.
type JWKSSource struct {
	URL  string
	File string
}

// TransformationPolicy rewrites requests before they reach the backend.
type TransformationPolicy struct {
	RequestMetadata map[string]string
}

// A2APolicy marks a route as serving the A2A protocol.
type A2APolicy struct{}

// URLRewritePolicy rewrites the request path before it reaches the backend.
type URLRewritePolicy struct {
	PathPrefix string
}

// AuthzPolicy is a set of CEL rules; a request is allowed when any rule
// evaluates true (an empty set denies).
type AuthzPolicy struct {
	Rules []string
}

// CORSPolicy describes cross-origin resource sharing for a route.
type CORSPolicy struct {
	AllowOrigins  []string
	AllowMethods  []string
	AllowHeaders  []string
	ExposeHeaders []string
}

// FrontendConnectPolicy describes frontend CONNECT handling.
type FrontendConnectPolicy struct {
	Enabled       bool
	Authorization *AuthzPolicy
}
