package declarative

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common/gitutil"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/spf13/cobra"
)

// PullCmd is the cobra command for "pull".
// Tests should use NewPullCmd() for a fresh instance.
var PullCmd = newPullCmd()

// NewPullCmd returns a new "pull" cobra command.
func NewPullCmd() *cobra.Command {
	return newPullCmd()
}

func newPullCmd() *cobra.Command {
	var version string
	cmd := &cobra.Command{
		Use:   "pull TYPE NAME [DIRECTORY]",
		Short: "Fetch a registry resource's source repo to local",
		Long: `Fetch a registry resource's source repository to a local directory.

Supported types: agent, mcp, skill. Reads the resource's
Spec.Source.Repository.URL from the registry and clones it into DIRECTORY
(defaults to NAME if omitted).`,
		Example: `  arctl pull agent myagent
  arctl pull mcp myserver ./vendor/myserver
  arctl pull skill myskill --version 1.2.0`,
		SilenceUsage: true,
		Args:         cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ, name := args[0], args[1]
			outDir := name
			if len(args) == 3 {
				outDir = args[2]
			}
			abs, err := filepath.Abs(outDir)
			if err != nil {
				return err
			}
			return pullResource(cmd.Context(), typ, name, version, abs)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "Specific version to pull")
	return cmd
}

func pullResource(ctx context.Context, typ, name, version, outDir string) error {
	switch typ {
	case "agent", "mcp", "skill":
	default:
		return fmt.Errorf("unknown type %q (want one of: agent, mcp, skill)", typ)
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	var repoURL, subfolder string
	switch typ {
	case "agent":
		obj, err := client.GetTyped(ctx, apiClient, v1alpha1.KindAgent, v1alpha1.DefaultNamespace, name, version,
			func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
		if err != nil || obj == nil {
			return fmt.Errorf("fetch agent %q: %w", name, err)
		}
		if obj.Spec.Source == nil || obj.Spec.Source.Repository == nil || obj.Spec.Source.Repository.URL == "" {
			return fmt.Errorf("agent %q has no source repository URL set", name)
		}
		repoURL = obj.Spec.Source.Repository.URL
		subfolder = obj.Spec.Source.Repository.Subfolder
	case "mcp":
		obj, err := client.GetTyped(ctx, apiClient, v1alpha1.KindMCPServer, v1alpha1.DefaultNamespace, name, version,
			func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} })
		if err != nil || obj == nil {
			return fmt.Errorf("fetch mcp %q: %w", name, err)
		}
		if obj.Spec.Source == nil || obj.Spec.Source.Repository == nil || obj.Spec.Source.Repository.URL == "" {
			return fmt.Errorf("mcp %q has no source repository URL set", name)
		}
		repoURL = obj.Spec.Source.Repository.URL
		subfolder = obj.Spec.Source.Repository.Subfolder
	case "skill":
		obj, err := client.GetTyped(ctx, apiClient, v1alpha1.KindSkill, v1alpha1.DefaultNamespace, name, version,
			func() *v1alpha1.Skill { return &v1alpha1.Skill{} })
		if err != nil || obj == nil {
			return fmt.Errorf("fetch skill %q: %w", name, err)
		}
		if obj.Spec.Source == nil || obj.Spec.Source.Repository == nil || obj.Spec.Source.Repository.URL == "" {
			return fmt.Errorf("skill %q has no source repository URL set", name)
		}
		repoURL = obj.Spec.Source.Repository.URL
		subfolder = obj.Spec.Source.Repository.Subfolder
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	fmt.Printf("Cloning %s into %s\n", repoURL, outDir)
	if err := gitutil.CloneAndCopy(repoURL, outDir, false); err != nil {
		return err
	}
	if subfolder != "" {
		fmt.Printf("(subfolder hint: %s)\n", subfolder)
	}
	fmt.Printf("Pulled %s\n", name)
	return nil
}
