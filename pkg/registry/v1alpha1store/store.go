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
	pool    *pgxpool.Pool
	table   string
	legacy  bool
	kind    string
	auditor types.Auditor
}

// Re-exports of the StoreMode enum from pkg/types so callers that already
// import this package don't need to pull in pkg/types just to register
// an extra store. The canonical declarations live in pkg/types because
// pkg/types.AppOptions is the surface that consumes them and we don't
// want to invert the v1alpha1store -> types import direction.
type StoreMode = types.StoreMode

const (
	// StoreModeVersionedArtifact mirrors types.StoreModeVersionedArtifact.
	StoreModeVersionedArtifact = types.StoreModeVersionedArtifact
	// StoreModeMutable mirrors types.StoreModeMutable.
	StoreModeMutable = types.StoreModeMutable
)

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

// NewStore constructs a versioned-artifact Store bound to a single table
// (e.g. "v1alpha1.agents"). The table must exist in the schema; NewStore
// does not validate it.
//
// For the deployments table, use NewDeploymentStore — passing
// "v1alpha1.deployments" here is a programming error (the row layout
// differs and the wrong code path will be taken).
func NewStore(pool *pgxpool.Pool, table string, opts ...StoreOption) *Store {
	s := &Store{pool: pool, table: table, legacy: false, auditor: types.NoopAuditor}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// NewDeploymentStore constructs a legacy-mode Store for the deployments
// table. The table must exist in the schema; this constructor does not
// validate it.
//
// Deployment is the only kind that opts into the legacy shape today; if
// a future kind needs the same lifecycle-state semantics, plumb it
// through here rather than re-introducing a table-name flip.
func NewDeploymentStore(pool *pgxpool.Pool, table string, opts ...StoreOption) *Store {
	s := &Store{pool: pool, table: table, legacy: true, auditor: types.NoopAuditor}
	for _, opt := range opts {
		opt(s)
	}
	return s
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

// ErrPlanStale reports that an Apply call's UpsertPlan no longer
// reflects the live state of the row — between Plan and Apply another
// writer either created a new version, deleted the row, or shifted the
// tombstone. The approver's intent (encoded in the plan) is no longer
// safe to apply unchanged; the caller must re-Plan against the current
// state and re-confirm with the user.
var ErrPlanStale = errors.New("v1alpha1 store: plan is stale; re-plan and retry")

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

// UpsertPlan describes the write that Upsert (or its callers) would
// perform if applied right now. It is a snapshot — the actual Apply must
// re-check the world under the advisory lock and return ErrPlanStale if
// the state moved (different latest version, different latest hash, or
// the row went terminating). Plans are intended for approval-staging
// use cases: produce a Plan, persist it (alongside any human-review
// metadata), and later hand it back to Apply once a reviewer signs off.
//
// The exported fields carry the full identity + decision needed to
// replay the write. The unexported "witness" fields snapshot the live
// state Plan saw and are checked by Apply for TOCTOU safety. Only Plans
// produced by THIS process / this binary are safe to feed back into
// Apply — cross-process or cross-version serialization is a follow-up
// (it would require exporting the witness fields with a stable schema
// guarantee).
type UpsertPlan struct {
	// Kind is the canonical Kind name attached to audit events fired by
	// Apply. Equal to s.kindFor(obj) at Plan time so a future Apply does
	// not need the original object back.
	Kind string
	// Namespace, Name pin the identity Plan describes.
	Namespace string
	Name      string
	// Labels / Annotations are the canonical-form JSON the row will
	// carry after Apply (matches what canonicalJSONMap would emit for
	// the inbound metadata).
	Labels      json.RawMessage
	Annotations json.RawMessage
	// Spec is the raw JSON spec to write. Apply does not re-marshal it.
	Spec json.RawMessage
	// SpecHash is the SHA-256 of Spec, computed once at Plan time.
	SpecHash string
	// Outcome is the predicted UpsertOutcome (Created / NoOp /
	// LabelsUpdated). Apply executes the matching write.
	Outcome UpsertOutcome
	// Version is the proposed version Apply will INSERT at (Created)
	// or the existing version it will leave in place (NoOp /
	// LabelsUpdated).
	Version int

	// Witness fields snapshot the live state at Plan time. Apply
	// re-reads the same fields under the advisory lock and rejects
	// the call as ErrPlanStale if anything moved. Unexported because
	// they're an internal TOCTOU guard, not part of the user contract.
	witnessFound         bool
	witnessLatestVersion int
	witnessLatestHash    string
	witnessTombstoneMax  int
}

// errPlanLegacy reports that Plan/Apply was called on a legacy
// (deployments / providers) Store. The legacy upsert path uses
// caller-supplied string versions and update-in-place semantics that
// don't share the witness model the planning seam was designed
// around. Routed back through Upsert (which dispatches to upsertLegacy)
// when callers need legacy behaviour.
var errPlanLegacy = errors.New("v1alpha1 store: Plan/Apply only supported for versioned-artifact stores")

// Upsert applies obj into the Store. Behaviour depends on the table's
// versioning mode:
//
//   - Versioned-artifact tables (agents, mcp_servers, etc.) follow
//     hash-based append-only apply semantics:
//   - new (namespace, name) → insert at version=1 (or
//     tombstone.max_assigned+1 if any version was ever assigned)
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
//
// On the versioned-artifact path Upsert is a thin wrapper over
// Plan + Apply: the plan is computed under an advisory lock that
// auto-releases at the (read-only) Plan transaction's commit, then
// Apply re-acquires the lock to execute the write. Concurrent writers
// can race in between the two transactions; Upsert hides that by
// retrying ErrPlanStale internally so its synchronous-apply contract
// stays unchanged. Callers that need the plan visible before the write
// (approval staging) should call Plan and Apply directly and handle
// ErrPlanStale themselves.
func (s *Store) Upsert(ctx context.Context, obj v1alpha1.Object) (UpsertResult, error) {
	meta, specJSON, err := validateUpsertObject(obj)
	if err != nil {
		return UpsertResult{}, err
	}
	if s.legacy {
		return s.upsertLegacy(ctx, meta, specJSON)
	}
	// Bound the retry: a single Upsert call must not loop indefinitely
	// against a hot identity. upsertRetryBudget is generous enough that
	// a real workload always converges; if it doesn't, we surface the
	// staleness rather than pretending serialization holds.
	for range upsertRetryBudget {
		plan, err := s.planVersioned(ctx, obj, meta, specJSON)
		if err != nil {
			return UpsertResult{}, err
		}
		res, err := s.Apply(ctx, plan)
		if errors.Is(err, ErrPlanStale) {
			continue
		}
		return res, err
	}
	return UpsertResult{}, fmt.Errorf("v1alpha1 store: upsert exhausted %d retries against concurrent writers", upsertRetryBudget)
}

// upsertRetryBudget caps the Plan/Apply retry loop in Upsert. Picked
// generously: real contention serialises through the advisory lock
// during Plan, so back-to-back stales are unlikely. The bound exists
// to fail loudly if a starvation bug ever drives the loop forever.
const upsertRetryBudget = 8

// Plan computes the write Upsert would perform for obj without
// touching the row. It is the planning seam used by approval workflows:
// produce a Plan, persist it as "pending approval", and once a reviewer
// signs off feed it back through Apply. Plan is read-only at the row
// level — the only durable side effect within its transaction is the
// advisory-lock acquire/release, which auto-releases at commit.
//
// Returns errPlanLegacy on a legacy (Provider/Deployment) Store; that
// path uses caller-supplied versions and update-in-place semantics that
// do not match the witness-based TOCTOU model planning relies on.
func (s *Store) Plan(ctx context.Context, obj v1alpha1.Object) (UpsertPlan, error) {
	if s.legacy {
		return UpsertPlan{}, errPlanLegacy
	}
	meta, specJSON, err := validateUpsertObject(obj)
	if err != nil {
		return UpsertPlan{}, err
	}
	return s.planVersioned(ctx, obj, meta, specJSON)
}

// Apply executes a plan produced by Plan. Apply re-acquires the
// advisory lock for (namespace, name), re-reads the live state + the
// tombstone, and rejects the call with ErrPlanStale if the witness
// captured at Plan time no longer matches. On success Apply commits
// the row mutation and (for UpsertCreated outcomes only) fires
// auditor.ResourceVersionCreated AFTER the transaction commits.
//
// Apply does NOT validate the plan's payload — it trusts the caller
// produced it via Plan. Tampering with plan.Spec / plan.SpecHash will
// either short-circuit on a stale witness or write a poisoned row;
// callers that persist plans across trust boundaries must sign or
// otherwise integrity-check them.
//
// uid handling is intentionally deferred: the v1alpha1 schema uses a
// gen_random_uuid() column DEFAULT, so Apply never threads a uid into
// the INSERT. A future commit will plumb server-allocated uids through
// Plan (snapshotted) and Apply (asserted) so approvers see the exact
// uid that will be issued.
func (s *Store) Apply(ctx context.Context, plan UpsertPlan) (UpsertResult, error) {
	if s.legacy {
		return UpsertResult{}, errPlanLegacy
	}
	if plan.Namespace == "" || plan.Name == "" {
		return UpsertResult{}, errors.New("v1alpha1 store: plan namespace and name are required")
	}

	var result UpsertResult
	err := runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		key := s.advisoryLockKey(s.table, plan.Namespace, plan.Name)
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
			return fmt.Errorf("advisory lock: %w", err)
		}

		live, err := s.readPlanWitness(ctx, tx, plan.Namespace, plan.Name)
		if err != nil {
			return err
		}

		// Witness check: if any of the row + tombstone snapshot Plan saw
		// has shifted, the plan's outcome / version is no longer the
		// right answer for the current state. Reject and let the caller
		// re-plan.
		if !live.matches(plan) {
			return ErrPlanStale
		}
		if live.found && live.deletion.Valid {
			// Row went terminating since Plan — also stale. Surface as
			// ErrPlanStale rather than ErrTerminating so the staged
			// apply path treats it uniformly with "world moved".
			return ErrPlanStale
		}

		switch plan.Outcome {
		case UpsertNoOp:
			result = UpsertResult{Version: plan.Version, Outcome: UpsertNoOp}
			return nil

		case UpsertLabelsUpdated:
			if _, err := tx.Exec(ctx,
				fmt.Sprintf(`
					UPDATE %s
					SET labels = $4, annotations = $5
					WHERE namespace = $1 AND name = $2 AND version = $3`, s.table),
				plan.Namespace, plan.Name, plan.Version, []byte(plan.Labels), []byte(plan.Annotations)); err != nil {
				return fmt.Errorf("update labels: %w", err)
			}
			result = UpsertResult{Version: plan.Version, Outcome: UpsertLabelsUpdated}
			return nil

		case UpsertCreated:
			if _, err := tx.Exec(ctx,
				fmt.Sprintf(`
					INSERT INTO %s (namespace, name, version, labels, annotations, spec, spec_hash)
					VALUES ($1, $2, $3, $4, $5, $6, $7)`, s.table),
				plan.Namespace, plan.Name, plan.Version,
				[]byte(plan.Labels), []byte(plan.Annotations),
				[]byte(plan.Spec), plan.SpecHash); err != nil {
				// A unique-conflict on (namespace, name, version) means
				// another writer raced us in after the witness check
				// (rare — they would have had to acquire the same
				// advisory lock — but pg's advisory locks are advisory
				// only, and a direct-SQL writer could bypass them).
				// Treat as stale so the caller re-plans.
				return ErrPlanStale
			}
			if err := s.writeTombstone(ctx, tx, plan.Namespace, plan.Name, plan.Version); err != nil {
				return fmt.Errorf("update tombstone: %w", err)
			}
			result = UpsertResult{Version: plan.Version, Outcome: UpsertCreated}
			return nil

		default:
			return fmt.Errorf("v1alpha1 store: unknown plan outcome %d", plan.Outcome)
		}
	})
	if err != nil {
		return UpsertResult{}, err
	}
	// Audit fire happens AFTER tx commit, only for UpsertCreated. Same
	// gate as the pre-Plan/Apply Upsert.
	if result.Outcome == UpsertCreated {
		s.auditor.ResourceVersionCreated(ctx, plan.Kind, plan.Namespace, plan.Name, result.Version)
	}
	return result, nil
}

// planWitness snapshots the row + tombstone state Plan / Apply observe
// under the advisory lock. The same struct is populated identically by
// both phases so the witness comparison is a structural equality check.
type planWitness struct {
	found         bool
	latestVersion int
	latestHash    string
	latestLabels  []byte
	latestAnnots  []byte
	deletion      pgtype.Timestamptz
	tombstoneMax  int
}

// matches reports whether the witness Apply just observed agrees with
// the witness Plan recorded. Mismatches are TOCTOU races and yield
// ErrPlanStale. We compare only the fields that drive the decision
// (latest row version + hash, presence flag, tombstone high-water);
// labels/annotations on the latest row don't matter because Apply
// re-uses the labels/annotations from the plan itself.
func (w planWitness) matches(plan UpsertPlan) bool {
	if w.found != plan.witnessFound {
		return false
	}
	if w.found {
		if w.latestVersion != plan.witnessLatestVersion {
			return false
		}
		if w.latestHash != plan.witnessLatestHash {
			return false
		}
	}
	return w.tombstoneMax == plan.witnessTombstoneMax
}

// readPlanWitness reads the latest-live-row snapshot + tombstone
// high-water for (s.table, namespace, name) under the caller's tx.
// Used by both Plan (to record the witness) and Apply (to validate
// it). Caller must hold the advisory lock for this identity before
// calling.
func (s *Store) readPlanWitness(ctx context.Context, tx pgx.Tx, namespace, name string) (planWitness, error) {
	var w planWitness
	err := tx.QueryRow(ctx,
		fmt.Sprintf(`
			SELECT version, spec_hash, labels, annotations, deletion_timestamp
			FROM %s
			WHERE namespace=$1 AND name=$2
			ORDER BY version DESC
			LIMIT 1
			FOR UPDATE`, s.table),
		namespace, name,
	).Scan(&w.latestVersion, &w.latestHash, &w.latestLabels, &w.latestAnnots, &w.deletion)
	switch {
	case err == nil:
		w.found = true
	case errors.Is(err, pgx.ErrNoRows):
		w.found = false
	default:
		return planWitness{}, fmt.Errorf("load latest: %w", err)
	}

	// Always snapshot the tombstone (even when found) so Apply has the
	// full state Plan saw. Concurrent applies that bump the tombstone
	// will trip the witness check even if the live row's version stays
	// the same (e.g., delete + reapply between Plan and Apply).
	tombstoneMax, err := s.readTombstone(ctx, tx, namespace, name)
	if err != nil {
		return planWitness{}, fmt.Errorf("load tombstone: %w", err)
	}
	w.tombstoneMax = tombstoneMax
	return w, nil
}

// validateUpsertObject pulls the per-call invariants out of Upsert /
// Plan so both entrypoints share one definition of "valid object".
// Returns the metadata pointer + marshalled spec bytes, leaving the
// caller to dispatch on the Store's mode.
func validateUpsertObject(obj v1alpha1.Object) (*v1alpha1.ObjectMeta, json.RawMessage, error) {
	if obj == nil {
		return nil, nil, errors.New("v1alpha1 store: nil object")
	}
	meta := obj.GetMetadata()
	if meta == nil || meta.Namespace == "" || meta.Name == "" {
		return nil, nil, errors.New("v1alpha1 store: namespace and name are required")
	}
	specJSON, err := obj.MarshalSpec()
	if err != nil {
		return nil, nil, fmt.Errorf("v1alpha1 store: marshal spec: %w", err)
	}
	if len(specJSON) == 0 {
		return nil, nil, errors.New("v1alpha1 store: spec is required")
	}
	return meta, specJSON, nil
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

// planVersioned is the inner helper shared by Plan and Upsert. It runs
// the (lookup, decide) phase under an advisory lock and returns the
// UpsertPlan describing the write Apply would perform. The transaction
// is read-only at the row level — only the advisory lock changes
// state, and it auto-releases at commit so concurrent planners
// serialize without blocking the writer.
func (s *Store) planVersioned(
	ctx context.Context,
	obj v1alpha1.Object,
	meta *v1alpha1.ObjectMeta,
	specJSON json.RawMessage,
) (UpsertPlan, error) {
	incomingHash := SpecHash(specJSON)
	incomingLabelsJSON, err := canonicalJSONMap(meta.Labels)
	if err != nil {
		return UpsertPlan{}, fmt.Errorf("v1alpha1 store: marshal labels: %w", err)
	}
	incomingAnnotationsJSON, err := canonicalJSONMap(meta.Annotations)
	if err != nil {
		return UpsertPlan{}, fmt.Errorf("v1alpha1 store: marshal annotations: %w", err)
	}

	plan := UpsertPlan{
		Kind:        s.kindFor(obj),
		Namespace:   meta.Namespace,
		Name:        meta.Name,
		Labels:      json.RawMessage(incomingLabelsJSON),
		Annotations: json.RawMessage(incomingAnnotationsJSON),
		Spec:        specJSON,
		SpecHash:    incomingHash,
	}

	err = runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		key := s.advisoryLockKey(s.table, meta.Namespace, meta.Name)
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
			return fmt.Errorf("advisory lock: %w", err)
		}

		w, err := s.readPlanWitness(ctx, tx, meta.Namespace, meta.Name)
		if err != nil {
			return err
		}

		// Reject mutations on terminating rows the same way the pre-
		// Plan/Apply Upsert did. Plan reports the error to the caller;
		// Apply only sees plans we generated, so Apply's terminating
		// check guards against the row going terminating between
		// Plan and Apply.
		if w.found && w.deletion.Valid {
			return ErrTerminating
		}

		plan.witnessFound = w.found
		plan.witnessLatestVersion = w.latestVersion
		plan.witnessLatestHash = w.latestHash
		plan.witnessTombstoneMax = w.tombstoneMax

		switch {
		case !w.found:
			// Branch 1: no prior row. Resume from tombstone+1 so deleted
			// names don't recycle versions across delete cycles.
			plan.Outcome = UpsertCreated
			plan.Version = w.tombstoneMax + 1

		case incomingHash == w.latestHash:
			// Branch 2: hash match. Compare labels + annotations to
			// decide between NoOp and LabelsUpdated.
			labelsEqual := equalJSONMap(w.latestLabels, incomingLabelsJSON)
			annotationsEqual := equalJSONMap(w.latestAnnots, incomingAnnotationsJSON)
			if labelsEqual && annotationsEqual {
				plan.Outcome = UpsertNoOp
			} else {
				plan.Outcome = UpsertLabelsUpdated
			}
			plan.Version = w.latestVersion

		default:
			// Branch 3: spec change → MAX(version)+1.
			plan.Outcome = UpsertCreated
			plan.Version = w.latestVersion + 1
		}
		return nil
	})
	if err != nil {
		return UpsertPlan{}, err
	}
	return plan, nil
}

// readTombstone returns the high-water mark for (s.table, namespace, name)
// recorded in v1alpha1.version_tombstones. Returns 0 when no tombstone
// row has ever been written. Used to resume version numbering after
// DeleteAllVersions wipes the live rows.
func (s *Store) readTombstone(ctx context.Context, tx pgx.Tx, namespace, name string) (int, error) {
	var maxAssigned int
	err := tx.QueryRow(ctx, `
		SELECT max_assigned
		FROM v1alpha1.version_tombstones
		WHERE table_name=$1 AND namespace=$2 AND name=$3`,
		s.table, namespace, name,
	).Scan(&maxAssigned)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return maxAssigned, nil
}

// writeTombstone upserts the tombstone row keeping max_assigned monotonic.
// Concurrent inserts of differing versions all converge on the highest
// value via GREATEST(...). Never decremented; never deleted.
func (s *Store) writeTombstone(ctx context.Context, tx pgx.Tx, namespace, name string, newVersion int) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO v1alpha1.version_tombstones (table_name, namespace, name, max_assigned)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (table_name, namespace, name)
		DO UPDATE SET max_assigned = GREATEST(v1alpha1.version_tombstones.max_assigned, EXCLUDED.max_assigned),
		              updated_at = NOW()`,
		s.table, namespace, name, newVersion,
	)
	return err
}

// upsertLegacy implements the older string-version, in-place semantics
// for the legacy tables (deployments, providers). The caller supplies
// meta.Version explicitly; a re-apply with the same spec is a no-op
// (no generation today since the new outcome surface doesn't model
// it), a differing spec replaces the row, and labels/annotations
// always replace.
func (s *Store) upsertLegacy(ctx context.Context, meta *v1alpha1.ObjectMeta, specJSON json.RawMessage) (UpsertResult, error) {
	if meta.Version == "" {
		return UpsertResult{}, errors.New("v1alpha1 store: version is required for legacy-mode kinds")
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
			found          bool
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
			fmt.Sprintf(`UPDATE %s SET %s WHERE namespace=$1 AND name=$2 AND version=$3`,
				s.table, strings.Join(setClauses, ", ")),
			args...); err != nil {
			return fmt.Errorf("apply patch: %w", err)
		}
		return nil
	})
}

// loadPatchRow loads the columns ApplyPatch may mutate
// (status, annotations, and on legacy stores finalizers) and returns
// pkgdb.ErrNotFound if no row matches. The finalizers payload is empty
// for non-legacy stores.
func (s *Store) loadPatchRow(ctx context.Context, tx pgx.Tx, namespace, name, version string) (statusJSON, annotationsJSON, finalizersJSON []byte, err error) {
	if s.legacy {
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
				WHERE namespace=$1 AND name=$2 AND version=$3
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

// DeleteAllVersions hard-deletes every version row for (namespace, name)
// on a versioned-artifact table — the (ns, name) identity is freed
// entirely so the next apply starts at v1. This is the contract of the
// batch DELETE endpoint: identity is logical, callers cannot pin it to
// a single integer version. Per-version soft-delete tracking is
// unnecessary in versioned-artifact mode because every spec change
// already produces a fresh immutable row. Returns pkgdb.ErrNotFound
// when no row exists for (namespace, name).
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
