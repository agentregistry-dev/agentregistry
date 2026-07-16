// Package gateway defines the engine contract and desired config model.
package gateway

// MCPRouteName is the well-known route name for MCP target fanout.
const MCPRouteName = "mcp_route"

// Config is the desired gateway configuration.
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

// TLSConfig describes listener TLS termination.
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

// AllowedRouteNamespaces describes namespace route attachment.
type AllowedRouteNamespaces struct {
	From string
}

// ObjectRef references another object.
type ObjectRef struct {
	Group     string
	Kind      string
	Name      string
	Namespace string
}

// Route describes a routing rule. BackendRef.Name resolves against
// Config.Backends. MCP takes precedence over BackendRefs when both are set.
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

// RouteParentRef identifies a route parent.
type RouteParentRef struct {
	Group       string
	Kind        string
	Namespace   string
	Name        string
	SectionName string
}

// MCPBackend fans a route out to MCP targets.
type MCPBackend struct {
	Targets []MCPTarget
}

// MCPTarget is one named upstream inside an MCPBackend.
type MCPTarget struct {
	Name    string
	SSE     *SSETargetSpec
	Stdio   *StdioTargetSpec
	MCP     *MCPTargetSpec
	OpenAPI *OpenAPITargetSpec
}

// SSETargetSpec targets an SSE MCP server.
type SSETargetSpec struct {
	Scheme string
	Host   string
	Port   uint32
	Path   string
}

// StdioTargetSpec targets a stdio MCP server.
type StdioTargetSpec struct {
	Cmd  string
	Args []string
	Env  map[string]string
}

// MCPTargetSpec targets a pre-running MCP-over-HTTP server.
type MCPTargetSpec struct {
	Host    string
	Backend string
	Path    string
}

// OpenAPITargetSpec targets an OpenAPI-described backend.
type OpenAPITargetSpec struct {
	Host   string
	Port   uint32
	Schema any
}

// Backend is a named routing destination.
type Backend struct {
	Name       string
	URL        string
	MCP        *MCPBackend
	Extensions map[string]any
}

// BackendRef references a named Backend.
type BackendRef struct {
	Name   string
	Weight int
}

// PolicySpec carries typed policy payloads directly on listeners and routes.
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

// JWTAuthPolicy authenticates incoming JWTs.
type JWTAuthPolicy struct {
	Mode      string
	Providers []JWTProvider
}

// JWTProvider describes one trusted JWT issuer.
type JWTProvider struct {
	Issuer    string
	Audiences []string
	JWKS      JWKSSource
}

// JWKSSource locates a JSON Web Key Set.
type JWKSSource struct {
	URL  string
	File string
}

// TransformationPolicy rewrites requests.
type TransformationPolicy struct {
	RequestMetadata map[string]string
}

// A2APolicy marks an A2A route.
type A2APolicy struct{}

// URLRewritePolicy rewrites request paths.
type URLRewritePolicy struct {
	PathPrefix string
}

// AuthzPolicy is a set of CEL allow rules.
type AuthzPolicy struct {
	Rules []string
}

// CORSPolicy describes cross-origin resource sharing.
type CORSPolicy struct {
	AllowOrigins  []string
	AllowMethods  []string
	AllowHeaders  []string
	ExposeHeaders []string
}

// FrontendConnectPolicy enables frontend CONNECT handling.
type FrontendConnectPolicy struct {
	Enabled       bool
	Authorization *AuthzPolicy
}
