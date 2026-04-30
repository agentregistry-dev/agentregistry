package python

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
)

//go:embed templates/* templates/agent/* templates/mcp_server/* dice-agent-instruction.md
var templatesFS embed.FS

// PythonGenerator renders ADK Python agents.
type PythonGenerator struct {
	*common.BaseGenerator
}

// NewPythonGenerator instantiates an ADK Python generator.
func NewPythonGenerator() *PythonGenerator {
	return &PythonGenerator{
		BaseGenerator: common.NewBaseGenerator(templatesFS),
	}
}

// Generate scaffolds a new agent on disk.
func (g *PythonGenerator) Generate(agentConfig *common.AgentConfig) error {
	if agentConfig == nil {
		return fmt.Errorf("agent config is required")
	}

	projectPackageDir := filepath.Join(agentConfig.Directory, agentConfig.Name)
	if err := os.MkdirAll(projectPackageDir, 0o755); err != nil {
		return fmt.Errorf("failed to create package directory: %w", err)
	}

	if agentConfig.Instruction == "" {
		defaultInstructions, err := templatesFS.ReadFile("dice-agent-instruction.md")
		if err != nil {
			return fmt.Errorf("failed to read default instructions: %w", err)
		}
		agentConfig.Instruction = string(defaultInstructions)
	}

	agentConfig.Framework = "adk"
	agentConfig.Language = "python"

	if err := g.GenerateProject(*agentConfig); err != nil {
		return fmt.Errorf("failed to generate project: %w", err)
	}

	// agent.yaml is written by the caller (cmd/init.go's
	// writeDeclarativeAgentYAML) using the v1alpha1.Agent envelope.

	if err := relocateAgentPackage(agentConfig.Directory, projectPackageDir); err != nil {
		return err
	}

	printSummary(agentConfig)
	return nil
}

func relocateAgentPackage(projectDir, packageDir string) error {
	agentDir := filepath.Join(projectDir, "agent")
	if _, err := os.Stat(agentDir); err != nil {
		return nil
	}

	entries, err := os.ReadDir(agentDir)
	if err != nil {
		return fmt.Errorf("failed to read agent directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		src := filepath.Join(agentDir, entry.Name())
		dst := filepath.Join(packageDir, entry.Name())
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("failed to move %s to %s: %w", src, dst, err)
		}
	}

	if err := os.Remove(agentDir); err != nil {
		return fmt.Errorf("failed to remove agent directory: %w", err)
	}

	return nil
}

func printSummary(cfg *common.AgentConfig) {
	fmt.Printf("✅ Successfully created %s project in %s\n", cfg.Framework, cfg.Directory)
	fmt.Printf("🤖 Model configuration: %s (%s)\n", cfg.ModelProvider, cfg.ModelName)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", cfg.Name)
	fmt.Printf("   ├── %s/\n", cfg.Name)
	fmt.Printf("   │   ├── __init__.py\n")
	fmt.Printf("   │   ├── agent.py\n")
	fmt.Printf("   │   ├── mcp_tools.py\n")
	fmt.Printf("   │   └── agent-card.json\n")
	fmt.Printf("   ├── agent.yaml\n")
	fmt.Printf("   ├── pyproject.toml\n")
	fmt.Printf("   ├── Dockerfile\n")
	fmt.Printf("   ├── docker-compose.yaml\n")
	fmt.Printf("   ├── README.md\n")
	fmt.Printf("   ├── otel-collector-config.yaml\n")
	fmt.Printf("   └── .python-version\n")
}
