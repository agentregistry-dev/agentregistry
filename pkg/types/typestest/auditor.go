// Package typestest provides shared test helpers for pkg/types
// implementations.
package typestest

import (
	"context"
	"sync"

	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// ResourceVersionEvent is one captured Auditor.ResourceVersionCreated call.
type ResourceVersionEvent struct {
	Kind      string
	Namespace string
	Name      string
	Version   int
}

// RecordingAuditor is a thread-safe types.Auditor that captures every
// ResourceVersionCreated event for assertions in tests. The mutex is
// load-bearing because the v1alpha1store concurrency test invokes the
// auditor from multiple goroutines.
type RecordingAuditor struct {
	mu     sync.Mutex
	events []ResourceVersionEvent
}

// ResourceVersionCreated records the event under the auditor's mutex.
func (r *RecordingAuditor) ResourceVersionCreated(_ context.Context, kind, namespace, name string, version int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ResourceVersionEvent{
		Kind:      kind,
		Namespace: namespace,
		Name:      name,
		Version:   version,
	})
}

// Events returns a copy of the captured events. Callers may mutate the
// returned slice without affecting the auditor's internal state.
func (r *RecordingAuditor) Events() []ResourceVersionEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ResourceVersionEvent, len(r.events))
	copy(out, r.events)
	return out
}

var _ types.Auditor = (*RecordingAuditor)(nil)
