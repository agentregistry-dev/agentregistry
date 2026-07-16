package agentgateway

type agentGatewayConfig struct {
	Config   any            `json:"config" yaml:"config"`
	Binds    []localBind    `json:"binds,omitempty" yaml:"binds,omitempty"`
	Backends []localBackend `json:"backends,omitempty" yaml:"backends,omitempty"`
}

type localBackend struct {
	Name  string         `json:"name,omitempty" yaml:"name,omitempty"`
	MCP   *mcpBackend    `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	Extra map[string]any `json:"-" yaml:",inline"`
}

type localBind struct {
	Port      uint16          `json:"port" yaml:"port"`
	Listeners []localListener `json:"listeners" yaml:"listeners"`
}

type localListener struct {
	Name          string                `json:"name,omitempty" yaml:"name,omitempty"`
	GatewayName   string                `json:"gatewayName,omitempty" yaml:"gatewayName,omitempty"`
	Protocol      localListenerProtocol `json:"protocol" yaml:"protocol"`
	TLS           *localTLSServerConfig `json:"tls,omitempty" yaml:"tls,omitempty"`
	Routes        []localRoute          `json:"routes,omitempty" yaml:"routes,omitempty"`
	AllowedRoutes *localAllowedRoutes   `json:"allowedRoutes,omitempty" yaml:"allowedRoutes,omitempty"`
	Policies      *filterOrPolicy       `json:"policies,omitempty" yaml:"policies,omitempty"`
}

type localListenerProtocol string

const (
	localListenerProtocolHTTP  localListenerProtocol = "HTTP"
	localListenerProtocolHTTPS localListenerProtocol = "HTTPS"
)

type localTLSServerConfig struct {
	Mode            string                 `json:"mode,omitempty" yaml:"mode,omitempty"`
	CertificateRefs []localObjectReference `json:"certificateRefs,omitempty" yaml:"certificateRefs,omitempty"`
	Options         map[string]string      `json:"options,omitempty" yaml:"options,omitempty"`
}

type localObjectReference struct {
	Group     string `json:"group,omitempty" yaml:"group,omitempty"`
	Kind      string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Name      string `json:"name" yaml:"name"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
}

type localAllowedRoutes struct {
	Namespaces *localAllowedRouteNamespaces `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
	Kinds      []string                     `json:"kinds,omitempty" yaml:"kinds,omitempty"`
}

type localAllowedRouteNamespaces struct {
	From string `json:"from,omitempty" yaml:"from,omitempty"`
}

type localRoute struct {
	RouteName string          `json:"name,omitempty" yaml:"name,omitempty"`
	Hostnames []string        `json:"hostnames,omitempty" yaml:"hostnames,omitempty"`
	Matches   []routeMatch    `json:"matches,omitempty" yaml:"matches,omitempty"`
	Policies  *filterOrPolicy `json:"policies,omitempty" yaml:"policies,omitempty"`
	Backends  []routeBackend  `json:"backends,omitempty" yaml:"backends,omitempty"`
}

type routeMatch struct {
	Path pathMatch `json:"path" yaml:"path"`
}

type pathMatch struct {
	PathPrefix string `json:"pathPrefix,omitempty" yaml:"pathPrefix,omitempty"`
}

type routeBackend struct {
	Weight  int         `json:"weight" yaml:"weight"`
	MCP     *mcpBackend `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	Host    string      `json:"host,omitempty" yaml:"host,omitempty"`
	Backend string      `json:"backend,omitempty" yaml:"backend,omitempty"`
}

type mcpBackend struct {
	Targets []mcpTarget `json:"targets" yaml:"targets"`
}

type mcpTarget struct {
	Name    string             `json:"name" yaml:"name"`
	SSE     *sseTargetSpec     `json:"sse,omitempty" yaml:"sse,omitempty"`
	Stdio   *stdioTargetSpec   `json:"stdio,omitempty" yaml:"stdio,omitempty"`
	MCP     *mcpTargetSpec     `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	OpenAPI *openAPITargetSpec `json:"openapi,omitempty" yaml:"openapi,omitempty"`
}

type sseTargetSpec struct {
	Scheme string `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	Host   string `json:"host" yaml:"host"`
	Port   uint32 `json:"port" yaml:"port"`
	Path   string `json:"path" yaml:"path"`
}

type stdioTargetSpec struct {
	Cmd  string            `json:"cmd" yaml:"cmd"`
	Args []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env  map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

type mcpTargetSpec struct {
	Host    string `json:"host,omitempty" yaml:"host,omitempty"`
	Backend string `json:"backend,omitempty" yaml:"backend,omitempty"`
	Path    string `json:"path,omitempty" yaml:"path,omitempty"`
}

type openAPITargetSpec struct {
	Host   string `json:"host" yaml:"host"`
	Port   uint32 `json:"port" yaml:"port"`
	Schema any    `json:"schema" yaml:"schema"`
}

type filterOrPolicy struct {
	URLRewrite           *urlRewrite           `json:"urlRewrite,omitempty" yaml:"urlRewrite,omitempty"`
	CORS                 *cors                 `json:"cors,omitempty" yaml:"cors,omitempty"`
	MCPAuthorization     *mcpAuthorization     `json:"mcpAuthorization,omitempty" yaml:"mcpAuthorization,omitempty"`
	A2A                  *a2aPolicy            `json:"a2a,omitempty" yaml:"a2a,omitempty"`
	JWTAuth              *listenerJWTAuth      `json:"jwtAuth,omitempty" yaml:"jwtAuth,omitempty"`
	Transformations      *transformationPolicy `json:"transformations,omitempty" yaml:"transformations,omitempty"`
	TrafficAuthorization *trafficAuthorization `json:"trafficAuthorization,omitempty" yaml:"trafficAuthorization,omitempty"`
	FrontendConnect      *frontendConnect      `json:"frontendConnect,omitempty" yaml:"frontendConnect,omitempty"`
}

type urlRewrite struct {
	Path *pathRedirect `json:"path,omitempty" yaml:"path,omitempty"`
}

type pathRedirect struct {
	Prefix string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
}

type cors struct {
	AllowOrigins  []string `json:"allowOrigins,omitempty" yaml:"allowOrigins,omitempty"`
	AllowMethods  []string `json:"allowMethods,omitempty" yaml:"allowMethods,omitempty"`
	AllowHeaders  []string `json:"allowHeaders,omitempty" yaml:"allowHeaders,omitempty"`
	ExposeHeaders []string `json:"exposeHeaders,omitempty" yaml:"exposeHeaders,omitempty"`
}

type mcpAuthorization struct {
	Rules any `json:"rules" yaml:"rules"`
}

type trafficAuthorization struct {
	Rules any `json:"rules" yaml:"rules"`
}

type frontendConnect struct {
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Rules   any  `json:"rules,omitempty" yaml:"rules,omitempty"`
}

type a2aPolicy struct{}

type listenerJWTAuth struct {
	Mode      string        `json:"mode,omitempty" yaml:"mode,omitempty"`
	Providers []jwtProvider `json:"providers,omitempty" yaml:"providers,omitempty"`
}

type jwtProvider struct {
	Issuer    string     `json:"issuer" yaml:"issuer"`
	Audiences []string   `json:"audiences,omitempty" yaml:"audiences,omitempty"`
	JWKS      jwksSource `json:"jwks" yaml:"jwks"`
}

type jwksSource struct {
	URL  string `json:"url,omitempty" yaml:"url,omitempty"`
	File string `json:"file,omitempty" yaml:"file,omitempty"`
}

type transformationPolicy struct {
	Request *transformStage `json:"request,omitempty" yaml:"request,omitempty"`
}

type transformStage struct {
	Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}
