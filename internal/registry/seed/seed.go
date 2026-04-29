package seed

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

//go:embed seed.json
var builtinSeedData []byte

// ImportBuiltinSeedData populates the v1alpha1.mcp_servers table from
// the embedded seed.json corpus on first boot.
//
// Idempotent: each row is Upserted in the "default" namespace under its
// (name, version) identity. Re-running on a populated DB bumps nothing
// because Store.Upsert preserves generation when the marshaled spec
// bytes don't change.
//
// Seed content is trusted — we skip the Validate() pipeline and write
// directly to the Store. The upstream ServerJSON shape uses minor
// vocabulary differences (e.g. `repository.source=github`) that the
// v1alpha1 validator intentionally rejects for user-authored content.
// Server-curated seed does not go through user-validation.
func ImportBuiltinSeedData(ctx context.Context, pool *pgxpool.Pool) error {
	servers, err := loadSeedData(builtinSeedData)
	if err != nil {
		return fmt.Errorf("parse seed: %w", err)
	}

	mcpStore := v1alpha1store.NewStore(pool, "v1alpha1.mcp_servers")

	for _, srv := range servers {
		spec, err := seedServerToMCPSpec(srv)
		if err != nil {
			slog.Warn("seed: failed to translate server", "name", srv.Name, "error", err)
			continue
		}
		specJSON, err := json.Marshal(spec)
		if err != nil {
			slog.Warn("seed: marshal spec", "name", srv.Name, "error", err)
			continue
		}

		// Labels empty for now — could carry upstream _meta fields
		// (publisher identity, endpoint_health, etc.) once we decide
		// which ones are worth indexing. Tracked in project_spec_trimming.md.
		labels := map[string]string{
			"agentregistry.solo.io/seed": "builtin",
		}

		_, err = mcpStore.Upsert(ctx,
			v1alpha1.DefaultNamespace,
			srv.Name,
			srv.Version,
			specJSON,
			v1alpha1store.UpsertOpts{Labels: labels},
		)
		if err != nil {
			// Dup-version isn't fatal for seed; the existing row stays
			// and the next pass picks up any updates.
			if errors.Is(err, pkgdb.ErrAlreadyExists) || errors.Is(err, pkgdb.ErrDuplicateVersion) {
				slog.Debug("seed: row already present", "name", srv.Name, "version", srv.Version)
				continue
			}
			slog.Warn("seed: upsert failed", "name", srv.Name, "version", srv.Version, "error", err)
			continue
		}
	}

	// Seal the pass with a log record so ops can diff count-on-boot.
	slog.Info("seed: builtin MCPServer import complete",
		"rows_considered", len(servers), "t", time.Now().Format(time.RFC3339))
	return nil
}

// seedServerToMCPSpec translates an upstream ServerJSON (as stored in
// seed.json) into the fields we carry on v1alpha1.MCPServerSpec. We
// intentionally drop fields that the v1alpha1 spec doesn't model yet
// ($schema, _meta.publisher-provided) — see REBUILD_TRACKER.md for the
// deferred-fields list.
func seedServerToMCPSpec(s *seedServerJSON) (v1alpha1.MCPServerSpec, error) {
	spec := v1alpha1.MCPServerSpec{
		Description: s.Description,
		Title:       s.Title,
		WebsiteURL:  s.WebsiteURL,
	}
	if s.Repository != nil {
		spec.Repository = &v1alpha1.Repository{
			URL:       s.Repository.URL,
			Source:    s.Repository.Source,
			ID:        s.Repository.ID,
			Subfolder: s.Repository.Subfolder,
		}
	}
	for _, r := range s.Remotes {
		spec.Remotes = append(spec.Remotes, v1alpha1.MCPTransport{
			Type: r.Type,
			URL:  r.URL,
		})
	}
	for _, p := range s.Packages {
		pkg := v1alpha1.MCPPackage{
			RegistryType:    p.RegistryType,
			RegistryBaseURL: p.RegistryBaseURL,
			Identifier:      p.Identifier,
			Version:         p.Version,
			FileSHA256:      p.FileSHA256,
			RuntimeHint:     p.RuntimeHint,
			Transport: v1alpha1.MCPTransport{
				Type: p.Transport.Type,
				URL:  p.Transport.URL,
			},
		}
		spec.Packages = append(spec.Packages, pkg)
	}
	return spec, nil
}

// seedServerJSON is the minimal struct we decode seed.json into. Kept
// narrow so upstream vocabulary changes don't silently break us — any
// field not listed here is ignored. Mirror of the subset of ServerJSON
// we actually translate.
type seedServerJSON struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	WebsiteURL  string          `json:"websiteUrl"`
	Repository  *seedRepository `json:"repository,omitempty"`
	Remotes     []seedTransport `json:"remotes,omitempty"`
	Packages    []seedPackage   `json:"packages,omitempty"`
}

type seedRepository struct {
	URL       string `json:"url"`
	Source    string `json:"source"`
	ID        string `json:"id"`
	Subfolder string `json:"subfolder"`
}

type seedTransport struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type seedPackage struct {
	RegistryType    string        `json:"registryType"`
	RegistryBaseURL string        `json:"registryBaseUrl"`
	Identifier      string        `json:"identifier"`
	Version         string        `json:"version"`
	FileSHA256      string        `json:"fileSha256"`
	RuntimeHint     string        `json:"runtimeHint"`
	Transport       seedTransport `json:"transport"`
}

// loadSeedData decodes seed.json into the narrower shape used by the
// seeder.
func loadSeedData(data []byte) ([]*seedServerJSON, error) {
	var servers []*seedServerJSON
	if err := json.Unmarshal(data, &servers); err != nil {
		return nil, fmt.Errorf("failed to parse seed data: %w", err)
	}
	return servers, nil
}
