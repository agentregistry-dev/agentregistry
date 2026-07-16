package agentgateway

type AgentGatewayConfig struct {
	Config   any            `json:"config" yaml:"config"`
	Binds    []LocalBind    `json:"binds,omitempty" yaml:"binds,omitempty"`
	Backends []LocalBackend `json:"backends,omitempty" yaml:"backends,omitempty"`
}

type LocalBackend struct {
	Name  string         `json:"name,omitempty" yaml:"name,omitempty"`
	MCP   *MCPBackend    `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	Extra map[string]any `json:"-" yaml:",inline"`
}

type LocalBind struct {
	Port      uint16          `json:"port" yaml:"port"`
	Listeners []LocalListener `json:"listeners" yaml:"listeners"`
}

type LocalListener struct {
	Name          string                `json:"name,omitempty" yaml:"name,omitempty"`
	GatewayName   string                `json:"gatewayName,omitempty" yaml:"gatewayName,omitempty"`
	Protocol      LocalListenerProtocol `json:"protocol" yaml:"protocol"`
	TLS           *LocalTLSServerConfig `json:"tls,omitempty" yaml:"tls,omitempty"`
	Routes        []LocalRoute          `json:"routes,omitempty" yaml:"routes,omitempty"`
	AllowedRoutes *LocalAllowedRoutes   `json:"allowedRoutes,omitempty" yaml:"allowedRoutes,omitempty"`
	Policies      *FilterOrPolicy       `json:"policies,omitempty" yaml:"policies,omitempty"`
}

type LocalListenerProtocol string

const (
	LocalListenerProtocolHTTP  LocalListenerProtocol = "HTTP"
	LocalListenerProtocolHTTPS LocalListenerProtocol = "HTTPS"
)

type LocalTLSServerConfig struct {
	Mode            string                 `json:"mode,omitempty" yaml:"mode,omitempty"`
	CertificateRefs []LocalObjectReference `json:"certificateRefs,omitempty" yaml:"certificateRefs,omitempty"`
	Options         map[string]string      `json:"options,omitempty" yaml:"options,omitempty"`
}

type LocalObjectReference struct {
	Group     string `json:"group,omitempty" yaml:"group,omitempty"`
	Kind      string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Name      string `json:"name" yaml:"name"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
}

type LocalAllowedRoutes struct {
	Namespaces *LocalAllowedRouteNamespaces `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
	Kinds      []string                     `json:"kinds,omitempty" yaml:"kinds,omitempty"`
}

type LocalAllowedRouteNamespaces struct {
	From string `json:"from,omitempty" yaml:"from,omitempty"`
}

type LocalRoute struct {
	RouteName string          `json:"name,omitempty" yaml:"name,omitempty"`
	Hostnames []string        `json:"hostnames,omitempty" yaml:"hostnames,omitempty"`
	Matches   []RouteMatch    `json:"matches,omitempty" yaml:"matches,omitempty"`
	Policies  *FilterOrPolicy `json:"policies,omitempty" yaml:"policies,omitempty"`
	Backends  []RouteBackend  `json:"backends,omitempty" yaml:"backends,omitempty"`
}

type RouteMatch struct {
	Path PathMatch `json:"path" yaml:"path"`
}

type PathMatch struct {
	PathPrefix string `json:"pathPrefix,omitempty" yaml:"pathPrefix,omitempty"`
}

type RouteBackend struct {
	Weight  int         `json:"weight" yaml:"weight"`
	MCP     *MCPBackend `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	Host    string      `json:"host,omitempty" yaml:"host,omitempty"`
	Backend string      `json:"backend,omitempty" yaml:"backend,omitempty"`
}

type MCPBackend struct {
	Targets []MCPTarget `json:"targets" yaml:"targets"`
}

type MCPTarget struct {
	Name    string             `json:"name" yaml:"name"`
	SSE     *SSETargetSpec     `json:"sse,omitempty" yaml:"sse,omitempty"`
	Stdio   *StdioTargetSpec   `json:"stdio,omitempty" yaml:"stdio,omitempty"`
	MCP     *MCPTargetSpec     `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	OpenAPI *OpenAPITargetSpec `json:"openapi,omitempty" yaml:"openapi,omitempty"`
}

type SSETargetSpec struct {
	Scheme string `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	Host   string `json:"host" yaml:"host"`
	Port   uint32 `json:"port" yaml:"port"`
	Path   string `json:"path" yaml:"path"`
}

type StdioTargetSpec struct {
	Cmd  string            `json:"cmd" yaml:"cmd"`
	Args []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env  map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

type MCPTargetSpec struct {
	Host    string `json:"host,omitempty" yaml:"host,omitempty"`
	Backend string `json:"backend,omitempty" yaml:"backend,omitempty"`
	Path    string `json:"path,omitempty" yaml:"path,omitempty"`
}

type OpenAPITargetSpec struct {
	Host   string `json:"host" yaml:"host"`
	Port   uint32 `json:"port" yaml:"port"`
	Schema any    `json:"schema" yaml:"schema"`
}

type FilterOrPolicy struct {
	URLRewrite           *URLRewrite           `json:"urlRewrite,omitempty" yaml:"urlRewrite,omitempty"`
	CORS                 *CORS                 `json:"cors,omitempty" yaml:"cors,omitempty"`
	MCPAuthorization     *MCPAuthorization     `json:"mcpAuthorization,omitempty" yaml:"mcpAuthorization,omitempty"`
	A2A                  *A2APolicy            `json:"a2a,omitempty" yaml:"a2a,omitempty"`
	JWTAuth              *ListenerJWTAuth      `json:"jwtAuth,omitempty" yaml:"jwtAuth,omitempty"`
	Transformations      *TransformationPolicy `json:"transformations,omitempty" yaml:"transformations,omitempty"`
	TrafficAuthorization *TrafficAuthorization `json:"trafficAuthorization,omitempty" yaml:"trafficAuthorization,omitempty"`
	FrontendConnect      *FrontendConnect      `json:"frontendConnect,omitempty" yaml:"frontendConnect,omitempty"`
}

type URLRewrite struct {
	Path *PathRedirect `json:"path,omitempty" yaml:"path,omitempty"`
}

type PathRedirect struct {
	Prefix string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
}

type CORS struct {
	AllowOrigins  []string `json:"allowOrigins,omitempty" yaml:"allowOrigins,omitempty"`
	AllowMethods  []string `json:"allowMethods,omitempty" yaml:"allowMethods,omitempty"`
	AllowHeaders  []string `json:"allowHeaders,omitempty" yaml:"allowHeaders,omitempty"`
	ExposeHeaders []string `json:"exposeHeaders,omitempty" yaml:"exposeHeaders,omitempty"`
}

type MCPAuthorization struct {
	Rules any `json:"rules" yaml:"rules"`
}

type TrafficAuthorization struct {
	Rules any `json:"rules" yaml:"rules"`
}

type FrontendConnect struct {
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Rules   any  `json:"rules,omitempty" yaml:"rules,omitempty"`
}

type A2APolicy struct{}

type ListenerJWTAuth struct {
	Mode      string        `json:"mode,omitempty" yaml:"mode,omitempty"`
	Providers []JWTProvider `json:"providers,omitempty" yaml:"providers,omitempty"`
}

type JWTProvider struct {
	Issuer    string     `json:"issuer" yaml:"issuer"`
	Audiences []string   `json:"audiences,omitempty" yaml:"audiences,omitempty"`
	JWKS      JWKSSource `json:"jwks" yaml:"jwks"`
}

type JWKSSource struct {
	URL  string `json:"url,omitempty" yaml:"url,omitempty"`
	File string `json:"file,omitempty" yaml:"file,omitempty"`
}

type TransformationPolicy struct {
	Request *TransformStage `json:"request,omitempty" yaml:"request,omitempty"`
}

type TransformStage struct {
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}
