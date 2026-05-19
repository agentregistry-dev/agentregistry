// Package migrate exposes the `arctl db migrate` subcommand and a package
// private registry that lets the binary stack one or more migration sets
// onto the shared schema_migrations table. Downstream distributions can
// register additional sources alongside the OSS one via Register.
//
// Registration order is the order migrations run on `up`: the first source
// must set EnsureTable=true (creates schema_migrations); later sources set
// EnsureTable=false and share the same table.
package migrate

import (
	"sync"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// Source describes one set of migrations registered with the CLI. The
// runtime config is built lazily so registration can happen in `init()`
// before env-driven config has been loaded.
type Source struct {
	// Name is an internal label used in debug output only. Not surfaced
	// in default `status` / `version` output.
	Name string
	// BuildConfig returns the MigratorConfig at command-execution time
	// (after env has been read). The returned config's VersionOffset
	// defines this source's range in schema_migrations.
	BuildConfig func() database.MigratorConfig
}

var (
	sourcesMu sync.RWMutex
	sources   []Source
)

// Register adds a migration source to the package registry. Intended to
// be called from init() in the binary's root command package; the mutex
// is defense-in-depth so a future caller violating the init-only
// contract doesn't trigger a silent data race against Sources().
func Register(s Source) {
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	sources = append(sources, s)
}

// Sources returns a copy of the registered sources in registration order.
// Returning a copy prevents callers from holding a reference that could
// race with a subsequent (contract-violating) Register call.
func Sources() []Source {
	sourcesMu.RLock()
	defer sourcesMu.RUnlock()
	out := make([]Source, len(sources))
	copy(out, sources)
	return out
}
