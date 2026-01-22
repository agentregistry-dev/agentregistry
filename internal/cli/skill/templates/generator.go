package templates

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed all:hello-world
var templateFiles embed.FS

// Generator for Skills
type Generator struct{}

type ProjectConfig struct {
	NoGit       bool
	Directory   string
	Verbose     bool
	ProjectName string
	Empty       bool
}

// NewGenerator creates a new Skill generator
func NewGenerator() *Generator {
	return &Generator{}
}

// GenerateProject generates a new Python project
func (g *Generator) GenerateProject(config ProjectConfig) error {
	templateRoot, err := fs.Sub(templateFiles, "hello-world")
	if err != nil {
		return fmt.Errorf("failed to get templates subdirectory: %w", err)
	}

	// If Empty flag is set, only render specific templates
	var allowedTemplates map[string]bool
	if config.Empty {
		allowedTemplates = map[string]bool{
			"Dockerfile.tmpl":  true,
			"LICENSE.txt.tmpl": true,
			"SKILL.md.tmpl":    true,
		}
	}

	err = fs.WalkDir(templateRoot, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		destPath := filepath.Join(config.ProjectName, strings.TrimSuffix(path, ".tmpl"))

		if d.IsDir() {
			// When Empty flag is set, skip creating directories (allowed templates are in root)
			if config.Empty {
				return nil
			}
			// Create the directory if it doesn't exist
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", destPath, err)
			}
			return nil
		}

		// If Empty flag is set, only render allowed templates
		if config.Empty {
			baseName := filepath.Base(path)
			if !allowedTemplates[baseName] {
				return nil // Skip this template
			}
		}

		// Ensure parent directory exists
		parentDir := filepath.Dir(destPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", parentDir, err)
		}

		// Read template file
		templateContent, err := fs.ReadFile(templateRoot, path)
		if err != nil {
			return fmt.Errorf("failed to read template file %s: %w", path, err)
		}

		// Render template content
		renderedContent, err := renderTemplate(string(templateContent), config)
		if err != nil {
			return fmt.Errorf("failed to render template for %s: %w", path, err)
		}

		// Create file
		if err := os.WriteFile(destPath, []byte(renderedContent), 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", destPath, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to generate project files: %w", err)
	}

	// Initialize git repository
	if !config.NoGit {
		if err := g.initGitRepo(config.Directory, config.Verbose); err != nil {
			// Don't fail the whole operation if git init fails
			if config.Verbose {
				fmt.Printf("Warning: failed to initialize git repository: %v\n", err)
			}
		}
	}

	return nil
}

// initGitRepo initializes a git repository in the specified directory
func (g *Generator) initGitRepo(dir string, verbose bool) error {
	cmd := exec.Command("git", "init")
	cmd.Dir = dir

	if verbose {
		fmt.Printf("  Initializing git repository...\n")
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run git init: %w", err)
	}

	return nil
}

// renderTemplate renders a template string with the provided data
func renderTemplate(tmplContent string, data any) (string, error) {
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
