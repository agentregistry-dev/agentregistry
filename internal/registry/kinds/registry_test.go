package kinds_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
)

type fakeSpec struct {
	Foo string `yaml:"foo" json:"foo"`
}

func fakeKind() kinds.Kind {
	return kinds.Kind{
		Kind:     "fake",
		Plural:   "fakes",
		Aliases:  []string{"Fake"},
		SpecType: reflect.TypeFor[fakeSpec](),
	}
}

func TestRegisterAndLookupByCanonicalKind(t *testing.T) {
	r := kinds.NewRegistry()
	r.Register(fakeKind())
	got, err := r.Lookup("fake")
	if err != nil || got == nil || got.Kind != "fake" {
		t.Fatalf("expected to look up 'fake', got err=%v got=%v", err, got)
	}
}

func TestLookupByAlias(t *testing.T) {
	r := kinds.NewRegistry()
	r.Register(fakeKind())
	for _, alias := range []string{"Fake", "fakes"} {
		if _, err := r.Lookup(alias); err != nil {
			t.Fatalf("lookup by alias %q: %v", alias, err)
		}
	}
}

func TestLookupUnknownReturnsError(t *testing.T) {
	r := kinds.NewRegistry()
	if _, err := r.Lookup("nonsense"); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestDoubleRegistrationPanics(t *testing.T) {
	r := kinds.NewRegistry()
	r.Register(fakeKind())
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on double-registration")
		}
	}()
	r.Register(fakeKind())
}

func TestRegisterEmptyKindPanics(t *testing.T) {
	r := kinds.NewRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for empty Kind")
		}
	}()
	r.Register(kinds.Kind{SpecType: reflect.TypeFor[fakeSpec]()})
}

func TestRegisterNilSpecTypePanics(t *testing.T) {
	r := kinds.NewRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for nil SpecType")
		}
	}()
	r.Register(kinds.Kind{Kind: "x"})
}

func TestDecodeSingleDocumentProducesTypedSpec(t *testing.T) {
	r := kinds.NewRegistry()
	r.Register(fakeKind())
	doc, err := r.Decode([]byte(`
apiVersion: ar.dev/v1alpha1
kind: fake
metadata:
  name: n1
spec:
  foo: bar
`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	spec, ok := doc.Spec.(*fakeSpec)
	if !ok {
		t.Fatalf("expected *fakeSpec, got %T", doc.Spec)
	}
	if spec.Foo != "bar" {
		t.Fatalf("expected foo=bar, got %q", spec.Foo)
	}
	if doc.Kind != "fake" {
		t.Fatalf("expected canonical kind 'fake', got %q", doc.Kind)
	}
}

func TestDecodeUsesCanonicalKindEvenWhenAliasInInput(t *testing.T) {
	r := kinds.NewRegistry()
	r.Register(fakeKind())
	doc, err := r.Decode([]byte(`kind: Fake
metadata:
  name: n1
spec:
  foo: x
`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Kind != "fake" {
		t.Fatalf("expected canonical kind 'fake', got %q", doc.Kind)
	}
}

func TestDecodeUnknownKindErrors(t *testing.T) {
	r := kinds.NewRegistry()
	r.Register(fakeKind())
	_, err := r.Decode([]byte(`kind: nope
metadata: {name: n}`))
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestDecodeMissingKindErrors(t *testing.T) {
	r := kinds.NewRegistry()
	r.Register(fakeKind())
	_, err := r.Decode([]byte(`metadata: {name: n}`))
	if err == nil {
		t.Fatal("expected error for missing kind")
	}
}

func TestDecodeMultiSplitsDocuments(t *testing.T) {
	r := kinds.NewRegistry()
	r.Register(fakeKind())
	docs, err := r.DecodeMulti([]byte(`apiVersion: ar.dev/v1alpha1
kind: fake
metadata: {name: one}
spec: {foo: a}
---
apiVersion: ar.dev/v1alpha1
kind: fake
metadata: {name: two}
spec: {foo: b}
`))
	if err != nil {
		t.Fatalf("decode multi: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
	if !strings.EqualFold(docs[0].Metadata.Name, "one") || !strings.EqualFold(docs[1].Metadata.Name, "two") {
		t.Fatalf("wrong doc order: %+v, %+v", docs[0], docs[1])
	}
}

func TestAllReturnsRegistrationOrder(t *testing.T) {
	r := kinds.NewRegistry()
	r.Register(kinds.Kind{Kind: "a", SpecType: reflect.TypeFor[fakeSpec]()})
	r.Register(kinds.Kind{Kind: "b", SpecType: reflect.TypeFor[fakeSpec]()})
	r.Register(kinds.Kind{Kind: "c", SpecType: reflect.TypeFor[fakeSpec]()})
	all := r.All()
	if len(all) != 3 || all[0].Kind != "a" || all[1].Kind != "b" || all[2].Kind != "c" {
		t.Fatalf("unexpected order: %+v", all)
	}
}
