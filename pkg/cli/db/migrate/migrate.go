package migrate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/cli/annotations"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
)

const (
	dbURLEnv       = "AGENT_REGISTRY_DATABASE_URL"
	embeddingsFlag = "embeddings-enabled"
	embeddingsEnv  = "AGENT_REGISTRY_EMBEDDINGS_ENABLED"
)

// flags holds the migrate command's parsed flags.
var flags struct {
	dbURL             string
	embeddingsEnabled bool
}

// migrateCmd holds the *cobra.Command instance built by NewCommand so
// accessors (EmbeddingsEnabledOverride) can introspect parsed flag
// state. nil before NewCommand is called (e.g. in subcommand-level unit
// tests that construct only newUpCmd/newDownCmd), in which case the
// accessors fall through to env.
var migrateCmd *cobra.Command

// NewCommand returns the `migrate` parent command with all subcommands
// attached. Persistent flags (--db-url, --embeddings-enabled) and env
// fallbacks are wired here so each subcommand inherits the same
// connection setup.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply, roll back, and inspect database migrations",
		Long: `Apply, roll back, and inspect database migrations independently of
server startup. Reads ` + dbURLEnv + ` from the environment when --db-url
is omitted; reads ` + embeddingsEnv + ` when --` + embeddingsFlag + ` is omitted.`,
		Annotations: map[string]string{
			annotations.AnnotationSkipTokenResolution: "true",
		},
	}
	cmd.PersistentFlags().StringVar(&flags.dbURL, "db-url",
		"",
		"PostgreSQL connection URL (defaults to value of "+dbURLEnv+" env var)")
	cmd.PersistentFlags().BoolVar(&flags.embeddingsEnabled, embeddingsFlag,
		false,
		"Enable the pgvector-dependent embeddings migration (overrides "+embeddingsEnv+")")

	migrateCmd = cmd
	cmd.AddCommand(newUpCmd())
	cmd.AddCommand(newDownCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newGotoCmd())
	cmd.AddCommand(newForceCmd())
	return cmd
}

// EmbeddingsEnabledOverride returns (value, true) when the user passed
// --embeddings-enabled on the command line and (false, false) otherwise.
// Source-registration BuildConfig closures use the (set==false) signal
// to fall through to the env var so the CLI's view of the embeddings
// flag matches the server's by default.
func EmbeddingsEnabledOverride() (value bool, set bool) {
	if migrateCmd == nil {
		return false, false
	}
	if !migrateCmd.PersistentFlags().Changed(embeddingsFlag) {
		return false, false
	}
	return flags.embeddingsEnabled, true
}

// orderedSource pairs a registered Source with its lazily-evaluated
// MigratorConfig so the CLI can sort by VersionOffset without invoking
// BuildConfig multiple times during sort comparisons.
type orderedSource struct {
	src Source
	cfg database.MigratorConfig
}

// orderedSources returns the registered sources with their BuildConfig
// results, sorted ascending by VersionOffset. CLI subcommands route
// against this order instead of raw registration order so cross-package
// init() ordering (which is not deterministic) can't silently mis-route
// down/goto operations. Errors when two registered sources share a
// VersionOffset — that's a misconfiguration that would otherwise route
// silently to the first match.
func orderedSources() ([]orderedSource, error) {
	srcs := Sources()
	out := make([]orderedSource, len(srcs))
	for i, s := range srcs {
		out[i] = orderedSource{src: s, cfg: s.BuildConfig()}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].cfg.VersionOffset < out[j].cfg.VersionOffset
	})
	for i := 1; i < len(out); i++ {
		if out[i].cfg.VersionOffset == out[i-1].cfg.VersionOffset {
			return nil, fmt.Errorf("misconfigured migration sources: %q and %q share VersionOffset %d", out[i-1].src.Name, out[i].src.Name, out[i].cfg.VersionOffset)
		}
	}
	return out, nil
}

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withConn(cmd.Context(), func(ctx context.Context, conn *pgx.Conn) error {
				// Pre-count via Status so stdout carries the applied count
				// for CI/CD consumers that filter slog output. Sources
				// run in VersionOffset-ascending order so the OSS source
				// (which owns table creation) runs before extensions.
				total := 0
				srcsList, oerr := orderedSources()
				if oerr != nil {
					return oerr
				}
				for _, src := range srcsList {
					m := database.NewMigrator(conn, src.cfg)
					_, pending, err := m.Status(ctx)
					if err != nil {
						return err
					}
					total += len(pending)
					if err := m.Migrate(ctx); err != nil {
						return err
					}
				}
				if total == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "no pending migrations; schema is up to date")
					return nil
				}
				fmt.Fprintf(cmd.OutOrStdout(), "applied %d migration(s); schema is up to date\n", total)
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
				return fmt.Errorf("N must be a positive integer, got %q", args[0])
			}
			return withConn(cmd.Context(), func(ctx context.Context, conn *pgx.Conn) error {
				remaining := n
				// Roll back from the highest-offset source first; its
				// applied migrations are the system's most-recent.
				// orderedSources() sorts by VersionOffset so this is
				// correct regardless of registration order.
				srcs, oerr := orderedSources()
				if oerr != nil {
					return oerr
				}
				for i := len(srcs) - 1; i >= 0 && remaining > 0; i-- {
					m := database.NewMigrator(conn, srcs[i].cfg)
					applied, _, err := m.Status(ctx)
					if err != nil {
						return err
					}
					toRollback := min(remaining, len(applied))
					if toRollback == 0 {
						continue
					}
					if err := m.Down(ctx, toRollback); err != nil {
						return err
					}
					remaining -= toRollback
				}
				if remaining > 0 {
					return fmt.Errorf("cannot roll back %d more migration(s); ran out of applied migrations", remaining)
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
			return withConn(cmd.Context(), func(ctx context.Context, conn *pgx.Conn) error {
				appliedTotal, pendingTotal := 0, 0
				srcsList, oerr := orderedSources()
				if oerr != nil {
					return oerr
				}
				for _, src := range srcsList {
					m := database.NewMigrator(conn, src.cfg)
					applied, pending, err := m.Status(ctx)
					if err != nil {
						return err
					}
					appliedTotal += len(applied)
					pendingTotal += len(pending)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%d migration(s) applied, %d pending\n", appliedTotal, pendingTotal)
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
			return withConn(cmd.Context(), func(ctx context.Context, conn *pgx.Conn) error {
				maxV := 0
				srcsList, oerr := orderedSources()
				if oerr != nil {
					return oerr
				}
				for _, src := range srcsList {
					m := database.NewMigrator(conn, src.cfg)
					v, err := m.CurrentVersion(ctx)
					if err != nil {
						return err
					}
					if v > maxV {
						maxV = v
					}
				}
				fmt.Fprintln(cmd.OutOrStdout(), maxV)
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
V=0 is the special "empty schema" target: every registered source is
rolled back fully (errors with ErrNotReversible if any crossed migration
lacks a .down.sql sibling).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := strconv.Atoi(args[0])
			if err != nil || v < 0 {
				return fmt.Errorf("V must be a non-negative integer, got %q", args[0])
			}
			return withConn(cmd.Context(), func(ctx context.Context, conn *pgx.Conn) error {
				srcs, oerr := orderedSources()
				if oerr != nil {
					return oerr
				}
				if v == 0 {
					// Roll back every source fully, highest-offset first
					// so newer migrations come off before older ones.
					for i := len(srcs) - 1; i >= 0; i-- {
						m := database.NewMigrator(conn, srcs[i].cfg)
						applied, _, err := m.Status(ctx)
						if err != nil {
							return err
						}
						if len(applied) == 0 {
							continue
						}
						if err := m.Down(ctx, len(applied)); err != nil {
							return err
						}
					}
					fmt.Fprintln(cmd.OutOrStdout(), "schema is at version 0 (empty)")
					return nil
				}
				// Build (migrator, low, high) per source so we can route V.
				type srcInfo struct {
					m         *database.Migrator
					low, high int
				}
				infos := make([]srcInfo, len(srcs))
				targetIdx := -1
				for i, src := range srcs {
					mig := database.NewMigrator(conn, src.cfg)
					low, high := sourceBoundsFromConfig(src.cfg)
					infos[i] = srcInfo{m: mig, low: low, high: high}
					// First-match wins (sources are pre-sorted by offset,
					// so this is the lowest-offset match). Empty sources
					// report a sentinel `(low, low-1)` range which matches
					// no version, so they're naturally skipped here.
					if targetIdx < 0 && v >= low && v <= high {
						targetIdx = i
					}
				}
				if targetIdx < 0 {
					return fmt.Errorf("version %d is not in any registered source's range (may be filtered out by the Skip predicate)", v)
				}
				// Sources after targetIdx: roll back to floor.
				for i := len(infos) - 1; i > targetIdx; i-- {
					applied, _, err := infos[i].m.Status(ctx)
					if err != nil {
						return err
					}
					if len(applied) == 0 {
						continue
					}
					if err := infos[i].m.Down(ctx, len(applied)); err != nil {
						return err
					}
				}
				// Sources before targetIdx: bring fully forward.
				for i := 0; i < targetIdx; i++ {
					if err := infos[i].m.Migrate(ctx); err != nil {
						return err
					}
				}
				// Target source: MigrateTo(v).
				if err := infos[targetIdx].m.MigrateTo(ctx, v); err != nil {
					return err
				}
				// Re-query so the output reflects actual schema state,
				// not the requested target. Cheap insurance against the
				// MigrateTo contract drifting; an authoritative read also
				// helps operators trust the output as ground truth.
				actual := 0
				for _, src := range srcs {
					m := database.NewMigrator(conn, src.cfg)
					cv, err := m.CurrentVersion(ctx)
					if err != nil {
						return err
					}
					if cv > actual {
						actual = cv
					}
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
The version V should come from a prior failure message; idempotent if the
row already exists.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := strconv.Atoi(args[0])
			if err != nil || v < 1 {
				return fmt.Errorf("V must be a positive integer, got %q", args[0])
			}
			return withConn(cmd.Context(), func(ctx context.Context, conn *pgx.Conn) error {
				srcsList, oerr := orderedSources()
				if oerr != nil {
					return oerr
				}
				for _, src := range srcsList {
					low, high := sourceBoundsFromConfig(src.cfg)
					if v < low || v > high {
						continue
					}
					m := database.NewMigrator(conn, src.cfg)
					if err := m.Force(ctx, v); err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "version %d marked as applied\n", v)
					return nil
				}
				return fmt.Errorf("version %d is not in any registered source's range (may be filtered out by the Skip predicate)", v)
			})
		},
	}
}

// withConn opens a pgx connection from --db-url (or env), runs fn, and
// closes the connection. Centralizes the DSN resolution + error wrapping
// so each subcommand stays focused on its operation.
func withConn(ctx context.Context, fn func(ctx context.Context, conn *pgx.Conn) error) error {
	dsn := strings.TrimSpace(flags.dbURL)
	if dsn == "" {
		dsn = os.Getenv(dbURLEnv)
	}
	if dsn == "" {
		return fmt.Errorf("database URL not set; pass --db-url or set %s", dbURLEnv)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() {
		cerr := conn.Close(ctx)
		if cerr == nil {
			return
		}
		// Context cancellation / deadline produce close errors that
		// reflect the cancel signal rather than a real fault — don't
		// noisy-log either of them.
		if errors.Is(cerr, context.Canceled) || errors.Is(cerr, context.DeadlineExceeded) {
			return
		}
		// Connection close errors after the operation succeeded
		// shouldn't mask the operation's result; log via stderr.
		fmt.Fprintf(os.Stderr, "warning: failed to close db connection: %v\n", cerr)
	}()
	return fn(ctx, conn)
}

// sourceBoundsFromConfig is a thin wrapper over database.SourceRange so
// the CLI's routing math goes through the same path as the Migrator's
// internal range checks. Kept as a local name so future refactors that
// add CLI-specific concerns (Skip overrides, etc.) land here.
func sourceBoundsFromConfig(cfg database.MigratorConfig) (int, int) {
	return database.SourceRange(cfg)
}
