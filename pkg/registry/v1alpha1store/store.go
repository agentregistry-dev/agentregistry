package v1alpha1store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// Store is the single generic persistence layer for every v1alpha1 kind.
// One Store instance is bound to one table; callers construct one per kind
// (v1alpha1.agents, v1alpha1.mcp_servers, etc.).
//
// Store has two modes, picked at construction time:
//
//   - Versioned-artifact mode (the default; produced by NewStore).
//     Identity is (namespace, name, integer version). Rows are append-only
//     and the row with MAX(version) for a (namespace, name) is "latest".
//     Spec is JSONB with a CHAR(64) spec_hash so Upsert recognises an
//     unchanged spec and short-circuits rather than emitting a redundant
//     new version. Used for agents, mcp_servers, remote_mcp_servers,
//     skills, prompts, and providers.
//
//   - Legacy-deployment mode (produced by NewDeploymentStore). Identity is
//     (namespace, name, string version). Rows are mutable; re-applying
//     the same (namespace, name, version) updates spec in place. Carries
//     the legacy generation, finalizers, and is_latest_version columns.
//     Used only for v1alpha1.deployments — Deployment is intentionally
//     out of scope for the immutable-versioning redesign because it
//     models lifecycle state ("deploy resource X to provider Y") rather
//     than an artifact whose history is meaningful to readers.
//
// The legacy flag is set ONLY by NewDeploymentStore. The two constructors
// are mutually exclusive — do not mix tables across modes; do not flip
// the flag after construction. Adding new kinds means picking
// versioned-artifact (the default) at registration time; the legacy
// branch exists only to keep the deployments code path unforked while the
// rest of the system migrates.
//
// PatchStatus is disjoint from Upsert: it touches only status and
// updated_at, never spec. Reconcilers use PatchStatus exclusively; apply
// handlers use Upsert exclusively.
//
// Soft delete: Delete sets deletion_timestamp and leaves the row visible
// to GetLatest/Get/List. GC (PurgeFinalized) removes rows where
// deletion_timestamp IS NOT NULL AND finalizers = '[]' (deployments only).
type Store struct {
	pool   *pgxpool.Pool
	table  string
	legacy bool
}

// NewStore constructs a versioned-artifact Store bound to a single table
// (e.g. "v1alpha1.agents"). The table must exist in the schema; NewStore
// does not validate it.
//
// For the deployments table, use NewDeploymentStore — passing
// "v1alpha1.deployments" here is a programming error (the row layout
// differs and the wrong code path will be taken).
func NewStore(pool *pgxpool.Pool, table string) *Store {
	return &Store{pool: pool, table: table, legacy: false}
}

// NewDeploymentStore constructs a legacy-mode Store for the deployments
// table. The table must exist in the schema; this constructor does not
// validate it.
//
// Deployment is the only kind that opts into the legacy shape today; if
// a future kind needs the same lifecycle-state semantics, plumb it
// through here rather than re-introducing a table-name flip.
func NewDeploymentStore(pool *pgxpool.Pool, table string) *Store {
	return &Store{pool: pool, table: table, legacy: true}
}

// IsVersionedArtifact reports whether the Store operates in
// versioned-artifact mode (integer versions, append-only rows). Returns
// false for the legacy deployments mode. Callers gate URL-path /
// metadata.version validation on this — versioned-artifact tables
// require a positive integer; the deployments table accepts any string.
func (s *Store) IsVersionedArtifact() bool {
	return !s.legacy
}

// UpsertOutcome categorises what an Upsert call did.
type UpsertOutcome int

const (
	// UpsertCreated reports that a new immutable version row was inserted —
	// either because the (namespace, name) had no rows yet, or the incoming
	// spec hash differed from the latest live row's.
	UpsertCreated UpsertOutcome = iota
	// UpsertNoOp reports that the incoming spec matched the latest live row's
	// hash and labels/annotations were unchanged. No row was written.
	UpsertNoOp
	// UpsertLabelsUpdated reports that the incoming spec matched but
	// labels and/or annotations differed; the latest row's metadata was
	// patched in place without bumping the version.
	UpsertLabelsUpdated
)

// UpsertResult is the outcome of Upsert.
type UpsertResult struct {
	// Version is the integer version of the live row after the call. For
	// versioned-artifact tables this is the row's positive monotonic
	// version. For the legacy deployments path it is the integer parse of
	// the supplied string version when possible, else 0.
	Version int
	// Outcome categorises what the call did. See UpsertOutcome constants.
	Outcome UpsertOutcome
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
	// LatestOnly restricts to the highest-version live row per
	// (namespace, name). For versioned-artifact tables this resolves via a
	// MAX(version) filter; for the deployments table it consults the
	// legacy is_latest_version flag.
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
// the underlying query.
type listCursor struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Upsert applies obj into the Store. Behaviour depends on the table's
// versioning mode:
//
//   - Versioned-artifact tables (agents, mcp_servers, etc.) follow
//     hash-based append-only apply semantics:
//   - new (namespace, name) → insert at version=1
//   - same spec_hash as latest live row, same labels+annotations →
//     no-op, return the latest version
//   - same spec_hash, different labels or annotations → update labels /
//     annotations on the latest row in place (no version bump)
//   - different spec_hash → insert a new row at MAX(version)+1
//   - The legacy deployments table follows the older
//     update-in-place semantics: rows are keyed by the caller-supplied
//     string version and re-applied with the same spec do not bump
//     anything; differing spec replaces the row.
//
// Status is never touched by Upsert — use PatchStatus for that.
//
// Upsert ignores obj.Metadata.Version on the versioned-artifact path; the
// version is system-assigned. For the deployments path the metadata.version
// string IS the row's identity and must be supplied by the caller.
func (s *Store) Upsert(ctx context.Context, obj v1alpha1.Object) (UpsertResult, error) {
	if obj == nil {
		return UpsertResult{}, errors.New("v1alpha1 store: nil object")
	}
	meta := obj.GetMetadata()
	if meta == nil || meta.Namespace == "" || meta.Name == "" {
		return UpsertResult{}, errors.New("v1alpha1 store: namespace and name are required")
	}
	specJSON, err := obj.MarshalSpec()
	if err != nil {
		return UpsertResult{}, fmt.Errorf("v1alpha1 store: marshal spec: %w", err)
	}
	if len(specJSON) == 0 {
		return UpsertResult{}, errors.New("v1alpha1 store: spec is required")
	}

	if !s.legacy {
		return s.upsertVersioned(ctx, meta, specJSON)
	}
	return s.upsertLegacy(ctx, meta, specJSON)
}

// upsertVersioned implements the hash-based append-only apply semantics
// for versioned-artifact tables. See Upsert for the full state machine.
func (s *Store) upsertVersioned(ctx context.Context, meta *v1alpha1.ObjectMeta, specJSON json.RawMessage) (UpsertResult, error) {
	incomingHash := SpecHash(specJSON)
	incomingLabelsJSON, err := canonicalJSONMap(meta.Labels)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("v1alpha1 store: marshal labels: %w", err)
	}
	incomingAnnotationsJSON, err := canonicalJSONMap(meta.Annotations)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("v1alpha1 store: marshal annotations: %w", err)
	}

	var result UpsertResult
	err = runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			latestVersion       int
			latestHash          string
			latestLabelsRaw     []byte
			latestAnnotationRaw []byte
			latestDeletionTS    pgtype.Timestamptz
			found               bool
		)
		err := tx.QueryRow(ctx,
			fmt.Sprintf(`
				SELECT version, spec_hash, labels, annotations, deletion_timestamp
				FROM %s
				WHERE namespace=$1 AND name=$2
				ORDER BY version DESC
				LIMIT 1
				FOR UPDATE`, s.table),
			meta.Namespace, meta.Name).Scan(&latestVersion, &latestHash, &latestLabelsRaw, &latestAnnotationRaw, &latestDeletionTS)
		switch {
		case err == nil:
			found = true
		case errors.Is(err, pgx.ErrNoRows):
			found = false
		default:
			return fmt.Errorf("load latest: %w", err)
		}

		// Reject mutations on terminating rows. Mirrors Kubernetes:
		// `kubectl apply` on an object with deletionTimestamp returns 409.
		if found && latestDeletionTS.Valid {
			return ErrTerminating
		}

		// Branch 1: no prior row → INSERT at version 1.
		if !found {
			if _, err := tx.Exec(ctx,
				fmt.Sprintf(`
					INSERT INTO %s (namespace, name, version, labels, annotations, spec, spec_hash)
					VALUES ($1, $2, 1, $3, $4, $5, $6)`, s.table),
				meta.Namespace, meta.Name, incomingLabelsJSON, incomingAnnotationsJSON, []byte(specJSON), incomingHash); err != nil {
				return fmt.Errorf("insert v1: %w", err)
			}
			result = UpsertResult{Version: 1, Outcome: UpsertCreated}
			return nil
		}

		// Branch 2: spec hash matches the latest live row.
		if incomingHash == latestHash {
			labelsEqual := equalJSONMap(latestLabelsRaw, incomingLabelsJSON)
			annotationsEqual := equalJSONMap(latestAnnotationRaw, incomingAnnotationsJSON)
			if labelsEqual && annotationsEqual {
				result = UpsertResult{Version: latestVersion, Outcome: UpsertNoOp}
				return nil
			}
			if _, err := tx.Exec(ctx,
				fmt.Sprintf(`
					UPDATE %s
					SET labels = $4, annotations = $5
					WHERE namespace = $1 AND name = $2 AND version = $3`, s.table),
				meta.Namespace, meta.Name, latestVersion, incomingLabelsJSON, incomingAnnotationsJSON); err != nil {
				return fmt.Errorf("update labels: %w", err)
			}
			result = UpsertResult{Version: latestVersion, Outcome: UpsertLabelsUpdated}
			return nil
		}

		// Branch 3: spec hash differs → append a new row at MAX(version)+1.
		newVersion := latestVersion + 1
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`
				INSERT INTO %s (namespace, name, version, labels, annotations, spec, spec_hash)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`, s.table),
			meta.Namespace, meta.Name, newVersion, incomingLabelsJSON, incomingAnnotationsJSON, []byte(specJSON), incomingHash); err != nil {
			return fmt.Errorf("insert new version: %w", err)
		}
		result = UpsertResult{Version: newVersion, Outcome: UpsertCreated}
		return nil
	})
	if err != nil {
		return UpsertResult{}, err
	}
	return result, nil
}

// upsertLegacy implements the older string-version, in-place semantics for
// the deployments table. The caller supplies meta.Version explicitly; a
// re-apply with the same spec is a no-op (no generation today since the
// new outcome surface doesn't model it), a differing spec replaces the
// row, and labels/annotations always replace.
func (s *Store) upsertLegacy(ctx context.Context, meta *v1alpha1.ObjectMeta, specJSON json.RawMessage) (UpsertResult, error) {
	if meta.Version == "" {
		return UpsertResult{}, errors.New("v1alpha1 store: version is required for deployments")
	}
	labelsJSON, err := canonicalJSONMap(meta.Labels)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("v1alpha1 store: marshal labels: %w", err)
	}
	annotationsJSON, err := canonicalJSONMap(meta.Annotations)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("v1alpha1 store: marshal annotations: %w", err)
	}

	var result UpsertResult
	err = runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			oldSpec        []byte
			oldGen         int64
			oldFinalizers  []byte
			oldAnnotations []byte
			oldLabels      []byte
			oldDeletion   pgtype.Timestamptz
			found         bool
		)
		err := tx.QueryRow(ctx,
			fmt.Sprintf(`
				SELECT spec, generation, finalizers, annotations, labels, deletion_timestamp
				FROM %s
				WHERE namespace=$1 AND name=$2 AND version=$3
				FOR UPDATE`, s.table),
			meta.Namespace, meta.Name, meta.Version).Scan(&oldSpec, &oldGen, &oldFinalizers, &oldAnnotations, &oldLabels, &oldDeletion)
		switch {
		case err == nil:
			found = true
		case errors.Is(err, pgx.ErrNoRows):
			found = false
		default:
			return fmt.Errorf("load existing: %w", err)
		}

		if found && oldDeletion.Valid {
			return ErrTerminating
		}

		var newGen int64
		outcome := UpsertNoOp
		switch {
		case !found:
			newGen = 1
			outcome = UpsertCreated
		case !equalSpecJSON(oldSpec, specJSON):
			newGen = oldGen + 1
			outcome = UpsertCreated
		default:
			newGen = oldGen
			if !equalJSONMap(oldLabels, labelsJSON) || !equalJSONMap(oldAnnotations, annotationsJSON) {
				outcome = UpsertLabelsUpdated
			} else {
				outcome = UpsertNoOp
			}
		}

		finalizersJSON := oldFinalizers
		if !found {
			finalizersJSON = []byte("[]")
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
			meta.Namespace, meta.Name, meta.Version, newGen, labelsJSON, annotationsJSON, []byte(specJSON), finalizersJSON)
		if err != nil {
			return fmt.Errorf("upsert row: %w", err)
		}

		if err := s.recomputeLatestDeployments(ctx, tx, meta.Namespace, meta.Name); err != nil {
			return fmt.Errorf("recompute latest: %w", err)
		}

		v, _ := strconv.Atoi(meta.Version)
		result = UpsertResult{Version: v, Outcome: outcome}
		return nil
	})
	if err != nil {
		return UpsertResult{}, err
	}
	return result, nil
}

// PatchOpts bundles optional column mutations applied atomically by
// ApplyPatch. Nil mutators skip the corresponding column entirely; the
// row's other fields are never touched.
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
// Finalizers patching is supported only on the deployments table; the
// versioned-artifact tables don't carry a finalizers column. Calling
// PatchFinalizers on a versioned-artifact Store returns an error to
// surface the misconfiguration loudly rather than silently no-op.
func (s *Store) ApplyPatch(ctx context.Context, namespace, name, version string, patch PatchOpts) error {
	if patch.Status == nil && patch.Annotations == nil && patch.Finalizers == nil {
		return nil
	}
	if patch.Finalizers != nil && !s.legacy {
		return errors.New("v1alpha1 store: finalizers patching not supported on versioned-artifact tables")
	}
	return runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			statusJSON      []byte
			annotationsJSON []byte
			finalizersJSON  []byte
		)
		if !s.legacy {
			err := tx.QueryRow(ctx,
				fmt.Sprintf(`
					SELECT status, annotations FROM %s
					WHERE namespace=$1 AND name=$2 AND version=$3
					FOR UPDATE`, s.table),
				namespace, name, version,
			).Scan(&statusJSON, &annotationsJSON)
			if errors.Is(err, pgx.ErrNoRows) {
				return pkgdb.ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("load row: %w", err)
			}
		} else {
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

		_, err := tx.Exec(ctx,
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
// caller's opaque mutator and returns the replacement bytes.
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
// status case.
func (s *Store) PatchStatus(ctx context.Context, namespace, name, version string, mutate func(current json.RawMessage) (json.RawMessage, error)) error {
	return s.ApplyPatch(ctx, namespace, name, version, PatchOpts{Status: mutate})
}

// PatchFinalizers is a thin wrapper over ApplyPatch for the single-
// column finalizers case. Only valid for the deployments table.
func (s *Store) PatchFinalizers(ctx context.Context, namespace, name, version string, mutate func([]string) []string) error {
	return s.ApplyPatch(ctx, namespace, name, version, PatchOpts{Finalizers: mutate})
}

// PatchAnnotations is a thin wrapper over ApplyPatch for the single-
// column annotations case.
func (s *Store) PatchAnnotations(ctx context.Context, namespace, name, version string, mutate func(map[string]string) map[string]string) error {
	return s.ApplyPatch(ctx, namespace, name, version, PatchOpts{Annotations: mutate})
}

// Get returns a single row by (namespace, name, version), including
// terminating rows. Returns pkgdb.ErrNotFound if missing.
//
// version is parsed as an integer for versioned-artifact tables and
// passed through verbatim for the deployments table.
func (s *Store) Get(ctx context.Context, namespace, name, version string) (*v1alpha1.RawObject, error) {
	args, err := s.identityArgs(namespace, name, version)
	if err != nil {
		return nil, err
	}
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`
			SELECT %s
			FROM %s
			WHERE namespace=$1 AND name=$2 AND version=$3`, s.selectColumns(), s.table),
		args...)
	return scanRow(row, !s.legacy)
}

// GetLatest returns the highest-version live row for (namespace, name) on
// versioned-artifact tables, or the is_latest_version row on the
// deployments table. Returns pkgdb.ErrNotFound if no live version exists.
// Terminating rows are excluded.
func (s *Store) GetLatest(ctx context.Context, namespace, name string) (*v1alpha1.RawObject, error) {
	var query string
	if !s.legacy {
		query = fmt.Sprintf(`
			SELECT %s
			FROM %s
			WHERE namespace=$1 AND name=$2 AND deletion_timestamp IS NULL
			ORDER BY version DESC
			LIMIT 1`, s.selectColumns(), s.table)
	} else {
		query = fmt.Sprintf(`
			SELECT %s
			FROM %s
			WHERE namespace=$1 AND name=$2 AND is_latest_version`, s.selectColumns(), s.table)
	}
	row := s.pool.QueryRow(ctx, query, namespace, name)
	return scanRow(row, !s.legacy)
}

// Delete removes a single row. For deployments, the legacy soft-delete +
// finalizer drain dance still applies. For versioned-artifact tables,
// rows have no finalizers — Delete sets deletion_timestamp directly so
// reads filtered on deletion_timestamp IS NULL stop returning the row;
// PurgeFinalized hard-deletes terminating versioned-artifact rows on a
// separate GC pass. Returns pkgdb.ErrNotFound if the row doesn't exist.
func (s *Store) Delete(ctx context.Context, namespace, name, version string) error {
	args, err := s.identityArgs(namespace, name, version)
	if err != nil {
		return err
	}
	if !s.legacy {
		return s.deleteVersioned(ctx, args)
	}
	return s.deleteLegacy(ctx, args)
}

// ListVersions returns every non-deleted version row for (namespace,
// name), ordered by integer version descending. Versioned-artifact mode
// only — the legacy deployments table doesn't model "list every
// version of a logical resource" and reports an error.
//
// Returns an empty slice (no error) when no rows exist for the
// identity: list semantics differ from the single-row Get path. The
// HTTP layer surfaces empty results as 200 with `{"items": []}`.
func (s *Store) ListVersions(ctx context.Context, namespace, name string) ([]*v1alpha1.RawObject, error) {
	if s.legacy {
		return nil, errors.New("v1alpha1 store: ListVersions is not supported on the legacy deployments table")
	}
	if namespace == "" || name == "" {
		return nil, errors.New("v1alpha1 store: namespace and name are required")
	}
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`
			SELECT %s
			FROM %s
			WHERE namespace=$1 AND name=$2 AND deletion_timestamp IS NULL
			ORDER BY version DESC`, s.selectColumns(), s.table),
		namespace, name)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()

	out := make([]*v1alpha1.RawObject, 0, 4)
	for rows.Next() {
		obj, err := scanRow(rows, !s.legacy)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteAllVersions soft-deletes every live version row for
// (namespace, name) on a versioned-artifact table. This is the contract
// of the batch DELETE endpoint: identity is logical, callers cannot pin
// it to a single integer version. Returns pkgdb.ErrNotFound when no
// live row exists for (namespace, name).
//
// Calling on the legacy deployments Store is a programming error; the
// per-kind Store hands deployment to the single-version Delete path
// instead.
func (s *Store) DeleteAllVersions(ctx context.Context, namespace, name string) error {
	if s.legacy {
		return errors.New("v1alpha1 store: DeleteAllVersions is not supported on the legacy deployments table")
	}
	if namespace == "" || name == "" {
		return errors.New("v1alpha1 store: namespace and name are required")
	}
	tag, err := s.pool.Exec(ctx,
		fmt.Sprintf(`
			DELETE FROM %s
			WHERE namespace=$1 AND name=$2`, s.table),
		namespace, name)
	if err != nil {
		return fmt.Errorf("delete all versions: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkgdb.ErrNotFound
	}
	return nil
}

func (s *Store) deleteVersioned(ctx context.Context, args []any) error {
	return runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		var deletionTS pgtype.Timestamptz
		err := tx.QueryRow(ctx,
			fmt.Sprintf(`
				SELECT deletion_timestamp
				FROM %s
				WHERE namespace=$1 AND name=$2 AND version=$3
				FOR UPDATE`, s.table),
			args...).Scan(&deletionTS)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return pkgdb.ErrNotFound
			}
			return fmt.Errorf("load row: %w", err)
		}

		// Versioned-artifact tables have no finalizers — hard-delete
		// immediately. This matches the OSS fast-path for finalizer-free
		// rows: `arctl delete X` then `arctl apply X` works without any
		// background GC.
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE namespace=$1 AND name=$2 AND version=$3`, s.table),
			args...); err != nil {
			return fmt.Errorf("hard delete: %w", err)
		}
		return nil
	})
}

func (s *Store) deleteLegacy(ctx context.Context, args []any) error {
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
			args...).Scan(&finalizersRaw, &deletionTS)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return pkgdb.ErrNotFound
			}
			return fmt.Errorf("load row: %w", err)
		}

		hasFinalizers, err := jsonArrayNonEmpty(finalizersRaw)
		if err != nil {
			return fmt.Errorf("inspect finalizers: %w", err)
		}
		if !hasFinalizers {
			if _, err := tx.Exec(ctx,
				fmt.Sprintf(`DELETE FROM %s WHERE namespace=$1 AND name=$2 AND version=$3`, s.table),
				args...); err != nil {
				return fmt.Errorf("hard delete: %w", err)
			}
			return s.recomputeLatestDeployments(ctx, tx, args[0].(string), args[1].(string))
		}

		if deletionTS.Valid {
			return nil
		}

		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`UPDATE %s SET deletion_timestamp = NOW()
			             WHERE namespace=$1 AND name=$2 AND version=$3`, s.table),
			args...); err != nil {
			return fmt.Errorf("mark terminating: %w", err)
		}
		return s.recomputeLatestDeployments(ctx, tx, args[0].(string), args[1].(string))
	})
}

// jsonArrayNonEmpty reports whether raw decodes to a JSON array with
// at least one element.
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

// PurgeFinalized hard-deletes terminating rows. For deployments this
// requires finalizers to be empty; for versioned-artifact tables there is
// no finalizers column, so any row past deletion_timestamp is purged.
// Returns the number of rows purged.
func (s *Store) PurgeFinalized(ctx context.Context) (int64, error) {
	var query string
	if !s.legacy {
		query = fmt.Sprintf(`DELETE FROM %s WHERE deletion_timestamp IS NOT NULL`, s.table)
	} else {
		query = fmt.Sprintf(`
			DELETE FROM %s
			WHERE deletion_timestamp IS NOT NULL
			  AND finalizers = '[]'::jsonb`, s.table)
	}
	tag, err := s.pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("purge finalized: %w", err)
	}
	return tag.RowsAffected(), nil
}

// List returns rows filtered by opts, ordered by (namespace, name,
// version) ASC with updated_at as a stable tiebreaker. Pagination cursor
// is returned when more rows are available; pass it back via
// ListOpts.Cursor to continue. Terminating rows are excluded unless
// IncludeTerminating is true.
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
		if !s.legacy {
			// Pick the row with MAX(version) per (namespace, name) via a
			// correlated subquery; the (namespace, name, version DESC)
			// index serves the lookup efficiently.
			where = append(where, fmt.Sprintf(
				"version = (SELECT MAX(version) FROM %s sub WHERE sub.namespace = %s.namespace AND sub.name = %s.name AND sub.deletion_timestamp IS NULL)",
				s.table, s.table, s.table))
		} else {
			where = append(where, "is_latest_version")
		}
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
		// not let it skip across pages.
		cursorVersion, err := s.cursorVersionArg(cursor.Version)
		if err != nil {
			return nil, "", err
		}
		args = append(args, cursor.Namespace, cursor.Name, cursorVersion, cursor.UpdatedAt)
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
		SELECT %s
		FROM %s`, s.selectColumns(), s.table)
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
		obj, err := scanRow(rows, !s.legacy)
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
// `$(N+offset)`, preserving relative ordering. Pure regex rewrite — see
// the existing tests for the contract.
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
	// LatestOnly, when true, restricts to the latest live row per
	// (namespace, name).
	LatestOnly bool
	// IncludeTerminating, when true, keeps rows whose deletion_timestamp
	// is set. Default (false) excludes them.
	IncludeTerminating bool
}

// FindReferrers returns rows from this Store's table whose spec JSONB
// matches pathJSON (via the `@>` containment operator).
func (s *Store) FindReferrers(ctx context.Context, pathJSON json.RawMessage, opts FindReferrersOpts) ([]*v1alpha1.RawObject, error) {
	args := []any{[]byte(pathJSON)}
	query := fmt.Sprintf(`
		SELECT %s
		FROM %s
		WHERE spec @> $1::jsonb`, s.selectColumns(), s.table)
	if !opts.IncludeTerminating {
		query += " AND deletion_timestamp IS NULL"
	}
	if opts.Namespace != "" {
		args = append(args, opts.Namespace)
		query += fmt.Sprintf(" AND namespace = $%d", len(args))
	}
	if opts.LatestOnly {
		if !s.legacy {
			query += fmt.Sprintf(" AND version = (SELECT MAX(version) FROM %s sub WHERE sub.namespace = %s.namespace AND sub.name = %s.name AND sub.deletion_timestamp IS NULL)",
				s.table, s.table, s.table)
		} else {
			query += " AND is_latest_version"
		}
	}
	query += " ORDER BY updated_at DESC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("find referrers: %w", err)
	}
	defer rows.Close()

	out := make([]*v1alpha1.RawObject, 0, 8)
	for rows.Next() {
		obj, err := scanRow(rows, !s.legacy)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, rows.Err()
}

// recomputeLatestDeployments recomputes is_latest_version for the
// deployments table only. Versioned-artifact tables have no
// is_latest_version column — latest is MAX(version).
func (s *Store) recomputeLatestDeployments(ctx context.Context, tx pgx.Tx, namespace, name string) error {
	if !s.legacy {
		return nil
	}
	if _, err := tx.Exec(ctx,
		fmt.Sprintf(`
			UPDATE %s SET is_latest_version = false
			WHERE namespace=$1 AND name=$2 AND is_latest_version`, s.table),
		namespace, name); err != nil {
		return fmt.Errorf("clear latest: %w", err)
	}
	// Pick the most recently updated non-terminating row; deployments
	// version is a string and there's no semver convention here.
	var winner string
	err := tx.QueryRow(ctx,
		fmt.Sprintf(`
			SELECT version FROM %s
			WHERE namespace=$1 AND name=$2 AND deletion_timestamp IS NULL
			ORDER BY updated_at DESC, version DESC
			LIMIT 1`, s.table),
		namespace, name).Scan(&winner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("scan latest: %w", err)
	}
	if _, err := tx.Exec(ctx,
		fmt.Sprintf(`
			UPDATE %s SET is_latest_version = true
			WHERE namespace=$1 AND name=$2 AND version=$3`, s.table),
		namespace, name, winner); err != nil {
		return fmt.Errorf("set latest: %w", err)
	}
	return nil
}

// selectColumns returns the column list emitted by Get/List/FindReferrers
// queries. Deployments include legacy generation/finalizers columns;
// versioned-artifact tables emit synthetic placeholders for them so
// scanRow's column layout stays uniform.
func (s *Store) selectColumns() string {
	if !s.legacy {
		return `namespace, name, version, 0::bigint AS generation, labels, annotations, spec, status,
		       deletion_timestamp, '[]'::jsonb AS finalizers, created_at, updated_at`
	}
	return `namespace, name, version, generation, labels, annotations, spec, status,
		       deletion_timestamp, finalizers, created_at, updated_at`
}

// identityArgs converts (ns, name, version) into the bind args used by
// per-row queries. For versioned-artifact tables version is parsed to int.
func (s *Store) identityArgs(namespace, name, version string) ([]any, error) {
	if !s.legacy {
		v, err := strconv.Atoi(version)
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("v1alpha1 store: invalid integer version %q for table %s", version, s.table)
		}
		return []any{namespace, name, v}, nil
	}
	return []any{namespace, name, version}, nil
}

// cursorVersionArg parses a cursor's version field into the right SQL
// type for the Store's table.
func (s *Store) cursorVersionArg(version string) (any, error) {
	if !s.legacy {
		v, err := strconv.Atoi(version)
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("%w: cursor version %q is not a positive integer", ErrInvalidCursor, version)
		}
		return v, nil
	}
	return version, nil
}

// canonicalJSONMap renders m to canonical JSON suitable for an
// equality-by-bytes comparison after re-marshal. Nil + empty produce
// `{}` so the contract "no labels" reduces to one normalised form.
func canonicalJSONMap(m map[string]string) ([]byte, error) {
	if len(m) == 0 {
		return []byte(`{}`), nil
	}
	return json.Marshal(m)
}

// equalJSONMap reports whether two JSONB byte slices represent the same
// {string: string} map. Decodes both sides so that key order, whitespace,
// and stylistic differences (`null` vs `{}`) don't produce false
// inequalities.
func equalJSONMap(existing, incoming []byte) bool {
	var a, b map[string]string
	if len(existing) > 0 && string(existing) != "null" {
		if err := json.Unmarshal(existing, &a); err != nil {
			return false
		}
	}
	if len(incoming) > 0 && string(incoming) != "null" {
		if err := json.Unmarshal(incoming, &b); err != nil {
			return false
		}
	}
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// equalSpecJSON reports whether two JSON byte slices represent the same
// canonical spec content. Used by the legacy deployments path to
// detect spec-no-op apply.
func equalSpecJSON(existing []byte, incoming json.RawMessage) bool {
	return SpecHash(existing) == SpecHash(incoming)
}
