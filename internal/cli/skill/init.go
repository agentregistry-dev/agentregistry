package skill

import (
	"fmt"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/cli/skill/templates"
	"github.com/agentregistry-dev/agentregistry/pkg/validators"

	"github.com/spf13/cobra"
)

var InitCmd = &cobra.Command{
	Use:   "init [skill-name] [output-directory]",
	Short: "Initialize a new agentic skill project",
	Long: `Initialize a new agentic skill project.

If output-directory is provided, the project is created inside that directory
(e.g. "arctl skill init myskill ./skills/" creates ./skills/myskill/).
Otherwise, the project is created in the current directory.`,
	RunE: runInit,
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
	if err := validators.ValidateProjectName(projectName); err != nil {
		return fmt.Errorf("invalid project name: %w", err)
	}

	// Determine output path: if a second arg is given, create inside that directory
	var projectPath string
	if len(args) >= 2 {
		base, err := filepath.Abs(args[1])
		if err != nil {
			return fmt.Errorf("failed to get absolute path for output directory: %w", err)
		}
		projectPath = filepath.Join(base, projectName)
	} else {
		var err error
		projectPath, err = filepath.Abs(projectName)
		if err != nil {
			return fmt.Errorf("failed to get absolute path for project: %w", err)
		}
	}

	// Generate project files
	err := templates.NewGenerator().GenerateProject(templates.ProjectConfig{
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
