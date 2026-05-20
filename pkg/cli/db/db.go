// Package db hosts the `arctl db` parent command and its subcommands.
// Currently only `migrate` is wired; future siblings (`db dump`,
// `db reset`, `db ping`) attach here.
package db

import (
	"github.com/spf13/cobra"

	"github.com/agentregistry-dev/agentregistry/pkg/cli/db/migrate"
)

// NewCommand returns the `db` parent command with `migrate` attached.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database operations (migrations, inspection)",
	}
	cmd.AddCommand(migrate.NewCommand())
	return cmd
}
