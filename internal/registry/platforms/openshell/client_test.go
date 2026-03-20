package openshell

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pb "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/openshell/proto/gen"
)

func TestLoadGatewayMetadata(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
		wantEP  string
	}{
		{
			name:    "valid metadata with endpoint",
			content: `{"endpoint": "localhost:9090"}`,
			wantEP:  "localhost:9090",
		},
		{
			name:    "valid metadata with gateway_endpoint",
			content: `{"gateway_endpoint": "https://127.0.0.1:8080"}`,
			wantEP:  "https://127.0.0.1:8080",
		},
		{
			name:    "empty endpoint",
			content: `{"endpoint": ""}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			content: `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}

			meta, err := loadGatewayMetadata(dir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("loadGatewayMetadata() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && meta.Endpoint != tt.wantEP {
				t.Errorf("endpoint = %q, want %q", meta.Endpoint, tt.wantEP)
			}
		})
	}
}

func TestLoadGatewayMetadata_MissingFile(t *testing.T) {
	_, err := loadGatewayMetadata(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing metadata.json")
	}
}

func TestLoadMTLSConfig(t *testing.T) {
	mtlsDir := t.TempDir()

	// Test missing files
	_, err := loadMTLSConfig(mtlsDir)
	if err == nil {
		t.Fatal("expected error for missing certs")
	}
}

func TestSandboxToInfo(t *testing.T) {
	tests := []struct {
		name  string
		phase pb.SandboxPhase
		want  string
	}{
		{"ready", pb.SandboxPhase_SANDBOX_PHASE_READY, "SANDBOX_PHASE_READY"},
		{"provisioning", pb.SandboxPhase_SANDBOX_PHASE_PROVISIONING, "SANDBOX_PHASE_PROVISIONING"},
		{"error", pb.SandboxPhase_SANDBOX_PHASE_ERROR, "SANDBOX_PHASE_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := sandboxToInfo(&pb.Sandbox{Id: "id-1", Name: tt.name, Phase: tt.phase})
			if info.Name != tt.name {
				t.Errorf("Name = %q, want %q", info.Name, tt.name)
			}
			if info.Phase != tt.want {
				t.Errorf("Phase = %q, want %q", info.Phase, tt.want)
			}
		})
	}
}

func TestSandboxToInfo_Nil(t *testing.T) {
	info := sandboxToInfo(nil)
	if info == nil {
		t.Fatal("expected non-nil SandboxInfo for nil input")
	}
	if info.ID != "" || info.Name != "" {
		t.Errorf("expected zero value SandboxInfo, got %+v", info)
	}
}

func TestGatewayConfigDir(t *testing.T) {
	dir, err := gatewayConfigDir("test-gw")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("expected absolute path, got %q", dir)
	}
	if filepath.Base(dir) != "test-gw" {
		t.Errorf("expected dir to end with gateway name, got %q", dir)
	}
}

func TestCreateSandboxOptsMapping(t *testing.T) {
	opts := CreateSandboxOpts{
		Name:      "my-sandbox",
		Image:     "my-image:latest",
		Env:       map[string]string{"KEY": "VAL"},
		Providers: []string{"openai", "anthropic"},
		GPU:       true,
	}

	if opts.Name != "my-sandbox" {
		t.Errorf("Name = %q, want %q", opts.Name, "my-sandbox")
	}
	if opts.Image != "my-image:latest" {
		t.Errorf("Image = %q, want %q", opts.Image, "my-image:latest")
	}
	if len(opts.Env) != 1 || opts.Env["KEY"] != "VAL" {
		t.Errorf("Env = %v, want {KEY: VAL}", opts.Env)
	}
	if len(opts.Providers) != 2 {
		t.Errorf("Providers len = %d, want 2", len(opts.Providers))
	}
	if !opts.GPU {
		t.Error("GPU = false, want true")
	}
}

func TestNewGRPCClientFromEndpoint_Insecure(t *testing.T) {
	client, err := NewGRPCClientFromEndpoint("localhost:0", nil)
	if err != nil {
		t.Fatalf("NewGRPCClientFromEndpoint() error = %v", err)
	}
	defer client.Close()
}

func TestGatewayMetadataJSON(t *testing.T) {
	meta := gatewayMetadata{Endpoint: "example.com:443"}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}

	var decoded gatewayMetadata
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Endpoint != meta.Endpoint {
		t.Errorf("roundtrip: got %q, want %q", decoded.Endpoint, meta.Endpoint)
	}
}
