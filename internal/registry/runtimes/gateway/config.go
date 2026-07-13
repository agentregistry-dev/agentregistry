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
// Config.Backends to obtain the backend URL.
type Route struct {
	Name        string
	Hostnames   []string
	PathPrefix  string
	BackendRefs []BackendRef
	Policies    []PolicyRef
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
