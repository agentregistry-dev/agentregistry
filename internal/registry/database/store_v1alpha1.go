package database

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/mod/semver"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// Store is the single generic persistence layer for every v1alpha1 kind.
// One Store instance is bound to one table; callers construct one per kind
// (v1alpha1.agents, v1alpha1.mcp_servers, etc.).
//
// Identity is (namespace, name, version) across every table. Spec and status
// are JSONB columns; labels and finalizers are JSONB; generation,
// deletion_timestamp, created_at, updated_at are columns.
// is_latest_version is a per-(namespace, name) boolean toggled by Upsert so
// that at most one row per (namespace, name) carries it — the row with the
// highest semver wins, falling back to most-recently-updated when semver
// parse fails.
//
// PatchStatus is disjoint from Upsert: it touches only status and
// updated_at, never generation or spec. Reconcilers use PatchStatus
// exclusively; apply handlers use Upsert exclusively.
//
// Soft delete: Delete sets deletion_timestamp and leaves the row visible
// to GetLatest/Get/List. GC (PurgeFinalized) removes rows where
// deletion_timestamp IS NOT NULL AND finalizers = '[]'.
type Store struct {
	pool  *pgxpool.Pool
	table string
}

// NewStore constructs a Store bound to a single table (e.g.
// "v1alpha1.agents"). The table must exist in the schema; NewStore does
// not validate it.
func NewStore(pool *pgxpool.Pool, table string) *Store {
	return &Store{pool: pool, table: table}
}

// UpsertResult describes what happened on Upsert. SpecChanged is true when
// the incoming spec bytes differ from the existing row's spec (or when the
// row didn't exist). Generation reflects the final stored value.
type UpsertResult struct {
	Created     bool
	SpecChanged bool
	Generation  int64
}

// ErrInvalidCursor reports that a list pagination cursor could not be parsed.
var ErrInvalidCursor = errors.New("v1alpha1 store: invalid cursor")

// ErrInvalidExtraWhere reports that ListOpts.ExtraWhere references more
// placeholders than ExtraArgs has bind values (or vice versa), which
// would either be a silent misuse or a runtime pgx error.
var ErrInvalidExtraWhere = errors.New("v1alpha1 store: ExtraWhere / ExtraArgs placeholder mismatch")

// ListOpts controls paginated list queries.
type ListOpts struct {
	// Namespace narrows results to a specific namespace. Empty means "across
	// all namespaces".
	Namespace string
	// LabelSelector narrows results to rows whose labels JSONB contains
	// this subset (uses `@>` with a GIN index).
	LabelSelector map[string]string
	// Limit caps the number of rows returned. Zero means default (50).
	Limit int
	// Cursor is an opaque pagination token. Empty starts from the beginning.
	Cursor string
	// LatestOnly restricts to rows where is_latest_version=true (one per
	// (namespace, name)).
	LatestOnly bool
	// IncludeTerminating includes rows with deletion_timestamp set. Default
	// false — callers asking for "alive" rows shouldn't see terminating ones.
	IncludeTerminating bool
	// ExtraWhere appends a caller-supplied parameterized SQL predicate to
	// the WHERE clause. It's the RBAC / tenancy / enterprise-filter seam:
	// the generic Store stays kind-agnostic while a wrapper (e.g. the
	// enterprise DatabaseFactory) injects authz-derived constraints like
	// `namespace = ANY($1)`.
	//
	// Rules:
	//   - Placeholders are numbered from `$1` relative to ExtraArgs (so
	//     the fragment reads naturally on its own). The Store rebases them
	//     to continue after its own internal $N before executing.
	//   - The placeholder count in the fragment MUST equal len(ExtraArgs).
	//     List returns ErrInvalidExtraWhere when they disagree.
	//   - NEVER interpolate untrusted input into ExtraWhere with
	//     fmt.Sprintf/string concatenation — always use placeholders with
	//     ExtraArgs. Doing otherwise is a SQL injection; this is the
	//     authz surface.
	//   - The fragment is appended with a leading AND, so a single
	//     standalone predicate like "deleted_by IS NULL" is fine; don't
	//     prefix with "AND " yourself.
	ExtraWhere string
	// ExtraArgs are the bind parameters for ExtraWhere. Number of entries
	// MUST equal the distinct placeholder count in ExtraWhere.
	ExtraArgs []any
}

type listCursor struct {
	UpdatedAt time.Time `json:"updatedAt"`
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
}

// UpsertOpts carries optional knobs on Upsert.
//
// Finalizers is the desired set of finalizer tokens on the row
// post-apply; nil means "leave existing finalizers unchanged", while an
// explicit empty slice means "clear all finalizers".
//
// Annotations is the desired set of annotation key/value pairs on the
// row post-apply; nil means "leave existing annotations unchanged",
// while an explicit empty map means "clear all annotations".
//
// Labels go through Upsert's positional `labels` argument (not here)
// because labels are always fully replaced on apply — they're part of
// the user-authored resource state.
type UpsertOpts struct {
	Finalizers  []string
	Annotations map[string]string
}

// Upsert writes the given object under its (namespace, name, version). On
// update, generation bumps iff the marshaled spec bytes differ from what's
// already stored; no-op re-applies preserve generation. Status is never
// touched by this call — use PatchStatus for that. Finalizers are
// preserved across Upserts unless opts.Finalizers is non-nil.
//
// After the row write, is_latest_version is recomputed across all rows
// sharing this (namespace, name): the row with the highest semver wins
// (fallback: most-recently-updated). Terminating rows (deletion_timestamp
// IS NOT NULL) are excluded from the latest computation. All of this
// happens inside a single transaction.
func (s *Store) Upsert(ctx context.Context, namespace, name, version string, specJSON json.RawMessage, labels map[string]string, opts UpsertOpts) (*UpsertResult, error) {
	if namespace == "" || name == "" || version == "" {
		return nil, errors.New("v1alpha1 store: namespace, name, and version are required")
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
			oldSpec           []byte
			oldGeneration     int64
			oldFinalizersRaw  []byte
			oldAnnotationsRaw []byte
			found             bool
		)
		err := tx.QueryRow(ctx,
			fmt.Sprintf(`
				SELECT spec, generation, finalizers, annotations
				FROM %s
				WHERE namespace=$1 AND name=$2 AND version=$3
				FOR UPDATE`, s.table),
			namespace, name, version).Scan(&oldSpec, &oldGeneration, &oldFinalizersRaw, &oldAnnotationsRaw)
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

		// Finalizer handling: if caller didn't specify, keep the existing
		// set; otherwise use their explicit slice (which may be empty).
		finalizersJSON := oldFinalizersRaw
		if !found {
			finalizersJSON = []byte("[]")
		}
		if opts.Finalizers != nil {
			f, err := json.Marshal(opts.Finalizers)
			if err != nil {
				return fmt.Errorf("marshal finalizers: %w", err)
			}
			finalizersJSON = f
		}

		// Annotation handling mirrors finalizers: nil preserves existing,
		// non-nil (including empty map) replaces.
		annotationsJSON := oldAnnotationsRaw
		if !found {
			annotationsJSON = []byte("{}")
		}
		if opts.Annotations != nil {
			a, err := json.Marshal(opts.Annotations)
			if err != nil {
				return fmt.Errorf("marshal annotations: %w", err)
			}
			annotationsJSON = a
		}

		_, err = tx.Exec(ctx,
			fmt.Sprintf(`
				INSERT INTO %s (namespace, name, version, generation, labels, annotations, spec, finalizers)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				ON CONFLICT (namespace, name, version) DO UPDATE
				SET generation  = EXCLUDED.generation,
				    labels      = EXCLUDED.labels,
				    annotations = EXCLUDED.annotations,
				    spec        = EXCLUDED.spec,
				    finalizers  = EXCLUDED.finalizers
			`, s.table),
			namespace, name, version, newGen, labelsJSON, annotationsJSON, []byte(specJSON), finalizersJSON)
		if err != nil {
			return fmt.Errorf("upsert row: %w", err)
		}

		if err := s.recomputeLatest(ctx, tx, namespace, name); err != nil {
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
// (namespace, name, version). The mutate callback receives the current
// Status and may modify it (typically via SetCondition). Generation and
// spec are not touched. Returns pkgdb.ErrNotFound if the row doesn't exist.
func (s *Store) PatchStatus(ctx context.Context, namespace, name, version string, mutate func(*v1alpha1.Status)) error {
	return runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		var statusJSON []byte
		err := tx.QueryRow(ctx,
			fmt.Sprintf(`
				SELECT status FROM %s
				WHERE namespace=$1 AND name=$2 AND version=$3
				FOR UPDATE`, s.table),
			namespace, name, version).Scan(&statusJSON)
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
			fmt.Sprintf(`
				UPDATE %s SET status=$4
				WHERE namespace=$1 AND name=$2 AND version=$3`, s.table),
			namespace, name, version, newStatusJSON)
		if err != nil {
			return fmt.Errorf("write status: %w", err)
		}
		return nil
	})
}

// PatchFinalizers atomically reads+mutates+writes the finalizers list for
// (namespace, name, version). Used by reconcilers/controllers to add or
// remove a finalizer they own. Returns pkgdb.ErrNotFound if the row
// doesn't exist.
func (s *Store) PatchFinalizers(ctx context.Context, namespace, name, version string, mutate func(finalizers []string) []string) error {
	return runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		var finalizersJSON []byte
		err := tx.QueryRow(ctx,
			fmt.Sprintf(`
				SELECT finalizers FROM %s
				WHERE namespace=$1 AND name=$2 AND version=$3
				FOR UPDATE`, s.table),
			namespace, name, version).Scan(&finalizersJSON)
		if errors.Is(err, pgx.ErrNoRows) {
			return pkgdb.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load finalizers: %w", err)
		}

		var finalizers []string
		if len(finalizersJSON) > 0 {
			if err := json.Unmarshal(finalizersJSON, &finalizers); err != nil {
				return fmt.Errorf("decode finalizers: %w", err)
			}
		}
		finalizers = mutate(finalizers)
		if finalizers == nil {
			finalizers = []string{}
		}

		newJSON, err := json.Marshal(finalizers)
		if err != nil {
			return fmt.Errorf("encode finalizers: %w", err)
		}
		_, err = tx.Exec(ctx,
			fmt.Sprintf(`
				UPDATE %s SET finalizers=$4
				WHERE namespace=$1 AND name=$2 AND version=$3`, s.table),
			namespace, name, version, newJSON)
		if err != nil {
			return fmt.Errorf("write finalizers: %w", err)
		}
		return nil
	})
}

// PatchAnnotations atomically reads+mutates+writes the annotations map for
// (namespace, name, version). The mutate callback receives the current
// annotations and returns the replacement map. Returning nil clears the map.
// Generation and spec are not touched. Returns pkgdb.ErrNotFound if the row
// doesn't exist.
func (s *Store) PatchAnnotations(ctx context.Context, namespace, name, version string, mutate func(map[string]string) map[string]string) error {
	return runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		var annotationsJSON []byte
		err := tx.QueryRow(ctx,
			fmt.Sprintf(`
				SELECT annotations FROM %s
				WHERE namespace=$1 AND name=$2 AND version=$3
				FOR UPDATE`, s.table),
			namespace, name, version).Scan(&annotationsJSON)
		if errors.Is(err, pgx.ErrNoRows) {
			return pkgdb.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load annotations: %w", err)
		}

		annotations := map[string]string{}
		if len(annotationsJSON) > 0 {
			if err := json.Unmarshal(annotationsJSON, &annotations); err != nil {
				return fmt.Errorf("decode annotations: %w", err)
			}
		}
		if mutate != nil {
			annotations = mutate(annotations)
		}
		if annotations == nil {
			annotations = map[string]string{}
		}

		newAnnotationsJSON, err := json.Marshal(annotations)
		if err != nil {
			return fmt.Errorf("encode annotations: %w", err)
		}

		_, err = tx.Exec(ctx,
			fmt.Sprintf(`
				UPDATE %s
				SET annotations=$4, updated_at=NOW()
				WHERE namespace=$1 AND name=$2 AND version=$3`, s.table),
			namespace, name, version, newAnnotationsJSON)
		if err != nil {
			return fmt.Errorf("update annotations: %w", err)
		}
		return nil
	})
}

// Get returns a single row by (namespace, name, version), including
// terminating rows. Returns pkgdb.ErrNotFound if missing.
func (s *Store) Get(ctx context.Context, namespace, name, version string) (*v1alpha1.RawObject, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`
			SELECT namespace, name, version, generation, labels, annotations, spec, status,
			       deletion_timestamp, finalizers, created_at, updated_at
			FROM %s
			WHERE namespace=$1 AND name=$2 AND version=$3`, s.table),
		namespace, name, version)
	return scanRow(row)
}

// GetLatest returns the row where is_latest_version=true for
// (namespace, name), or pkgdb.ErrNotFound if no live version exists.
// Terminating rows are excluded from the latest computation.
func (s *Store) GetLatest(ctx context.Context, namespace, name string) (*v1alpha1.RawObject, error) {
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`
			SELECT namespace, name, version, generation, labels, annotations, spec, status,
			       deletion_timestamp, finalizers, created_at, updated_at
			FROM %s
			WHERE namespace=$1 AND name=$2 AND is_latest_version`, s.table),
		namespace, name)
	return scanRow(row)
}

// Delete soft-deletes a single row: it sets deletion_timestamp to NOW()
// without removing the row. Callers with finalizers will see the
// terminating row until they remove their finalizer via PatchFinalizers;
// GC (PurgeFinalized) hard-deletes rows whose finalizers slice is empty
// AND deletion_timestamp is set.
//
// On success, recomputes is_latest_version across surviving non-
// terminating rows for this (namespace, name). Returns pkgdb.ErrNotFound
// if the row doesn't exist.
func (s *Store) Delete(ctx context.Context, namespace, name, version string) error {
	return runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			fmt.Sprintf(`
				UPDATE %s SET deletion_timestamp = NOW()
				WHERE namespace=$1 AND name=$2 AND version=$3
				  AND deletion_timestamp IS NULL`, s.table),
			namespace, name, version)
		if err != nil {
			return fmt.Errorf("mark terminating: %w", err)
		}
		if tag.RowsAffected() == 0 {
			// Either the row doesn't exist or it's already terminating.
			// Distinguish so callers get a useful error.
			var exists bool
			err := tx.QueryRow(ctx,
				fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE namespace=$1 AND name=$2 AND version=$3)", s.table),
				namespace, name, version).Scan(&exists)
			if err != nil {
				return fmt.Errorf("check existence: %w", err)
			}
			if !exists {
				return pkgdb.ErrNotFound
			}
			// Already terminating — idempotent delete, no further action.
			return nil
		}
		return s.recomputeLatest(ctx, tx, namespace, name)
	})
}

// PurgeFinalized hard-deletes rows whose deletion_timestamp is set AND
// finalizers slice is empty. Intended to be called by a periodic GC
// worker. Returns the number of rows purged.
func (s *Store) PurgeFinalized(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		fmt.Sprintf(`
			DELETE FROM %s
			WHERE deletion_timestamp IS NOT NULL
			  AND finalizers = '[]'::jsonb`, s.table))
	if err != nil {
		return 0, fmt.Errorf("purge finalized: %w", err)
	}
	return tag.RowsAffected(), nil
}

// List returns rows filtered by opts, ordered by updated_at DESC with
// stable identity tie-breakers. A pagination cursor is returned when
// more rows are available; pass it back via ListOpts.Cursor to continue.
// Terminating rows are excluded unless IncludeTerminating is true.
func (s *Store) List(ctx context.Context, opts ListOpts) ([]*v1alpha1.RawObject, string, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	args := make([]any, 0, 4)
	where := make([]string, 0, 4)

	if opts.Namespace != "" {
		args = append(args, opts.Namespace)
		where = append(where, fmt.Sprintf("namespace = $%d", len(args)))
	}
	if opts.LatestOnly {
		where = append(where, "is_latest_version")
	}
	if !opts.IncludeTerminating {
		where = append(where, "deletion_timestamp IS NULL")
	}
	if len(opts.LabelSelector) > 0 {
		labelJSON, err := json.Marshal(opts.LabelSelector)
		if err != nil {
			return nil, "", fmt.Errorf("marshal labels: %w", err)
		}
		args = append(args, labelJSON)
		where = append(where, fmt.Sprintf("labels @> $%d", len(args)))
	}
	if opts.Cursor != "" {
		cursor, err := decodeListCursor(opts.Cursor)
		if err != nil {
			return nil, "", err
		}
		args = append(args, cursor.UpdatedAt, cursor.Namespace, cursor.Name, cursor.Version)
		where = append(where, fmt.Sprintf(
			"(updated_at, namespace, name, version) < ($%d, $%d, $%d, $%d)",
			len(args)-3, len(args)-2, len(args)-1, len(args),
		))
	}
	if opts.ExtraWhere != "" || len(opts.ExtraArgs) > 0 {
		placeholders := countDistinctPlaceholders(opts.ExtraWhere)
		if placeholders != len(opts.ExtraArgs) {
			return nil, "", fmt.Errorf("%w: fragment references %d distinct placeholder(s) but %d arg(s) supplied",
				ErrInvalidExtraWhere, placeholders, len(opts.ExtraArgs))
		}
		if len(opts.ExtraArgs) > 0 {
			args = append(args, opts.ExtraArgs...)
		}
		if opts.ExtraWhere != "" {
			where = append(where, rebaseSQLPlaceholders(opts.ExtraWhere, len(args)-len(opts.ExtraArgs)))
		}
	}

	query := fmt.Sprintf(`
		SELECT namespace, name, version, generation, labels, annotations, spec, status,
		       deletion_timestamp, finalizers, created_at, updated_at
		FROM %s`, s.table)
	if len(where) > 0 {
		query += " WHERE " + join(where, " AND ")
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY updated_at DESC, namespace DESC, name DESC, version DESC LIMIT $%d", len(args))

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
		cursor, err := encodeListCursor(out[len(out)-1])
		if err != nil {
			return nil, "", fmt.Errorf("encode next cursor: %w", err)
		}
		nextCursor = cursor
	}
	return out, nextCursor, nil
}

var sqlPlaceholderPattern = regexp.MustCompile(`\$(\d+)`)

func rebaseSQLPlaceholders(clause string, offset int) string {
	if clause == "" || offset == 0 {
		return clause
	}
	return sqlPlaceholderPattern.ReplaceAllStringFunc(clause, func(token string) string {
		n, err := strconv.Atoi(token[1:])
		if err != nil {
			return token
		}
		return fmt.Sprintf("$%d", n+offset)
	})
}

// countDistinctPlaceholders returns the number of distinct `$N` tokens
// in a SQL fragment, independent of how many times each appears.
// Used to validate ListOpts.ExtraWhere against ExtraArgs — a fragment
// of "namespace = ANY($1) AND tenant = $2" has 2 distinct placeholders
// and requires 2 ExtraArgs. Repeated use of $1 counts once.
//
// Does not attempt to exclude `$` inside string literals — a fragment
// containing a `$N`-looking string literal will over-count. Callers
// are documented to use only parameterized SQL.
func countDistinctPlaceholders(clause string) int {
	if clause == "" {
		return 0
	}
	seen := map[int]struct{}{}
	for _, m := range sqlPlaceholderPattern.FindAllStringSubmatch(clause, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		seen[n] = struct{}{}
	}
	return len(seen)
}

func decodeListCursor(token string) (listCursor, error) {
	var cursor listCursor
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return listCursor{}, fmt.Errorf("%w: decode token: %v", ErrInvalidCursor, err)
	}
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return listCursor{}, fmt.Errorf("%w: decode payload: %v", ErrInvalidCursor, err)
	}
	if cursor.UpdatedAt.IsZero() || cursor.Namespace == "" || cursor.Name == "" || cursor.Version == "" {
		return listCursor{}, fmt.Errorf("%w: missing position fields", ErrInvalidCursor)
	}
	return cursor, nil
}

func encodeListCursor(obj *v1alpha1.RawObject) (string, error) {
	if obj == nil {
		return "", errors.New("nil row")
	}
	cursor := listCursor{
		UpdatedAt: obj.Metadata.UpdatedAt,
		Namespace: obj.Metadata.Namespace,
		Name:      obj.Metadata.Name,
		Version:   obj.Metadata.Version,
	}
	if cursor.UpdatedAt.IsZero() || cursor.Namespace == "" || cursor.Name == "" || cursor.Version == "" {
		return "", errors.New("missing row position")
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("marshal cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

// FindReferrersOpts controls the FindReferrers scan.
type FindReferrersOpts struct {
	// Namespace, when non-empty, restricts results to a single namespace.
	Namespace string
	// LatestOnly, when true, restricts to is_latest_version rows.
	LatestOnly bool
	// IncludeTerminating, when true, keeps rows whose deletion_timestamp
	// is set. Default (false) excludes them — URL-uniqueness and cross-
	// kind ref checks want to avoid conflicting with a soft-deleted row
	// that is about to be GC'd.
	IncludeTerminating bool
}

// FindReferrers returns rows from this Store's table whose spec JSONB
// matches pathJSON (via the `@>` containment operator). Callers build the
// JSONB fragment per-kind (e.g. `{"mcpServers":[{"namespace":"...","name":"...","version":"..."}]}`)
// and this method stays generic across ResourceRef shapes.
func (s *Store) FindReferrers(ctx context.Context, pathJSON json.RawMessage, opts FindReferrersOpts) ([]*v1alpha1.RawObject, error) {
	args := []any{[]byte(pathJSON)}
	query := fmt.Sprintf(`
		SELECT namespace, name, version, generation, labels, annotations, spec, status,
		       deletion_timestamp, finalizers, created_at, updated_at
		FROM %s
		WHERE spec @> $1::jsonb`, s.table)
	if !opts.IncludeTerminating {
		query += " AND deletion_timestamp IS NULL"
	}
	if opts.Namespace != "" {
		args = append(args, opts.Namespace)
		query += fmt.Sprintf(" AND namespace = $%d", len(args))
	}
	if opts.LatestOnly {
		query += " AND is_latest_version"
	}
	query += " ORDER BY updated_at DESC"

	rows, err := s.pool.Query(ctx, query, args...)
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

// recomputeLatest recomputes is_latest_version across all non-terminating
// rows with the given (namespace, name), inside the supplied transaction.
// The row with the highest valid semver wins; failing that, the most-
// recently-updated row wins. Terminating rows are ineligible.
func (s *Store) recomputeLatest(ctx context.Context, tx pgx.Tx, namespace, name string) error {
	rows, err := tx.Query(ctx,
		fmt.Sprintf(`
			SELECT version FROM %s
			WHERE namespace=$1 AND name=$2 AND deletion_timestamp IS NULL
			ORDER BY updated_at DESC`, s.table),
		namespace, name)
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

	// Clear is_latest_version for this (namespace, name) first so we never
	// leave stale winners when the only surviving rows are terminating.
	_, err = tx.Exec(ctx,
		fmt.Sprintf(`
			UPDATE %s SET is_latest_version = false
			WHERE namespace=$1 AND name=$2 AND is_latest_version`, s.table),
		namespace, name)
	if err != nil {
		return fmt.Errorf("clear latest: %w", err)
	}
	if len(versions) == 0 {
		return nil
	}

	winner := pickLatestVersion(versions)
	_, err = tx.Exec(ctx,
		fmt.Sprintf(`
			UPDATE %s SET is_latest_version = true
			WHERE namespace=$1 AND name=$2 AND version=$3`, s.table),
		namespace, name, winner)
	if err != nil {
		return fmt.Errorf("set latest: %w", err)
	}
	return nil
}

// pickLatestVersion returns the highest semver among versions. If no
// version parses as semver (per golang.org/x/mod/semver which requires a
// leading 'v'), returns the first element — which, since the caller passes
// them in updated_at DESC order, is the most-recently-updated.
//
// Versions are normalized with a leading 'v' prefix for semver comparison,
// so "1.2.3" and "v1.2.3" sort identically.
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

