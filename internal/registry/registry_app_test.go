package registry

import "testing"

func TestBuildStoresAndImporterAddsExtraStoreTables(t *testing.T) {
	stores, importer := buildStoresAndImporter(nil, nil, map[string]string{
		"ExtensionOnly": "v1alpha1.extension_only",
	}, nil, nil)
	if importer != nil {
		t.Fatalf("importer with nil pool = %v, want nil", importer)
	}
	if stores["ExtensionOnly"] == nil {
		t.Fatalf("extra v1alpha1 store was not registered")
	}
}
