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

const (
	dbURLEnv   = "AGENT_REGISTRY_DATABASE_URL"
	sourceFlag = "source"
)

var flags struct {
	dbURL  string
	source string
}

// NewCommand returns the `migrate` parent command with all
// subcommands attached.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply, roll back, and inspect database migrations",
		Long: `Apply, roll back, and inspect database migrations independently
of server startup. Reads ` + dbURLEnv + ` from the environment when
--db-url is omitted.

When more than one migration source is registered, per-source
operations (down, goto, force, version) require --source. up and
status aggregate across all registered sources without a flag.`,
		Annotations: map[string]string{
			annotations.AnnotationSkipTokenResolution: "true",
		},
	}
	cmd.PersistentFlags().StringVar(&flags.dbURL, "db-url", "",
		"PostgreSQL connection URL (defaults to value of "+dbURLEnv+" env var)")
	cmd.PersistentFlags().StringVar(&flags.source, sourceFlag, "",
		"Migration source name (required for per-source ops when more than one source is registered)")

	cmd.AddCommand(newUpCmd())
	cmd.AddCommand(newDownCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newGotoCmd())
	cmd.AddCommand(newForceCmd())
	return cmd
}

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

// resolveSource picks the source for a per-source operation. With one
// source registered it's returned directly; with more than one the
// operator must pass --source and we report the registered set when
// they don't.
func resolveSource() (Source, error) {
	srcs := Sources()
	if len(srcs) == 0 {
		return Source{}, errors.New("no migration sources registered")
	}
	if len(srcs) == 1 {
		if flags.source != "" && flags.source != srcs[0].Name {
			return Source{}, fmt.Errorf("--source %q not registered; registered source: %s", flags.source, srcs[0].Name)
		}
		return srcs[0], nil
	}
	if flags.source == "" {
		return Source{}, fmt.Errorf("registered sources: %s; pass --source", sourceNames(srcs))
	}
	for _, s := range srcs {
		if s.Name == flags.source {
			return s, nil
		}
	}
	return Source{}, fmt.Errorf("--source %q not registered; registered sources: %s", flags.source, sourceNames(srcs))
}

func sourceNames(srcs []Source) string {
	names := make([]string, len(srcs))
	for i, s := range srcs {
		names[i] = s.Name
	}
	return strings.Join(names, ", ")
}

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

// readVersion returns the highest applied version for mg, treating
// ErrNilVersion (nothing applied) as 0.
func readVersion(mg *migrate.Migrate) (uint, error) {
	v, _, err := mg.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, nil
		}
		return 0, fmt.Errorf("read version: %w", err)
	}
	return v, nil
}

// countSourceFiles returns the number of NNN_name.up.sql files in
// src.Files/src.Dir.
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

// upSnapshot tracks (src, mg, preVersion) for cross-track rollback.
// The migrator is held open until the loop finishes so a rollback
// after a later-track failure can call RollbackToVersion on a still-
// initialized handle.
type upSnapshot struct {
	src        Source
	mg         *migrate.Migrate
	preVersion uint
}

// closeUpSnapshots closes every snapshot's migrator. Best-effort; any
// close errors are logged via stderr but don't bubble up because the
// op succeeded or failed already.
func closeUpSnapshots(snaps []upSnapshot) {
	for _, s := range snaps {
		srcErr, dbErr := s.mg.Close()
		if srcErr != nil {
			fmt.Fprintf(os.Stderr, "warning: closing %s migrator source: %v\n", s.src.Name, srcErr)
		}
		if dbErr != nil {
			fmt.Fprintf(os.Stderr, "warning: closing %s migrator db: %v\n", s.src.Name, dbErr)
		}
	}
}

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations across every registered source",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if flags.source != "" {
				return errors.New("up aggregates across all registered sources; --source is not applicable")
			}
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			srcs := Sources()
			if len(srcs) == 0 {
				return errors.New("no migration sources registered")
			}

			// Snapshot pre-versions across all sources; iterate in
			// registration order. On failure of source N, roll
			// sources 0..N-1 back to their pre-versions so the whole
			// `up` is atomic across tracks.
			snaps := make([]upSnapshot, 0, len(srcs))
			defer func() { closeUpSnapshots(snaps) }()

			for _, src := range srcs {
				mg, err := src.NewMigrator(dsn)
				if err != nil {
					return fmt.Errorf("construct %s migrator: %w", src.Name, err)
				}
				preVersion, err := readVersion(mg)
				if err != nil {
					_, _ = mg.Close()
					return fmt.Errorf("read pre-version for %s: %w", src.Name, err)
				}
				snaps = append(snaps, upSnapshot{src: src, mg: mg, preVersion: preVersion})

				if _, runErr := database.RunUpWithRecovery(mg, src.Name); runErr != nil {
					// Best-effort cross-track rollback. Sources whose
					// migrations have reversible .down.sql files
					// (e.g. downstream extensions with real downs)
					// will roll back; sources whose .down.sql files
					// raise-exception (the OSS source's up-only
					// pattern) will fail to roll back and the warning
					// directs the operator to inspect that track. The
					// caller is left with a non-atomic state across
					// tracks in that case — surface explicitly rather
					// than silently swallow.
					for i := len(snaps) - 2; i >= 0; i-- {
						prior := snaps[i]
						if rbErr := database.RollbackToVersion(prior.mg, prior.src.Name, prior.preVersion); rbErr != nil {
							fmt.Fprintf(os.Stderr,
								"warning: rolling %s back to its pre-up version failed (likely up-only migrations); %s is left at its post-up state and may need manual reconciliation: %v\n",
								prior.src.Name, prior.src.Name, rbErr)
						}
					}
					return runErr
				}
			}

			// Aggregate applied count across all sources from
			// post-Up versions.
			totalApplied := 0
			for _, s := range snaps {
				postVersion, err := readVersion(s.mg)
				if err != nil {
					return err
				}
				totalApplied += int(postVersion) - int(s.preVersion)
			}
			if totalApplied == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no pending migrations; schema is up to date")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "applied %d migration(s); schema is up to date\n", totalApplied)
			return nil
		},
	}
}

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down N",
		Short: "Roll back the N most-recent applied migrations for the selected source",
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
			src, err := resolveSource()
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
		Short: "Show how many migrations are applied vs pending across all sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if flags.source != "" {
				return errors.New("status aggregates across all registered sources; --source is not applicable")
			}
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			srcs := Sources()
			if len(srcs) == 0 {
				return errors.New("no migration sources registered")
			}

			type lineRow struct {
				src     Source
				applied int
				pending int
			}
			lines := make([]lineRow, 0, len(srcs))
			appliedTotal, pendingTotal := 0, 0
			for _, src := range srcs {
				total, err := countSourceFiles(src)
				if err != nil {
					return err
				}
				var applied int
				if rerr := withSourceMigrator(src, dsn, func(mg *migrate.Migrate) error {
					v, err := readVersion(mg)
					if err != nil {
						return err
					}
					if int(v) > total {
						// Binary embedded fewer migrations than the
						// DB reports applied — operator is running an
						// older arctl against a DB migrated by a newer
						// build. Warn but don't fail; the operator
						// should reconcile by upgrading the binary.
						fmt.Fprintf(os.Stderr,
							"warning: %s reports version %d but this binary only ships migrations up to %d (older binary against newer DB?)\n",
							src.Name, v, total)
					}
					applied = min(int(v), total)
					return nil
				}); rerr != nil {
					return rerr
				}
				pending := total - applied
				lines = append(lines, lineRow{src: src, applied: applied, pending: pending})
				appliedTotal += applied
				pendingTotal += pending
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%d migration(s) applied, %d pending\n", appliedTotal, pendingTotal)
			if len(lines) > 1 {
				for _, l := range lines {
					fmt.Fprintf(out, "  %s: %d applied, %d pending\n", l.src.Name, l.applied, l.pending)
				}
			}
			return nil
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the highest applied migration version",
		Long: `Print the highest applied migration version.
For a single registered source the value is on one line; multi-source
binaries print one line per source.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			srcs := Sources()
			if len(srcs) == 0 {
				return errors.New("no migration sources registered")
			}
			out := cmd.OutOrStdout()
			if len(srcs) == 1 {
				return withSourceMigrator(srcs[0], dsn, func(mg *migrate.Migrate) error {
					v, err := readVersion(mg)
					if err != nil {
						return err
					}
					fmt.Fprintln(out, v)
					return nil
				})
			}
			for _, src := range srcs {
				if err := withSourceMigrator(src, dsn, func(mg *migrate.Migrate) error {
					v, err := readVersion(mg)
					if err != nil {
						return err
					}
					fmt.Fprintf(out, "%s: %d\n", src.Name, v)
					return nil
				}); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func newGotoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "goto V",
		Short: "Move the selected source's schema to version V",
		Long: `Move the selected source's schema to version V (forward or backward).
V=0 is the special "empty schema" target: every applied migration in
the source is rolled back.`,
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
			src, err := resolveSource()
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
				actual, aerr := readVersion(mg)
				if aerr != nil {
					return aerr
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
		Long: `Used to reconcile the selected source's schema_migrations table
after manual remediation. The version V should come from a prior
failure message.`,
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
			src, err := resolveSource()
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
