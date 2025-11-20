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

// AgentConfig captures the data required to render an agent project from templates.
type AgentConfig struct {
	Name        string
	Description string
	Image       string
	Directory   string
	Verbose     bool

	Instruction   string
	ModelProvider string
	ModelName     string
	Framework     string
	Language      string
	CLIVersion    string

	McpServers []McpServerType
	EnvVars    []string
	InitGit    bool
}

func (c AgentConfig) shouldInitGit() bool {
	return c.InitGit
}

// ShouldSkipPath allows template walkers to skip specific directories.
func (c AgentConfig) ShouldSkipPath(path string) bool {
	// Skip MCP server assets. They are generated via specific commands.
	return strings.HasPrefix(path, "mcp_server")
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
func (g *BaseGenerator) RenderTemplate(tmplContent string, data interface{}) (string, error) {
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
