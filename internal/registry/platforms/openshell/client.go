package openshell

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pb "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/openshell/proto/gen"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// ProviderInfo holds the identity and type of an OpenShell inference provider.
type ProviderInfo struct {
	Name string
	Type string
}

// Client is the interface for interacting with an OpenShell gateway.
type Client interface {
	CreateSandbox(ctx context.Context, opts CreateSandboxOpts) (*SandboxInfo, error)
	GetSandbox(ctx context.Context, name string) (*SandboxInfo, error)
	ListSandboxes(ctx context.Context) ([]SandboxInfo, error)
	DeleteSandbox(ctx context.Context, name string) error
	GetSandboxLogs(ctx context.Context, sandboxID string) ([]string, error)
	HealthCheck(ctx context.Context) error
	ListProviders(ctx context.Context) ([]ProviderInfo, error)
	EnsureProvider(ctx context.Context, name, providerType string, credentials map[string]string) error
	Close() error
}

// CreateSandboxOpts holds the parameters for creating a sandbox.
type CreateSandboxOpts struct {
	Name      string
	Image     string
	Env       map[string]string
	Providers []string // OpenShell provider names to attach
	GPU       bool
}

// SandboxInfo holds the status of a sandbox.
type SandboxInfo struct {
	ID    string
	Name  string
	Phase string // SANDBOX_PHASE_PROVISIONING, SANDBOX_PHASE_READY, etc.
}

// gatewayMetadata is the structure of ~/.config/openshell/gateways/{name}/metadata.json.
type gatewayMetadata struct {
	Endpoint        string `json:"endpoint,omitempty"`
	GatewayEndpoint string `json:"gateway_endpoint,omitempty"`
}

// grpcClient implements Client using the OpenShell gRPC API.
type grpcClient struct {
	conn   *grpc.ClientConn
	client pb.OpenShellClient
}

// NewGRPCClient creates a Client by discovering the gateway endpoint and mTLS certs.
//
// Resolution order:
//  1. OPENSHELL_GATEWAY_ENDPOINT env var — if set, connects directly (with
//     OPENSHELL_GATEWAY_INSECURE=true to skip mTLS, useful for local dev).
//  2. ~/.config/openshell/gateways/{gatewayName}/metadata.json — standard
//     OpenShell config with mTLS certs.
//
// If gatewayName is empty, it auto-discovers the first available gateway.
func NewGRPCClient(gatewayName string) (Client, error) {
	// Option 1: explicit endpoint via env var.
	if endpoint := os.Getenv("OPENSHELL_GATEWAY_ENDPOINT"); endpoint != "" {
		var tlsCfg *tls.Config
		if os.Getenv("OPENSHELL_GATEWAY_INSECURE") == "true" {
			// Skip mTLS entirely.
		} else if mtlsDir := os.Getenv("OPENSHELL_GATEWAY_MTLS_DIR"); mtlsDir != "" {
			// Load certs from explicit directory.
			var err error
			tlsCfg, err = loadMTLSConfig(mtlsDir)
			if err != nil {
				return nil, fmt.Errorf("load mTLS config from %s: %w", mtlsDir, err)
			}
		} else {
			// Fall back to gateway filesystem config for certs.
			if gatewayName == "" {
				gatewayName = "default"
			}
			configDir, err := gatewayConfigDir(gatewayName)
			if err != nil {
				return nil, fmt.Errorf("resolve openshell config dir: %w", err)
			}
			tlsCfg, err = loadMTLSConfig(filepath.Join(configDir, "mtls"))
			if err != nil {
				return nil, fmt.Errorf("load mTLS config: %w", err)
			}
		}
		return NewGRPCClientFromEndpoint(endpoint, tlsCfg)
	}

	// Option 2: discover from filesystem config.
	if gatewayName == "" {
		var dErr error
		gatewayName, dErr = discoverGatewayName()
		if dErr != nil {
			return nil, fmt.Errorf("discover openshell gateway: %w", dErr)
		}
	}

	configDir, err := gatewayConfigDir(gatewayName)
	if err != nil {
		return nil, fmt.Errorf("resolve openshell config dir: %w", err)
	}

	metadata, err := loadGatewayMetadata(configDir)
	if err != nil {
		return nil, fmt.Errorf("load gateway metadata: %w", err)
	}

	tlsCfg, err := loadMTLSConfig(filepath.Join(configDir, "mtls"))
	if err != nil {
		return nil, fmt.Errorf("load mTLS config: %w", err)
	}

	return NewGRPCClientFromEndpoint(metadata.Endpoint, tlsCfg)
}

// NewGRPCClientFromEndpoint creates a Client from an explicit endpoint and TLS config.
// If tlsCfg is nil, an insecure connection is used (for testing).
// The endpoint should be host:port; any https:// or http:// prefix is stripped.
func NewGRPCClientFromEndpoint(endpoint string, tlsCfg *tls.Config) (Client, error) {
	endpoint = strings.TrimPrefix(endpoint, "https://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	var dialOpts []grpc.DialOption
	if tlsCfg != nil {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(endpoint, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("dial openshell gateway %s: %w", endpoint, err)
	}

	return &grpcClient{
		conn:   conn,
		client: pb.NewOpenShellClient(conn),
	}, nil
}

func (c *grpcClient) CreateSandbox(ctx context.Context, opts CreateSandboxOpts) (*SandboxInfo, error) {
	resp, err := c.client.CreateSandbox(ctx, &pb.CreateSandboxRequest{
		Name: opts.Name,
		Spec: &pb.SandboxSpec{
			Environment: opts.Env,
			Providers:   opts.Providers,
			Gpu:         opts.GPU,
			Template: &pb.SandboxTemplate{
				Image: opts.Image,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}
	return sandboxToInfo(resp.GetSandbox()), nil
}

func (c *grpcClient) GetSandbox(ctx context.Context, name string) (*SandboxInfo, error) {
	resp, err := c.client.GetSandbox(ctx, &pb.GetSandboxRequest{Name: name})
	if err != nil {
		return nil, fmt.Errorf("get sandbox %s: %w", name, err)
	}
	return sandboxToInfo(resp.GetSandbox()), nil
}

func (c *grpcClient) ListSandboxes(ctx context.Context) ([]SandboxInfo, error) {
	resp, err := c.client.ListSandboxes(ctx, &pb.ListSandboxesRequest{})
	if err != nil {
		return nil, fmt.Errorf("list sandboxes: %w", err)
	}
	result := make([]SandboxInfo, len(resp.GetSandboxes()))
	for i, s := range resp.GetSandboxes() {
		result[i] = *sandboxToInfo(s)
	}
	return result, nil
}

func (c *grpcClient) DeleteSandbox(ctx context.Context, name string) error {
	_, err := c.client.DeleteSandbox(ctx, &pb.DeleteSandboxRequest{Name: name})
	if err != nil {
		return fmt.Errorf("delete sandbox %s: %w", name, err)
	}
	return nil
}

func (c *grpcClient) GetSandboxLogs(ctx context.Context, sandboxID string) ([]string, error) {
	resp, err := c.client.GetSandboxLogs(ctx, &pb.GetSandboxLogsRequest{SandboxId: sandboxID})
	if err != nil {
		return nil, fmt.Errorf("get sandbox logs %s: %w", sandboxID, err)
	}
	lines := make([]string, len(resp.GetLogs()))
	for i, entry := range resp.GetLogs() {
		lines[i] = entry.GetMessage()
	}
	return lines, nil
}

func (c *grpcClient) HealthCheck(ctx context.Context) error {
	resp, err := c.client.Health(ctx, &pb.HealthRequest{})
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	if resp.GetStatus() != pb.ServiceStatus_SERVICE_STATUS_HEALTHY {
		return fmt.Errorf("gateway unhealthy: %s", resp.GetStatus().String())
	}
	return nil
}

func (c *grpcClient) ListProviders(ctx context.Context) ([]ProviderInfo, error) {
	resp, err := c.client.ListProviders(ctx, &pb.ListProvidersRequest{})
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	result := make([]ProviderInfo, len(resp.GetProviders()))
	for i, p := range resp.GetProviders() {
		result[i] = ProviderInfo{Name: p.GetName(), Type: p.GetType()}
	}
	return result, nil
}

func (c *grpcClient) EnsureProvider(ctx context.Context, name, providerType string, credentials map[string]string) error {
	_, err := c.client.GetProvider(ctx, &pb.GetProviderRequest{Name: name})
	if err == nil {
		return nil
	}
	_, err = c.client.CreateProvider(ctx, &pb.CreateProviderRequest{
		Provider: &pb.Provider{
			Name:        name,
			Type:        providerType,
			Credentials: credentials,
		},
	})
	if err != nil {
		return fmt.Errorf("create openshell provider %s: %w", name, err)
	}
	return nil
}

func (c *grpcClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// sandboxToInfo converts a proto Sandbox to our domain SandboxInfo.
func sandboxToInfo(s *pb.Sandbox) *SandboxInfo {
	if s == nil {
		return &SandboxInfo{}
	}
	return &SandboxInfo{
		ID:    s.GetId(),
		Name:  s.GetName(),
		Phase: s.GetPhase().String(),
	}
}

// discoverGatewayName finds the first available gateway in ~/.config/openshell/gateways/.
func discoverGatewayName() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	gatewaysDir := filepath.Join(home, ".config", "openshell", "gateways")
	entries, err := os.ReadDir(gatewaysDir)
	if err != nil {
		return "", fmt.Errorf("no openshell gateways found in %s: %w", gatewaysDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			metaPath := filepath.Join(gatewaysDir, e.Name(), "metadata.json")
			if _, sErr := os.Stat(metaPath); sErr == nil {
				return e.Name(), nil
			}
		}
	}
	return "", fmt.Errorf("no openshell gateways found in %s", gatewaysDir)
}

// gatewayConfigDir returns the path to the OpenShell gateway config directory.
func gatewayConfigDir(gatewayName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".config", "openshell", "gateways", gatewayName), nil
}

// loadGatewayMetadata reads the gateway metadata.json file.
func loadGatewayMetadata(configDir string) (*gatewayMetadata, error) {
	data, err := os.ReadFile(filepath.Join(configDir, "metadata.json"))
	if err != nil {
		return nil, fmt.Errorf("read metadata.json: %w", err)
	}
	var meta gatewayMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse metadata.json: %w", err)
	}
	// Support both "endpoint" and "gateway_endpoint" field names.
	if meta.Endpoint == "" && meta.GatewayEndpoint != "" {
		meta.Endpoint = meta.GatewayEndpoint
	}
	if meta.Endpoint == "" {
		return nil, fmt.Errorf("metadata.json: endpoint is empty")
	}
	return &meta, nil
}

// loadMTLSConfig loads mTLS certificates from a directory containing ca.crt, tls.crt, tls.key.
func loadMTLSConfig(mtlsDir string) (*tls.Config, error) {
	caCert, err := os.ReadFile(filepath.Join(mtlsDir, "ca.crt"))
	if err != nil {
		return nil, fmt.Errorf("read ca.crt: %w", err)
	}

	cert, err := tls.LoadX509KeyPair(
		filepath.Join(mtlsDir, "tls.crt"),
		filepath.Join(mtlsDir, "tls.key"),
	)
	if err != nil {
		return nil, fmt.Errorf("load client certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to append CA certificate")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		MinVersion:   tls.VersionTLS12,
		// Allow overriding the expected server name for environments where the
		// connect address differs from the cert's CN/SAN (e.g. Docker connecting
		// to host.docker.internal with a cert issued for 127.0.0.1).
		ServerName: os.Getenv("OPENSHELL_GATEWAY_TLS_SERVER_NAME"),
	}, nil
}
