package kinds

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"

	"gopkg.in/yaml.v3"
)

// Registry is a concurrent-safe collection of Kind registrations.
// After registration is complete (typically at application startup), all
// public methods are safe for concurrent read.
type Registry struct {
	mu      sync.RWMutex
	byName  map[string]*Kind
	ordered []*Kind
}

// NewRegistry returns an empty Registry ready for Register calls.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]*Kind)}
}

// Register adds a Kind. It panics if:
//   - k.Kind is empty
//   - k.SpecType is nil
//   - any of the canonical name, plural, or aliases collides with an existing
//     registration
//
// Registrations are programmer-level setup; collisions are bugs that should
// fail fast at startup, not be handled gracefully at runtime.
func (r *Registry) Register(k Kind) {
	if k.Kind == "" {
		panic("kinds.Register: Kind is required")
	}
	if k.SpecType == nil {
		panic(fmt.Sprintf("kinds.Register(%q): SpecType is required", k.Kind))
	}
	if k.SpecType.Kind() == reflect.Ptr {
		panic(fmt.Sprintf("kinds.Register(%q): SpecType must be a value type, not a pointer (got %s)", k.Kind, k.SpecType))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	names := make([]string, 0, 2+len(k.Aliases))
	names = append(names, k.Kind)
	if k.Plural != "" {
		names = append(names, k.Plural)
	}
	names = append(names, k.Aliases...)

	// Validate before committing so a collision does not leave the registry half-populated.
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		if _, exists := r.byName[n]; exists {
			panic(fmt.Sprintf("kinds.Register: %q already registered", n))
		}
	}

	kc := k // copy so external mutation of the argument does not corrupt the registry
	for n := range seen {
		r.byName[n] = &kc
	}
	r.ordered = append(r.ordered, &kc)
}

// ErrUnknownKind is returned by Lookup when no registration matches.
var ErrUnknownKind = errors.New("unknown kind")

// Lookup resolves a Kind by its canonical name, plural, or any alias.
func (r *Registry) Lookup(name string) (*Kind, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if k, ok := r.byName[name]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownKind, name)
}

// All returns every registered Kind in registration order.
func (r *Registry) All() []*Kind {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Kind, len(r.ordered))
	copy(out, r.ordered)
	return out
}

// Decode parses a single YAML document (or JSON object) and returns a Document
// whose Spec is a concrete *T matching the kind's SpecType. The Document's
// Kind field is the canonical kind name even if the input used an alias.
func (r *Registry) Decode(raw []byte) (*Document, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return nil, fmt.Errorf("parsing document: %w", err)
	}
	// yaml.Unmarshal of a single-doc source yields a document node wrapping
	// the content. Walk into the content node for a consistent shape with
	// DecodeMulti's per-document nodes.
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		node = *node.Content[0]
	}
	return r.decodeNode(&node)
}

// DecodeMulti splits a multi-document YAML stream on `---` and decodes each document.
func (r *Registry) DecodeMulti(raw []byte) ([]*Document, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	var docs []*Document
	for {
		var node yaml.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		// Skip empty documents (blank yaml separators).
		if node.Kind == 0 {
			continue
		}
		// yaml.Decoder yields document nodes; step into the content.
		if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
			node = *node.Content[0]
		}
		doc, err := r.decodeNode(&node)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// decodeNode is the shared typed-decode path used by both Decode and DecodeMulti.
func (r *Registry) decodeNode(node *yaml.Node) (*Document, error) {
	var envelope struct {
		APIVersion string    `yaml:"apiVersion"`
		Kind       string    `yaml:"kind"`
		Metadata   Metadata  `yaml:"metadata"`
		Spec       yaml.Node `yaml:"spec"`
	}
	if err := node.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("parsing document envelope: %w", err)
	}
	if envelope.Kind == "" {
		return nil, fmt.Errorf("document is missing kind")
	}
	k, err := r.Lookup(envelope.Kind)
	if err != nil {
		return nil, err
	}
	specPtr := reflect.New(k.SpecType).Interface()
	if envelope.Spec.Kind != 0 {
		if err := envelope.Spec.Decode(specPtr); err != nil {
			return nil, fmt.Errorf("decoding spec for kind %q: %w", k.Kind, err)
		}
	}
	return &Document{
		APIVersion: envelope.APIVersion,
		Kind:       k.Kind,
		Metadata:   envelope.Metadata,
		Spec:       specPtr,
	}, nil
}
