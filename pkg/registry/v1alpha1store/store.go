package v1alpha1store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
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
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// StoreBehavior names the private persistence behavior used below the single
// public v1alpha1 API shape.
type StoreBehavior string

const (
	// TaggedArtifactStore keys immutable-ish registry artifacts by
	// namespace/name/tag.
	TaggedArtifactStore StoreBehavior = "TaggedArtifactStore"
	// MutableObjectStore keys normal Kubernetes-like objects by namespace/name
	// in the public API, using a hidden constant identity where old tables
	// still need one.
	MutableObjectStore StoreBehavior = "MutableObjectStore"
)

const defaultMutableObjectIdentity = "1"

// DefaultMutableObjectIdentity is the compatibility row identity used by
// mutable-object tables that still retain a private version column.
func DefaultMutableObjectIdentity() string { return defaultMutableObjectIdentity }

// Store is the single generic persistence layer for every v1alpha1 kind.
// One Store instance is bound to one table; callers construct one per kind
// (v1alpha1.agents, v1alpha1.mcp_servers, etc.).
//
// Store has two private behaviors, picked at construction time:
//
//   - TaggedArtifactStore (the default; produced by NewStore).
//     Identity is (namespace, name, tag). Users may supply the tag
//     declaratively; missing tags are filled with the literal "latest".
//     Re-applying the same tag replaces the prior row atomically when the
//     content changes. Used for agents, mcp_servers,
//     remote_mcp_servers, skills, and prompts.
//
//   - MutableObjectStore (produced by NewMutableObjectStore). Public
//     identity is (namespace, name). Existing tables may retain a hidden
//     constant version column internally, but that detail must not leak into
//     routes, manifests, refs, or responses. Used for Provider/Deployment and
//     downstream mutable/security/config kinds such as AccessPolicy.
//
// PatchStatus is disjoint from Upsert: it touches only status and
// updated_at, never spec. Reconcilers use PatchStatus exclusively; apply
// handlers use Upsert exclusively.
//
// Soft delete: Delete sets deletion_timestamp and leaves the row visible
// to GetLatest/Get/List. GC (PurgeFinalized) removes rows where
// deletion_timestamp IS NOT NULL AND finalizers = '[]' (deployments only).
type Store struct {
	pool            *pgxpool.Pool
	table           string
	behavior        StoreBehavior
	mutableIdentity string
	kind            string
	auditor         types.Auditor
}

// StoreOption configures an optional Store behaviour at construction
// time. Options compose; later options override earlier ones for the
// same field.
type StoreOption func(*Store)

// WithAuditor plugs a types.Auditor into the Store so every state
// change the Store considers significant fires the matching audit
// event after the underlying transaction commits. Default is
// types.NoopAuditor.
func WithAuditor(a types.Auditor) StoreOption {
	return func(s *Store) {
		if a != nil {
			s.auditor = a
		}
	}
}

// WithKind tags a Store with the canonical v1alpha1 Kind name (e.g.
// v1alpha1.KindAgent) so audit events can name the kind without the
// caller having to set obj.TypeMeta. NewStores sets this for every
// kind; ad-hoc constructors leave it empty unless the caller passes
// WithKind explicitly. When unset, the Store falls back to the Kind
// carried on the inbound object (if any).
func WithKind(kind string) StoreOption {
	return func(s *Store) { s.kind = kind }
}

// WithMutableObjectIdentity sets the private row identity used by
// MutableObjectStore tables that still carry a version column. The default is
// DefaultMutableObjectIdentity(); callers should only override it for
// compatibility with pre-seeded rows.
func WithMutableObjectIdentity(identity string) StoreOption {
	return func(s *Store) { s.mutableIdentity = identity }
}

// NewStore constructs a tagged-artifact Store bound to a single table
// (e.g. "v1alpha1.agents"). The table must exist in the schema; NewStore
// does not validate it.
//
// For mutable object tables, use NewMutableObjectStore.
func NewStore(pool *pgxpool.Pool, table string, opts ...StoreOption) *Store {
	s := &Store{pool: pool, table: table, behavior: TaggedArtifactStore, auditor: types.NoopAuditor}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// NewMutableObjectStore constructs a mutable-object Store for tables whose
// public identity is namespace/name. The table may retain a private identity
// column internally for compatibility with existing migrations.
func NewMutableObjectStore(pool *pgxpool.Pool, table string, opts ...StoreOption) *Store {
	s := &Store{pool: pool, table: table, behavior: MutableObjectStore, mutableIdentity: defaultMutableObjectIdentity, auditor: types.NoopAuditor}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// IsTaggedArtifact reports whether the Store operates in tagged content mode.
// Returns false for mutable object stores.
func (s *Store) IsTaggedArtifact() bool {
	return s.behavior == TaggedArtifactStore
}

// IsMutableObject reports whether the Store hides its private row identity
// behind public namespace/name semantics.
func (s *Store) IsMutableObject() bool {
	return s.behavior == MutableObjectStore
}

// MutableObjectIdentity returns the private backing-row identity used by
// mutable-object stores. It is a storage compatibility detail, not public
// v1alpha1 metadata.
func (s *Store) MutableObjectIdentity() string {
	return s.mutableIdentity
}

// RowIdentity returns the private row identity carried by a RawObject returned
// from this Store. Tagged-artifact rows use metadata.tag; mutable-object rows
// use RawObject.StorageIdentity.
func (s *Store) RowIdentity(row *v1alpha1.RawObject) string {
	if row == nil {
		return ""
	}
	if s.behavior == TaggedArtifactStore {
		return row.Metadata.Tag
	}
	return row.StorageIdentity
}

// UpsertOutcome categorises what an Upsert call did.
type UpsertOutcome int

const (
	// UpsertCreated reports that a new tag row was inserted.
	UpsertCreated UpsertOutcome = iota
	// UpsertNoOp reports that the incoming content matched the existing row
	// for the tag. No row was written.
	UpsertNoOp
	// UpsertReplaced reports that an existing tag row was atomically replaced
	// with new content.
	UpsertReplaced
)

// UpsertResult is the outcome of Upsert.
type UpsertResult struct {
	// Tag is the content identity after the call for tagged-artifact tables.
	Tag string
	// Version is populated only for mutable-object stores, where the public
	// namespace/name identity may still map to a private storage version column.
	Version int
	// UID is the server-managed row identity after the write.
	UID string
	// Generation is the server-managed row generation after the call.
	Generation int64
	// Outcome categorises what the call did. See UpsertOutcome constants.
	Outcome UpsertOutcome
}

// UpsertOpts customizes create-time behavior for Store.Upsert.
type UpsertOpts struct {
	// InitialFinalizers is applied only on the create path for legacy mutable
	// stores. Updates preserve existing finalizers.
	InitialFinalizers []string
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
	// LatestOnly restricts to the literal "latest" tag per (namespace, name),
	// or the private latest row for mutable-object stores.
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
// mirror the (namespace, name, tag/version, updated_at) sort order used by
// the underlying query.
type listCursor struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Identity  string    `json:"identity"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Upsert applies obj into the Store. Behaviour depends on the table's
// versioning mode:
//
//   - Tagged-artifact tables (agents, mcp_servers, etc.) follow
//     declarative tag semantics:
//   - missing metadata.tag → default to the literal "latest" tag
//   - new (namespace, name, tag) → insert the row
//   - same tag and same canonical content hash → no-op
//   - same tag and different content hash → replace the row in place
//   - Mutable-object tables follow Kubernetes-like update-in-place
//     semantics behind public namespace/name identity. A hidden storage
//     identity may exist only to satisfy existing table schemas.
//
// Status is never touched by Upsert — use PatchStatus for that.
//
// Upsert derives mutable-object storage identity from Store configuration so
// the v1alpha1 metadata contract stays free of private backing-row fields.
func (s *Store) Upsert(ctx context.Context, obj v1alpha1.Object, opts ...UpsertOpts) (UpsertResult, error) {
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

	var opt UpsertOpts
	if len(opts) > 0 {
		opt = opts[0]
	}

	if s.behavior == TaggedArtifactStore {
		res, err := s.upsertTagged(ctx, meta, specJSON)
		if err != nil {
			return res, err
		}
		// Fire the audit event AFTER the transaction commits. If the tx
		// rolls back (err != nil above) the event is suppressed. Branch 2
		// outcomes (UpsertNoOp, UpsertLabelsUpdated) do not introduce a
		// new tag row, so they are not recorded.
		if res.Outcome == UpsertCreated {
			s.auditor.ResourceTagCreated(ctx, s.kindFor(obj), meta.Namespace, meta.Name, res.Tag)
		}
		return res, nil
	}
	return s.upsertMutable(ctx, meta, specJSON, opt)
}

// kindFor returns the canonical Kind name to attach to audit events.
// Prefers the Kind set at construction time (NewStores does this);
// falls back to the inbound object's TypeMeta.Kind. May be "" when
// neither is populated (ad-hoc unit-test construction).
func (s *Store) kindFor(obj v1alpha1.Object) string {
	if s.kind != "" {
		return s.kind
	}
	return obj.GetKind()
}

// upsertTagged implements the tag apply semantics for tagged artifact tables.
// See Upsert for the full state machine.
func (s *Store) upsertTagged(ctx context.Context, meta *v1alpha1.ObjectMeta, specJSON json.RawMessage) (UpsertResult, error) {
	if meta.Tag == "" {
		meta.Tag = DefaultTag()
	}
	incomingHash, err := ContentHash(meta, specJSON)
	if err != nil {
		return UpsertResult{}, fmt.Errorf("v1alpha1 store: content hash: %w", err)
	}
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
		// Serialize concurrent applies for the same (namespace, name).
		// `SELECT ... FOR UPDATE` is row-level and provides no gap-lock
		// semantics: goroutines that see "no prior row" can all proceed
		// to INSERT the same tag before one wins the unique constraint.
		// An advisory transaction lock serializes the entire
		// (lookup, insert) decision per identity. The lock auto-releases
		// at COMMIT/ROLLBACK because we use pg_advisory_xact_lock.
		key := s.advisoryLockKey(s.table, meta.Namespace, meta.Name)
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
			return fmt.Errorf("advisory lock: %w", err)
		}

		var (
			existingHash       string
			existingDeletionTS pgtype.Timestamptz
			existingGeneration int64
			existingUID        string
			found              bool
		)
		err := tx.QueryRow(ctx,
			fmt.Sprintf(`
						SELECT content_hash, deletion_timestamp, generation, uid::text
						FROM %s
						WHERE namespace=$1 AND name=$2 AND tag=$3
						FOR UPDATE`, s.table),
			meta.Namespace, meta.Name, meta.Tag).Scan(&existingHash, &existingDeletionTS, &existingGeneration, &existingUID)
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
		if found && existingDeletionTS.Valid {
			return ErrTerminating
		}

		if !found {
			var uid string
			if err := tx.QueryRow(ctx,
				fmt.Sprintf(`
						INSERT INTO %s (namespace, name, tag, labels, annotations, spec, content_hash)
						VALUES ($1, $2, $3, $4, $5, $6, $7)
						RETURNING uid::text`, s.table),
				meta.Namespace, meta.Name, meta.Tag, incomingLabelsJSON, incomingAnnotationsJSON, []byte(specJSON), incomingHash).Scan(&uid); err != nil {
				return fmt.Errorf("insert tag: %w", err)
			}
			result = UpsertResult{Tag: meta.Tag, UID: uid, Generation: 1, Outcome: UpsertCreated}
			return nil
		}

		if incomingHash == existingHash {
			result = UpsertResult{Tag: meta.Tag, UID: existingUID, Generation: existingGeneration, Outcome: UpsertNoOp}
			return nil
		}

		nextGeneration := existingGeneration + 1
		var uid string
		if err := tx.QueryRow(ctx,
			fmt.Sprintf(`
						UPDATE %s
						SET labels=$4, annotations=$5, spec=$6, content_hash=$7, generation=$8, status='{}'::jsonb, deletion_timestamp=NULL
						WHERE namespace=$1 AND name=$2 AND tag=$3
						RETURNING uid::text`, s.table),
			meta.Namespace, meta.Name, meta.Tag, incomingLabelsJSON, incomingAnnotationsJSON, []byte(specJSON), incomingHash, nextGeneration).Scan(&uid); err != nil {
			return fmt.Errorf("replace tag: %w", err)
		}
		result = UpsertResult{Tag: meta.Tag, UID: uid, Generation: nextGeneration, Outcome: UpsertReplaced}
		return nil
	})
	if err != nil {
		return UpsertResult{}, err
	}
	return result, nil
}

// upsertMutable implements in-place semantics for mutable-object tables. The
// public API is namespace/name; Store owns the private row identity for tables
// that still have a version column.
func (s *Store) upsertMutable(ctx context.Context, meta *v1alpha1.ObjectMeta, specJSON json.RawMessage, opts UpsertOpts) (UpsertResult, error) {
	identity := s.mutableIdentity
	if identity == "" {
		return UpsertResult{}, errors.New("v1alpha1 store: hidden storage identity is required for mutable-object kinds")
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
			oldDeletion    pgtype.Timestamptz
			oldUID         string
			found          bool
		)
		err := tx.QueryRow(ctx,
			fmt.Sprintf(`
					SELECT spec, generation, finalizers, annotations, labels, deletion_timestamp, uid::text
					FROM %s
					WHERE namespace=$1 AND name=$2 AND version=$3
					FOR UPDATE`, s.table),
			meta.Namespace, meta.Name, identity).Scan(&oldSpec, &oldGen, &oldFinalizers, &oldAnnotations, &oldLabels, &oldDeletion, &oldUID)
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

		var (
			newGen  int64
			outcome UpsertOutcome
		)
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
				outcome = UpsertReplaced
			} else {
				outcome = UpsertNoOp
			}
		}

		finalizersJSON := oldFinalizers
		if !found {
			if len(opts.InitialFinalizers) > 0 {
				b, err := json.Marshal(opts.InitialFinalizers)
				if err != nil {
					return fmt.Errorf("marshal initial finalizers: %w", err)
				}
				finalizersJSON = b
			} else {
				finalizersJSON = []byte("[]")
			}
		}

		var uid string
		err = tx.QueryRow(ctx,
			fmt.Sprintf(`
					INSERT INTO %s (namespace, name, version, generation, labels, annotations, spec, finalizers)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				ON CONFLICT (namespace, name, version) DO UPDATE
				SET generation  = EXCLUDED.generation,
					    labels      = EXCLUDED.labels,
					    annotations = EXCLUDED.annotations,
					    spec        = EXCLUDED.spec,
					    finalizers  = EXCLUDED.finalizers
					RETURNING uid::text
				`, s.table),
			meta.Namespace, meta.Name, identity, newGen, labelsJSON, annotationsJSON, []byte(specJSON), finalizersJSON).Scan(&uid)
		if err != nil {
			return fmt.Errorf("upsert row: %w", err)
		}
		if uid == "" {
			uid = oldUID
		}

		if err := s.recomputeLatestMutable(ctx, tx, meta.Namespace, meta.Name); err != nil {
			return fmt.Errorf("recompute latest: %w", err)
		}

		v, _ := strconv.Atoi(identity)
		result = UpsertResult{Version: v, UID: uid, Generation: newGen, Outcome: outcome}
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
// (namespace, name, identity) inside a single transaction. Columns whose
// mutator is nil are left untouched. Returns pkgdb.ErrNotFound if the
// row doesn't exist.
//
// Finalizers patching is supported only on the deployments table; the
// tagged-artifact tables don't carry a finalizers column. Calling
// PatchFinalizers on a tagged-artifact Store returns an error to
// surface the misconfiguration loudly rather than silently no-op.
func (s *Store) ApplyPatch(ctx context.Context, namespace, name, version string, patch PatchOpts) error {
	if patch.Status == nil && patch.Annotations == nil && patch.Finalizers == nil {
		return nil
	}
	if patch.Finalizers != nil && s.behavior == TaggedArtifactStore {
		return errors.New("v1alpha1 store: finalizers patching not supported on tagged-artifact tables")
	}
	if version == "" && s.behavior == MutableObjectStore {
		version = s.mutableIdentity
	}
	return runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		statusJSON, annotationsJSON, finalizersJSON, err := s.loadPatchRow(ctx, tx, namespace, name, version)
		if err != nil {
			return err
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

		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`UPDATE %s SET %s WHERE namespace=$1 AND name=$2 AND %s=$3`,
				s.table, strings.Join(setClauses, ", "), s.identityColumn()),
			args...); err != nil {
			return fmt.Errorf("apply patch: %w", err)
		}
		return nil
	})
}

// loadPatchRow loads the columns ApplyPatch may mutate
// (status, annotations, and on mutable-object stores finalizers) and returns
// pkgdb.ErrNotFound if no row matches. The finalizers payload is empty
// for tagged-artifact stores.
func (s *Store) loadPatchRow(ctx context.Context, tx pgx.Tx, namespace, name, version string) (statusJSON, annotationsJSON, finalizersJSON []byte, err error) {
	if s.behavior == MutableObjectStore {
		err = tx.QueryRow(ctx,
			fmt.Sprintf(`
				SELECT status, annotations, finalizers FROM %s
				WHERE namespace=$1 AND name=$2 AND version=$3
				FOR UPDATE`, s.table),
			namespace, name, version,
		).Scan(&statusJSON, &annotationsJSON, &finalizersJSON)
	} else {
		err = tx.QueryRow(ctx,
			fmt.Sprintf(`
				SELECT status, annotations FROM %s
				WHERE namespace=$1 AND name=$2 AND tag=$3
				FOR UPDATE`, s.table),
			namespace, name, version,
		).Scan(&statusJSON, &annotationsJSON)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, pkgdb.ErrNotFound
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load row: %w", err)
	}
	return statusJSON, annotationsJSON, finalizersJSON, nil
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

// Get returns a single row by private row identity, including
// terminating rows. Returns pkgdb.ErrNotFound if missing.
//
// identity is a tag for tagged-artifact stores and the hidden storage identity
// for mutable-object stores.
func (s *Store) Get(ctx context.Context, namespace, name, identity string) (*v1alpha1.RawObject, error) {
	args, err := s.identityArgs(namespace, name, identity)
	if err != nil {
		return nil, err
	}
	col := "version"
	if s.behavior == TaggedArtifactStore {
		col = "tag"
	}
	row := s.pool.QueryRow(ctx,
		fmt.Sprintf(`
			SELECT %s
			FROM %s
			WHERE namespace=$1 AND name=$2 AND %s=$3`, s.selectColumns(), s.table, col),
		args...)
	return scanRow(row, s.behavior == TaggedArtifactStore)
}

// GetLatest returns the literal "latest" live tag for (namespace, name) on
// tagged-artifact tables, or the current live row for mutable-object stores.
// Returns pkgdb.ErrNotFound if no live row exists.
// Terminating rows are excluded.
func (s *Store) GetLatest(ctx context.Context, namespace, name string) (*v1alpha1.RawObject, error) {
	var query string
	if s.behavior == TaggedArtifactStore {
		query = fmt.Sprintf(`
			SELECT %s
			FROM %s
			WHERE namespace=$1 AND name=$2 AND tag=$3 AND deletion_timestamp IS NULL`, s.selectColumns(), s.table)
		row := s.pool.QueryRow(ctx, query, namespace, name, DefaultTag())
		return scanRow(row, true)
	} else {
		query = fmt.Sprintf(`
			SELECT %s
			FROM %s
			WHERE namespace=$1 AND name=$2 AND is_latest_version`, s.selectColumns(), s.table)
	}
	row := s.pool.QueryRow(ctx, query, namespace, name)
	return scanRow(row, false)
}

// Delete removes a single row. Mutable-object stores may use soft-delete plus
// finalizer drain. Tagged-artifact rows have no finalizers and are hard-deleted
// immediately so name/tag can be reapplied without waiting for GC. Returns
// pkgdb.ErrNotFound if the row doesn't exist.
func (s *Store) Delete(ctx context.Context, namespace, name, identity string) error {
	args, err := s.identityArgs(namespace, name, identity)
	if err != nil {
		return err
	}
	if s.behavior == TaggedArtifactStore {
		return s.deleteTagged(ctx, args)
	}
	return s.deleteMutable(ctx, args)
}

// ListTags returns every non-deleted tag row for (namespace,
// name), ordered by most recently applied first. Tagged-artifact mode
// only — mutable-object stores do not model "list every tag of a logical
// resource" and report an error.
//
// Returns an empty slice (no error) when no rows exist for the
// identity: list semantics differ from the single-row Get path. The
// HTTP layer surfaces empty results as 200 with `{"items": []}`.
func (s *Store) ListTags(ctx context.Context, namespace, name string) ([]*v1alpha1.RawObject, error) {
	if s.behavior == MutableObjectStore {
		return nil, errors.New("v1alpha1 store: ListTags is not supported on mutable-object stores")
	}
	if namespace == "" || name == "" {
		return nil, errors.New("v1alpha1 store: namespace and name are required")
	}
	rows, err := s.pool.Query(ctx,
		fmt.Sprintf(`
			SELECT %s
			FROM %s
			WHERE namespace=$1 AND name=$2 AND deletion_timestamp IS NULL
			ORDER BY updated_at DESC, tag DESC`, s.selectColumns(), s.table),
		namespace, name)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()

	out := make([]*v1alpha1.RawObject, 0, 4)
	for rows.Next() {
		obj, err := scanRow(rows, s.behavior == TaggedArtifactStore)
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

// DeleteAllTags hard-deletes every tag row for (namespace, name)
// on a tagged-artifact table. This is the contract of the
// batch DELETE endpoint: identity is logical, callers cannot pin it to
// a single tag unless they include metadata.tag. Returns pkgdb.ErrNotFound
// when no row exists for (namespace, name).
//
// Calling on a mutable-object Store is a programming error; the per-kind Store
// hands mutable objects to the single-row Delete path
// instead.
func (s *Store) DeleteAllTags(ctx context.Context, namespace, name string) error {
	if s.behavior == MutableObjectStore {
		return errors.New("v1alpha1 store: DeleteAllTags is not supported on mutable-object stores")
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
		return fmt.Errorf("delete all tags: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkgdb.ErrNotFound
	}
	return nil
}

func (s *Store) deleteTagged(ctx context.Context, args []any) error {
	return runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		var deletionTS pgtype.Timestamptz
		err := tx.QueryRow(ctx,
			fmt.Sprintf(`
				SELECT deletion_timestamp
				FROM %s
				WHERE namespace=$1 AND name=$2 AND tag=$3
				FOR UPDATE`, s.table),
			args...).Scan(&deletionTS)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return pkgdb.ErrNotFound
			}
			return fmt.Errorf("load row: %w", err)
		}

		// Tagged-artifact tables have no finalizers — hard-delete
		// immediately. This matches the OSS fast-path for finalizer-free
		// rows: `arctl delete X` then `arctl apply X` works without any
		// background GC.
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE namespace=$1 AND name=$2 AND tag=$3`, s.table),
			args...); err != nil {
			return fmt.Errorf("hard delete: %w", err)
		}
		return nil
	})
}

func (s *Store) deleteMutable(ctx context.Context, args []any) error {
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
			return s.recomputeLatestMutable(ctx, tx, args[0].(string), args[1].(string))
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
		return s.recomputeLatestMutable(ctx, tx, args[0].(string), args[1].(string))
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
// requires finalizers to be empty; for tagged-artifact tables there is
// no finalizers column, so any row past deletion_timestamp is purged.
// Returns the number of rows purged.
func (s *Store) PurgeFinalized(ctx context.Context) (int64, error) {
	var query string
	if s.behavior == TaggedArtifactStore {
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
// tag/version) ASC with updated_at as a stable tiebreaker. Pagination cursor
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
		if s.behavior == TaggedArtifactStore {
			args = append(args, DefaultTag())
			where = append(where, fmt.Sprintf("tag = $%d", len(args)))
		} else if opts.IncludeTerminating {
			where = append(where, "(is_latest_version OR deletion_timestamp IS NOT NULL)")
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
		// Order by stable identity (namespace, name, tag/version) first so a
		// row's updated_at changing under a concurrent PatchStatus does
		// not let it skip across pages.
		args = append(args, cursor.Namespace, cursor.Name, cursor.Identity, cursor.UpdatedAt)
		idCol := s.identityColumn()
		where = append(where, fmt.Sprintf(
			"(namespace, name, %s, updated_at) > ($%d, $%d, $%d, $%d)",
			idCol,
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
	query += fmt.Sprintf(" ORDER BY namespace, name, %s, updated_at LIMIT $%d", s.identityColumn(), len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list: %w", err)
	}
	defer rows.Close()

	out := make([]*v1alpha1.RawObject, 0, limit)
	for rows.Next() {
		obj, err := scanRow(rows, s.behavior == TaggedArtifactStore)
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
	if cursor.UpdatedAt.IsZero() || cursor.Namespace == "" || cursor.Name == "" || cursor.Identity == "" {
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
		Identity:  obj.StorageIdentity,
	}
	if obj.Metadata.Tag != "" {
		cursor.Identity = obj.Metadata.Tag
	}
	if cursor.UpdatedAt.IsZero() || cursor.Namespace == "" || cursor.Name == "" || cursor.Identity == "" {
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
	// LatestOnly, when true, restricts to the literal "latest" tag per
	// (namespace, name), or the private latest row for mutable-object stores.
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
		if s.behavior == TaggedArtifactStore {
			args = append(args, DefaultTag())
			query += fmt.Sprintf(" AND tag = $%d", len(args))
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
		obj, err := scanRow(rows, s.behavior == TaggedArtifactStore)
		if err != nil {
			return nil, err
		}
		out = append(out, obj)
	}
	return out, rows.Err()
}

// recomputeLatestMutable recomputes is_latest_version for mutable-object
// tables. Tagged-artifact tables have no is_latest_version column; latest is
// the literal tag named by DefaultTag().
func (s *Store) recomputeLatestMutable(ctx context.Context, tx pgx.Tx, namespace, name string) error {
	if s.behavior == TaggedArtifactStore {
		return nil
	}
	if _, err := tx.Exec(ctx,
		fmt.Sprintf(`
			UPDATE %s SET is_latest_version = false
			WHERE namespace=$1 AND name=$2 AND is_latest_version`, s.table),
		namespace, name); err != nil {
		return fmt.Errorf("clear latest: %w", err)
	}
	// Pick the most recently updated non-terminating row. The storage
	// version is private and has no semantic ordering contract.
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
// queries. Mutable-object tables include generation/finalizers columns;
// tagged-artifact tables emit synthetic placeholders for them so scanRow's
// column layout stays uniform.
func (s *Store) selectColumns() string {
	if s.behavior == TaggedArtifactStore {
		return `namespace, name, tag, uid::text, generation, labels, annotations, spec, status,
		       deletion_timestamp, '[]'::jsonb AS finalizers, created_at, updated_at`
	}
	return `namespace, name, version, uid::text, generation, labels, annotations, spec, status,
		       deletion_timestamp, finalizers, created_at, updated_at`
}

// identityArgs converts (ns, name, private identity) into the bind args used
// by per-row queries.
func (s *Store) identityArgs(namespace, name, identity string) ([]any, error) {
	if identity == "" && s.behavior == MutableObjectStore {
		identity = s.mutableIdentity
	}
	if identity == "" {
		return nil, fmt.Errorf("v1alpha1 store: identity is required for table %s", s.table)
	}
	return []any{namespace, name, identity}, nil
}

func (s *Store) identityColumn() string {
	if s.behavior == TaggedArtifactStore {
		return "tag"
	}
	return "version"
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
// canonical spec content. Used by the mutable-object path to
// detect spec-no-op apply.
func equalSpecJSON(existing []byte, incoming json.RawMessage) bool {
	return SpecHash(existing) == SpecHash(incoming)
}

// advisoryLockKey returns a deterministic 64-bit key for advisory locks
// scoped to a (table, namespace, name) tuple. Postgres advisory locks
// take a single bigint key (or a pair of int4s); we hash the composite
// with FNV-64a — collisions are harmless for serialization correctness
// (they only cause unrelated identities to occasionally serialize) and
// the upsert critical section is short, so contention from collisions
// is negligible in practice.
func (s *Store) advisoryLockKey(table, ns, name string) int64 {
	h := fnv.New64a()
	h.Write([]byte(table))
	h.Write([]byte{0})
	h.Write([]byte(ns))
	h.Write([]byte{0})
	h.Write([]byte(name))
	return int64(h.Sum64())
}
