package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/spf13/cobra"

	"github.com/agentregistry-dev/agentregistry/pkg/cli/annotations"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/orchestrator"
)

const (
	dbURLEnv   = "AGENT_REGISTRY_DATABASE_URL"
	sourceFlag = "source"
)

var flags struct {
	dbURL  string
	source string
}

// migrateCmd holds the parent cobra.Command after NewCommand
// constructs it. EnableSourceSelection writes additional flags onto
// this reference for downstream multi-source binaries; nil before
// NewCommand has been called.
var migrateCmd *cobra.Command

// NewCommand returns the `migrate` parent command with all
// subcommands attached. The `--source` flag is intentionally NOT
// wired by default — single-source binaries never need it. Downstream
// binaries that register a second migration source call
// EnableSourceSelection after Register to expose the flag.
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
	migrateCmd = cmd
	return cmd
}

// EnableSourceSelection wires the `--source` persistent flag onto
// the `migrate` parent command. Intended for downstream binaries
// that register more than one migration source via Register —
// single-source binaries leave the flag off and let resolveSource
// infer the sole source.
//
// Behavior with the flag wired:
//   - `up` and `status` reject --source as "not applicable" (they
//     aggregate across every registered source).
//   - `down`, `goto`, `force` require --source when more than one
//     source is registered; --source is otherwise inferred to the
//     sole registered source.
//   - `version` accepts --source as a filter; without it, prints
//     one line per registered source.
//
// Must be called AFTER NewCommand has constructed the parent (the
// function panics otherwise) and AFTER all Register calls have run
// — practically, near the end of the downstream binary's `init()`
// or early in its main(), once both the OSS and downstream sources
// have registered themselves.
//
// Idempotent: repeat calls after the flag is already wired no-op.
// Tests and downstream init() that may run more than once in the
// same process are safe.
func EnableSourceSelection() {
	if migrateCmd == nil {
		panic("migrate.EnableSourceSelection: called before NewCommand")
	}
	if migrateCmd.PersistentFlags().Lookup(sourceFlag) != nil {
		return
	}
	migrateCmd.PersistentFlags().StringVar(&flags.source, sourceFlag, "",
		"Migration source name (required for per-source ops when more than one source is registered)")
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

func withSourceMigrator(ctx context.Context, src Source, dsn string, fn func(mg *migrate.Migrate) error) error {
	mg, err := database.NewMigrator(ctx, dsn, src.Files, src.Dir, src.Schema)
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

// readVersion returns the highest applied version for mg and whether
// the schema_migrations row is dirty (mid-failed-migration). Callers
// that don't need the dirty flag pass _ — but every display path
// should surface it, since a dirty schema is a real operator signal
// go-migrate gives us for free.
//
// ErrNilVersion (nothing applied) is normalized to (0, false, nil).
func readVersion(mg *migrate.Migrate) (uint, bool, error) {
	v, dirty, err := mg.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("read version: %w", err)
	}
	return v, dirty, nil
}

// sourceFileVersions returns the (ascending-sorted) NNN versions
// parsed from every NNN_name.up.sql file in src.Files/src.Dir.
//
// The set isn't required to be contiguous — gaps (e.g. a deleted
// 005) are real and we treat the missing numbers as
// not-applicable-to-this-binary. status/desync math goes via this
// list so the count of files and the highest version stay distinct.
func sourceFileVersions(src Source) ([]int, error) {
	entries, err := fs.ReadDir(src.Files, src.Dir)
	if err != nil {
		return nil, fmt.Errorf("read migration dir %s: %w", src.Dir, err)
	}
	var versions []int
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
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		versions = append(versions, v)
	}
	slices.Sort(versions)
	return versions, nil
}

// lineRow carries per-source status data through the status command's
// text and JSON output paths.
type lineRow struct {
	src        Source
	applied    int
	pending    int
	dbVersion  int  // raw DB version for desync reporting
	downgraded bool // dbVersion > total
	dirty      bool // mid-failed-migration; surfaced as a (dirty) annotation
}

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations across every registered source",
		Long: `Applies pending migrations for every registered source in
registration order. Per source, the orchestrator acquires a
pg_advisory_lock so concurrent pods serialize, then runs
Steps(1) → LegacyRun (if defined) → Up().

The --source flag is intentionally not applicable to up; pass it only
on the per-source subcommands (down/goto/force).`,
		Args: cobra.NoArgs,
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

			ctx := cmd.Context()
			// Pre-snapshot pending counts across all sources so we
			// can report "applied N migration(s)" after orchestrator
			// success. The snapshot reads version + counts files;
			// gaps in the file sequence (e.g. a deleted v5) are
			// handled by the same `sourceFileVersions` primitive used
			// by `status`.
			prePending := 0
			for _, src := range srcs {
				p, err := pendingCount(ctx, src, dsn)
				if err != nil {
					return err
				}
				prePending += p
			}

			if err := orchestrator.RunUp(ctx, dsn, srcs); err != nil {
				return err
			}

			if prePending == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no pending migrations; schema is up to date")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "applied %d migration(s); schema is up to date\n", prePending)
			return nil
		},
	}
}

// pendingCount counts NNN_*.up.sql files whose version is greater
// than the source's current applied version. Uses the same
// `sourceFileVersions` primitive as `status` so the two paths can't
// drift.
func pendingCount(ctx context.Context, src Source, dsn string) (int, error) {
	versions, err := sourceFileVersions(src)
	if err != nil {
		return 0, err
	}
	var pending int
	err = withSourceMigrator(ctx, src, dsn, func(mg *migrate.Migrate) error {
		v, _, verr := readVersion(mg)
		if verr != nil {
			return verr
		}
		for _, fv := range versions {
			if uint(fv) > v {
				pending++
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return pending, nil
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
			return withSourceMigrator(cmd.Context(), src, dsn, func(mg *migrate.Migrate) error {
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
	var output string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show how many migrations are applied vs pending across all sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Dead in OSS-only — flag is unregistered, value stays ""
			// — but live once EnableSourceSelection wires --source for
			// a downstream multi-source build. See newUpCmd.
			if flags.source != "" {
				return errors.New("status aggregates across all registered sources; --source is not applicable")
			}
			if output != "text" && output != "json" {
				return fmt.Errorf("invalid --output %q; supported: text, json", output)
			}
			dsn, err := resolveDSN()
			if err != nil {
				return err
			}
			srcs := Sources()
			if len(srcs) == 0 {
				return errors.New("no migration sources registered")
			}

			lines := make([]lineRow, 0, len(srcs))
			appliedTotal, pendingTotal := 0, 0
			// Multi-source binaries surface a per-source desync line
			// on stdout (see the breakdown loop below). Single-source
			// binaries don't print that breakdown, so the stderr
			// warning is their only signal — emit only when there's
			// no stdout breakdown that would carry it.
			multiSource := len(srcs) > 1
			for _, src := range srcs {
				versions, err := sourceFileVersions(src)
				if err != nil {
					return err
				}
				maxFileVersion := 0
				if len(versions) > 0 {
					maxFileVersion = versions[len(versions)-1]
				}
				var applied, dbVersion int
				var downgraded, dirty bool
				if rerr := withSourceMigrator(cmd.Context(), src, dsn, func(mg *migrate.Migrate) error {
					v, d, err := readVersion(mg)
					if err != nil {
						return err
					}
					dbVersion = int(v)
					dirty = d
					// applied counts files whose version is ≤ dbVersion;
					// pending counts the rest. This survives gaps in
					// the version sequence (e.g. a deleted v5) where
					// `count(files)` and `max(version)` diverge.
					for _, fv := range versions {
						if fv <= dbVersion {
							applied++
						}
					}
					if dbVersion > maxFileVersion {
						// Binary's highest shipped version is below the
						// DB's recorded version — operator is running an
						// older arctl against a DB migrated by a newer
						// build. Warn but don't fail; the operator
						// should reconcile by upgrading the binary.
						downgraded = true
						if !multiSource {
							fmt.Fprintf(os.Stderr,
								"warning: %s reports version %d but this binary's highest shipped migration is v%d (older binary against newer DB?)\n",
								src.Name, v, maxFileVersion)
						}
					}
					return nil
				}); rerr != nil {
					return rerr
				}
				pending := len(versions) - applied
				lines = append(lines, lineRow{src: src, applied: applied, pending: pending, dbVersion: dbVersion, downgraded: downgraded, dirty: dirty})
				appliedTotal += applied
				pendingTotal += pending
			}

			out := cmd.OutOrStdout()
			if output == "json" {
				return writeStatusJSON(out, lines, appliedTotal, pendingTotal)
			}
			if multiSource {
				fmt.Fprintf(out, "%d migration(s) applied, %d pending\n", appliedTotal, pendingTotal)
				// Same `multiSource` used to gate the stderr desync
				// warning above — both signals live and die together so
				// future "skip-this-source" branches can't desync them.
				for _, l := range lines {
					if l.downgraded {
						// "db reports v" carries the version; no
						// redundant "at v" needed.
						fmt.Fprintf(out, "  %s: %d applied, %d pending (db reports v%d%s — binary out of date)\n",
							l.src.Name, l.applied, l.pending, l.dbVersion, dirtyTag(l.dirty))
					} else {
						fmt.Fprintf(out, "  %s: %d applied (at v%d%s), %d pending\n",
							l.src.Name, l.applied, l.dbVersion, dirtyTag(l.dirty), l.pending)
					}
				}
			} else {
				// Single-source: fold the version into the headline
				// line so operators don't have to run `version`
				// separately. dbVersion is the raw schema_migrations
				// value (matches `force V` semantics); applied is the
				// count capped at the file count, surfacing a desync
				// when those numbers differ.
				l := lines[0]
				fmt.Fprintf(out, "%d migration(s) applied (at v%d%s), %d pending\n",
					l.applied, l.dbVersion, dirtyTag(l.dirty), l.pending)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "text",
		`Output format: "text" (default) or "json"`)
	return cmd
}

// statusJSON is the wire format for `arctl db migrate status -o json`.
// Stable contract — operators may consume it from CI/CD shell scripts
// via `jq`.
type statusJSON struct {
	Applied int                `json:"applied"`
	Pending int                `json:"pending"`
	Sources []statusSourceJSON `json:"sources"`
}

type statusSourceJSON struct {
	Name       string `json:"name"`
	Applied    int    `json:"applied"`
	Pending    int    `json:"pending"`
	Version    int    `json:"version"`
	Downgraded bool   `json:"downgraded"`
	Dirty      bool   `json:"dirty"`
}

func writeStatusJSON(out io.Writer, lines []lineRow, appliedTotal, pendingTotal int) error {
	payload := statusJSON{
		Applied: appliedTotal,
		Pending: pendingTotal,
		Sources: make([]statusSourceJSON, 0, len(lines)),
	}
	for _, l := range lines {
		payload.Sources = append(payload.Sources, statusSourceJSON{
			Name:       l.src.Name,
			Applied:    l.applied,
			Pending:    l.pending,
			Version:    l.dbVersion,
			Downgraded: l.downgraded,
			Dirty:      l.dirty,
		})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// dirtyTag returns " (dirty)" when the source is mid-failed-migration,
// "" otherwise. Used to annotate the version in status/version text
// output without adding a separate line.
func dirtyTag(dirty bool) string {
	if dirty {
		return " (dirty)"
	}
	return ""
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the highest applied migration version",
		Long: `Print the highest applied migration version.
For a single registered source the value is on one line; multi-source
binaries print one line per source. When multiple sources are
registered, --source filters to a single track.`,
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
			// --source filters the output even though version is
			// otherwise an aggregate op. Empty flag = print all.
			if flags.source != "" {
				picked := -1
				for i, s := range srcs {
					if s.Name == flags.source {
						picked = i
						break
					}
				}
				if picked < 0 {
					return fmt.Errorf("--source %q not registered; registered sources: %s", flags.source, sourceNames(srcs))
				}
				srcs = []Source{srcs[picked]}
			}
			out := cmd.OutOrStdout()
			if len(srcs) == 1 {
				return withSourceMigrator(cmd.Context(), srcs[0], dsn, func(mg *migrate.Migrate) error {
					v, dirty, err := readVersion(mg)
					if err != nil {
						return err
					}
					fmt.Fprintf(out, "%d%s\n", v, dirtyTag(dirty))
					return nil
				})
			}
			for _, src := range srcs {
				if err := withSourceMigrator(cmd.Context(), src, dsn, func(mg *migrate.Migrate) error {
					v, dirty, err := readVersion(mg)
					if err != nil {
						return err
					}
					fmt.Fprintf(out, "%s: %d%s\n", src.Name, v, dirtyTag(dirty))
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
			return withSourceMigrator(cmd.Context(), src, dsn, func(mg *migrate.Migrate) error {
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
				actual, _, aerr := readVersion(mg)
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
			return withSourceMigrator(cmd.Context(), src, dsn, func(mg *migrate.Migrate) error {
				if err := mg.Force(v); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "version %d marked as applied\n", v)
				return nil
			})
		},
	}
}
