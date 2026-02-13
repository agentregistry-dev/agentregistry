package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/project"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/tui"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var AddMcpCmd = &cobra.Command{
	Use:   "add-mcp [name] [args...]",
	Short: "Add an MCP server entry to agent.yaml",
	Long:  `Add an MCP server entry to agent.yaml. Use flags for non-interactive setup or run without flags to open the wizard.`,
	Args:  cobra.ArbitraryArgs,
	RunE:  runAddMcp,
}

var (
	projectDir                 string
	remoteURL                  string
	headers                    []string
	command                    string
	args                       []string
	env                        []string
	image                      string
	build                      string
	registryURL                string
	registryServerName         string
	registryServerVersion      string
	registryServerPreferRemote bool
)

func init() {
	AddMcpCmd.Flags().StringVar(&projectDir, "project-dir", ".", "Project directory (default: current directory)")
	AddMcpCmd.Flags().StringVar(&remoteURL, "remote", "", "Remote MCP server URL (http/https)")
	AddMcpCmd.Flags().StringSliceVar(&headers, "header", nil, "HTTP header for remote MCP in KEY=VALUE format (repeatable, supports ${VAR} for env vars)")
	AddMcpCmd.Flags().StringVar(&command, "command", "", "Command to run MCP server (e.g., npx, uvx, arctl, or a binary)")
	AddMcpCmd.Flags().StringSliceVar(&args, "arg", nil, "Command argument (repeatable)")
	AddMcpCmd.Flags().StringSliceVar(&env, "env", nil, "Environment variable in KEY=VALUE format (repeatable)")
	AddMcpCmd.Flags().StringVar(&image, "image", "", "Container image (optional; mutually exclusive with --build)")
	AddMcpCmd.Flags().StringVar(&build, "build", "", "Container build (optional; mutually exclusive with --image)")
	AddMcpCmd.Flags().StringVar(&registryURL, "registry-url", "", "Registry URL (e.g., https://registry.example.com) (optional; mutually exclusive with --remote, --command, --image, --build)")
	AddMcpCmd.Flags().StringVar(&registryServerName, "registry-server-name", "", "Registry-deployed MCP server name (optional; mutually exclusive with --remote, --command, --image, --build)")
	AddMcpCmd.Flags().StringVar(&registryServerVersion, "registry-server-version", "", "Version of the MCP server to deploy from registry (e.g., 1.0.0) (optional)")
	AddMcpCmd.Flags().BoolVar(&registryServerPreferRemote, "registry-server-prefer-remote", false, "Prefer remote MCP server (optional)")
}

// addMcpCmd runs the interactive flow to append an MCP server to agent.yaml
// registry-based mcp servers are resolved at runtime, meaning they are just stored as a reference in the agent manifest at add-time.
func addMcpCmd(name string) error {
	// Determine project directory
	resolvedDir, err := project.ResolveProjectDir(projectDir)
	if err != nil {
		return err
	}

	// Load manifest
	manifest, err := project.LoadManifest(resolvedDir)
	if err != nil {
		return err
	}

	if verbose {
		fmt.Printf("Loaded manifest for agent '%s' from %s\n", manifest.Name, resolvedDir)
	}

	// If flags provided, build non-interactively; else run wizard
	var res models.McpServerType
	if remoteURL != "" || command != "" || image != "" || build != "" || registryURL != "" || registryServerName != "" {
		if remoteURL != "" {
			headerMap, err := utils.ParseKeyValuePairs(headers)
			if err != nil {
				return fmt.Errorf("failed to parse headers: %w", err)
			}
			res = models.McpServerType{
				Type:    "remote",
				URL:     remoteURL,
				Name:    name,
				Headers: headerMap,
			}
		} else if registryURL != "" && registryServerName != "" {
			res = models.McpServerType{
				Type:                       "registry",
				Name:                       name,
				RegistryURL:                registryURL,
				RegistryServerName:         registryServerName,
				RegistryServerVersion:      registryServerVersion,
				RegistryServerPreferRemote: registryServerPreferRemote,
				Env:                        env,
			}
		} else {
			if image != "" && build != "" {
				return fmt.Errorf("only one of --image or --build may be set")
			}
			res = models.McpServerType{
				Type:    "command",
				Name:    name,
				Command: command,
				Args:    args,
				Env:     env,
				Image:   image,
				Build:   build,
			}
		}
	} else {
		// Prefer the wizard experience
		wiz := tui.NewMcpServerWizard()
		p := tea.NewProgram(wiz)
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("failed to run TUI: %w", err)
		}
		if !wiz.Ok() {
			fmt.Println("Canceled.")
			return nil
		}
		res = wiz.Result()
		if name != "" {
			res.Name = name
		}
	}

	// Ensure unique name
	for _, existing := range manifest.McpServers {
		if strings.EqualFold(existing.Name, res.Name) {
			return fmt.Errorf("an MCP server named '%s' already exists in agent.yaml", res.Name)
		}
	}

	// Append and validate
	manifest.McpServers = append(manifest.McpServers, res)
	manager := common.NewManifestManager(resolvedDir)

	if err := manager.Save(manifest); err != nil {
		return fmt.Errorf("failed to save agent.yaml: %w", err)
	}

	// Regenerate mcp_tools.py with updated MCP servers for ADK Python projects
	if err := project.RegenerateMcpTools(resolvedDir, manifest, verbose); err != nil {
		return fmt.Errorf("failed to regenerate mcp_tools.py: %w", err)
	}

	// Create/update individual MCP server directories with config.yaml
	if err := project.EnsureMcpServerDirectories(resolvedDir, manifest, verbose); err != nil {
		return fmt.Errorf("failed to ensure MCP server directories: %w", err)
	}

	// Regenerate docker-compose.yaml with updated MCP server configuration
	if err := project.RegenerateDockerCompose(resolvedDir, manifest, "", verbose); err != nil {
		return fmt.Errorf("failed to regenerate docker-compose.yaml: %w", err)
	}

	fmt.Printf("✓ Added MCP server '%s' (%s) to agent.yaml\n", res.Name, res.Type)
	return nil
}

func runAddMcp(cmd *cobra.Command, positionalArgs []string) error {
	name := ""

	if len(positionalArgs) > 0 {
		name = positionalArgs[0]
		if len(positionalArgs) > 1 && command != "" {
			// Additional positional args after name are appended to command args
			args = append(args, positionalArgs[1:]...)
		}
	}

	if err := addMcpCmd(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return nil
}
