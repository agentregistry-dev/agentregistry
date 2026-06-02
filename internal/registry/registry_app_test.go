package registry

import (
	"testing"

	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

func TestBuildStoresAddsExtraStoreTables(t *testing.T) {
	stores := buildStores(nil, map[string]string{
		"ExtensionOnly": "extension_only",
	}, nil, nil)
	if stores["ExtensionOnly"] == nil {
		t.Fatalf("extra v1alpha1 store was not registered")
	}
}

func TestResolveExtraStoreSchema(t *testing.T) {
	oss := pkgdb.MustNewSchema(pkgdb.OSSSchema)
	tests := []struct {
		name       string
		table      string
		wantSchema string
		wantTable  string
	}{
		{"bare table stays in OSS schema", "widgets", pkgdb.OSSSchema, "widgets"},
		{"qualified resolves to its schema", "ext.widgets", "ext", "widgets"},
		{"splits on first dot only", "ext.a.b", "ext", "a.b"},
		{"trailing dot yields empty table", "ext.", "ext", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSchema, gotTable := resolveExtraStoreSchema(tt.table, oss)
			if gotSchema.Name() != tt.wantSchema {
				t.Errorf("schema = %q, want %q", gotSchema.Name(), tt.wantSchema)
			}
			if gotTable != tt.wantTable {
				t.Errorf("table = %q, want %q", gotTable, tt.wantTable)
			}
		})
	}
}

func TestResolveExtraStoreSchemaPanicsOnInvalidSchema(t *testing.T) {
	oss := pkgdb.MustNewSchema(pkgdb.OSSSchema)
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic on invalid schema identifier")
		}
	}()
	resolveExtraStoreSchema("BadSchema.widgets", oss)
}
