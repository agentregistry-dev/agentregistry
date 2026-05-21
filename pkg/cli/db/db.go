// Package db hosts the `arctl db` parent command and its subcommands.
// Currently only `migrate` is wired; future siblings (`db dump`,
// `db reset`, `db ping`) attach here.
package db

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/agentregistry-dev/agentregistry/pkg/cli/db/migrate"
)

// NewCommand returns the `db` parent command with `migrate` attached.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database operations (migrations, inspection)",
	}
	cmd.AddCommand(migrate.NewCommand())

	// Hide --registry-url and --registry-token from help across the
	// entire `db` subtree. They're persistent flags on the arctl root
	// (pkg/cli/root.go:133), so cobra inherits them onto every
	// subcommand. The db commands talk to Postgres directly via
	// --db-url; pkg/cli/root.go's preRunSkipCommands maps
	// "arctl"->"db"->true, which short-circuits the PersistentPreRunE
	// that would otherwise resolve a registry token. The flags are
	// accepted by cobra and ignored at runtime — hiding them in
	// --help removes the misleading affordance.
	//
	// Hidden is a property of the flag itself (shared across the
	// whole tree), so we can't flip it permanently. The HelpFunc
	// override toggles it for the duration of the help render and
	// restores it after. Children of `db` that don't set their own
	// HelpFunc walk the parent chain and pick this one up.
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		for _, name := range []string{"registry-url", "registry-token"} {
			if f := c.InheritedFlags().Lookup(name); f != nil {
				f.Hidden = true
				defer func(f *pflag.Flag) { f.Hidden = false }(f)
			}
		}
		c.Root().HelpFunc()(c, args)
	})

	return cmd
}
