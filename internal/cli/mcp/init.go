package mcp

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/frameworks"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/templates"

	"github.com/spf13/cobra"
)

var InitCmd = &cobra.Command{
	Use:   "init [project-type] [project-name]",
	Short: "Initialize a new MCP server project",
	Long: `Initialize a new MCP server project with dynamic tool loading.

This command provides subcommands to initialize a new MCP server project
using one of the supported frameworks.`,
	RunE: runInit,
}

var (
	initForce          bool
	initNoGit          bool
	initAuthor         string
	initEmail          string
	initDescription    string
	initNonInteractive bool
)

func init() {
	InitCmd.PersistentFlags().BoolVar(&initForce, "force", false, "Overwrite existing directory")
	InitCmd.PersistentFlags().BoolVar(&initNoGit, "no-git", false, "Skip git initialization")
	InitCmd.PersistentFlags().StringVar(&initAuthor, "author", "", "Author name for the project")
	InitCmd.PersistentFlags().StringVar(&initEmail, "email", "", "Author email for the project")
	InitCmd.PersistentFlags().StringVar(&initDescription, "description", "", "Description for the project")
	InitCmd.PersistentFlags().BoolVar(&initNonInteractive, "non-interactive", false, "Run in non-interactive mode")
}

func runInit(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	return nil
}

func runInitFramework(
	projectName, framework string,
	customizeProjectConfig func(*templates.ProjectConfig) error,
) error {
	// Validate project name
	if err := validateProjectName(projectName); err != nil {
		return fmt.Errorf("invalid project name: %w", err)
	}

	if !initNonInteractive {
		if initDescription == "" {
			initDescription = promptForDescription()
		}
		if initAuthor == "" {
			initAuthor = promptForAuthor()
		}
		if initEmail == "" {
			initEmail = promptForEmail()
		}
	}

	// Create project manifest
	projectManifest := manifest.GetDefault(projectName, framework, initDescription, initAuthor, initEmail)

	// Check if directory exists
	projectPath, err := filepath.Abs(projectName)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for project: %w", err)
	}

	// Create project configuration
	projectConfig := templates.ProjectConfig{
		ProjectName: projectManifest.Name,
		Version:     projectManifest.Version,
		Description: projectManifest.Description,
		Author:      projectManifest.Author,
		Email:       projectManifest.Email,
		Tools:       projectManifest.Tools,
		Secrets:     projectManifest.Secrets,
		Directory:   projectPath,
		NoGit:       initNoGit,
	}

	// Customize project config for the specific framework
	if customizeProjectConfig != nil {
		if err := customizeProjectConfig(&projectConfig); err != nil {
			return fmt.Errorf("failed to customize project config: %w", err)
		}
	}

	// Generate project files
	generator, err := frameworks.GetGenerator(framework)
	if err != nil {
		return err
	}
	if err := generator.GenerateProject(projectConfig); err != nil {
		return fmt.Errorf("failed to generate project: %w", err)
	}

	fmt.Printf("To build the server:\n")
	fmt.Printf("  arctl mcp build %s\n", projectPath)

	return manifest.NewManager(projectPath).Save(projectManifest)
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

// Prompts for user input

func promptForAuthor() string {
	fmt.Print("Enter author name (optional): ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(input)
}

func promptForEmail() string {
	for {
		fmt.Print("Enter author email (optional): ")
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			return ""
		}
		email := strings.TrimSpace(input)

		// If empty, allow it (optional field)
		if email == "" {
			return email
		}

		// Basic email validation
		if isValidEmail(email) {
			return email
		}

		fmt.Println("Invalid email format. Please enter a valid email address or leave empty.")
	}
}

func promptForDescription() string {
	fmt.Print("Enter description (optional): ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(input)
}

// isValidEmail performs basic email validation
func isValidEmail(email string) bool {
	// Basic email regex pattern
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}

func promptForInput(promptText string) (string, error) {
	fmt.Print(promptText)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}

// fileExists checks if a file exists at the given path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
