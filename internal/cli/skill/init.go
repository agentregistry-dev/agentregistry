package skill

import (
	"fmt"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/cli/skill/templates"
	"github.com/agentregistry-dev/agentregistry/pkg/cli/annotations"
	"github.com/agentregistry-dev/agentregistry/pkg/validators"

	"github.com/spf13/cobra"
)

var InitCmd = &cobra.Command{
	Use:         "init [skill-name]",
	Annotations: map[string]string{annotations.SkipDaemonAnnotation: "true"},
	Short:       "Initialize a new agentic skill project",
	Long:        `Initialize a new agentic skill project.`,
	RunE:        runInit,
}

var (
	initForce     bool
	initNoGit     bool
	initVerbose   bool
	initEmpty     bool
	initDirectory string
)

func init() {
	InitCmd.PersistentFlags().BoolVar(&initForce, "force", false, "Overwrite existing directory")
	InitCmd.PersistentFlags().BoolVar(&initNoGit, "no-git", false, "Skip git initialization")
	InitCmd.PersistentFlags().BoolVar(&initVerbose, "verbose", false, "Enable verbose output during initialization")
	InitCmd.PersistentFlags().BoolVar(&initEmpty, "empty", false, "Create an empty skill project")
	InitCmd.PersistentFlags().StringVar(&initDirectory, "output-dir", "", "Output directory for the skill project. If not provided, the project is created in the current directory under the skill name.")
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

	// Determine output path: if the output directory flag is provided, create inside that directory; otherwise, create in the current directory under the skill name
	projectPath, err := resolveProjectPath(projectName, initDirectory)
	if err != nil {
		return err
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

// resolveProjectPath returns the absolute project directory path. If
// the output directory flag is provided, the project is created inside
// that directory; otherwise it is created relative to the current
// working directory.
// We pass in the output directory to the method for easy testability.
func resolveProjectPath(projectName string, outputDir string) (string, error) {
	if outputDir != "" {
		base, err := filepath.Abs(outputDir)
		if err != nil {
			return "", fmt.Errorf("failed to get absolute path for output directory: %w", err)
		}
		return filepath.Join(base, projectName), nil
	}

	abs, err := filepath.Abs(projectName)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for project: %w", err)
	}
	return abs, nil
}
