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

var (
	pullTag     string
	pullVersion string
)

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
	PullCmd.Flags().StringVar(&pullTag, "tag", "", "Tag to pull (if not specified and multiple tags exist, you will be prompted)")
	PullCmd.Flags().StringVar(&pullVersion, "version", "", "Deprecated alias for --tag")
}

func runPull(cmd *cobra.Command, args []string) error {
	skillName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}
	if pullTag != "" && pullVersion != "" {
		return fmt.Errorf("--tag and --version are mutually exclusive")
	}
	requestedTag := pullTag
	if requestedTag == "" {
		requestedTag = pullVersion
	}

	// Determine output directory
	outputDir := ""
	if len(args) > 1 {
		outputDir = args[1]
	} else {
		outputDir = filepath.Join("skills", skillName)
	}

	printer.PrintInfo(fmt.Sprintf("Pulling skill: %s", skillName))

	// 1. Resolve which tag to pull
	tag, err := resolveSkillTag(cmd.Context(), skillName, requestedTag)
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
		tag,
		func() *v1alpha1.Skill { return &v1alpha1.Skill{} },
	)
	if err != nil {
		return fmt.Errorf("failed to fetch skill from registry: %w", err)
	}

	if skillResp == nil {
		return fmt.Errorf("skill '%s' tag '%s' not found in registry", skillName, tag)
	}

	printer.PrintSuccess(fmt.Sprintf("Found skill: %s (tag %s)", skillResp.Metadata.Name, skillResp.Metadata.Tag))

	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("failed to resolve output directory: %w", err)
	}

	if err := os.MkdirAll(absOutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if skillResp.Spec.Source != nil && skillResp.Spec.Source.Repository != nil && strings.TrimSpace(skillResp.Spec.Source.Repository.URL) != "" {
		if err := pullFromGit(skillResp.Spec.Source.Repository.URL, absOutputDir); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("skill has no Git repository")
	}

	printer.PrintSuccess(fmt.Sprintf("Successfully pulled skill to: %s", absOutputDir))
	return nil
}

// resolveSkillTag determines which tag to pull.
// If a tag is explicitly provided, it is used directly.
// If only one tag exists, that tag is selected automatically.
// If multiple tags exist, the user is prompted to specify one.
//
// ctx flows in from the cobra command so Ctrl-C / parent timeouts cancel
// the registry list call cleanly.
func resolveSkillTag(ctx context.Context, skillName, requestedTag string) (string, error) {
	if requestedTag != "" {
		return requestedTag, nil
	}

	tags, err := client.ListTagsOfName(
		ctx,
		apiClient,
		v1alpha1.KindSkill,
		v1alpha1.DefaultNamespace,
		skillName,
		func() *v1alpha1.Skill { return &v1alpha1.Skill{} },
	)
	if err != nil {
		return "", fmt.Errorf("failed to list skill tags: %w", err)
	}

	if len(tags) == 0 {
		return "", fmt.Errorf("skill '%s' not found in registry", skillName)
	}

	if len(tags) == 1 {
		return tags[0].Metadata.Tag, nil
	}

	printer.PrintError(fmt.Sprintf("skill '%s' has %d tags, please specify one with --tag:", skillName, len(tags)))
	for i, v := range tags {
		latest := ""
		if i == 0 {
			latest = " (latest)"
		}
		printer.PrintInfo(fmt.Sprintf("  %s%s", v.Metadata.Tag, latest))
	}

	return "", fmt.Errorf("multiple tags available, specify one with --tag")
}

// pullFromGit clones a git repository and copies the skill files to the output directory.
func pullFromGit(repoURL, absOutputDir string) error {
	printer.PrintInfo(fmt.Sprintf("Cloning from git: %s", repoURL))
	return gitutil.CloneAndCopy(repoURL, absOutputDir, true)
}
