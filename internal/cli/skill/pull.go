package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common/gitutil"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

var pullVersion string

var PullCmd = &cobra.Command{
	Use:   "pull <skill-name> [output-directory]",
	Short: "Pull a skill from the registry and extract it locally",
	Long: `Pull a skill from the registry and extract its contents to a local directory.
Supports skills hosted in Git repositories.

If output-directory is not specified, it will be extracted to ./skills/<skill-name>`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runPull,
}

func init() {
	PullCmd.Flags().StringVar(&pullVersion, "version", "", "Version to pull (if not specified and multiple versions exist, you will be prompted)")
}

func runPull(cmd *cobra.Command, args []string) error {
	skillName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Determine output directory
	outputDir := ""
	if len(args) > 1 {
		outputDir = args[1]
	} else {
		outputDir = filepath.Join("skills", skillName)
	}

	printer.PrintInfo(fmt.Sprintf("Pulling skill: %s", skillName))

	// 1. Resolve which version to pull
	version, err := resolveSkillVersion(cmd.Context(), skillName, pullVersion)
	if err != nil {
		return err
	}

	// 2. Fetch skill metadata from registry
	printer.PrintInfo("Fetching skill metadata from registry...")
	skillResp, err := client.GetTyped(
		cmd.Context(),
		apiClient,
		v1alpha1.KindSkill,
		v1alpha1.DefaultNamespace,
		skillName,
		version,
		func() *v1alpha1.Skill { return &v1alpha1.Skill{} },
	)
	if err != nil {
		return fmt.Errorf("failed to fetch skill from registry: %w", err)
	}

	if skillResp == nil {
		return fmt.Errorf("skill '%s' version '%s' not found in registry", skillName, version)
	}

	printer.PrintSuccess(fmt.Sprintf("Found skill: %s (version %s)", skillResp.Metadata.Name, skillResp.Metadata.Version))

	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("failed to resolve output directory: %w", err)
	}

	if err := os.MkdirAll(absOutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if skillResp.Spec.Repository != nil && strings.TrimSpace(skillResp.Spec.Repository.URL) != "" {
		if err := pullFromGit(skillResp.Spec.Repository.URL, absOutputDir); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("skill has no Git repository")
	}

	printer.PrintSuccess(fmt.Sprintf("Successfully pulled skill to: %s", absOutputDir))
	return nil
}

// resolveSkillVersion determines which version to pull.
// If a version is explicitly provided, it is used directly.
// If only one version exists, that version is selected automatically.
// If multiple versions exist, the user is prompted to specify one.
//
// ctx flows in from the cobra command so Ctrl-C / parent timeouts cancel
// the registry list call cleanly.
func resolveSkillVersion(ctx context.Context, skillName, requestedVersion string) (string, error) {
	if requestedVersion != "" {
		return requestedVersion, nil
	}

	versions, err := client.ListVersionsOfName(
		ctx,
		apiClient,
		v1alpha1.KindSkill,
		v1alpha1.DefaultNamespace,
		skillName,
		func() *v1alpha1.Skill { return &v1alpha1.Skill{} },
	)
	if err != nil {
		return "", fmt.Errorf("failed to list skill versions: %w", err)
	}

	if len(versions) == 0 {
		return "", fmt.Errorf("skill '%s' not found in registry", skillName)
	}

	if len(versions) == 1 {
		return versions[0].Metadata.Version, nil
	}

	printer.PrintError(fmt.Sprintf("skill '%s' has %d versions, please specify one with --version:", skillName, len(versions)))
	for i, v := range versions {
		latest := ""
		if i == 0 {
			latest = " (latest)"
		}
		printer.PrintInfo(fmt.Sprintf("  %s%s", v.Metadata.Version, latest))
	}

	return "", fmt.Errorf("multiple versions available, specify one with --version")
}

// pullFromGit clones a git repository and copies the skill files to the output directory.
func pullFromGit(repoURL, absOutputDir string) error {
	printer.PrintInfo(fmt.Sprintf("Cloning from git: %s", repoURL))
	return gitutil.CloneAndCopy(repoURL, absOutputDir, true)
}
