package migrate

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// Source registration is package-global. Tests reset and restore the
// slice so they can't leak state into each other or into the binary's
// own init-time registration.
func resetSources(t *testing.T) {
	t.Helper()
	original := sources
	t.Cleanup(func() { sources = original })
	sources = nil
}

func TestRegister_PreservesOrder(t *testing.T) {
	resetSources(t)

	Register(Source{Name: "oss", BuildConfig: func() database.MigratorConfig { return database.MigratorConfig{VersionOffset: 200} }})
	Register(Source{Name: "enterprise", BuildConfig: func() database.MigratorConfig { return database.MigratorConfig{VersionOffset: 500} }})

	got := Sources()
	if len(got) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(got))
	}
	if got[0].Name != "oss" || got[1].Name != "enterprise" {
		t.Fatalf("registration order not preserved: got %q, %q", got[0].Name, got[1].Name)
	}
}

func TestRegister_BuildConfigCalledLazily(t *testing.T) {
	resetSources(t)

	calls := 0
	Register(Source{
		Name: "oss",
		BuildConfig: func() database.MigratorConfig {
			calls++
			return database.MigratorConfig{VersionOffset: 200}
		},
	})

	// Registration alone must not invoke BuildConfig — env-driven
	// values aren't valid yet at init() time.
	if calls != 0 {
		t.Fatalf("BuildConfig invoked at registration time; want lazy, got %d calls", calls)
	}

	// Explicit invocation works.
	_ = Sources()[0].BuildConfig()
	if calls != 1 {
		t.Fatalf("BuildConfig should be invoked exactly once, got %d", calls)
	}
}
