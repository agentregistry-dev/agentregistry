package declarative

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common/docker"
	mcpbuild "github.com/agentregistry-dev/agentregistry/internal/cli/mcp/build"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/agentregistry-dev/agentregistry/internal/version"
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

Reads the declarative YAML file in the project directory (agent.yaml, mcp.yaml,
or skill.yaml) to determine the resource kind and image tag, then runs docker build.

Supported kinds: Agent, MCPServer, Skill

Examples:
  arctl build ./my-agent
  arctl build ./my-server --push
  arctl build ./my-skill  --image ghcr.io/acme/my-skill:v1.0.0 --platform linux/amd64`,
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

			r, yamlFile, err := findDeclarativeResource(projectDir)
			if err != nil {
				return err
			}

			// Validate the kind against the registry, then dispatch by canonical kind name.
			// Build is intentionally kept as a CLI-side per-kind dispatch because the build
			// logic depends on CLI packages (docker executor, mcp builder) that must not be
			// imported by the server-side registry/kinds packages. The dispatch key is the
			// canonical kind string from the registry — not a raw YAML field.
			if defaultRegistry != nil {
				if _, kerr := defaultRegistry.Lookup(r.Kind); kerr != nil {
					return fmt.Errorf("unknown kind %q in %s", r.Kind, yamlFile)
				}
			}

			out := cmd.OutOrStdout()
			switch r.Kind {
			case "agent":
				return buildAgent(out, projectDir, r, buildImage, buildPlatform, buildPush)
			case "mcp":
				return buildMCPServer(out, projectDir, r, buildImage, buildPlatform, buildPush)
			case "skill":
				return buildSkill(out, projectDir, r, buildImage, buildPlatform, buildPush)
			case "prompt":
				return fmt.Errorf("prompts have no build step — use 'arctl apply -f %s' directly", yamlFile)
			default:
				// Registry validated the kind above; reaching here means a kind that exists in
				// the registry has no build action — skip silently (acceptable per task spec).
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&buildImage, "image", "", "Docker image tag override (default: from spec.image / spec.packages[0].identifier)")
	cmd.Flags().BoolVar(&buildPush, "push", false, "Push the image after building")
	cmd.Flags().StringVar(&buildPlatform, "platform", "", "Target platform (e.g. linux/amd64, linux/arm64)")

	return cmd
}

// findDeclarativeResource looks for a known declarative YAML file in the
// project directory and returns the parsed resource and file name found.
func findDeclarativeResource(projectDir string) (*scheme.Resource, string, error) {
	candidates := []string{"agent.yaml", "mcp.yaml", "skill.yaml", "prompt.yaml"}
	for _, name := range candidates {
		path := filepath.Join(projectDir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		resources, err := scheme.DecodeFile(defaultRegistry, path)
		if err != nil {
			return nil, name, fmt.Errorf("parsing %s: %w", name, err)
		}
		if len(resources) == 0 {
			continue
		}
		return resources[0], name, nil
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
//  2. specImage (from spec.image or spec.packages[0].identifier)
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

// agentSpecImage extracts spec.image for an Agent resource.
func agentSpecImage(r *scheme.Resource) string {
	if spec, ok := r.Spec.(*kinds.AgentSpec); ok {
		return spec.Image
	}
	return ""
}

// mcpSpecPackageIdentifier extracts spec.packages[0].identifier for an MCPServer resource.
func mcpSpecPackageIdentifier(r *scheme.Resource) string {
	if spec, ok := r.Spec.(*kinds.MCPSpec); ok && len(spec.Packages) > 0 {
		return spec.Packages[0].Identifier
	}
	return ""
}

// skillSpecPackageIdentifier extracts spec.packages[0].identifier for a Skill resource.
func skillSpecPackageIdentifier(r *scheme.Resource) string {
	if spec, ok := r.Spec.(*kinds.SkillSpec); ok && len(spec.Packages) > 0 {
		return spec.Packages[0].Identifier
	}
	return ""
}

func buildAgent(out io.Writer, projectDir string, r *scheme.Resource, flagImage, platform string, push bool) error {
	dockerfilePath := filepath.Join(projectDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err != nil {
		return fmt.Errorf("dockerfile not found in %s", projectDir)
	}

	image := resolveImage(flagImage, agentSpecImage(r), r.Metadata.Name)

	exec := docker.NewExecutor(false, projectDir)
	if err := exec.CheckAvailability(); err != nil {
		return fmt.Errorf("docker check failed: %w", err)
	}

	var extraArgs []string
	if platform != "" {
		extraArgs = append(extraArgs, "--platform", platform)
	}

	fmt.Fprintf(out, "Building agent image: %s\n", image)
	if err := exec.Build(image, ".", extraArgs...); err != nil {
		return err
	}
	if push {
		return exec.Push(image)
	}
	return nil
}

func buildMCPServer(out io.Writer, projectDir string, r *scheme.Resource, flagImage, platform string, push bool) error {
	image := resolveImage(flagImage, mcpSpecPackageIdentifier(r), r.Metadata.Name)

	fmt.Fprintf(out, "Building MCP server image: %s\n", image)
	builder := mcpbuild.New()
	if err := builder.Build(mcpbuild.Options{
		ProjectDir: projectDir,
		Tag:        image,
		Platform:   platform,
	}); err != nil {
		return err
	}
	if push {
		exec := docker.NewExecutor(false, "")
		return exec.Push(image)
	}
	return nil
}

// CheckDockerAvailable returns nil if docker is reachable, or an error.
// Exported for use in tests.
func CheckDockerAvailable() error {
	return docker.NewExecutor(false, "").CheckAvailability()
}

// skillDockerfile packages skill assets into a minimal OCI image.
// Skills are metadata-only containers (no runtime); FROM scratch is intentional —
// there is no entrypoint or shell, just the raw files copied in.
const skillDockerfile = "FROM scratch\nCOPY . .\n"

func buildSkill(out io.Writer, projectDir string, r *scheme.Resource, flagImage, platform string, push bool) error {
	image := resolveImage(flagImage, skillSpecPackageIdentifier(r), r.Metadata.Name)

	fmt.Fprintf(out, "Building skill image: %s\n", image)

	tmpFile, err := os.CreateTemp("", "skill-dockerfile-*")
	if err != nil {
		return fmt.Errorf("creating temp Dockerfile: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(skillDockerfile); err != nil {
		tmpFile.Close()
		return fmt.Errorf("writing temp Dockerfile: %w", err)
	}
	tmpFile.Close()

	var extraArgs []string
	extraArgs = append(extraArgs, "-f", tmpFile.Name())
	if platform != "" {
		extraArgs = append(extraArgs, "--platform", platform)
	}

	exec := docker.NewExecutor(false, projectDir)
	if err := exec.CheckAvailability(); err != nil {
		return fmt.Errorf("docker check failed: %w", err)
	}
	if err := exec.Build(image, projectDir, extraArgs...); err != nil {
		return err
	}
	if push {
		return exec.Push(image)
	}
	return nil
}
