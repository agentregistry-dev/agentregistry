package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	cliruntime "github.com/agentregistry-dev/agentregistry/pkg/cli/runtime"
	"github.com/agentregistry-dev/agentregistry/pkg/gitutil"
)

func NewPullCmd(deps cliruntime.Deps) *cobra.Command {
	var tag string
	cmd := &cobra.Command{
		Use:   cliruntime.CommandPull + " TYPE NAME [DIRECTORY]",
		Short: "Fetch a registry resource's source repo to a local directory",
		Long: `Fetch a registry resource's source repository to a local directory.

Supported type: skill. Reads the resource's
Spec.Source.Repository.URL from the registry and clones it into DIRECTORY
(defaults to NAME if omitted). NAME resolves in the namespace selected by
--namespace/-n, else ARCTL_NAMESPACE, else "default"; NAMESPACE/NAME is also
accepted.`,
		Example: `  arctl pull skill myskill --tag stable
  arctl pull skill myskill -n team-a`,
		SilenceUsage: true,
		Args:         cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			typ := args[0]
			sel, err := selectedNamespace(deps)
			if err != nil {
				return err
			}
			ref, err := resolveResourceRef(args[1], sel)
			if err != nil {
				return err
			}
			outDir := ref.Name
			if len(args) == 3 {
				outDir = args[2]
			}
			abs, err := filepath.Abs(outDir)
			if err != nil {
				return err
			}
			return pullResource(cmd.Context(), deps, typ, ref, tag, abs)
		},
	}
	cmd.Flags().StringVar(&tag, "tag", "", "Specific tag to pull")
	return cmd
}

func pullResource(ctx context.Context, deps cliruntime.Deps, typ string, ref resourceLookupRef, tag, outDir string) error {
	switch typ {
	case "skill":
	default:
		return fmt.Errorf("unknown type %q (want skill)", typ)
	}

	if deps.Runtime == nil {
		return fmt.Errorf("registry runtime not configured")
	}
	c, err := deps.Runtime.RegistryClient(ctx)
	if err != nil {
		return fmt.Errorf("resolving registry client: %w", err)
	}

	var repo *v1alpha1.Repository
	switch typ {
	case "skill":
		obj, err := client.GetTyped(ctx, c, v1alpha1.KindSkill, ref.Namespace, ref.Name, tag,
			func() *v1alpha1.Skill { return &v1alpha1.Skill{} })
		if err != nil || obj == nil {
			return fmt.Errorf("fetch skill %q: %w", ref, err)
		}
		if obj.Spec.Source == nil || obj.Spec.Source.Repository == nil || obj.Spec.Source.Repository.URL == "" {
			return fmt.Errorf("skill %q has no source repository URL set", ref)
		}
		repo = obj.Spec.Source.Repository
	}

	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	switch {
	case repo.Commit != "":
		fmt.Printf("Cloning %s @ %s into %s\n", repo.URL, repo.Commit, outDir)
	case repo.Branch != "":
		fmt.Printf("Cloning %s (branch %s) into %s\n", repo.URL, repo.Branch, outDir)
	default:
		fmt.Printf("Cloning %s into %s\n", repo.URL, outDir)
	}
	if _, err := gitutil.NewSource(nil, gitutil.Limits{MaxBytes: bundle.MaxBundleBytes, MaxEntries: bundle.MaxBundleFiles}).Fetch(ctx, "", repo, outDir); err != nil {
		return err
	}
	if repo.Subfolder != "" {
		fmt.Printf("(subfolder hint: %s)\n", repo.Subfolder)
	}
	fmt.Printf("Pulled %s\n", ref)
	return nil
}
