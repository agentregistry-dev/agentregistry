// Package typestest provides shared test helpers for pkg/types
// implementations.
package typestest

import (
	"context"
	"sync"

	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// ResourceTagEvent is one captured Auditor.ResourceTagCreated call.
type ResourceTagEvent struct {
	Kind      string
	Namespace string
	Name      string
	Tag       string
}

// RecordingAuditor is a thread-safe types.Auditor that captures every
// ResourceTagCreated event for assertions in tests. The mutex is
// load-bearing because the v1alpha1store concurrency test invokes the
// auditor from multiple goroutines.
type RecordingAuditor struct {
	mu     sync.Mutex
	events []ResourceTagEvent
}

// ResourceTagCreated records the event under the auditor's mutex.
func (r *RecordingAuditor) ResourceTagCreated(_ context.Context, kind, namespace, name, tag string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ResourceTagEvent{
		Kind:      kind,
		Namespace: namespace,
		Name:      name,
		Tag:       tag,
	})
}

// Events returns a copy of the captured events. Callers may mutate the
// returned slice without affecting the auditor's internal state.
func (r *RecordingAuditor) Events() []ResourceTagEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ResourceTagEvent, len(r.events))
	copy(out, r.events)
	return out
}

var _ types.Auditor = (*RecordingAuditor)(nil)
