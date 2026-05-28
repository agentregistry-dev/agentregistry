package registry

import "testing"

func TestBuildStoresAddsExtraStoreTables(t *testing.T) {
	stores := buildStores(nil, map[string]string{
		"ExtensionOnly": "extension_only",
	}, nil, nil)
	if stores["ExtensionOnly"] == nil {
		t.Fatalf("extra v1alpha1 store was not registered")
	}
}
