package v1alpha1store

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

func TestStoreTableNameFromDescriptor(t *testing.T) {
	tests := []struct {
		name       string
		descriptor v1alpha1.KindDescriptor
		want       string
	}{
		{
			name:       "strips v1alpha1 logical prefix",
			descriptor: v1alpha1.KindDescriptor{Kind: v1alpha1.KindMCPServer, Table: "v1alpha1.mcp_servers"},
			want:       "mcp_servers",
		},
		{
			name:       "keeps bare table name",
			descriptor: v1alpha1.KindDescriptor{Kind: v1alpha1.KindAgent, Table: "agents"},
			want:       "agents",
		},
		{
			name:       "trims whitespace",
			descriptor: v1alpha1.KindDescriptor{Kind: v1alpha1.KindSkill, Table: " v1alpha1.skills "},
			want:       "skills",
		},
		{
			name:       "leaves other qualified names alone",
			descriptor: v1alpha1.KindDescriptor{Kind: "Extension", Table: "extension.widgets"},
			want:       "extension.widgets",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := storeTableNameFromDescriptor(tt.descriptor); got != tt.want {
				t.Fatalf("storeTableNameFromDescriptor() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewStoresDerivesBuiltInTablesFromKindDescriptors(t *testing.T) {
	stores := NewStores(nil, pkgdb.OSSSchemaRegistry())
	if len(stores) != len(builtInKinds) {
		t.Fatalf("NewStores() returned %d stores, want %d", len(stores), len(builtInKinds))
	}

	descriptors := make(map[string]v1alpha1.KindDescriptor)
	for _, descriptor := range v1alpha1.KindDescriptors() {
		descriptors[descriptor.Kind] = descriptor
	}

	for kind := range builtInKinds {
		store, ok := stores[kind]
		if !ok {
			t.Fatalf("NewStores() missing store for %s", kind)
		}
		descriptor, ok := descriptors[kind]
		if !ok {
			t.Fatalf("missing kind descriptor for %s", kind)
		}
		if got, want := store.table, storeTableNameFromDescriptor(descriptor); got != want {
			t.Fatalf("%s store table = %q, want descriptor table %q", kind, got, want)
		}
		if got, want := store.kind, kind; got != want {
			t.Fatalf("%s store kind = %q, want %q", kind, got, want)
		}
		wantBehavior := TaggedArtifactStore
		if descriptor.Storage == v1alpha1.KindStorageMutableObject {
			wantBehavior = MutableObjectStore
		}
		if got := store.Behavior(); got != wantBehavior {
			t.Fatalf("%s store behavior = %q, want %q", kind, got, wantBehavior)
		}
	}
}
