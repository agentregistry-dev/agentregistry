// Package migrate exposes the `arctl db migrate` subcommand. The root
// CLI normally passes migration sources directly to NewCommand; the
// package registry remains for lower-level callers that construct the
// migrate command without explicit sources.
//
// Each source owns its own Postgres schema (set via
// `golang-migrate`'s `migratepgx.Config{SchemaName: ...}`), so adding a
// source never moves the OSS source's integer counter — the
// addressing footgun from the prior shared-table design is gone.
package migrate

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/orchestrator"
)

// sourceNameRE constrains Source.Name to Postgres-identifier-friendly
// characters. Name flows into the source's advisory-lock key
// derivation, into `--source <name>` output, and (lightly) into
// log lines; the regex keeps operator-facing strings predictable.
var sourceNameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Source aliases orchestrator.Source so a single struct value
// satisfies both the CLI registry (per-source operations) and the
// orchestrator's RunUp input (cross-source up). Fields are documented
// on orchestrator.Source; the alias keeps consumers from needing to
// import both packages.
type Source = orchestrator.Source

var (
	sourcesMu sync.RWMutex
	sources   []Source
)

// Register adds a migration source to the package registry.
//
// Validates Name against ^[a-z][a-z0-9_]*$. Panics on invalid charset
// and duplicate Name so source misconfiguration fails fast.
//
// The mutex is defense-in-depth so a contract-violating caller running
// Register concurrently doesn't trigger a silent data race against Sources().
func Register(s Source) {
	if !sourceNameRE.MatchString(s.Name) {
		panic(fmt.Sprintf("migrate.Register: source Name=%q must match %s", s.Name, sourceNameRE.String()))
	}
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	for _, existing := range sources {
		if existing.Name == s.Name {
			panic(fmt.Sprintf("migrate.Register: source %q already registered; each source must have a unique Name", s.Name))
		}
	}
	sources = append(sources, s)
}

// Sources returns a copy of the registered sources in registration
// order. Returning a copy prevents callers from holding a reference
// that could race with a subsequent Register call.
func Sources() []Source {
	sourcesMu.RLock()
	defer sourcesMu.RUnlock()
	out := make([]Source, len(sources))
	copy(out, sources)
	return out
}

// ResetForTesting clears the source registry. Test-only.
func ResetForTesting() {
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	sources = nil
}
