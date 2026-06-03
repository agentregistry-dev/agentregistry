package v1alpha1store_test

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/migrationlint"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// TestMigrationsLint runs the shared migration lint over the embedded OSS
// migration set. The rule catalogue and the lint engine's own unit tests
// live in pkg/registry/database/migrationlint; downstream migration sets
// run the same Check against their own embed (see that package's doc).
func TestMigrationsLint(t *testing.T) {
	violations, err := migrationlint.Check(v1alpha1store.MigrationFiles, v1alpha1store.MigrationsDir)
	if err != nil {
		t.Fatalf("lint OSS migrations: %v", err)
	}
	for _, v := range violations {
		t.Errorf("%s", v)
	}
}
