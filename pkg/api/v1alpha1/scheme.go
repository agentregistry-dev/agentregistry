package v1alpha1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync"

	"sigs.k8s.io/yaml"
)

// Scheme routes a raw YAML/JSON envelope to a typed object by kind. Apply
// handlers, CLI decode, and the generic store all share one instance.
//
// A zero-value Scheme is not usable — construct via NewScheme or use the
// package-level Default.
type Scheme struct {
	mu    sync.RWMutex
	kinds map[string]kindEntry
}

type kindEntry struct {
	// specType is the concrete spec struct's reflect.Type (e.g. AgentSpec).
	specType reflect.Type
	// newObject constructs an empty typed envelope value (e.g. *Agent).
	newObject func() any
}

// NewScheme returns an empty Scheme. Prefer Default for the built-in kinds.
func NewScheme() *Scheme {
	return &Scheme{kinds: make(map[string]kindEntry)}
}

// Default is the package-level Scheme pre-registered with every kind defined
// in this package. Extensions (e.g. enterprise-added kinds) may register onto
// it at init.
var Default = newDefaultScheme()

func newDefaultScheme() *Scheme {
	s := NewScheme()
	s.MustRegister(KindAgent, AgentSpec{}, func() any { return &Agent{} })
	s.MustRegister(KindMCPServer, MCPServerSpec{}, func() any { return &MCPServer{} })
	s.MustRegister(KindSkill, SkillSpec{}, func() any { return &Skill{} })
	s.MustRegister(KindPrompt, PromptSpec{}, func() any { return &Prompt{} })
	s.MustRegister(KindDeployment, DeploymentSpec{}, func() any { return &Deployment{} })
	s.MustRegister(KindProvider, ProviderSpec{}, func() any { return &Provider{} })
	return s
}

// Register associates a kind name with a spec type and a constructor for the
// typed envelope. newObject must return a pointer to a zero-valued envelope
// (e.g. &Agent{}). Kind names are matched case-insensitively but stored in
// their canonical form.
func (s *Scheme) Register(kind string, specSample any, newObject func() any) error {
	if kind == "" {
		return errors.New("v1alpha1: cannot register empty kind")
	}
	if newObject == nil {
		return fmt.Errorf("v1alpha1: nil newObject for kind %q", kind)
	}
	t := reflect.TypeOf(specSample)
	if t == nil {
		return fmt.Errorf("v1alpha1: nil specSample for kind %q", kind)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.kinds[strings.ToLower(kind)]; exists {
		return fmt.Errorf("v1alpha1: kind %q already registered", kind)
	}
	s.kinds[strings.ToLower(kind)] = kindEntry{specType: t, newObject: newObject}
	return nil
}

// MustRegister is Register that panics on error. Use at init.
func (s *Scheme) MustRegister(kind string, specSample any, newObject func() any) {
	if err := s.Register(kind, specSample, newObject); err != nil {
		panic(err)
	}
}

// Kinds returns the registered kind names in lexical order.
func (s *Scheme) Kinds() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.kinds))
	for k := range s.kinds {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Lookup returns the spec reflect.Type and envelope constructor for a kind,
// or (nil, nil, false) if the kind is unknown.
func (s *Scheme) Lookup(kind string) (reflect.Type, func() any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.kinds[strings.ToLower(kind)]
	if !ok {
		return nil, nil, false
	}
	return e.specType, e.newObject, true
}

// Decode parses a single YAML or JSON document into a typed object pointer
// (*Agent, *MCPServer, etc.) routed by its kind field. Unknown kinds return
// an error. Input may be YAML or JSON — detection is delegated to sigs.k8s.io/yaml.
func (s *Scheme) Decode(data []byte) (any, error) {
	var raw RawObject
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("v1alpha1: decode envelope: %w", err)
	}
	if raw.APIVersion == "" {
		return nil, errors.New("v1alpha1: missing apiVersion")
	}
	if raw.APIVersion != GroupVersion {
		return nil, fmt.Errorf("v1alpha1: unsupported apiVersion %q (want %q)", raw.APIVersion, GroupVersion)
	}
	if raw.Kind == "" {
		return nil, errors.New("v1alpha1: missing kind")
	}

	_, newObj, ok := s.Lookup(raw.Kind)
	if !ok {
		return nil, fmt.Errorf("v1alpha1: unknown kind %q", raw.Kind)
	}

	obj := newObj()
	// Re-unmarshal the original bytes into the typed envelope so Spec is
	// decoded with its concrete struct tags rather than left as RawMessage.
	if err := yaml.Unmarshal(data, obj); err != nil {
		return nil, fmt.Errorf("v1alpha1: decode %s: %w", raw.Kind, err)
	}
	return obj, nil
}

// DecodeMulti parses a YAML stream (possibly containing multiple `---`-
// separated documents) or a single JSON document, returning one typed object
// per non-empty document. Empty documents are skipped.
func (s *Scheme) DecodeMulti(data []byte) ([]any, error) {
	docs, err := splitYAMLDocs(data)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(docs))
	for i, doc := range docs {
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		obj, err := s.Decode(doc)
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", i, err)
		}
		out = append(out, obj)
	}
	return out, nil
}

// DecodeInto is a typed-destination variant: the caller provides the empty
// typed envelope (e.g. &Agent{}) and Decode fills it in place. Useful when
// the kind is known statically.
func (s *Scheme) DecodeInto(data []byte, dst any) error {
	dstValue := reflect.ValueOf(dst)
	if !dstValue.IsValid() || dstValue.Kind() != reflect.Ptr || dstValue.IsNil() {
		return fmt.Errorf("v1alpha1: decode into requires a non-nil pointer destination, got %T", dst)
	}
	obj, err := s.Decode(data)
	if err != nil {
		return err
	}
	objValue := reflect.ValueOf(obj)
	if objValue.Type() != dstValue.Type() {
		return fmt.Errorf("v1alpha1: decode into destination %s does not match decoded type %s", dstValue.Type(), objValue.Type())
	}
	dstValue.Elem().Set(objValue.Elem())
	return nil
}

// splitYAMLDocs splits a multi-document YAML stream on `---` boundaries.
// Leading/trailing whitespace is preserved inside each document. For a pure
// JSON input (no `---` separators) the entire input is returned as one doc.
func splitYAMLDocs(data []byte) ([][]byte, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		return [][]byte{data}, nil
	}
	var docs [][]byte
	r := bytes.NewReader(data)
	buf := &bytes.Buffer{}
	line := &bytes.Buffer{}
	readLine := func() ([]byte, error) {
		line.Reset()
		for {
			b, err := r.ReadByte()
			if err == io.EOF {
				if line.Len() == 0 {
					return nil, io.EOF
				}
				return line.Bytes(), io.EOF
			}
			if err != nil {
				return nil, err
			}
			line.WriteByte(b)
			if b == '\n' {
				return line.Bytes(), nil
			}
		}
	}
	for {
		lb, err := readLine()
		if err == io.EOF {
			if len(lb) > 0 {
				buf.Write(lb)
			}
			docs = append(docs, append([]byte(nil), buf.Bytes()...))
			break
		}
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimRight(string(lb), "\n\r\t ")
		if trimmed == "---" {
			docs = append(docs, append([]byte(nil), buf.Bytes()...))
			buf.Reset()
			continue
		}
		buf.Write(lb)
	}
	return docs, nil
}

// Encode marshals a typed envelope (or any value) to YAML. It's a convenience
// wrapper around sigs.k8s.io/yaml.Marshal so callers don't need to import the
// yaml library directly.
func Encode(v any) ([]byte, error) { return yaml.Marshal(v) }

// EncodeJSON marshals a typed envelope to canonical JSON.
func EncodeJSON(v any) ([]byte, error) { return json.Marshal(v) }

// EnvelopeFromRaw materializes a typed envelope T from a RawObject. It
// stamps TypeMeta from the package-level GroupVersion + supplied kind,
// copies ObjectMeta + Status, and unmarshals the raw spec JSON into the
// typed Spec field. newObj must return a fresh zero value on each call.
//
// Shared helper used by every surface that reads RawObject rows (HTTP
// resource handler, MCP bridge, etc.) so every API surface hands back
// an identically-shaped envelope.
func EnvelopeFromRaw[T Object](newObj func() T, raw *RawObject, kind string) (T, error) {
	out := newObj()
	out.SetTypeMeta(TypeMeta{APIVersion: GroupVersion, Kind: kind})
	out.SetMetadata(raw.Metadata)
	if len(raw.Status) > 0 {
		if err := out.UnmarshalStatus(raw.Status); err != nil {
			return out, fmt.Errorf("unmarshal status: %w", err)
		}
	}
	if len(raw.Spec) > 0 {
		if err := out.UnmarshalSpec(raw.Spec); err != nil {
			return out, fmt.Errorf("unmarshal spec: %w", err)
		}
	}
	return out, nil
}
