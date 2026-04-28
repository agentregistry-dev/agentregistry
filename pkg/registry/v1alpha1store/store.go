package v1alpha1store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

// ErrTerminating reports that an Upsert targeted a row whose
// deletion_timestamp is set — the row is mid-teardown and cannot be
// mutated until its finalizers drain and the GC pass hard-deletes it.
// Matches Kubernetes semantics: `kubectl apply` against a terminating
// object returns 409 AlreadyExists ("object is being deleted; delete and
// recreate").
//
// Callers MUST NOT attempt to resurrect by clearing deletion_timestamp
// in-place; wait for the row to finalize (the normal finalizer flow) or
// drop the finalizer explicitly via PatchFinalizers, then re-apply once
// the row has been purged.
var ErrTerminating = errors.New("v1alpha1 store: object is terminating")

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

// listCursor is the opaque pagination position for List. The fields
// mirror the (namespace, name, version, updated_at) sort order used by
// the underlying query. UpdatedAt rides along as a tiebreaker only —
// (namespace, name, version) is already unique per table, so it never
// actually disambiguates rows; it's preserved for symmetry with the
// SQL predicate so encoding/decoding stay round-trip stable.
type listCursor struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// UpsertOpts carries optional metadata on Upsert. All three maps/slices
// describe the desired post-apply state of their respective columns.
//
// Labels are the full replacement label set for the row. Apply-style
// replacement: every Upsert fully overwrites the stored labels. Nil
// and empty map both produce no labels after the call. (Controller-
// managed labels don't survive a user apply unless the apply handler
// merges them in before calling Upsert.)
//
// Annotations is the desired set of annotation key/value pairs on the
// row post-apply; nil means "leave existing annotations unchanged",
// while an explicit empty map means "clear all annotations". Used by
// controllers that add annotations out-of-band with user applies.
type UpsertOpts struct {
	Labels      map[string]string
	Annotations map[string]string
}

// Upsert writes the given object under its (namespace, name, version). On
// update, generation bumps iff the marshaled spec bytes differ from what's
// already stored; no-op re-applies preserve generation. Status is never
// touched by this call — use PatchStatus for that. Finalizers are
// preserved across Upserts; mutate them via PatchFinalizers (the
// orphan-reconciler seam — there's no public Upsert path for finalizers).
//
// After the row write, is_latest_version is recomputed across all rows
// sharing this (namespace, name): the row with the highest semver wins
// (fallback: most-recently-updated). Terminating rows (deletion_timestamp
// IS NOT NULL) are excluded from the latest computation. All of this
// happens inside a single transaction.
func (s *Store) Upsert(ctx context.Context, namespace, name, version string, specJSON json.RawMessage, opts UpsertOpts) (*UpsertResult, error) {
	if namespace == "" || name == "" || version == "" {
		return nil, errors.New("v1alpha1 store: namespace, name, and version are required")
	}
	if len(specJSON) == 0 {
		return nil, errors.New("v1alpha1 store: spec is required")
	}
	labels := opts.Labels
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
			oldDeletionTS     pgtype.Timestamptz
			found             bool
		)
		err := tx.QueryRow(ctx,
			fmt.Sprintf(`
				SELECT spec, generation, finalizers, annotations, deletion_timestamp
				FROM %s
				WHERE namespace=$1 AND name=$2 AND version=$3
				FOR UPDATE`, s.table),
			namespace, name, version).Scan(&oldSpec, &oldGeneration, &oldFinalizersRaw, &oldAnnotationsRaw, &oldDeletionTS)
		switch {
		case err == nil:
			found = true
		case errors.Is(err, pgx.ErrNoRows):
			found = false
		default:
			return fmt.Errorf("load existing: %w", err)
		}

		// Reject mutations on terminating rows. Mirrors Kubernetes:
		// `kubectl apply` on an object with deletionTimestamp returns 409.
		// The correct recovery is to let finalizers drain (or drop them
		// explicitly via PatchFinalizers) so PurgeFinalized can hard-
		// delete the row; re-apply succeeds once the row is gone.
		if found && oldDeletionTS.Valid {
			return ErrTerminating
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

		// Finalizers preserved verbatim from the existing row (or `[]`
		// for new rows). The public Upsert API has no `Finalizers`
		// option anymore — the column is retained for the future
		// orphan-reconciler hook only. PatchFinalizers (still exported)
		// is the sole way to mutate it.
		finalizersJSON := oldFinalizersRaw
		if !found {
			finalizersJSON = []byte("[]")
		}

		// Annotation handling: nil preserves existing,
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

// PatchOpts bundles optional column mutations applied atomically by
// ApplyPatch. Nil mutators skip the corresponding column entirely; the
// row's other fields (spec, generation, labels) are never touched.
//
//   - Status: opaque-bytes mutator — receives the row's current status
//     JSONB payload (nil when empty) and returns the replacement. Kinds
//     that use the typed v1alpha1.Status schema wrap their logic with
//     v1alpha1.StatusPatcher so the callback keeps its typed shape
//     while the Store stays schema-agnostic.
//   - Annotations: callback receives the current annotations map and
//     returns the replacement. Returning nil clears the map. For
//     idempotent merges, copy the input + overlay caller keys.
//   - Finalizers: callback receives the current slice and returns the
//     replacement. Nil → empty slice.
type PatchOpts struct {
	Status      func(current json.RawMessage) (json.RawMessage, error)
	Annotations func(map[string]string) map[string]string
	Finalizers  func([]string) []string
}

// ApplyPatch atomically applies PatchOpts to the row at
// (namespace, name, version) inside a single transaction. Columns whose
// mutator is nil are left untouched. Returns pkgdb.ErrNotFound if the
// row doesn't exist.
//
// Use this when a caller needs to update more than one column in lockstep
// — e.g. the deployment coordinator threading adapter output into status,
// annotations, and finalizers as a single observation.
//
// For single-column updates, the PatchStatus / PatchAnnotations /
// PatchFinalizers wrappers below are thin shortcuts.
func (s *Store) ApplyPatch(ctx context.Context, namespace, name, version string, patch PatchOpts) error {
	if patch.Status == nil && patch.Annotations == nil && patch.Finalizers == nil {
		return nil
	}
	return runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		var statusJSON, annotationsJSON, finalizersJSON []byte
		err := tx.QueryRow(ctx,
			fmt.Sprintf(`
				SELECT status, annotations, finalizers FROM %s
				WHERE namespace=$1 AND name=$2 AND version=$3
				FOR UPDATE`, s.table),
			namespace, name, version,
		).Scan(&statusJSON, &annotationsJSON, &finalizersJSON)
		if errors.Is(err, pgx.ErrNoRows) {
			return pkgdb.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load row: %w", err)
		}

		setClauses := make([]string, 0, 3)
		args := []any{namespace, name, version}

		if patch.Status != nil {
			newJSON, err := buildStatusPatch(statusJSON, patch.Status)
			if err != nil {
				return err
			}
			args = append(args, newJSON)
			setClauses = append(setClauses, fmt.Sprintf("status=$%d", len(args)))
		}
		if patch.Annotations != nil {
			newJSON, err := buildAnnotationsPatch(annotationsJSON, patch.Annotations)
			if err != nil {
				return err
			}
			args = append(args, newJSON)
			setClauses = append(setClauses, fmt.Sprintf("annotations=$%d", len(args)))
		}
		if patch.Finalizers != nil {
			newJSON, err := buildFinalizersPatch(finalizersJSON, patch.Finalizers)
			if err != nil {
				return err
			}
			args = append(args, newJSON)
			setClauses = append(setClauses, fmt.Sprintf("finalizers=$%d", len(args)))
		}

		// updated_at is maintained by the v1alpha1.set_updated_at() BEFORE
		// UPDATE trigger, so we don't set it explicitly here. Keeps
		// PatchAnnotations / PatchStatus / PatchFinalizers consistent
		// about when the timestamp advances.
		_, err = tx.Exec(ctx,
			fmt.Sprintf(`UPDATE %s SET %s WHERE namespace=$1 AND name=$2 AND version=$3`,
				s.table, strings.Join(setClauses, ", ")),
			args...)
		if err != nil {
			return fmt.Errorf("apply patch: %w", err)
		}
		return nil
	})
}

// buildStatusPatch hands the row's current status JSONB payload to the
// caller's opaque mutator and returns the replacement bytes. The Store
// is schema-agnostic here — typed kinds layer their own decode/encode
// via v1alpha1.StatusPatcher (see v1alpha1/status.go).
func buildStatusPatch(current []byte, mutate func(json.RawMessage) (json.RawMessage, error)) ([]byte, error) {
	var in json.RawMessage
	if len(current) > 0 {
		in = json.RawMessage(current)
	}
	out, err := mutate(in)
	if err != nil {
		return nil, fmt.Errorf("status mutator: %w", err)
	}
	return out, nil
}

// buildAnnotationsPatch decodes the row's current annotations JSON,
// applies the caller's mutator (nil return → empty map), and marshals
// the result.
func buildAnnotationsPatch(current []byte, mutate func(map[string]string) map[string]string) ([]byte, error) {
	annotations := map[string]string{}
	if len(current) > 0 {
		if err := json.Unmarshal(current, &annotations); err != nil {
			return nil, fmt.Errorf("decode annotations: %w", err)
		}
	}
	annotations = mutate(annotations)
	if annotations == nil {
		annotations = map[string]string{}
	}
	out, err := json.Marshal(annotations)
	if err != nil {
		return nil, fmt.Errorf("encode annotations: %w", err)
	}
	return out, nil
}

// buildFinalizersPatch decodes the row's current finalizers JSON,
// applies the caller's mutator (nil return → empty slice), and marshals
// the result.
func buildFinalizersPatch(current []byte, mutate func([]string) []string) ([]byte, error) {
	var finalizers []string
	if len(current) > 0 {
		if err := json.Unmarshal(current, &finalizers); err != nil {
			return nil, fmt.Errorf("decode finalizers: %w", err)
		}
	}
	finalizers = mutate(finalizers)
	if finalizers == nil {
		finalizers = []string{}
	}
	out, err := json.Marshal(finalizers)
	if err != nil {
		return nil, fmt.Errorf("encode finalizers: %w", err)
	}
	return out, nil
}

// PatchStatus is a thin wrapper over ApplyPatch for the single-column
// status case. The mutator receives the current status JSONB payload as
// opaque bytes (nil when empty) and returns the replacement. Kinds that
// bind their status to the typed v1alpha1.Status wrap the callback via
// v1alpha1.StatusPatcher.
func (s *Store) PatchStatus(ctx context.Context, namespace, name, version string, mutate func(current json.RawMessage) (json.RawMessage, error)) error {
	return s.ApplyPatch(ctx, namespace, name, version, PatchOpts{Status: mutate})
}

// PatchFinalizers is a thin wrapper over ApplyPatch for the single-
// column finalizers case. See ApplyPatch for the semantics.
func (s *Store) PatchFinalizers(ctx context.Context, namespace, name, version string, mutate func([]string) []string) error {
	return s.ApplyPatch(ctx, namespace, name, version, PatchOpts{Finalizers: mutate})
}

// PatchAnnotations is a thin wrapper over ApplyPatch for the single-
// column annotations case. See ApplyPatch for the semantics.
func (s *Store) PatchAnnotations(ctx context.Context, namespace, name, version string, mutate func(map[string]string) map[string]string) error {
	return s.ApplyPatch(ctx, namespace, name, version, PatchOpts{Annotations: mutate})
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

// Delete removes a single row. Finalizer-bearing rows go through
// soft-delete (deletion_timestamp set, row stays visible until
// finalizers drain via PatchFinalizers, then PurgeFinalized hard-deletes).
// Rows with no finalizers hard-delete immediately — matches Kubernetes,
// where a finalizer-free object is gone the moment the API server
// processes the DELETE call.
//
// The fast-path matters in practice: Deployment / Provider / Role /
// most kinds today carry no finalizers, so without it `arctl delete X`
// then `arctl apply X` would race the (currently non-existent)
// background GC and hit ErrTerminating until something else purged the
// row. With the fast-path, delete-then-reapply just works for the
// common case while the soft-delete + drain dance is preserved for
// kinds that actually use finalizers.
//
// On success, recomputes is_latest_version across surviving non-
// terminating rows for this (namespace, name). Returns pkgdb.ErrNotFound
// if the row doesn't exist.
func (s *Store) Delete(ctx context.Context, namespace, name, version string) error {
	return runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			finalizersRaw []byte
			deletionTS    pgtype.Timestamptz
		)
		err := tx.QueryRow(ctx,
			fmt.Sprintf(`
				SELECT finalizers, deletion_timestamp
				FROM %s
				WHERE namespace=$1 AND name=$2 AND version=$3
				FOR UPDATE`, s.table),
			namespace, name, version).Scan(&finalizersRaw, &deletionTS)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return pkgdb.ErrNotFound
			}
			return fmt.Errorf("load row: %w", err)
		}

		// Finalizer-free → hard-delete immediately. Same logic
		// PurgeFinalized would run on a separate GC pass; collapsing it
		// into Delete avoids the "object is terminating; delete + re-apply
		// once GC purges the row" race that blocks re-apply with no GC
		// running.
		hasFinalizers, err := jsonArrayNonEmpty(finalizersRaw)
		if err != nil {
			return fmt.Errorf("inspect finalizers: %w", err)
		}
		if !hasFinalizers {
			if _, err := tx.Exec(ctx,
				fmt.Sprintf(`DELETE FROM %s WHERE namespace=$1 AND name=$2 AND version=$3`, s.table),
				namespace, name, version); err != nil {
				return fmt.Errorf("hard delete: %w", err)
			}
			return s.recomputeLatest(ctx, tx, namespace, name)
		}

		// Already terminating with finalizers attached — idempotent
		// delete, no further action.
		if deletionTS.Valid {
			return nil
		}

		// Soft-delete: row has finalizers, mark terminating and wait
		// for them to drain.
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`UPDATE %s SET deletion_timestamp = NOW()
			             WHERE namespace=$1 AND name=$2 AND version=$3`, s.table),
			namespace, name, version); err != nil {
			return fmt.Errorf("mark terminating: %w", err)
		}
		return s.recomputeLatest(ctx, tx, namespace, name)
	})
}

// jsonArrayNonEmpty reports whether raw decodes to a JSON array with
// at least one element. Used by Delete to distinguish finalizer-free
// rows (hard-delete fast path) from rows that need soft-delete + drain.
func jsonArrayNonEmpty(raw []byte) (bool, error) {
	if len(raw) == 0 {
		return false, nil
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return false, err
	}
	return len(arr) > 0, nil
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
		// Order by stable identity (namespace, name, version) first so a
		// row's updated_at changing under a concurrent PatchStatus does
		// not let it skip across pages. (namespace, name, version) is
		// unique per table, so updated_at as the trailing tiebreaker is
		// belt-and-braces.
		args = append(args, cursor.Namespace, cursor.Name, cursor.Version, cursor.UpdatedAt)
		where = append(where, fmt.Sprintf(
			"(namespace, name, version, updated_at) > ($%d, $%d, $%d, $%d)",
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
		query += " WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY namespace, name, version, updated_at LIMIT $%d", len(args))

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

// rebaseSQLPlaceholders rewrites every `$N` token in a SQL fragment to
// `$(N+offset)`, preserving relative ordering. It is the core of the
// ExtraWhere contract: callers author fragments in their own
// $1-relative numbering and the Store rebases them to continue past
// its own internal placeholders before executing.
//
// Contract / known limitations:
//   - `offset` must be ≥ 0. The only production caller derives offset
//     from `len(args) - len(opts.ExtraArgs)` after appending, which is
//     always non-negative. Negative offsets that push a placeholder
//     below 1 produce strings PostgreSQL rejects at parse time
//     (`$-2`, `$0`); they are not a security concern but they're not
//     a contract this function honors either.
//   - The implementation is a pure regex pass over `\$\d+`. It does NOT
//     parse SQL. `$N`-looking text inside a string literal would be
//     rebased indistinguishably from a real placeholder, so callers
//     MUST author ExtraWhere as parameterized SQL — no string literals
//     containing `$\d+`, no string concatenation of untrusted input.
//     This is the documented authz seam (see ListOpts.ExtraWhere); the
//     security boundary is the parameterization rule, not the rewriter.
//   - `$0` is rebased to `$(offset)`. PostgreSQL rejects `$0` at parse
//     time anyway, so there's no silent-error path here — a caller
//     using `$0` will get a SQL error when the query executes, with or
//     without rebasing.
//   - Empty `clause` and zero `offset` short-circuit unchanged. This
//     keeps the no-op happy path allocation-free for callers that
//     supply ExtraWhere only sometimes.
//   - strconv.Atoi failures (overflow on extremely long digit runs)
//     leave the token in place. This is a defense-in-depth fallback;
//     the caller will get a SQL error on execution from the un-rebased
//     placeholder, which is the safer failure mode than silently
//     producing a different number.
//
// rebaseSQLPlaceholders is exhaustively fuzz-tested in
// FuzzRebaseSQLPlaceholders to lock the rebase invariants: for any
// input with offset ≥ 0, the per-token relative ordering of
// placeholders is preserved, the token count is preserved, every
// appearance of `$N` shifts by exactly `offset`, and the
// non-placeholder bytes of the fragment are byte-identical between
// input and output.
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
	// `version DESC` is the deterministic tie-breaker for the non-semver
	// fallback path in pickLatestVersion — when two rows share the same
	// updated_at (possible on batch upserts inside a microsecond), the
	// winner would otherwise be whichever row the query engine picked
	// first. Without the tie-breaker, is_latest_version can flip between
	// concurrent upserts.
	rows, err := tx.Query(ctx,
		fmt.Sprintf(`
			SELECT version FROM %s
			WHERE namespace=$1 AND name=$2 AND deletion_timestamp IS NULL
			ORDER BY updated_at DESC, version DESC`, s.table),
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
