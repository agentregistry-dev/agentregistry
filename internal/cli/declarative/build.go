package declarative

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/buildconfig"
	"github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/common/docker"
	"github.com/agentregistry-dev/agentregistry/internal/cli/plugins"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/spf13/cobra"
)

// BuildCmd is the cobra command for "build".
// Tests should use NewBuildCmd() for a fresh instance.
var BuildCmd = newBuildCmd()

// NewBuildCmd returns a new "build" cobra command.
func NewBuildCmd() *cobra.Command {
	return newBuildCmd()
}

func newBuildCmd() *cobra.Command {
	var (
		buildImage    string
		buildPush     bool
		buildPlatform string
	)

	cmd := &cobra.Command{
		Use:   "build DIRECTORY",
		Short: "Build a Docker image for a declarative resource project",
		Long: `Build the Docker image for a project created with 'arctl init'.

Reads arctl.yaml in the project directory to determine the (framework, language)
plugin and dispatches to that plugin's build command. Image tag is taken from the
declarative YAML's spec (or --image override).

Supported kinds: Agent, MCPServer

Examples:
  arctl build ./my-agent
  arctl build ./my-server --push
  arctl build ./my-agent  --image ghcr.io/acme/my-agent:v1.0.0 --platform linux/amd64`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("resolving project directory: %w", err)
			}
			info, err := os.Stat(projectDir)
			if err != nil {
				return fmt.Errorf("project directory not found: %s", projectDir)
			}
			if !info.IsDir() {
				return fmt.Errorf("expected a project directory, not a file — try: arctl build ./my-project")
			}

			obj, yamlFile, err := findDeclarativeResource(projectDir)
			if err != nil {
				return err
			}

			kind := obj.GetKind()
			// Validate the kind against the CLI dispatch table, then
			// dispatch by canonical kind name.
			if _, kerr := scheme.Lookup(kind); kerr != nil {
				return fmt.Errorf("unknown kind %q in %s", kind, yamlFile)
			}

			out := cmd.OutOrStdout()
			switch kind {
			case v1alpha1.KindAgent, v1alpha1.KindMCPServer:
				return buildViaPlugin(out, projectDir, obj, buildImage, buildPlatform, buildPush)
			case v1alpha1.KindPrompt:
				return fmt.Errorf("prompts have no build step — use 'arctl apply -f %s' directly", yamlFile)
			case v1alpha1.KindSkill:
				return fmt.Errorf("skills have no build step — use 'arctl apply -f %s' directly", yamlFile)
			default:
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&buildImage, "image", "", "Docker image tag override (default: from spec.source.image / spec.packages[0].identifier)")
	cmd.Flags().BoolVar(&buildPush, "push", false, "Push the image after building")
	cmd.Flags().StringVar(&buildPlatform, "platform", "", "Target platform (e.g. linux/amd64, linux/arm64)")

	// build is an offline command — hide inherited registry flags from --help output.
	common.HideRegistryFlags(cmd)
	return cmd
}

// findDeclarativeResource looks for a known declarative YAML file in the
// project directory and returns the parsed object and file name found.
func findDeclarativeResource(projectDir string) (v1alpha1.Object, string, error) {
	candidates := []string{"agent.yaml", "mcp.yaml", "skill.yaml", "prompt.yaml"}
	for _, name := range candidates {
		path := filepath.Join(projectDir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		objs, err := scheme.DecodeFile(path)
		if err != nil {
			return nil, name, fmt.Errorf("parsing %s: %w", name, err)
		}
		if len(objs) == 0 {
			continue
		}
		return objs[0], name, nil
	}
	return nil, "", fmt.Errorf(
		"no declarative YAML found in %s (expected agent.yaml, mcp.yaml, skill.yaml, or prompt.yaml)",
		projectDir,
	)
}

// defaultImage returns registry/name:latest as a fallback image tag.
func defaultImage(name string) string {
	registry := strings.TrimSuffix(version.DockerRegistry, "/")
	if registry == "" {
		registry = "localhost:5001"
	}
	return fmt.Sprintf("%s/%s:latest", registry, name)
}

// resolveImage returns the image to use, in priority order:
//  1. --image flag
//  2. specImage (from spec.source.image or spec.source.package.identifier)
//  3. default registry/name:latest
func resolveImage(flagImage, specImage, name string) string {
	if flagImage != "" {
		return flagImage
	}
	if specImage != "" {
		return specImage
	}
	return defaultImage(name)
}

// agentSpecImage extracts spec.source.image for an Agent resource.
func agentSpecImage(obj v1alpha1.Object) string {
	if a, ok := obj.(*v1alpha1.Agent); ok && a.Spec.Source != nil {
		return a.Spec.Source.Image
	}
	return ""
}

// mcpSpecPackageIdentifier extracts spec.source.package.identifier for an MCPServer resource.
func mcpSpecPackageIdentifier(obj v1alpha1.Object) string {
	if s, ok := obj.(*v1alpha1.MCPServer); ok && s.Spec.Source != nil && s.Spec.Source.Package != nil {
		return s.Spec.Source.Package.Identifier
	}
	return ""
}

// buildViaPlugin dispatches the build to the (framework, language) plugin
// declared in arctl.yaml. The plugin's Build command is exec'd in the project
// directory with template vars {Image, ProjectDir, Platform, PluginDir}.
func buildViaPlugin(out io.Writer, projectDir string, obj v1alpha1.Object, flagImage, platform string, push bool) error {
	cfg, err := buildconfig.Read(projectDir)
	if err != nil {
		return fmt.Errorf("read arctl.yaml: %w", err)
	}

	r, err := loadPluginRegistry(projectDir)
	if err != nil {
		return err
	}

	pluginType := "agent"
	specImage := agentSpecImage(obj)
	if obj.GetKind() == v1alpha1.KindMCPServer {
		pluginType = "mcp"
		specImage = mcpSpecPackageIdentifier(obj)
	}

	p, ok := r.Lookup(pluginType, cfg.Framework, cfg.Language)
	if !ok {
		return fmt.Errorf("no plugin for %s framework=%s language=%s", pluginType, cfg.Framework, cfg.Language)
	}

	image := resolveImage(flagImage, specImage, obj.GetMetadata().Name)
	vars := map[string]any{
		"Image":      image,
		"ProjectDir": projectDir,
		"Platform":   platform,
		"PluginDir":  p.SourceDir,
	}

	rendered, err := plugins.RenderArgs(p.Build.Command, vars)
	if err != nil {
		return fmt.Errorf("render build command: %w", err)
	}
	fmt.Fprintf(out, "→ %s: %s\n", p.Name, strings.Join(rendered, " "))
	if err := plugins.ExecForeground(p.Build, projectDir, vars, nil); err != nil {
		return fmt.Errorf("plugin build: %w", err)
	}
	if push {
		fmt.Fprintf(out, "→ pushing %s...\n", image)
		pushCmd := exec.Command("docker", "push", image)
		pushCmd.Stdout = out
		pushCmd.Stderr = out
		if err := pushCmd.Run(); err != nil {
			return fmt.Errorf("docker push: %w", err)
		}
	}
	fmt.Fprintf(out, "✓ Built %s\n", image)
	return nil
}

// CheckDockerAvailable returns nil if docker is reachable, or an error.
// Exported for use in tests.
func CheckDockerAvailable() error {
	return docker.NewExecutor(false, "").CheckAvailability()
}
