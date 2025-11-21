package skill

import (
	"fmt"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/cli/skill/templates"

	"github.com/spf13/cobra"
)

var InitCmd = &cobra.Command{
	Use:   "init [skill-name]",
	Short: "Initialize a new agentic skill project",
	Long:  `Initialize a new agentic skill project.`,
	RunE:  runInit,
}

var (
	initForce   bool
	initNoGit   bool
	initVerbose bool
	initEmpty   bool
)

func init() {
	InitCmd.PersistentFlags().BoolVar(&initForce, "force", false, "Overwrite existing directory")
	InitCmd.PersistentFlags().BoolVar(&initNoGit, "no-git", false, "Skip git initialization")
	InitCmd.PersistentFlags().BoolVar(&initVerbose, "verbose", false, "Enable verbose output during initialization")
	InitCmd.PersistentFlags().BoolVar(&initEmpty, "empty", false, "Create an empty skill project")
}

func runInit(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	projectName := args[0]

	// Validate project name
	if err := validateProjectName(projectName); err != nil {
		return fmt.Errorf("invalid project name: %w", err)
	}

	// Check if directory exists
	projectPath, err := filepath.Abs(projectName)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for project: %w", err)
	}

	// Generate project files
	err = templates.NewGenerator().GenerateProject(templates.ProjectConfig{
		NoGit:       initNoGit,
		Directory:   projectPath,
		Verbose:     false,
		ProjectName: projectName,
		Empty:       initEmpty,
	})
	if err != nil {
		return err
	}

	fmt.Printf("To build the skill:\n")
	fmt.Printf(" 	arctl skill publish --docker-url <docker-url> %s\n", projectPath)
	fmt.Printf("For example:\n")
	fmt.Printf("	arctl skill publish --docker-url docker.io/myorg %s\n", projectPath)
	fmt.Printf("  arctl skill publish --docker-url ghcr.io/myorg %s\n", projectPath)
	fmt.Printf("  arctl skill publish --docker-url localhost:5001/myorg %s\n", projectPath)

	return nil
}
