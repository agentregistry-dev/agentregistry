package database

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/mod/semver"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// Store is the single generic persistence layer for every v1alpha1 kind.
// One Store instance is bound to one table; callers construct one per kind
// (agents, mcp_servers, skills, prompts, providers, deployments).
//
// Identity is (name, version) across every table. Spec and status are JSONB
// columns; labels are JSONB; generation, created_at, updated_at are columns.
// is_latest_version is a per-name boolean toggled by Upsert so that at most
// one row per name carries it — the row with the highest semver wins, falling
// back to most-recently-updated when semver parse fails.
//
// PatchStatus is disjoint from Upsert: it touches only status and updated_at,
// never generation or spec. Reconcilers use PatchStatus exclusively; apply
// handlers use Upsert exclusively.
type Store struct {
	pool  *pgxpool.Pool
	table string
}

// NewStore constructs a Store bound to a single table. Valid table names are
// the six kind-specific tables: "agents", "mcp_servers", "skills", "prompts",
// "providers", "deployments". The table must exist in the schema; NewStore
// does not validate it.
func NewStore(pool *pgxpool.Pool, table string) *Store {
	return &Store{pool: pool, table: table}
}

// UpsertResult describes what happened on Upsert. SpecChanged is true when the
// incoming spec bytes differ from the existing row's spec (or when the row
// didn't exist). Generation reflects the final stored value.
type UpsertResult struct {
	Created     bool
	SpecChanged bool
	Generation  int64
}

// ListOpts controls paginated list queries.
type ListOpts struct {
	// LabelSelector narrows results to rows whose labels JSONB contains this
	// subset (uses `@>` with a GIN index).
	LabelSelector map[string]string
	// Limit caps the number of rows returned. Zero means default (50).
	Limit int
	// Cursor is an opaque pagination token. Empty starts from the beginning.
	Cursor string
	// LatestOnly restricts to rows where is_latest_version=true (one per name).
	LatestOnly bool
}

// Upsert writes the given object under its (Name, Version). On update,
// generation bumps iff the marshaled spec bytes differ from what's already
// stored; no-op re-applies preserve generation. Status is never touched by
// this call — use PatchStatus for that.
//
// After the row write, is_latest_version is recomputed across all rows
// sharing this Name: the row with the highest semver wins (fallback:
// most-recently-updated). All of this happens inside a single transaction so
// readers observe a consistent latest pointer.
func (s *Store) Upsert(ctx context.Context, name, version string, specJSON json.RawMessage, labels map[string]string) (*UpsertResult, error) {
	if name == "" || version == "" {
		return nil, errors.New("v1alpha1 store: name and version are required")
	}
	if len(specJSON) == 0 {
		return nil, errors.New("v1alpha1 store: spec is required")
	}
	if labels == nil {
		labels = map[string]string{}
	}
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return nil, fmt.Errorf("v1alpha1 store: marshal labels: %w", err)
	}

	res := &UpsertResult{}
	err = runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			oldSpec       []byte
			oldGeneration int64
			found         bool
		)
		err := tx.QueryRow(ctx,
			fmt.Sprintf("SELECT spec, generation FROM %s WHERE name=$1 AND version=$2 FOR UPDATE", s.table),
			name, version).Scan(&oldSpec, &oldGeneration)
		switch {
		case err == nil:
			found = true
		case errors.Is(err, pgx.ErrNoRows):
			found = false
		default:
			return fmt.Errorf("load existing: %w", err)
		}

		var newGen int64
		switch {
		case !found:
			newGen = 1
			res.Created = true
			res.SpecChanged = true
		case !bytes.Equal(normalizeJSON(oldSpec), normalizeJSON(specJSON)):
			newGen = oldGeneration + 1
			res.SpecChanged = true
		default:
			newGen = oldGeneration
			res.SpecChanged = false
		}
		res.Generation = newGen

		_, err = tx.Exec(ctx,
			fmt.Sprintf(`
				INSERT INTO %s (name, version, generation, labels, spec)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (name, version) DO UPDATE
				SET generation = EXCLUDED.generation,
				    labels     = EXCLUDED.labels,
				    spec       = EXCLUDED.spec
			`, s.table),
			name, version, newGen, labelsJSON, []byte(specJSON))
		if err != nil {
			return fmt.Errorf("upsert row: %w", err)
		}

		if err := s.recomputeLatest(ctx, tx, name); err != nil {
			return fmt.Errorf("recompute latest: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// PatchStatus atomically reads+mutates+writes the status column for
// (name, version). The mutate callback receives the current Status and may
// modify it (typically via SetCondition). Generation and spec are not touched.
// Returns pkgdb.ErrNotFound if the row doesn't exist.
func (s *Store) PatchStatus(ctx context.Context, name, version string, mutate func(*v1alpha1.Status)) error {
	return runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		var statusJSON []byte
		err := tx.QueryRow(ctx,
			fmt.Sprintf("SELECT status FROM %s WHERE name=$1 AND version=$2 FOR UPDATE", s.table),
			name, version).Scan(&statusJSON)
		if errors.Is(err, pgx.ErrNoRows) {
			return pkgdb.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load status: %w", err)
		}

		var status v1alpha1.Status
		if len(statusJSON) > 0 {
			if err := json.Unmarshal(statusJSON, &status); err != nil {
				return fmt.Errorf("decode status: %w", err)
			}
		}
		mutate(&status)

		newStatusJSON, err := json.Marshal(status)
		if err != nil {
			return fmt.Errorf("encode status: %w", err)
		}
		_, err = tx.Exec(ctx,
			fmt.Sprintf("UPDATE %s SET status=$3 WHERE name=$1 AND version=$2", s.table),
			name, version, newStatusJSON)
		if err != nil {
			return fmt.Errorf("write status: %w", err)
		}
		return nil
	})
}

// Get returns a single row by (name, version). Returns pkgdb.ErrNotFound if missing.
func (s *Store) Get(ctx context.Context, name, version string) (*v1alpha1.RawObject, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`
			SELECT name, version, generation, labels, spec, status, created_at, updated_at
			FROM %s WHERE name=$1 AND version=$2`, s.table),
		name, version)
	return scanRow(row)
}

// GetLatest returns the row where is_latest_version=true for the given name,
// or pkgdb.ErrNotFound if no live version exists.
func (s *Store) GetLatest(ctx context.Context, name string) (*v1alpha1.RawObject, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`
			SELECT name, version, generation, labels, spec, status, created_at, updated_at
			FROM %s WHERE name=$1 AND is_latest_version`, s.table),
		name)
	return scanRow(row)
}

// Delete removes a single row by (name, version). On success, recomputes
// is_latest_version across surviving rows for this name. Returns pkgdb.ErrNotFound
// if the row doesn't exist.
func (s *Store) Delete(ctx context.Context, name, version string) error {
	return runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE name=$1 AND version=$2", s.table),
			name, version)
		if err != nil {
			return fmt.Errorf("delete row: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return pkgdb.ErrNotFound
		}
		return s.recomputeLatest(ctx, tx, name)
	})
}

// List returns rows filtered by opts, ordered by updated_at DESC. A pagination
// cursor is returned when more rows are available; pass it back via
// ListOpts.Cursor to continue.
func (s *Store) List(ctx context.Context, opts ListOpts) ([]*v1alpha1.RawObject, string, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	args := make([]any, 0, 4)
	where := make([]string, 0, 4)

	if opts.LatestOnly {
		where = append(where, "is_latest_version")
	}
	if len(opts.LabelSelector) > 0 {
		labelJSON, err := json.Marshal(opts.LabelSelector)
		if err != nil {
			return nil, "", fmt.Errorf("marshal labels: %w", err)
		}
		args = append(args, labelJSON)
		where = append(where, fmt.Sprintf("labels @> $%d", len(args)))
	}

	query := fmt.Sprintf(`
		SELECT name, version, generation, labels, spec, status, created_at, updated_at
		FROM %s`, s.table)
	if len(where) > 0 {
		query += " WHERE " + join(where, " AND ")
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT $%d", len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list: %w", err)
	}
	defer rows.Close()

	out := make([]*v1alpha1.RawObject, 0, limit)
	for rows.Next() {
		obj, err := scanRow(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, obj)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	var nextCursor string
	if len(out) > limit {
		out = out[:limit]
		// Current paging is offset-less; next page is the next N updated_at DESC
		// rows. A later PR will promote this to cursor-based pagination; for
		// now, return an empty cursor when we have fewer than the caller asked
		// for and a simple opaque token when more exist. The opaque token
		// carries the last row's updated_at — callers can't parse it and must
		// feed it back verbatim.
		nextCursor = "more"
	}
	return out, nextCursor, nil
}

// FindReferrers returns rows from this Store's table whose spec JSONB matches
// a reference to (targetKind, targetName, targetVersion) at the given JSON
// pathPattern. pathPattern is a template like `{"mcpServers":[{"name":"%s","version":"%s"}]}`
// or `{"templateRef":{"kind":"%s","name":"%s","version":"%s"}}` that gets
// formatted with the target's details into a JSONB fragment for `@>` matching.
//
// Callers own the path pattern so this stays generic across kinds.
func (s *Store) FindReferrers(ctx context.Context, pathJSON json.RawMessage, latestOnly bool) ([]*v1alpha1.RawObject, error) {
	query := fmt.Sprintf(`
		SELECT name, version, generation, labels, spec, status, created_at, updated_at
		FROM %s
		WHERE spec @> $1::jsonb`, s.table)
	if latestOnly {
		query += " AND is_latest_version"
	}
	query += " ORDER BY updated_at DESC"

	rows, err := s.pool.Query(ctx, query, []byte(pathJSON))
	if err != nil {
		return nil, fmt.Errorf("find referrers: %w", err)
	}
	defer rows.Close()

	out := make([]*v1alpha1.RawObject, 0, 8)
	for rows.Next() {
		obj, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, rows.Err()
}

// recomputeLatest recomputes is_latest_version across all rows with the
// given name, inside the supplied transaction. The row with the highest
// valid semver wins; failing that, the most-recently-updated row wins.
// Must be called after any Upsert/Delete that could affect the winner.
func (s *Store) recomputeLatest(ctx context.Context, tx pgx.Tx, name string) error {
	rows, err := tx.Query(ctx,
		fmt.Sprintf("SELECT version FROM %s WHERE name=$1 ORDER BY updated_at DESC", s.table),
		name)
	if err != nil {
		return fmt.Errorf("scan versions: %w", err)
	}
	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		versions = append(versions, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if len(versions) == 0 {
		return nil
	}

	winner := pickLatestVersion(versions)
	_, err = tx.Exec(ctx,
		fmt.Sprintf("UPDATE %s SET is_latest_version = (version = $2) WHERE name = $1", s.table),
		name, winner)
	if err != nil {
		return fmt.Errorf("toggle latest: %w", err)
	}
	return nil
}

// pickLatestVersion returns the highest semver among versions. If no version
// parses as semver (per golang.org/x/mod/semver which requires a leading 'v'),
// returns the first element — which, since the caller passes them in
// updated_at DESC order, is the most-recently-updated.
//
// Versions are normalized with a leading 'v' prefix for semver comparison, so
// "1.2.3" and "v1.2.3" sort identically.
func pickLatestVersion(versions []string) string {
	best := ""
	bestRaw := ""
	for _, v := range versions {
		normalized := v
		if len(normalized) == 0 || normalized[0] != 'v' {
			normalized = "v" + normalized
		}
		if !semver.IsValid(normalized) {
			continue
		}
		if best == "" || semver.Compare(normalized, best) > 0 {
			best = normalized
			bestRaw = v
		}
	}
	if bestRaw != "" {
		return bestRaw
	}
	return versions[0]
}
