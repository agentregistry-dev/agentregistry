package local

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	api "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/compose-spec/compose-go/v2/types"
	"go.yaml.in/yaml/v3"
)

const (
	composeFileName      = "docker-compose.yaml"
	agentGatewayFileName = "agent-gateway.yaml"
	defaultProjectName   = "agentregistry_runtime"
)

func ComposeFilePath(runtimeDir string) string {
	return filepath.Join(runtimeDir, composeFileName)
}

func AgentGatewayFilePath(runtimeDir string) string {
	return filepath.Join(runtimeDir, agentGatewayFileName)
}

func LoadDockerComposeConfig(runtimeDir string) (*api.DockerComposeConfig, error) {
	path := ComposeFilePath(runtimeDir)
	project := &api.DockerComposeConfig{
		Name:       defaultProjectName,
		WorkingDir: runtimeDir,
		Services:   map[string]types.ServiceConfig{},
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return project, nil
		}
		return nil, fmt.Errorf("read docker compose config: %w", err)
	}
	if err := yaml.Unmarshal(data, project); err != nil {
		return nil, fmt.Errorf("unmarshal docker compose config: %w", err)
	}
	if project.Name == "" {
		project.Name = defaultProjectName
	}
	if project.WorkingDir == "" {
		project.WorkingDir = runtimeDir
	}
	if project.Services == nil {
		project.Services = map[string]types.ServiceConfig{}
	}
	return project, nil
}

func WriteDockerComposeConfig(runtimeDir string, project *api.DockerComposeConfig) error {
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	if project == nil {
		project = &api.DockerComposeConfig{
			Name:       defaultProjectName,
			WorkingDir: runtimeDir,
			Services:   map[string]types.ServiceConfig{},
		}
	}
	if project.Name == "" {
		project.Name = defaultProjectName
	}
	if project.WorkingDir == "" {
		project.WorkingDir = runtimeDir
	}
	if project.Services == nil {
		project.Services = map[string]types.ServiceConfig{}
	}
	content, err := project.MarshalYAML()
	if err != nil {
		return fmt.Errorf("marshal docker compose config: %w", err)
	}
	if err := os.WriteFile(ComposeFilePath(runtimeDir), content, 0644); err != nil {
		return fmt.Errorf("write docker compose config: %w", err)
	}
	return nil
}

func LoadAgentGatewayConfig(runtimeDir string, port uint16) (*api.AgentGatewayConfig, error) {
	path := AgentGatewayFilePath(runtimeDir)
	cfg := DefaultAgentGatewayConfig(port)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read agent gateway config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("unmarshal agent gateway config: %w", err)
	}
	EnsureAgentGatewayDefaults(cfg, port)
	return cfg, nil
}

func WriteAgentGatewayConfig(runtimeDir string, cfg *api.AgentGatewayConfig, port uint16) error {
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	if cfg == nil {
		cfg = DefaultAgentGatewayConfig(port)
	}
	EnsureAgentGatewayDefaults(cfg, port)
	content, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal agent gateway config: %w", err)
	}
	if err := os.WriteFile(AgentGatewayFilePath(runtimeDir), content, 0644); err != nil {
		return fmt.Errorf("write agent gateway config: %w", err)
	}
	return nil
}

func DefaultAgentGatewayConfig(port uint16) *api.AgentGatewayConfig {
	return &api.AgentGatewayConfig{
		Config: struct{}{},
		Binds: []api.LocalBind{{
			Port: port,
			Listeners: []api.LocalListener{{
				Name:     "default",
				Protocol: api.LocalListenerProtocolHTTP,
				Routes:   []api.LocalRoute{},
			}},
		}},
	}
}

func EnsureAgentGatewayDefaults(cfg *api.AgentGatewayConfig, port uint16) {
	if cfg.Config == nil {
		cfg.Config = struct{}{}
	}
	if len(cfg.Binds) == 0 {
		cfg.Binds = DefaultAgentGatewayConfig(port).Binds
		return
	}
	if cfg.Binds[0].Port == 0 {
		cfg.Binds[0].Port = port
	}
	if len(cfg.Binds[0].Listeners) == 0 {
		cfg.Binds[0].Listeners = []api.LocalListener{{
			Name:     "default",
			Protocol: api.LocalListenerProtocolHTTP,
			Routes:   []api.LocalRoute{},
		}}
		return
	}
	if cfg.Binds[0].Listeners[0].Protocol == "" {
		cfg.Binds[0].Listeners[0].Protocol = api.LocalListenerProtocolHTTP
	}
}

func ComposeUp(ctx context.Context, runtimeDir string, verbose bool) error {
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "up", "-d", "--remove-orphans", "--force-recreate")
	cmd.Dir = runtimeDir
	var stderrBuf bytes.Buffer
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	} else {
		cmd.Stderr = &stderrBuf
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start docker compose: %w: %s", err, strings.TrimSpace(stderrBuf.String()))
	}
	return nil
}
