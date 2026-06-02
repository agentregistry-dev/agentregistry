package legacymigrate

import (
	"io"
	"regexp"
	"slices"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// TestOSSTablesMatchInitialSchema is the only thing standing between
// "added a table in 001_initial_schema" and "forgot to extend the
// legacy copy" — keep them in sync via the migration FS itself.
func TestOSSTablesMatchInitialSchema(t *testing.T) {
	body, err := v1alpha1store.MigrationFiles.Open(v1alpha1store.MigrationsDir + "/001_initial_schema.up.sql")
	if err != nil {
		t.Fatalf("open 001_initial_schema.up.sql: %v", err)
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read 001_initial_schema.up.sql: %v", err)
	}
	src := string(data)

	re := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+IF\s+NOT\s+EXISTS\s+(\w+)`)
	matches := re.FindAllStringSubmatch(src, -1)
	if len(matches) == 0 {
		t.Fatalf("no CREATE TABLE matches in 001_initial_schema.up.sql; regex or file changed")
	}
	got := make([]string, 0, len(matches))
	for _, m := range matches {
		got = append(got, m[1])
	}
	slices.Sort(got)

	want := append([]string(nil), ossTables...)
	slices.Sort(want)

	if !equalSlices(got, want) {
		t.Errorf("legacymigrate.ossTables out of sync with 001_initial_schema.up.sql\n  migration tables: %v\n  ossTables:        %v", got, want)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
