package types

import (
	"net"
	"time"
)

type AgentGatewayConfig struct {
	Config    any             `json:"config" yaml:"config"`
	Binds     []LocalBind     `json:"binds,omitempty" yaml:"binds,omitempty"`
	Workloads []LocalWorkload `json:"workloads,omitempty" yaml:"workloads,omitempty"`
	Services  []Service       `json:"services,omitempty" yaml:"services,omitempty"`
}

type LocalBind struct {
	Port      uint16          `json:"port" yaml:"port"`
	Listeners []LocalListener `json:"listeners" yaml:"listeners"`
}

type LocalListener struct {
	Name        string                `json:"name,omitempty" yaml:"name,omitempty"`
	GatewayName string                `json:"gatewayName,omitempty" yaml:"gatewayName,omitempty"`
	Hostname    string                `json:"hostname,omitempty" yaml:"hostname,omitempty"`
	Protocol    LocalListenerProtocol `json:"protocol" yaml:"protocol"`
	TLS         *LocalTLSServerConfig `json:"tls,omitempty" yaml:"tls,omitempty"`
	Routes      []LocalRoute          `json:"routes,omitempty" yaml:"routes,omitempty"`
	TCPRoutes   []LocalTCPRoute       `json:"tcpRoutes,omitempty" yaml:"tcpRoutes,omitempty"`
}

type LocalListenerProtocol string

const (
	LocalListenerProtocolHTTP  LocalListenerProtocol = "HTTP"
	LocalListenerProtocolHTTPS LocalListenerProtocol = "HTTPS"
	LocalListenerProtocolTLS   LocalListenerProtocol = "TLS"
	LocalListenerProtocolTCP   LocalListenerProtocol = "TCP"
	LocalListenerProtocolHBONE LocalListenerProtocol = "HBONE"
)

type LocalTLSServerConfig struct {
	Cert string `json:"cert" yaml:"cert"`
	Key  string `json:"key" yaml:"key"`
}

type LocalRoute struct {
	RouteName string          `json:"name,omitempty" yaml:"name,omitempty"`
	RuleName  string          `json:"ruleName,omitempty" yaml:"ruleName,omitempty"`
	Hostnames []string        `json:"hostnames,omitempty" yaml:"hostnames,omitempty"`
	Matches   []RouteMatch    `json:"matches,omitempty" yaml:"matches,omitempty"`
	Policies  *FilterOrPolicy `json:"policies,omitempty" yaml:"policies,omitempty"`
	Backends  []RouteBackend  `json:"backends,omitempty" yaml:"backends,omitempty"`
}

type LocalTCPRoute struct {
	RouteName string             `json:"name,omitempty" yaml:"name,omitempty"`
	RuleName  string             `json:"ruleName,omitempty" yaml:"ruleName,omitempty"`
	Hostnames []string           `json:"hostnames,omitempty" yaml:"hostnames,omitempty"`
	Policies  *TCPFilterOrPolicy `json:"policies,omitempty" yaml:"policies,omitempty"`
	Backends  []TCPRouteBackend  `json:"backends,omitempty" yaml:"backends,omitempty"`
}

type LocalWorkload struct {
	Workload Workload                     `json:",inline" yaml:",inline"`
	Services map[string]map[uint16]uint16 `json:"services,omitempty" yaml:"services,omitempty"`
}

type FilterOrPolicy struct {
	RequestHeaderModifier  *HeaderModifier   `json:"requestHeaderModifier,omitempty" yaml:"requestHeaderModifier,omitempty"`
	ResponseHeaderModifier *HeaderModifier   `json:"responseHeaderModifier,omitempty" yaml:"responseHeaderModifier,omitempty"`
	RequestRedirect        *RequestRedirect  `json:"requestRedirect,omitempty" yaml:"requestRedirect,omitempty"`
	URLRewrite             *URLRewrite       `json:"urlRewrite,omitempty" yaml:"urlRewrite,omitempty"`
	RequestMirror          *RequestMirror    `json:"requestMirror,omitempty" yaml:"requestMirror,omitempty"`
	DirectResponse         *DirectResponse   `json:"directResponse,omitempty" yaml:"directResponse,omitempty"`
	CORS                   *CORS             `json:"cors,omitempty" yaml:"cors,omitempty"`
	MCPAuthorization       *MCPAuthorization `json:"mcpAuthorization,omitempty" yaml:"mcpAuthorization,omitempty"`
	A2A                    *A2APolicy        `json:"a2a,omitempty" yaml:"a2a,omitempty"`
	AI                     any               `json:"ai,omitempty" yaml:"ai,omitempty"`
	BackendTLS             *BackendTLS       `json:"backendTLS,omitempty" yaml:"backendTLS,omitempty"`
	BackendAuth            *BackendAuth      `json:"backendAuth,omitempty" yaml:"backendAuth,omitempty"`
	LocalRateLimit         []any             `json:"localRateLimit,omitempty" yaml:"localRateLimit,omitempty"`
	RemoteRateLimit        any               `json:"remoteRateLimit,omitempty" yaml:"remoteRateLimit,omitempty"`
	JWTAuth                any               `json:"jwtAuth,omitempty" yaml:"jwtAuth,omitempty"`
	ExtAuthz               any               `json:"extAuthz,omitempty" yaml:"extAuthz,omitempty"`
	Timeout                *TimeoutPolicy    `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retry                  *RetryPolicy      `json:"retry,omitempty" yaml:"retry,omitempty"`
}

type TCPFilterOrPolicy struct {
	BackendTLS *BackendTLS `json:"backendTLS,omitempty" yaml:"backendTLS,omitempty"`
}

type RouteMatch struct {
	Headers []HeaderMatch `json:"headers,omitempty" yaml:"headers,omitempty"`
	Path    PathMatch     `json:"path" yaml:"path"`
	Method  *MethodMatch  `json:"method,omitempty" yaml:"method,omitempty"`
	Query   []QueryMatch  `json:"query,omitempty" yaml:"query,omitempty"`
}

type HeaderMatch struct {
	Name  string           `json:"name" yaml:"name"`
	Value HeaderValueMatch `json:"value" yaml:"value"`
}

type HeaderValueMatch struct {
	Exact string `json:"exact,omitempty" yaml:"exact,omitempty"`
	Regex string `json:"regex,omitempty" yaml:"regex,omitempty"`
}

type PathMatch struct {
	Exact      string `json:"exact,omitempty" yaml:"exact,omitempty"`
	PathPrefix string `json:"pathPrefix,omitempty" yaml:"pathPrefix,omitempty"`
	Regex      *struct {
		Pattern string `json:"pattern" yaml:"pattern"`
		Length  int    `json:"length" yaml:"length"`
	} `json:"regex,omitempty" yaml:"regex,omitempty"`
}

type MethodMatch struct {
	Method string `json:"method" yaml:"method"`
}

type QueryMatch struct {
	Name  string          `json:"name" yaml:"name"`
	Value QueryValueMatch `json:"value" yaml:"value"`
}

type QueryValueMatch struct {
	Exact string `json:"exact,omitempty" yaml:"exact,omitempty"`
	Regex string `json:"regex,omitempty" yaml:"regex,omitempty"`
}

type RouteBackend struct {
	Weight  int             `json:"weight" yaml:"weight"`
	Service *ServiceBackend `json:"service,omitempty" yaml:"service,omitempty"`
	Opaque  *Target         `json:"opaque,omitempty" yaml:"opaque,omitempty"`
	Dynamic *struct{}       `json:"dynamic,omitempty" yaml:"dynamic,omitempty"`
	MCP     *MCPBackend     `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	AI      *AIBackend      `json:"ai,omitempty" yaml:"ai,omitempty"`
	Invalid bool            `json:"invalid,omitempty" yaml:"invalid,omitempty"`
	Filters []RouteFilter   `json:"filters,omitempty" yaml:"filters,omitempty"`
	Host    string          `json:"host,omitempty" yaml:"host,omitempty"`
}

type TCPRouteBackend struct {
	Weight  int           `json:"weight" yaml:"weight"`
	Backend SimpleBackend `json:"backend" yaml:"backend"`
}

type SimpleBackend struct {
	Service *ServiceBackend `json:"service,omitempty" yaml:"service,omitempty"`
	Opaque  *Target         `json:"opaque,omitempty" yaml:"opaque,omitempty"`
	Invalid bool            `json:"invalid,omitempty" yaml:"invalid,omitempty"`
}

type ServiceBackend struct {
	Name NamespacedHostname `json:"name" yaml:"name"`
	Port uint16             `json:"port" yaml:"port"`
}

type Target struct {
	Address  *net.TCPAddr `json:"address,omitempty" yaml:"address,omitempty"`
	Hostname *struct {
		Host string `json:"host" yaml:"host"`
		Port uint16 `json:"port" yaml:"port"`
	} `json:"hostname,omitempty" yaml:"hostname,omitempty"`
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
	Filters []any              `json:"filters,omitempty" yaml:"filters,omitempty"`
}

type SSETargetSpec struct {
	Scheme string `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	Host   string `json:"host" yaml:"host"`
	Port   uint32 `json:"port" yaml:"port"`
	Path   string `json:"path" yaml:"path"`
}

type MCPTargetSpec struct {
	Host string `json:"host" yaml:"host"`
}

type StdioTargetSpec struct {
	Cmd  string            `json:"cmd" yaml:"cmd"`
	Args []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env  map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

type OpenAPITargetSpec struct {
	Host   string `json:"host" yaml:"host"`
	Port   uint32 `json:"port" yaml:"port"`
	Schema any    `json:"schema" yaml:"schema"`
}

type AIBackend struct {
	Name string `json:"name" yaml:"name"`
}

type RouteFilter struct {
	RequestHeaderModifier  *HeaderModifier  `json:"requestHeaderModifier,omitempty" yaml:"requestHeaderModifier,omitempty"`
	ResponseHeaderModifier *HeaderModifier  `json:"responseHeaderModifier,omitempty" yaml:"responseHeaderModifier,omitempty"`
	RequestRedirect        *RequestRedirect `json:"requestRedirect,omitempty" yaml:"requestRedirect,omitempty"`
	URLRewrite             *URLRewrite      `json:"urlRewrite,omitempty" yaml:"urlRewrite,omitempty"`
	RequestMirror          *RequestMirror   `json:"requestMirror,omitempty" yaml:"requestMirror,omitempty"`
	DirectResponse         *DirectResponse  `json:"directResponse,omitempty" yaml:"directResponse,omitempty"`
	CORS                   *CORS            `json:"cors,omitempty" yaml:"cors,omitempty"`
}

type HeaderModifier struct {
	Add    map[string]string `json:"add,omitempty" yaml:"add,omitempty"`
	Set    map[string]string `json:"set,omitempty" yaml:"set,omitempty"`
	Remove []string          `json:"remove,omitempty" yaml:"remove,omitempty"`
}

type RequestRedirect struct {
	Scheme    string        `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	Authority *HostRedirect `json:"authority,omitempty" yaml:"authority,omitempty"`
	Path      *PathRedirect `json:"path,omitempty" yaml:"path,omitempty"`
	Status    *int          `json:"status,omitempty" yaml:"status,omitempty"`
}

type URLRewrite struct {
	Authority *HostRedirect `json:"authority,omitempty" yaml:"authority,omitempty"`
	Path      *PathRedirect `json:"path,omitempty" yaml:"path,omitempty"`
}

type RequestMirror struct {
	Backend    SimpleBackend `json:"backend" yaml:"backend"`
	Percentage float64       `json:"percentage" yaml:"percentage"`
}

type DirectResponse struct {
	Status  int               `json:"status" yaml:"status"`
	Body    string            `json:"body,omitempty" yaml:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

type CORS struct {
	AllowOrigins     []string `json:"allowOrigins,omitempty" yaml:"allowOrigins,omitempty"`
	AllowMethods     []string `json:"allowMethods,omitempty" yaml:"allowMethods,omitempty"`
	AllowHeaders     []string `json:"allowHeaders,omitempty" yaml:"allowHeaders,omitempty"`
	ExposeHeaders    []string `json:"exposeHeaders,omitempty" yaml:"exposeHeaders,omitempty"`
	MaxAge           *int     `json:"maxAge,omitempty" yaml:"maxAge,omitempty"`
	AllowCredentials bool     `json:"allowCredentials,omitempty" yaml:"allowCredentials,omitempty"`
}

type HostRedirect struct {
	Full string  `json:"full,omitempty" yaml:"full,omitempty"`
	Host string  `json:"host,omitempty" yaml:"host,omitempty"`
	Port *uint16 `json:"port,omitempty" yaml:"port,omitempty"`
}

type PathRedirect struct {
	Full   string `json:"full,omitempty" yaml:"full,omitempty"`
	Prefix string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
}

type MCPAuthorization struct {
	Rules any `json:"rules" yaml:"rules"`
}

type A2APolicy struct{}

type BackendTLS struct {
	Insecure     bool   `json:"insecure,omitempty" yaml:"insecure,omitempty"`
	InsecureHost bool   `json:"insecureHost,omitempty" yaml:"insecureHost,omitempty"`
	Cert         string `json:"cert,omitempty" yaml:"cert,omitempty"`
	Key          string `json:"key,omitempty" yaml:"key,omitempty"`
	Root         string `json:"root,omitempty" yaml:"root,omitempty"`
}

type BackendAuth struct {
	Type   string `json:"type" yaml:"type"`
	Config any    `json:"config,omitempty" yaml:"config,omitempty"`
}

type TimeoutPolicy struct {
	RequestTimeout        *time.Duration `json:"requestTimeout,omitempty" yaml:"requestTimeout,omitempty"`
	BackendRequestTimeout *time.Duration `json:"backendRequestTimeout,omitempty" yaml:"backendRequestTimeout,omitempty"`
}

type RetryPolicy struct {
	Attempts      int           `json:"attempts" yaml:"attempts"`
	PerTryTimeout time.Duration `json:"perTryTimeout" yaml:"perTryTimeout"`
	RetryOn       []string      `json:"retryOn,omitempty" yaml:"retryOn,omitempty"`
}

type Workload struct {
	WorkloadIPs    []net.IP             `json:"workloadIps" yaml:"workloadIps"`
	Waypoint       *GatewayAddress      `json:"waypoint,omitempty" yaml:"waypoint,omitempty"`
	NetworkGateway *GatewayAddress      `json:"networkGateway,omitempty" yaml:"networkGateway,omitempty"`
	Protocol       InboundProtocol      `json:"protocol" yaml:"protocol"`
	NetworkMode    NetworkMode          `json:"networkMode" yaml:"networkMode"`
	UID            string               `json:"uid,omitempty" yaml:"uid,omitempty"`
	Name           string               `json:"name" yaml:"name"`
	Namespace      string               `json:"namespace" yaml:"namespace"`
	TrustDomain    string               `json:"trustDomain,omitempty" yaml:"trustDomain,omitempty"`
	ServiceAccount string               `json:"serviceAccount,omitempty" yaml:"serviceAccount,omitempty"`
	Network        string               `json:"network,omitempty" yaml:"network,omitempty"`
	WorkloadName   string               `json:"workloadName,omitempty" yaml:"workloadName,omitempty"`
	WorkloadType   string               `json:"workloadType,omitempty" yaml:"workloadType,omitempty"`
	CanonicalName  string               `json:"canonicalName,omitempty" yaml:"canonicalName,omitempty"`
	CanonicalRev   string               `json:"canonicalRevision,omitempty" yaml:"canonicalRevision,omitempty"`
	Hostname       string               `json:"hostname,omitempty" yaml:"hostname,omitempty"`
	Node           string               `json:"node,omitempty" yaml:"node,omitempty"`
	AuthPolicies   []string             `json:"authorizationPolicies,omitempty" yaml:"authorizationPolicies,omitempty"`
	Status         HealthStatus         `json:"status" yaml:"status"`
	ClusterID      string               `json:"clusterId" yaml:"clusterId"`
	Locality       Locality             `json:"locality,omitempty" yaml:"locality,omitempty"`
	Services       []NamespacedHostname `json:"services,omitempty" yaml:"services,omitempty"`
	Capacity       uint32               `json:"capacity" yaml:"capacity"`
}

type Service struct {
	Name            string                 `json:"name" yaml:"name"`
	Namespace       string                 `json:"namespace" yaml:"namespace"`
	Hostname        string                 `json:"hostname" yaml:"hostname"`
	VIPs            []NetworkAddress       `json:"vips" yaml:"vips"`
	Ports           map[uint16]uint16      `json:"ports" yaml:"ports"`
	AppProtocols    map[uint16]AppProtocol `json:"appProtocols,omitempty" yaml:"appProtocols,omitempty"`
	Endpoints       map[string]Endpoint    `json:"endpoints,omitempty" yaml:"endpoints,omitempty"`
	SubjectAltNames []string               `json:"subjectAltNames,omitempty" yaml:"subjectAltNames,omitempty"`
	Waypoint        *GatewayAddress        `json:"waypoint,omitempty" yaml:"waypoint,omitempty"`
	LoadBalancer    *LoadBalancer          `json:"loadBalancer,omitempty" yaml:"loadBalancer,omitempty"`
	IPFamilies      *IPFamily              `json:"ipFamilies,omitempty" yaml:"ipFamilies,omitempty"`
}

type NamespacedHostname struct {
	Namespace string `json:"namespace" yaml:"namespace"`
	Hostname  string `json:"hostname" yaml:"hostname"`
}

type NetworkAddress struct {
	Network string `json:"network" yaml:"network"`
	Address net.IP `json:"address" yaml:"address"`
}

type GatewayAddress struct {
	Destination   GatewayDestination `json:"destination" yaml:"destination"`
	HBONEMTLSPort uint16             `json:"hboneMtlsPort" yaml:"hboneMtlsPort"`
}

type GatewayDestination struct {
	Address  *NetworkAddress     `json:"address,omitempty" yaml:"address,omitempty"`
	Hostname *NamespacedHostname `json:"hostname,omitempty" yaml:"hostname,omitempty"`
}

type Endpoint struct {
	WorkloadUID string            `json:"workloadUid" yaml:"workloadUid"`
	Port        map[uint16]uint16 `json:"port" yaml:"port"`
	Status      HealthStatus      `json:"status" yaml:"status"`
}

type LoadBalancer struct {
	RoutingPreferences []LoadBalancerScope      `json:"routingPreferences" yaml:"routingPreferences"`
	Mode               LoadBalancerMode         `json:"mode" yaml:"mode"`
	HealthPolicy       LoadBalancerHealthPolicy `json:"healthPolicy" yaml:"healthPolicy"`
}

type Locality struct {
	Region  string `json:"region" yaml:"region"`
	Zone    string `json:"zone" yaml:"zone"`
	Subzone string `json:"subzone" yaml:"subzone"`
}

type Identity struct {
	TrustDomain    string `json:"trustDomain" yaml:"trustDomain"`
	Namespace      string `json:"namespace" yaml:"namespace"`
	ServiceAccount string `json:"serviceAccount" yaml:"serviceAccount"`
}

type InboundProtocol string

const (
	InboundProtocolTCP             InboundProtocol = "TCP"
	InboundProtocolHBONE           InboundProtocol = "HBONE"
	InboundProtocolLegacyIstioMTLS InboundProtocol = "LegacyIstioMtls"
)

type OutboundProtocol string

const (
	OutboundProtocolTCP         OutboundProtocol = "TCP"
	OutboundProtocolHBONE       OutboundProtocol = "HBONE"
	OutboundProtocolDoubleHBONE OutboundProtocol = "DOUBLEHBONE"
)

type NetworkMode string

const (
	NetworkModeStandard    NetworkMode = "Standard"
	NetworkModeHostNetwork NetworkMode = "HostNetwork"
)

type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "Healthy"
	HealthStatusUnhealthy HealthStatus = "Unhealthy"
)

type AppProtocol string

const (
	AppProtocolHTTP11 AppProtocol = "Http11"
	AppProtocolHTTP2  AppProtocol = "Http2"
	AppProtocolGRPC   AppProtocol = "Grpc"
)

type IPFamily string

const (
	IPFamilyDual IPFamily = "Dual"
	IPFamilyIPv4 IPFamily = "IPv4"
	IPFamilyIPv6 IPFamily = "IPv6"
)

type LoadBalancerScope string

const (
	LoadBalancerScopeRegion  LoadBalancerScope = "Region"
	LoadBalancerScopeZone    LoadBalancerScope = "Zone"
	LoadBalancerScopeSubzone LoadBalancerScope = "Subzone"
	LoadBalancerScopeNode    LoadBalancerScope = "Node"
	LoadBalancerScopeCluster LoadBalancerScope = "Cluster"
	LoadBalancerScopeNetwork LoadBalancerScope = "Network"
)

type LoadBalancerMode string

const (
	LoadBalancerModeStandard LoadBalancerMode = "Standard"
	LoadBalancerModeStrict   LoadBalancerMode = "Strict"
	LoadBalancerModeFailover LoadBalancerMode = "Failover"
)

type LoadBalancerHealthPolicy string

const (
	LoadBalancerHealthPolicyOnlyHealthy LoadBalancerHealthPolicy = "OnlyHealthy"
	LoadBalancerHealthPolicyAllowAll    LoadBalancerHealthPolicy = "AllowAll"
)
