package registry

import (
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func TestBuildStoresAndImporterAddsExtraStoreTables(t *testing.T) {
	stores, importer, err := buildStoresAndImporter(nil, nil, []types.ExtraStore{
		{Kind: "EnterpriseOnly", Table: "v1alpha1.enterprise_only", Mode: types.StoreModeVersionedArtifact},
	}, nil)
	if err != nil {
		t.Fatalf("buildStoresAndImporter err = %v, want nil", err)
	}
	if importer != nil {
		t.Fatalf("importer with nil pool = %v, want nil", importer)
	}
	if stores["EnterpriseOnly"] == nil {
		t.Fatalf("extra v1alpha1 store was not registered")
	}
	if !stores["EnterpriseOnly"].IsVersionedArtifact() {
		t.Fatalf("EnterpriseOnly store: IsVersionedArtifact = false, want true")
	}
}

// TestBuildStoresAndImporter_RoutesMutableMode covers the
// StoreModeMutable branch — the row shape used by enterprise infra/config
// kinds (AccessPolicy, Gateway). The resulting Store must report
// IsVersionedArtifact() == false so URL-path validation accepts the
// legacy string-version shape.
func TestBuildStoresAndImporter_RoutesMutableMode(t *testing.T) {
	stores, _, err := buildStoresAndImporter(nil, nil, []types.ExtraStore{
		{Kind: "AccessPolicy", Table: "v1alpha1.access_policies", Mode: types.StoreModeMutable},
	}, nil)
	if err != nil {
		t.Fatalf("buildStoresAndImporter err = %v, want nil", err)
	}
	if stores["AccessPolicy"] == nil {
		t.Fatalf("AccessPolicy store was not registered")
	}
	if stores["AccessPolicy"].IsVersionedArtifact() {
		t.Fatalf("AccessPolicy store: IsVersionedArtifact = true, want false (mutable mode)")
	}
}

// TestBuildStoresAndImporter_RoutesVersionedArtifactMode is the explicit
// counterpart to the mutable-mode test — registering a kind with
// StoreModeVersionedArtifact must produce a Store reporting
// IsVersionedArtifact() == true.
func TestBuildStoresAndImporter_RoutesVersionedArtifactMode(t *testing.T) {
	stores, _, err := buildStoresAndImporter(nil, nil, []types.ExtraStore{
		{Kind: "EnterpriseAgent", Table: "v1alpha1.enterprise_agents", Mode: types.StoreModeVersionedArtifact},
	}, nil)
	if err != nil {
		t.Fatalf("buildStoresAndImporter err = %v, want nil", err)
	}
	if stores["EnterpriseAgent"] == nil {
		t.Fatalf("EnterpriseAgent store was not registered")
	}
	if !stores["EnterpriseAgent"].IsVersionedArtifact() {
		t.Fatalf("EnterpriseAgent store: IsVersionedArtifact = false, want true (versioned-artifact mode)")
	}
}

// TestBuildStoresAndImporter_EmptyModeRejected guards the failure-closed
// behaviour: an extra store registered without an explicit Mode is a
// configuration error, not a silent default. The whole point of the
// StoreMode enum is to force the caller to pick.
func TestBuildStoresAndImporter_EmptyModeRejected(t *testing.T) {
	_, _, err := buildStoresAndImporter(nil, nil, []types.ExtraStore{
		{Kind: "Misconfigured", Table: "v1alpha1.misconfigured"},
	}, nil)
	if err == nil {
		t.Fatalf("buildStoresAndImporter err = nil, want error for empty Mode")
	}
	if !strings.Contains(err.Error(), "Misconfigured") {
		t.Fatalf("error %q does not name the offending kind", err.Error())
	}
}
