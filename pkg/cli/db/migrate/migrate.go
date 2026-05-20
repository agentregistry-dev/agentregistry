package migrate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/spf13/cobra"

	"github.com/agentregistry-dev/agentregistry/pkg/cli/annotations"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

const dbURLEnv = "AGENT_REGISTRY_DATABASE_URL"

// flags holds the migrate command's parsed flags.
var flags struct {
	dbURL string
}

// NewCommand returns the `migrate` parent command with all
// subcommands attached.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply, roll back, and inspect database migrations",
		Long: `Apply, roll back, and inspect database migrations independently
of server startup. Reads ` + dbURLEnv + ` from the environment when
--db-url is omitted.`,
		Annotations: map[string]string{
			annotations.AnnotationSkipTokenResolution: "true",
		},
	}
	cmd.PersistentFlags().StringVar(&flags.dbURL, "db-url", "",
		"PostgreSQL connection URL (defaults to value of "+dbURLEnv+" env var)")

	cmd.AddCommand(newUpCmd())
	cmd.AddCommand(newDownCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newGotoCmd())
	cmd.AddCommand(newForceCmd())
	return cmd
}

// resolveDSN returns the DSN from --db-url or the env fallback.
func resolveDSN() (string, error) {
	dsn := strings.TrimSpace(flags.dbURL)
	if dsn == "" {
		dsn = os.Getenv(dbURLEnv)
	}
	if dsn == "" {
		return "", fmt.Errorf("database URL not set; pass --db-url or set %s", dbURLEnv)
	}
	return dsn, nil
}

// withSourceMigrator opens a *migrate.Migrate for src, runs fn, and
// closes the migrator. Centralizes the open/close discipline so each
// subcommand stays focused on its operation.
func withSourceMigrator(src Source, dsn string, fn func(mg *migrate.Migrate) error) error {
	mg, err := src.NewMigrator(dsn)
	if err != nil {
		return fmt.Errorf("construct %s migrator: %w", src.Name, err)
	}
	defer func() {
		srcErr, dbErr := mg.Close()
		if srcErr != nil {
			fmt.Fprintf(os.Stderr, "warning: closing %s migrator source: %v\n", src.Name, srcErr)
		}
		if dbErr != nil {
			fmt.Fprintf(os.Stderr, "warning: closing %s migrator db: %v\n", src.Name, dbErr)
		}
	}()
	return fn(mg)
}

// soleSource returns the single registered source or an error
// instructing the operator to pass --source (which arrives in a
// later commit). Until --source lands, multi-source binaries are an
// explicit unsupported configuration.
func soleSource() (Source, error) {
	srcs := Sources()
	if len(srcs) == 0 {
		return Source{}, errors.New("no migration sources registered")
	}
	if len(srcs) > 1 {
		names := make([]string, 0, len(srcs))
		for _, s := range srcs {
			names = append(names, s.Name)
		}
		return Source{}, fmt.Errorf("multiple migration sources registered (%s); --source selection is a forthcoming feature", strings.Join(names, ", "))
	}
	return srcs[0], nil
}

// countSourceFiles returns the number of NNN_name.up.sql files in
// src.Files/src.Dir. Used by status to compute "pending = total - applied"
// without piercing migrate.Migrate's source-handle internals.
func countSourceFiles(src Source) (int, error) {
	entries, err := fs.ReadDir(src.Files, src.Dir)
	if err != nil {
		return 0, fmt.Errorf("read migration dir %s: %w", src.Dir, err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		// Sanity: must parse as NNN_*.
		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 {
			continue
		}
		if _, err := strconv.Atoi(parts[0]); err != nil {
			continue
		}
		n++
	}
	return n, nil
}

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			src, err := soleSource()
			if err != nil {
				return err
			}
			return withSourceMigrator(src, dsn, func(mg *migrate.Migrate) error {
				preVersion, runErr := database.RunUpWithRecovery(mg, src.Name)
				if runErr != nil {
					return runErr
				}
				postVersion, _, vErr := mg.Version()
				if vErr != nil && !errors.Is(vErr, migrate.ErrNilVersion) {
					return fmt.Errorf("read post-up version: %w", vErr)
				}
				if errors.Is(vErr, migrate.ErrNilVersion) {
					postVersion = 0
				}
				applied := int(postVersion) - int(preVersion)
				if applied == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no pending migrations; schema is up to date")
					return nil
				}
				fmt.Fprintf(cmd.OutOrStdout(), "applied %d migration(s); schema is up to date\n", applied)
				return nil
			})
		},
	}
}

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down N",
		Short: "Roll back the N most-recent applied migrations",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[0])
			if err != nil || n < 1 {
				return fmt.Errorf("expected a positive integer for N, got %q", args[0])
			}
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			src, err := soleSource()
			if err != nil {
				return err
			}
			return withSourceMigrator(src, dsn, func(mg *migrate.Migrate) error {
				if err := mg.Steps(-n); err != nil {
					if errors.Is(err, migrate.ErrNoChange) {
						fmt.Fprintln(cmd.OutOrStdout(), "no migrations to roll back")
						return nil
					}
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "rolled back %d migration(s)\n", n)
				return nil
			})
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show how many migrations are applied vs pending",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			src, err := soleSource()
			if err != nil {
				return err
			}
			total, err := countSourceFiles(src)
			if err != nil {
				return err
			}
			return withSourceMigrator(src, dsn, func(mg *migrate.Migrate) error {
				current, _, vErr := mg.Version()
				if vErr != nil && !errors.Is(vErr, migrate.ErrNilVersion) {
					return fmt.Errorf("read version: %w", vErr)
				}
				if errors.Is(vErr, migrate.ErrNilVersion) {
					current = 0
				}
				applied := int(current)
				if applied > total {
					applied = total
				}
				pending := total - applied
				fmt.Fprintf(cmd.OutOrStdout(), "%d migration(s) applied, %d pending\n", applied, pending)
				return nil
			})
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the highest applied migration version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			src, err := soleSource()
			if err != nil {
				return err
			}
			return withSourceMigrator(src, dsn, func(mg *migrate.Migrate) error {
				current, _, vErr := mg.Version()
				if vErr != nil && !errors.Is(vErr, migrate.ErrNilVersion) {
					return fmt.Errorf("read version: %w", vErr)
				}
				if errors.Is(vErr, migrate.ErrNilVersion) {
					current = 0
				}
				fmt.Fprintln(cmd.OutOrStdout(), current)
				return nil
			})
		},
	}
}

func newGotoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "goto V",
		Short: "Move the schema to version V (forward or backward)",
		Long: `Move the schema to version V (forward or backward).
V=0 is the special "empty schema" target: every applied migration is
rolled back.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := strconv.Atoi(args[0])
			if err != nil || v < 0 {
				return fmt.Errorf("expected a non-negative integer for V, got %q", args[0])
			}
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			src, err := soleSource()
			if err != nil {
				return err
			}
			return withSourceMigrator(src, dsn, func(mg *migrate.Migrate) error {
				if v == 0 {
					if err := mg.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
						return err
					}
					fmt.Fprintln(cmd.OutOrStdout(), "schema is at version 0 (empty)")
					return nil
				}
				if err := mg.Migrate(uint(v)); err != nil && !errors.Is(err, migrate.ErrNoChange) {
					return err
				}
				actual, _, vErr := mg.Version()
				if vErr != nil && !errors.Is(vErr, migrate.ErrNilVersion) {
					return fmt.Errorf("read version: %w", vErr)
				}
				if errors.Is(vErr, migrate.ErrNilVersion) {
					actual = 0
				}
				fmt.Fprintf(cmd.OutOrStdout(), "schema is at version %d\n", actual)
				return nil
			})
		},
	}
}

func newForceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "force V",
		Short: "Mark version V as applied without running its SQL",
		Long: `Used to reconcile schema_migrations after manual remediation.
The version V should come from a prior failure message.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := strconv.Atoi(args[0])
			if err != nil || v < 1 {
				return fmt.Errorf("expected a positive integer for V, got %q", args[0])
			}
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			src, err := soleSource()
			if err != nil {
				return err
			}
			return withSourceMigrator(src, dsn, func(mg *migrate.Migrate) error {
				if err := mg.Force(v); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "version %d marked as applied\n", v)
				return nil
			})
		},
	}
}

