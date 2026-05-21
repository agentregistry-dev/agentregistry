package v1alpha1store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultWorkBatchLimit = 100

// ReconcileWork is durable, current coordination state. It is intentionally
// separate from ReconcileEvent history.
type ReconcileWork struct {
	Key           string
	Resource      ResourceKey
	UID           string
	Generation    int64
	Action        string
	Reason        string
	Payload       json.RawMessage
	State         string
	Attempt       int
	NextAttemptAt time.Time
	LeaseOwner    string
	LeaseUntil    *time.Time
	LastError     string
	CompletedAt   *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ReconcileWorkStore owns durable work claims and leases.
type ReconcileWorkStore struct {
	pool *pgxpool.Pool
}

// NewReconcileWorkStore constructs a reconcile work store.
func NewReconcileWorkStore(pool *pgxpool.Pool) *ReconcileWorkStore {
	return &ReconcileWorkStore{pool: pool}
}

// Upsert inserts or coalesces work for the same key. Existing running/backoff
// state is reset to pending only when the requested generation/action key is the
// same durable work item being refreshed.
func (s *ReconcileWorkStore) Upsert(ctx context.Context, work ReconcileWork) error {
	if s == nil || s.pool == nil {
		return errors.New("v1alpha1 store: reconcile work store has nil pool")
	}
	if work.Key == "" || work.Resource.Kind == "" || work.Resource.Namespace == "" || work.Resource.Name == "" || work.Action == "" {
		return errors.New("v1alpha1 store: reconcile work key, resource identity, and action are required")
	}
	if work.Generation <= 0 {
		return errors.New("v1alpha1 store: reconcile work generation must be positive")
	}
	if len(work.Payload) == 0 {
		work.Payload = json.RawMessage(`{}`)
	}
	if work.NextAttemptAt.IsZero() {
		work.NextAttemptAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO v1alpha1.reconcile_work (
			key, kind, namespace, name, tag, uid, generation, action, reason, payload, state, next_attempt_at
		) VALUES (
			$1, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7, $8, $9, $10, 'pending', $11
		)
		ON CONFLICT (key) DO UPDATE
		SET kind = EXCLUDED.kind,
		    namespace = EXCLUDED.namespace,
		    name = EXCLUDED.name,
		    tag = EXCLUDED.tag,
		    uid = EXCLUDED.uid,
		    generation = EXCLUDED.generation,
		    action = EXCLUDED.action,
		    reason = EXCLUDED.reason,
		    payload = EXCLUDED.payload,
		    state = 'pending',
		    next_attempt_at = LEAST(v1alpha1.reconcile_work.next_attempt_at, EXCLUDED.next_attempt_at),
		    lease_owner = NULL,
		    lease_until = NULL,
		    completed_at = NULL,
		    last_error = NULL`,
		work.Key,
		work.Resource.Kind,
		work.Resource.Namespace,
		work.Resource.Name,
		work.Resource.Tag,
		work.UID,
		work.Generation,
		work.Action,
		work.Reason,
		[]byte(work.Payload),
		work.NextAttemptAt,
	)
	if err != nil {
		return fmt.Errorf("upsert reconcile work: %w", err)
	}
	return nil
}

// ClaimDue claims currently due work using row locks so multiple workers can
// race safely. Expired running leases are claimable again.
func (s *ReconcileWorkStore) ClaimDue(ctx context.Context, owner string, leaseUntil time.Time, limit int) ([]ReconcileWork, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("v1alpha1 store: reconcile work store has nil pool")
	}
	if owner == "" {
		return nil, errors.New("v1alpha1 store: reconcile work lease owner is required")
	}
	if leaseUntil.IsZero() {
		return nil, errors.New("v1alpha1 store: reconcile work lease_until is required")
	}
	if limit <= 0 {
		limit = defaultWorkBatchLimit
	}
	rows, err := s.pool.Query(ctx, `
		WITH due AS (
			SELECT key
			FROM v1alpha1.reconcile_work
			WHERE (
				state IN ('pending', 'backoff')
				AND next_attempt_at <= NOW()
			) OR (
				state = 'running'
				AND lease_until < NOW()
			)
			ORDER BY next_attempt_at, key
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE v1alpha1.reconcile_work w
		SET state = 'running',
		    lease_owner = $2,
		    lease_until = $3,
		    attempt = w.attempt + 1,
		    last_error = NULL
		FROM due
		WHERE w.key = due.key
		RETURNING w.key, w.kind, w.namespace, w.name, w.tag, COALESCE(w.uid::text, ''),
		          w.generation, w.action, w.reason, w.payload, w.state, w.attempt,
		          w.next_attempt_at, w.lease_owner, w.lease_until, w.last_error,
		          w.completed_at, w.created_at, w.updated_at`,
		limit, owner, leaseUntil)
	if err != nil {
		return nil, fmt.Errorf("claim reconcile work: %w", err)
	}
	defer rows.Close()

	var out []ReconcileWork
	for rows.Next() {
		work, err := scanReconcileWork(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, work)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read claimed reconcile work: %w", err)
	}
	return out, nil
}

// Complete deletes work after the caller has recorded its outcome in
// reconcile_events. A missing key is a no-op, which keeps executor retries
// idempotent around crash recovery.
func (s *ReconcileWorkStore) Complete(ctx context.Context, key string) error {
	if s == nil || s.pool == nil {
		return errors.New("v1alpha1 store: reconcile work store has nil pool")
	}
	if key == "" {
		return errors.New("v1alpha1 store: reconcile work key is required")
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM v1alpha1.reconcile_work WHERE key=$1`, key); err != nil {
		return fmt.Errorf("complete reconcile work: %w", err)
	}
	return nil
}

// Backoff releases a claim and makes the work due again at nextAttemptAt.
func (s *ReconcileWorkStore) Backoff(ctx context.Context, key, lastError string, nextAttemptAt time.Time) error {
	if s == nil || s.pool == nil {
		return errors.New("v1alpha1 store: reconcile work store has nil pool")
	}
	if key == "" {
		return errors.New("v1alpha1 store: reconcile work key is required")
	}
	if nextAttemptAt.IsZero() {
		return errors.New("v1alpha1 store: reconcile work next_attempt_at is required")
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE v1alpha1.reconcile_work
		SET state='backoff',
		    next_attempt_at=$2,
		    lease_owner=NULL,
		    lease_until=NULL,
		    last_error=$3
		WHERE key=$1`, key, nextAttemptAt, lastError)
	if err != nil {
		return fmt.Errorf("backoff reconcile work: %w", err)
	}
	return nil
}

// Prune removes completed or abandoned rows in bounded batches. Pending,
// running, and backoff rows remain actionable and are never pruned here.
func (s *ReconcileWorkStore) Prune(ctx context.Context, before time.Time, limit int) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, errors.New("v1alpha1 store: reconcile work store has nil pool")
	}
	if before.IsZero() {
		return 0, errors.New("v1alpha1 store: reconcile work prune requires an age bound")
	}
	if limit <= 0 {
		limit = defaultWorkBatchLimit
	}
	cmdTag, err := s.pool.Exec(ctx, `
		WITH doomed AS (
			SELECT key
			FROM v1alpha1.reconcile_work
			WHERE state IN ('completed', 'abandoned')
			  AND COALESCE(completed_at, updated_at) < $1
			ORDER BY COALESCE(completed_at, updated_at), key
			LIMIT $2
		)
		DELETE FROM v1alpha1.reconcile_work w
		USING doomed
		WHERE w.key = doomed.key`, before, limit)
	if err != nil {
		return 0, fmt.Errorf("prune reconcile work: %w", err)
	}
	return cmdTag.RowsAffected(), nil
}

// ReconcileEvent is append-only-ish operational history. It may be pruned by
// policy, but it is never used as source-of-truth desired state.
type ReconcileEvent struct {
	ID            int64
	WorkKey       string
	Resource      ResourceKey
	UID           string
	Generation    int64
	Action        string
	Attempt       int
	Outcome       string
	Message       string
	Error         string
	NextAttemptAt *time.Time
	CreatedAt     time.Time
}

// ReconcileEventStore owns attempt history.
type ReconcileEventStore struct {
	pool *pgxpool.Pool
}

// NewReconcileEventStore constructs a reconcile event store.
func NewReconcileEventStore(pool *pgxpool.Pool) *ReconcileEventStore {
	return &ReconcileEventStore{pool: pool}
}

// Append records one reconcile attempt/outcome.
func (s *ReconcileEventStore) Append(ctx context.Context, event ReconcileEvent) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, errors.New("v1alpha1 store: reconcile event store has nil pool")
	}
	if event.WorkKey == "" || event.Resource.Kind == "" || event.Resource.Namespace == "" || event.Resource.Name == "" || event.Action == "" || event.Outcome == "" {
		return 0, errors.New("v1alpha1 store: reconcile event work key, identity, action, and outcome are required")
	}
	if event.Generation <= 0 {
		return 0, errors.New("v1alpha1 store: reconcile event generation must be positive")
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO v1alpha1.reconcile_events (
			work_key, kind, namespace, name, tag, uid, generation, action,
			attempt, outcome, message, error, next_attempt_at
		) VALUES (
			$1, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7, $8,
			$9, $10, $11, $12, $13
		)
		RETURNING id`,
		event.WorkKey,
		event.Resource.Kind,
		event.Resource.Namespace,
		event.Resource.Name,
		event.Resource.Tag,
		event.UID,
		event.Generation,
		event.Action,
		event.Attempt,
		event.Outcome,
		event.Message,
		event.Error,
		event.NextAttemptAt,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("append reconcile event: %w", err)
	}
	return id, nil
}

// ListByWorkKey returns recent history for one work key.
func (s *ReconcileEventStore) ListByWorkKey(ctx context.Context, workKey string, limit int) ([]ReconcileEvent, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("v1alpha1 store: reconcile event store has nil pool")
	}
	if workKey == "" {
		return nil, errors.New("v1alpha1 store: reconcile event work key is required")
	}
	if limit <= 0 {
		limit = defaultWorkBatchLimit
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, work_key, kind, namespace, name, tag, COALESCE(uid::text, ''),
		       generation, action, attempt, outcome, message, error, next_attempt_at, created_at
		FROM v1alpha1.reconcile_events
		WHERE work_key=$1
		ORDER BY created_at DESC, id DESC
		LIMIT $2`, workKey, limit)
	if err != nil {
		return nil, fmt.Errorf("list reconcile events: %w", err)
	}
	defer rows.Close()

	var out []ReconcileEvent
	for rows.Next() {
		event, err := scanReconcileEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read reconcile events: %w", err)
	}
	return out, nil
}

// Prune deletes old reconcile history in bounded batches.
func (s *ReconcileEventStore) Prune(ctx context.Context, before time.Time, limit int) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, errors.New("v1alpha1 store: reconcile event store has nil pool")
	}
	if before.IsZero() {
		return 0, errors.New("v1alpha1 store: reconcile event prune requires an age bound")
	}
	if limit <= 0 {
		limit = defaultWorkBatchLimit
	}
	cmdTag, err := s.pool.Exec(ctx, `
		WITH doomed AS (
			SELECT id
			FROM v1alpha1.reconcile_events
			WHERE created_at < $1
			ORDER BY created_at, id
			LIMIT $2
		)
		DELETE FROM v1alpha1.reconcile_events e
		USING doomed
		WHERE e.id = doomed.id`, before, limit)
	if err != nil {
		return 0, fmt.Errorf("prune reconcile events: %w", err)
	}
	return cmdTag.RowsAffected(), nil
}

func scanReconcileWork(row pgx.Row) (ReconcileWork, error) {
	var (
		work        ReconcileWork
		leaseUntil  sql.NullTime
		completedAt sql.NullTime
		payload     []byte
		uid         string
		leaseOwner  sql.NullString
		lastError   sql.NullString
	)
	if err := row.Scan(
		&work.Key,
		&work.Resource.Kind,
		&work.Resource.Namespace,
		&work.Resource.Name,
		&work.Resource.Tag,
		&uid,
		&work.Generation,
		&work.Action,
		&work.Reason,
		&payload,
		&work.State,
		&work.Attempt,
		&work.NextAttemptAt,
		&leaseOwner,
		&leaseUntil,
		&lastError,
		&completedAt,
		&work.CreatedAt,
		&work.UpdatedAt,
	); err != nil {
		return ReconcileWork{}, fmt.Errorf("scan reconcile work: %w", err)
	}
	work.UID = uid
	work.Payload = append(json.RawMessage(nil), payload...)
	if leaseOwner.Valid {
		work.LeaseOwner = leaseOwner.String
	}
	if leaseUntil.Valid {
		work.LeaseUntil = &leaseUntil.Time
	}
	if lastError.Valid {
		work.LastError = lastError.String
	}
	if completedAt.Valid {
		work.CompletedAt = &completedAt.Time
	}
	return work, nil
}

func scanReconcileEvent(row pgx.Row) (ReconcileEvent, error) {
	var (
		event         ReconcileEvent
		uid           string
		nextAttemptAt sql.NullTime
	)
	if err := row.Scan(
		&event.ID,
		&event.WorkKey,
		&event.Resource.Kind,
		&event.Resource.Namespace,
		&event.Resource.Name,
		&event.Resource.Tag,
		&uid,
		&event.Generation,
		&event.Action,
		&event.Attempt,
		&event.Outcome,
		&event.Message,
		&event.Error,
		&nextAttemptAt,
		&event.CreatedAt,
	); err != nil {
		return ReconcileEvent{}, fmt.Errorf("scan reconcile event: %w", err)
	}
	event.UID = uid
	if nextAttemptAt.Valid {
		event.NextAttemptAt = &nextAttemptAt.Time
	}
	return event, nil
}
