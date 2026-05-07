package registry

import "testing"

func TestBuildStoresAndImporterAddsExtraStoreTables(t *testing.T) {
	stores, importer := buildStoresAndImporter(nil, nil, map[string]string{
		"EnterpriseOnly": "v1alpha1.enterprise_only",
	}, nil, nil)
	if importer != nil {
		t.Fatalf("importer with nil pool = %v, want nil", importer)
	}
	if stores["EnterpriseOnly"] == nil {
		t.Fatalf("extra v1alpha1 store was not registered")
	}
}
