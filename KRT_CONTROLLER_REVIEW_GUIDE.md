# KRT Controller Foundations Review Guide

## Current Branch State

- OSS branch: `ilackarms/krt-controller`
- Rebased on current OSS `origin/main` before this guide was written.
- Enterprise reference PR reviewed: `solo-io/agentregistry-enterprise#699`, merged as commit `cfc6fa60`.
- Focused verification run after rebase:
  - `go test ./internal/registry/controller`
  - `go test ./internal/registry/config`
  - `go test -tags=integration ./pkg/registry/v1alpha1store`

## Enterprise PR 699 Findings

PR 699 adds a concrete Kubernetes-side KRT pattern in enterprise:

- `internal/kube/apiclient/` wraps the agentgateway kube client so the process shares one Istio `kube.Client`, typed Agentgateway clientset, type registration, object filter, and `RunAndWait` lifecycle.
- `internal/kube/krtutil/options.go` centralizes `krt.WithName`, `krt.WithDebugging`, and `krt.WithStop` so collection constructors do not repeat those options everywhere.
- `internal/agentgateway/binding/store.go` defines a narrow `binding.Store` interface. The concrete `binding/k8s` package owns the KRT graph and Kubernetes SSA writes.
- `internal/agentgateway/binding/k8s/collections.go` builds the KRT graph as wrapped Gateway/HTTPRoute clients, filtered labeled collections, indices, and derived `RouteBinding` projections.
- `internal/registry/extensions/deployments/virtual_adapter.go` keeps desired output derivation mostly pure (`deriveAgwOutput`) and then applies backends/routes through the binding store.
- `internal/registry/extensions/deployments/virtual_retrigger.go` is explicitly a temporary bridge until the OSS KRT reconciler exists.

What we can reuse conceptually:

- Keep source graph construction, output derivation, write/apply surfaces, and app startup wiring separate.
- Have constructors wire collections but let the app layer own informer start/sync ordering.
- Expose small interfaces at package boundaries instead of leaking KRT collection types everywhere.
- Make reconciler derivation pure enough that tests can cover logic without a live kube client.

What is different from our OSS branch:

- Enterprise PR 699 watches Kubernetes resources directly; our branch lays down the database invalidation/event log and durable work substrate for an OSS controller.
- Enterprise PR 699 adds real runtime behavior for `Virtual`; our branch is intentionally foundations-only and does not run adapters.
- Enterprise has KRT and kube dependencies; this OSS branch currently avoids adding those dependencies until the concrete controller loop lands.

Adoption caveat:

- In `internal/agentgateway/binding/k8s/collections.go`, `labeledGW.Equals` ignores Gateway status while `buildGatewayAttachment` reads Gateway status for listener readiness and addresses. Before copying that pattern into OSS, confirm whether enterprise has another refresh path for status changes or adjust equality to compare the reduced status projection that the derived binding actually reads.

## Review Order

### 1. `pkg/registry/v1alpha1store/migrations/009_controller_foundations.sql`

Review this first because it defines the durable contract.

Check:

- `control_plane_events` is identity-only invalidation state, not object history.
- Triggers exist for all v1alpha1 source tables the future projector should observe.
- Status-only updates are suppressed, while spec/labels/annotations/finalizer/deletion changes emit events.
- `pg_notify('v1alpha1_control_plane_changed', ...)` is coarse wakeup only; consumers must replay the event table.
- `reconcile_work` models current durable work with leases/backoff.
- `reconcile_events` is attempt history and is not required for desired-state recovery.
- Indexes match expected access paths: event replay by revision, work claiming by state/time, history lookup by work key.

Questions to keep in mind:

- Are the trigger comparisons broad enough to catch every source change the reconciler should care about?
- Do we want successful work to be deleted forever, or eventually moved through `completed` before pruning? The current Go `Complete` method deletes rows.

### 2. `pkg/registry/v1alpha1store/control_plane_events.go`

This is the Go API over the invalidation log.

Check:

- `ListAfter` is revision-ordered and bounded.
- `OldestRevision` plus `CurrentRevision` give projectors enough information to detect retention gaps.
- `PruneBefore` requires either an age bound or a revision retention bound.
- Returned events carry only resource identity, UID, generation, operation, and commit time.

### 3. `pkg/registry/v1alpha1store/reconcile_store.go`

This is the durable work queue and attempt-history API.

Check:

- `Upsert` validates identity/action/generation and resets an existing work key to pending.
- `ClaimDue` uses `FOR UPDATE SKIP LOCKED` and treats expired running leases as claimable.
- `Backoff` releases leases and schedules retry.
- `Complete` is idempotent around crash recovery.
- `ReconcileEventStore.Append` records attempts after the executor has a result.

Review carefully:

- Conflict behavior in `Upsert`: refreshing a work key while another worker is running clears the lease.
- Lease time comparison uses database `NOW()`, while callers supply `leaseUntil`.

### 4. `internal/registry/controller/projector.go`

This is the behavior-preserving replay skeleton.

Check:

- It refuses to run without an event reader.
- It full-resyncs when the checkpoint falls behind retained events.
- It only advances checkpoint after each event is applied.
- `SourceCollection` is a small placeholder/test skeleton, not the final KRT collection implementation.

Potential future connection to PR 699:

- This is where a later OSS implementation can swap `SourceCollection` for real KRT source collections, using the enterprise-style separation between collection wiring and app lifecycle.

### 5. `internal/registry/controller/retention.go`

This is the maintenance seam over the bounded-history tables.

Check:

- Zero or negative durations disable pruning for a table.
- `control_plane_events` pruning passes both a time cutoff and `EventKeepAfterRev`, so operators can preserve events needed by lagging projectors.
- Pruning errors are contextual and joined so one failed table does not hide another.
- Canonical source tables are never touched by this maintenance path.

### 6. `internal/registry/controller/deployment.go`

This derives durable work from a Deployment row.

Check:

- Apply/remove action selection matches Deployment semantics:
  - empty or `Deployed` -> `apply`
  - `Undeployed` or deletion timestamp -> `remove`
- Work keys include kind, namespace, name, UID, generation, and action.
- Payload stores refs and desired state, not full object snapshots.
- Executors are expected to re-read the current source rows after claiming work.

### 7. `internal/registry/config/config.go` and `internal/registry/config/validate.go`

These expose retention knobs under the existing `AGENT_REGISTRY_` env prefix.

Check:

- Event, work, and attempt-history retention can be tuned independently.
- `CONTROLLER_EVENT_KEEP_AFTER_REVISION` gives the future controller runner a safety knob for projector checkpoints.
- Negative durations/revisions/batch limits are rejected.

### 8. Tests

Review these after the implementation files:

- `internal/registry/controller/projector_test.go`
  - event replay into source collection
  - retention-gap full resync
  - delete event behavior
- `internal/registry/controller/deployment_test.go`
  - deployment desired-state to work derivation
  - terminating deployment removal
  - invalid desired state rejection
- `pkg/registry/v1alpha1store/store_test.go`
  - control-plane events track source writes only
  - status-only patches do not invalidate source collections
  - reconcile work claim/backoff/event/complete flow
  - control-plane event pruning preserves the keep-after revision
  - work and attempt-history pruning remove bounded history while preserving pending work
- `internal/registry/controller/retention_test.go`
  - retention cutoffs and batch limit are passed to the right stores
  - disabled policy does not call pruners
  - joined errors keep table-specific context
- `internal/registry/config/config_test.go`
  - env overrides parse retention durations, keep-after revision, and batch size

## Suggested Review Questions

- Is this enough foundation for the next PR to add a concrete controller runner without another schema migration?
- Should the next implementation mirror enterprise PR 699's package shape: source graph, pure derivation, executor/apply surface, app wiring?
- Should OSS introduce a tiny `krtutil` equivalent only when the first concrete KRT collection lands?
- Should the enterprise Virtual retrigger shim be removed only after OSS has an equivalent notify-driven deployment reconciler, or should we first expose a shared reconciliation seam enterprise can call into?
