package v1alpha1

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"unicode"
)

// KindStorage describes the private persistence semantics attached to a
// v1alpha1 kind.
type KindStorage string

const (
	KindStorageTaggedArtifact KindStorage = "TaggedArtifact"
	KindStorageMutableObject  KindStorage = "MutableObject"
)

// ProjectionPolicy tells the controller source projector how to read rows for a
// kind. It does not decide what work to do; it only controls which rows are
// visible to the read model.
type ProjectionPolicy struct {
	// IncludeTerminating keeps soft-deleted rows visible to the projector.
	// Kinds that need async delete cleanup, such as Deployment finalizer
	// teardown, need this so the controller can still see the row after DELETE.
	IncludeTerminating bool
}

// KindDescriptor is the single registration record for a v1alpha1 kind. Scheme
// decoding, store construction, plural routing, and source projection all share
// this metadata.
type KindDescriptor struct {
	Kind       string
	SpecSample any
	NewObject  func() any
	Plural     string
	Table      string
	Storage    KindStorage
	Projection ProjectionPolicy
}

// KindRegistry owns registered v1alpha1 kind metadata.
type KindRegistry struct {
	mu          sync.RWMutex
	descriptors map[string]KindDescriptor
}

// NewKindRegistry constructs an empty kind registry.
func NewKindRegistry() *KindRegistry {
	return &KindRegistry{descriptors: make(map[string]KindDescriptor)}
}

// DefaultKindRegistry is the package-level registry used by the app, stores,
// controller sources, and generic clients. Kind packages register here at init.
var DefaultKindRegistry = NewKindRegistry()

// KindOption customizes RegisterKind defaults.
type KindOption func(*KindDescriptor)

// WithPlural sets the route plural for the kind.
func WithPlural(plural string) KindOption {
	return func(d *KindDescriptor) {
		d.Plural = strings.TrimSpace(plural)
	}
}

// WithTable sets the backing PostgreSQL table for the kind.
func WithTable(table string) KindOption {
	return func(d *KindDescriptor) {
		d.Table = strings.TrimSpace(table)
	}
}

// WithTaggedArtifactStorage marks the kind as namespace/name/tag content.
func WithTaggedArtifactStorage() KindOption {
	return func(d *KindDescriptor) {
		d.Storage = KindStorageTaggedArtifact
	}
}

// WithMutableObjectStorage marks the kind as namespace/name mutable state.
func WithMutableObjectStorage() KindOption {
	return func(d *KindDescriptor) {
		d.Storage = KindStorageMutableObject
	}
}

// WithProjectionPolicy sets controller source projection behavior.
func WithProjectionPolicy(policy ProjectionPolicy) KindOption {
	return func(d *KindDescriptor) {
		d.Projection = policy
	}
}

// RegisterKind registers kind metadata and wires the package Default scheme.
func RegisterKind[T Object, S any](kind string, opts ...KindOption) error {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return errors.New("v1alpha1: cannot register empty kind")
	}

	var spec S
	newObject, err := newObjectFor[T](kind)
	if err != nil {
		return err
	}
	descriptor := KindDescriptor{
		Kind:       kind,
		SpecSample: spec,
		NewObject:  newObject,
		Plural:     defaultRoutePlural(kind),
		Table:      defaultTableForKind(kind),
		Storage:    KindStorageTaggedArtifact,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&descriptor)
		}
	}
	pluralOverridesMu.RLock()
	if plural, ok := pluralOverrides[strings.ToLower(kind)]; ok {
		descriptor.Plural = plural
	}
	pluralOverridesMu.RUnlock()
	if descriptor.Plural == "" {
		return fmt.Errorf("v1alpha1: empty plural for kind %q", kind)
	}
	if descriptor.Table == "" {
		return fmt.Errorf("v1alpha1: empty table for kind %q", kind)
	}
	if err := Default.Register(kind, spec, newObject); err != nil {
		return err
	}
	if err := DefaultKindRegistry.Register(descriptor); err != nil {
		return err
	}
	return nil
}

// MustRegisterKind is RegisterKind that panics on error. Use at init.
func MustRegisterKind[T Object, S any](kind string, opts ...KindOption) {
	if err := RegisterKind[T, S](kind, opts...); err != nil {
		panic(err)
	}
}

// Register adds descriptor to the registry.
func (r *KindRegistry) Register(descriptor KindDescriptor) error {
	if r == nil {
		return errors.New("v1alpha1: nil kind registry")
	}
	if descriptor.Kind == "" {
		return errors.New("v1alpha1: cannot register empty kind")
	}
	if descriptor.SpecSample == nil {
		return fmt.Errorf("v1alpha1: nil spec sample for kind %q", descriptor.Kind)
	}
	if descriptor.NewObject == nil {
		return fmt.Errorf("v1alpha1: nil object constructor for kind %q", descriptor.Kind)
	}
	if descriptor.Plural == "" {
		descriptor.Plural = defaultRoutePlural(descriptor.Kind)
	}
	if descriptor.Table == "" {
		descriptor.Table = defaultTableForKind(descriptor.Kind)
	}
	if descriptor.Storage == "" {
		descriptor.Storage = KindStorageTaggedArtifact
	}

	key := strings.ToLower(descriptor.Kind)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.descriptors[key]; exists {
		return fmt.Errorf("v1alpha1: kind %q already registered", descriptor.Kind)
	}
	r.descriptors[key] = descriptor
	return nil
}

// Kinds returns registered canonical kind names in deterministic order.
func (r *KindRegistry) Kinds() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.descriptors))
	for _, descriptor := range r.descriptors {
		out = append(out, descriptor.Kind)
	}
	slices.SortFunc(out, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return out
}

// Descriptors returns registered descriptors in deterministic kind order.
func (r *KindRegistry) Descriptors() []KindDescriptor {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]KindDescriptor, 0, len(r.descriptors))
	for _, descriptor := range r.descriptors {
		out = append(out, descriptor)
	}
	slices.SortFunc(out, func(a, b KindDescriptor) int {
		return strings.Compare(strings.ToLower(a.Kind), strings.ToLower(b.Kind))
	})
	return out
}

// Lookup returns descriptor for kind.
func (r *KindRegistry) Lookup(kind string) (KindDescriptor, bool) {
	if r == nil {
		return KindDescriptor{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	descriptor, ok := r.descriptors[strings.ToLower(kind)]
	return descriptor, ok
}

// UpdatePlural updates or records the route plural for kind.
func (r *KindRegistry) UpdatePlural(kind, plural string) error {
	if r == nil {
		return errors.New("v1alpha1: nil kind registry")
	}
	kind = strings.TrimSpace(kind)
	plural = strings.TrimSpace(plural)
	if kind == "" {
		return errors.New("v1alpha1: cannot register plural for empty kind")
	}
	if plural == "" {
		return fmt.Errorf("v1alpha1: cannot register empty plural for kind %q", kind)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.ToLower(kind)
	descriptor, ok := r.descriptors[key]
	if !ok {
		pluralOverridesMu.Lock()
		defer pluralOverridesMu.Unlock()
		if existing, ok := pluralOverrides[key]; ok && existing != plural {
			return fmt.Errorf("v1alpha1: plural for kind %q already registered as %q", kind, existing)
		}
		pluralOverrides[key] = plural
		return nil
	}
	descriptor.Plural = plural
	r.descriptors[key] = descriptor
	return nil
}

// RegisteredKinds returns canonical names for every registered kind.
func RegisteredKinds() []string {
	return DefaultKindRegistry.Kinds()
}

// KindDescriptors returns descriptors for every registered kind.
func KindDescriptors() []KindDescriptor {
	return DefaultKindRegistry.Descriptors()
}

// KindDescriptorFor returns descriptor for kind.
func KindDescriptorFor(kind string) (KindDescriptor, bool) {
	return DefaultKindRegistry.Lookup(kind)
}

func newObjectFor[T Object](kind string) (func() any, error) {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil || t.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("v1alpha1: kind %q object type must be a pointer, got %T", kind, zero)
	}
	return func() any {
		return reflect.New(t.Elem()).Interface()
	}, nil
}

func defaultRoutePlural(kind string) string {
	return strings.ToLower(kind) + "s"
}

func defaultTableForKind(kind string) string {
	return "v1alpha1." + snakeCase(pluralizeKind(kind))
}

func pluralizeKind(kind string) string {
	if len(kind) > 1 && strings.HasSuffix(kind, "y") {
		prev := rune(kind[len(kind)-2])
		if !strings.ContainsRune("aeiouAEIOU", prev) {
			return kind[:len(kind)-1] + "ies"
		}
	}
	return kind + "s"
}

func snakeCase(s string) string {
	runes := []rune(s)
	var out []rune
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				var next rune
				if i+1 < len(runes) {
					next = runes[i+1]
				}
				if unicode.IsLower(prev) || unicode.IsDigit(prev) || (next != 0 && unicode.IsLower(next)) {
					out = append(out, '_')
				}
			}
			out = append(out, unicode.ToLower(r))
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
