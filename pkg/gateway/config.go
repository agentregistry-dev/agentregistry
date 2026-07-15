// Package gateway defines a gateway-implementation-agnostic model for
// describing desired gateway configuration — listeners, routes, backends,
// and policies — plus the Engine contract for applying that model to a
// Target. Concrete engines (e.g. agentgateway) live in subpackages such as
// gateway/agentgateway, translate this model into their native config format
// internally, and implement Engine directly.
package gateway

// MCPRouteName is the well-known name of the single route that fans out to
// every MCP target. Render groups all desired MCP targets under a route with
// this name; concrete engines key their merge/filter logic off it so
// multiple deployments can share one route.
const MCPRouteName = "mcp_route"

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
	// ParentRefs attaches this route to parent routes or gateways (Gateway API
	// route delegation). Engines without a parent concept (e.g. the file-config
	// standalone engine) ignore it.
	ParentRefs []RouteParentRef
	// Extensions carry engine-specific route configuration the neutral model
	// does not describe (see Extension).
	Extensions []Extension
}

// RouteParentRef identifies a parent this route attaches to — e.g. a parent
// HTTPRoute (for delegation) or a Gateway. SectionName optionally selects a
// listener/section on the parent.
type RouteParentRef struct {
	Group       string
	Kind        string
	Namespace   string
	Name        string
	SectionName string
}

// Extension is the plug-in point that lets a gateway-specific engine carry
// configuration the neutral model does not describe, without expanding this
// API. Type identifies the consuming engine/feature; Spec is the opaque
// payload. Engines that recognize Type interpret Spec; all others ignore it.
// This is how new gateway implementations plug into the existing Engine
// contract instead of growing the shared Config surface.
type Extension struct {
	Type string
	Spec map[string]any
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

// MCPTargetSpec targets a pre-running MCP-over-HTTP server. Set Host to reach
// the server directly at a URL, or set Backend to route through another named
// Backend with Path appended. Host and Backend are mutually exclusive.
type MCPTargetSpec struct {
	Host string
	// Backend names a Config.Backend to route this target through instead of
	// dialing Host directly. Rendered as a backend reference.
	Backend string
	// Path is the MCP path appended when routing through Backend (e.g. "/mcp").
	Path string
}

// OpenAPITargetSpec targets an OpenAPI-described HTTP backend; Schema is
// translated into MCP tools.
type OpenAPITargetSpec struct {
	Host   string
	Port   uint32
	Schema any
}

// Backend is a named routing destination. Set exactly one shape:
//   - URL: a simple weighted HTTP host, inlined into the referencing route.
//   - MCP: a named top-level MCP backend, emitted at Config top level and
//     referenced by name from routes; its targets may themselves reference
//     another Backend by name.
//   - Extensions: engine-specific backend kinds this model does not describe
//     natively (e.g. a cloud-provider or enterprise backend). The engine passes
//     each Extension.Spec through to the native config keyed under
//     Extension.Type without interpreting it, so those concerns stay out of
//     this model (see Extension).
type Backend struct {
	Name       string
	URL        string
	MCP        *MCPBackend
	Extensions []Extension
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
	JWTAuth              *JWTAuthPolicy
	Transformation       *TransformationPolicy
	CORS                 *CORSPolicy
}

// JWTAuthPolicy authenticates incoming JWTs against one or more providers.
// Mode is the enforcement mode (e.g. "strict"). Attach it to a listener to
// gate every route beneath it.
type JWTAuthPolicy struct {
	Mode      string
	Providers []JWTProvider
}

// JWTProvider describes one JWT issuer trusted by a JWTAuthPolicy: the expected
// Issuer, the accepted Audiences, and the JWKS source used to verify
// signatures.
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
// RequestMetadata sets per-request metadata entries whose values are CEL
// expressions evaluated by the gateway (e.g. mapping a JWT claim into a
// `role` used by authorization rules).
type TransformationPolicy struct {
	RequestMetadata map[string]string
}

// A2APolicy marks a route as serving the A2A (agent-to-agent) protocol; it
// carries no configuration.
type A2APolicy struct{}

// URLRewritePolicy rewrites the request path before it reaches the backend.
// PathPrefix replaces the matched path with this prefix.
type URLRewritePolicy struct {
	PathPrefix string
}

// AuthzPolicy is a set of CEL rules; a request is allowed when any rule
// evaluates true (an empty set denies). It maps onto agentgateway's RuleSet
// wire shape (a flat list of CEL expression strings) for MCP, traffic, and
// frontend-connect authorization alike.
type AuthzPolicy struct {
	Rules []string
}

// CORSPolicy describes cross-origin resource sharing for a route. Empty slices
// render as absent. Attach it to a route to allow browser-based MCP clients.
type CORSPolicy struct {
	AllowOrigins  []string
	AllowMethods  []string
	AllowHeaders  []string
	ExposeHeaders []string
}

// FrontendConnectPolicy describes frontend CONNECT handling. Authorization is
// an optional authorization applied to CONNECT requests.
type FrontendConnectPolicy struct {
	Enabled       bool
	Authorization *AuthzPolicy
}
