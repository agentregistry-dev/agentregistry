package registry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

func TestDeploymentControllerConfigMapsRetentionSettings(t *testing.T) {
	cfg := &config.Config{
		ControllerEventRetention:             2 * time.Hour,
		ControllerEventKeepAfterRevision:     42,
		ControllerRetentionPruneBatchLimit:   17,
		ControllerDiscoveryInterval:          15 * time.Second,
		ControllerDiscoveryStaleAfterMisses:  2,
		ControllerDiscoveryDeleteAfterMisses: 4,
	}

	got := deploymentControllerConfig(cfg)

	require.Equal(t, 2*time.Hour, got.Retention.ControlPlaneEvents)
	require.Equal(t, int64(42), got.Retention.EventKeepAfterRev)
	require.Equal(t, 17, got.Retention.BatchLimit)
	require.Equal(t, 15*time.Second, got.DiscoveryInterval)
	require.Equal(t, 2, got.DiscoveryStaleAfterMisses)
	require.Equal(t, 4, got.DiscoveryDeleteAfterMisses)
}

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
