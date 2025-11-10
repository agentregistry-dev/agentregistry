package skill

import (
	"fmt"
	"github.com/agentregistry-dev/agentregistry/internal/cli/skill/templates"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [skill-name]",
	Short: "Initialize a new agentic skill project",
	Long:  `Initialize a new agentic skill project.`,
	RunE:  runInit,
}

var (
	initForce   bool
	initNoGit   bool
	initVerbose bool
)

func init() {
	SkillCmd.AddCommand(initCmd)
	initCmd.PersistentFlags().BoolVar(&initForce, "force", false, "Overwrite existing directory")
	initCmd.PersistentFlags().BoolVar(&initNoGit, "no-git", false, "Skip git initialization")
	initCmd.PersistentFlags().BoolVar(&initVerbose, "verbose", false, "Enable verbose output during initialization")
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
	})
	if err != nil {
		return err
	}

	fmt.Printf("  To build the skill:\n")
	fmt.Printf("  arctl skill build --project-dir %s\n", projectPath)

	return nil
}

func validateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}

	// Check for invalid characters
	if strings.ContainsAny(name, " \t\n\r/\\:*?\"<>|") {
		return fmt.Errorf("project name contains invalid characters")
	}

	// Check if it starts with a dot
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("project name cannot start with a dot")
	}

	return nil
}
