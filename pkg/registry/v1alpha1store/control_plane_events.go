package v1alpha1store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ControlPlaneNotifyChannel is the single coarse wakeup channel for controller
// projectors. The payload is only a hint; projectors must replay
// control_plane_events and re-read canonical source rows.
const ControlPlaneNotifyChannel = "v1alpha1_control_plane_changed"

const defaultEventBatchLimit = 500

// ResourceKey identifies a source row in the v1alpha1 control plane.
type ResourceKey struct {
	Kind      string
	Namespace string
	Name      string
	Tag       string
}

// ControlPlaneEvent records that a canonical v1alpha1 source row changed after
// a monotonic revision. It intentionally carries identity only, not object
// payload or derived desired state.
type ControlPlaneEvent struct {
	Revision    int64
	Key         ResourceKey
	UID         string
	Generation  int64
	Operation   string
	CommittedAt time.Time
}

// ControlPlaneEventStore reads and prunes the durable invalidation cursor used
// by KRT projectors.
type ControlPlaneEventStore struct {
	pool *pgxpool.Pool
}

// NewControlPlaneEventStore constructs a control-plane event reader.
func NewControlPlaneEventStore(pool *pgxpool.Pool) *ControlPlaneEventStore {
	return &ControlPlaneEventStore{pool: pool}
}

// ListAfter returns events with revision > afterRevision, ordered by revision.
func (s *ControlPlaneEventStore) ListAfter(ctx context.Context, afterRevision int64, limit int) ([]ControlPlaneEvent, error) {
	if s == nil || s.pool == nil {
		return nil, errors.New("v1alpha1 store: control-plane event store has nil pool")
	}
	if limit <= 0 {
		limit = defaultEventBatchLimit
	}
	rows, err := s.pool.Query(ctx, `
		SELECT revision, kind, namespace, name, tag, uid::text, generation, op, committed_at
		FROM v1alpha1.control_plane_events
		WHERE revision > $1
		ORDER BY revision
		LIMIT $2`, afterRevision, limit)
	if err != nil {
		return nil, fmt.Errorf("list control-plane events: %w", err)
	}
	defer rows.Close()

	var out []ControlPlaneEvent
	for rows.Next() {
		event, err := scanControlPlaneEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read control-plane events: %w", err)
	}
	return out, nil
}

// OldestRevision returns the oldest retained event revision. ok=false means the
// table is empty.
func (s *ControlPlaneEventStore) OldestRevision(ctx context.Context) (revision int64, ok bool, err error) {
	if s == nil || s.pool == nil {
		return 0, false, errors.New("v1alpha1 store: control-plane event store has nil pool")
	}
	var oldest sql.NullInt64
	if err := s.pool.QueryRow(ctx, `SELECT MIN(revision) FROM v1alpha1.control_plane_events`).Scan(&oldest); err != nil {
		return 0, false, fmt.Errorf("load oldest control-plane event revision: %w", err)
	}
	if !oldest.Valid {
		return 0, false, nil
	}
	return oldest.Int64, true, nil
}

// CurrentRevision returns the current high-water revision, or 0 when the event
// table is empty.
func (s *ControlPlaneEventStore) CurrentRevision(ctx context.Context) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, errors.New("v1alpha1 store: control-plane event store has nil pool")
	}
	var revision int64
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(revision), 0) FROM v1alpha1.control_plane_events`).Scan(&revision); err != nil {
		return 0, fmt.Errorf("load current control-plane event revision: %w", err)
	}
	return revision, nil
}

// PruneBefore deletes retained events in bounded batches. At least one of
// before or keepAfterRevision must be set. Projectors must use gap detection
// before relying on pruning in production.
func (s *ControlPlaneEventStore) PruneBefore(ctx context.Context, before time.Time, keepAfterRevision int64, limit int) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, errors.New("v1alpha1 store: control-plane event store has nil pool")
	}
	if before.IsZero() && keepAfterRevision <= 0 {
		return 0, errors.New("v1alpha1 store: control-plane event prune requires an age or revision bound")
	}
	if limit <= 0 {
		limit = defaultEventBatchLimit
	}
	var beforeArg any
	if !before.IsZero() {
		beforeArg = before
	}
	cmdTag, err := s.pool.Exec(ctx, `
		WITH doomed AS (
			SELECT revision
			FROM v1alpha1.control_plane_events
			WHERE ($1::timestamptz IS NULL OR committed_at < $1)
			  AND ($2::bigint <= 0 OR revision < $2)
			ORDER BY revision
			LIMIT $3
		)
		DELETE FROM v1alpha1.control_plane_events e
		USING doomed
		WHERE e.revision = doomed.revision`, beforeArg, keepAfterRevision, limit)
	if err != nil {
		return 0, fmt.Errorf("prune control-plane events: %w", err)
	}
	return cmdTag.RowsAffected(), nil
}

func scanControlPlaneEvent(row pgx.Row) (ControlPlaneEvent, error) {
	var event ControlPlaneEvent
	if err := row.Scan(
		&event.Revision,
		&event.Key.Kind,
		&event.Key.Namespace,
		&event.Key.Name,
		&event.Key.Tag,
		&event.UID,
		&event.Generation,
		&event.Operation,
		&event.CommittedAt,
	); err != nil {
		return ControlPlaneEvent{}, fmt.Errorf("scan control-plane event: %w", err)
	}
	return event, nil
}
