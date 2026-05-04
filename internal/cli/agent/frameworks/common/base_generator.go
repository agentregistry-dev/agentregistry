// Package common holds the agent-project scaffolder shared between
// language/framework-specific generators (today: ADK Python). It is
// not part of the v1alpha1 wire model and never reads or writes
// agent.yaml — the on-disk manifest is a v1alpha1.Agent envelope
// owned by internal/cli/declarative + internal/cli/agent/project.
//
// The scaffolder takes an AgentConfig (the user's `arctl init agent`
// inputs) and renders a starter project tree (Dockerfile,
// pyproject.toml, agent.py, docker-compose.yaml, README, ...) into a
// fresh directory. After scaffolding, init.go overwrites agent.yaml
// with the canonical v1alpha1 envelope, and the scaffolder is never
// invoked again — `arctl agent run`'s template re-renders go through
// internal/cli/agent/project, which feeds the live AgentManifest into
// the runtime-shared subset of the same templates (docker-compose,
// mcp_tools).
package common

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

// AgentConfig is the input bundle for the project scaffolder. It is
// purely a render-time DTO for `arctl init agent`'s template tree; it
// does NOT model an agent's runtime state and shares no fields with
// v1alpha1.Agent / AgentManifest. Mutating an AgentConfig has no effect
// on any registry resource.
type AgentConfig struct {
	Name        string
	Version     string
	Description string
	Image       string
	Directory   string
	Verbose     bool

	Instruction           string
	ModelProvider         string
	ModelName             string
	Framework             string
	Language              string
	KagentADKImageVersion string
	KagentADKPyVersion    string
	Port                  int

	EnvVars []string
	InitGit bool
}

// McpServers + HasSkills are no-op render shims, NOT data.
//
// `docker-compose.yaml.tmpl` and `mcp_tools.py.tmpl` are shared between
// two render entrypoints: `arctl init agent` (against AgentConfig at
// scaffold time, no MCP servers / skills declared yet) and `arctl
// agent run` (against an anonymous struct carrying the resolved
// AgentManifest at runtime). text/template requires both `.McpServers`
// and `.HasSkills` to resolve regardless of which struct it's called
// against, so AgentConfig exposes them as methods that always return
// the empty values the scaffold-time render expects. Move the runtime
// fields into AgentConfig and the meaning of these methods will silently
// change — keep them as method-shaped zero-value shims.

func (c AgentConfig) McpServers() []struct{} { return nil }

func (c AgentConfig) HasSkills() bool { return false }

func (c AgentConfig) shouldInitGit() bool {
	return c.InitGit
}

// ShouldSkipPath allows template walkers to skip specific directories.
func (c AgentConfig) ShouldSkipPath(path string) bool {
	// Skip MCP server assets. They are generated via specific commands.
	if strings.HasPrefix(path, "mcp_server") {
		return true
	}
	return false
}

// BaseGenerator renders template trees into a destination directory.
type BaseGenerator struct {
	templateFiles fs.FS
	templateRoot  string
}

// NewBaseGenerator returns a template renderer rooted at "templates".
func NewBaseGenerator(templateFiles fs.FS) *BaseGenerator {
	return &BaseGenerator{
		templateFiles: templateFiles,
		templateRoot:  "templates",
	}
}

// GenerateProject walks the template tree and renders files to disk.
func (g *BaseGenerator) GenerateProject(config AgentConfig) error {
	if config.Directory == "" {
		return fmt.Errorf("project directory is required")
	}

	if err := os.MkdirAll(config.Directory, 0o755); err != nil {
		return fmt.Errorf("failed to ensure project directory: %w", err)
	}

	templateRoot, err := fs.Sub(g.templateFiles, g.templateRoot)
	if err != nil {
		return fmt.Errorf("failed to open template root: %w", err)
	}

	err = fs.WalkDir(templateRoot, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if config.ShouldSkipPath(path) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		destPath := strings.TrimSuffix(path, ".tmpl")
		destPath = filepath.Join(config.Directory, destPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}

		content, err := fs.ReadFile(templateRoot, path)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", path, err)
		}

		rendered, err := g.RenderTemplate(string(content), config)
		if err != nil {
			return fmt.Errorf("failed to render template %s: %w", path, err)
		}

		if err := os.WriteFile(destPath, []byte(rendered), 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", destPath, err)
		}

		if config.Verbose {
			fmt.Printf("  Generated: %s\n", destPath)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk templates: %w", err)
	}

	if config.shouldInitGit() {
		if err := initGitRepo(config.Directory, config.Verbose); err != nil && config.Verbose {
			fmt.Printf("Warning: git init failed: %v\n", err)
		}
	}

	return nil
}

// RenderTemplate renders a template string with the provided data.
func (g *BaseGenerator) RenderTemplate(tmplContent string, data any) (string, error) {
	tmpl, err := template.New("template").Parse(tmplContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return result.String(), nil
}

// ReadTemplateFile reads a raw template file from the generator's embedded filesystem.
func (g *BaseGenerator) ReadTemplateFile(templatePath string) ([]byte, error) {
	fullPath := filepath.Join(g.templateRoot, templatePath)
	return fs.ReadFile(g.templateFiles, fullPath)
}

func initGitRepo(dir string, verbose bool) error {
	cmd := exec.Command("git", "init")
	cmd.Dir = dir

	if verbose {
		fmt.Println("  Initializing git repository…")
	}

	return cmd.Run()
}
