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
	remoteStore := v1alpha1store.NewStore(pool, "v1alpha1.remote_mcp_servers")

	labels := map[string]string{
		"agentregistry.solo.io/seed": "builtin",
	}

	var (
		mcpRows    int
		remoteRows int
	)
	for _, srv := range servers {
		hasPackages := len(srv.Packages) > 0
		hasRemotes := len(srv.Remotes) > 0

		annotations := map[string]string{}
		if hasPackages && hasRemotes {
			// Sibling rows — link them so the catalog UI can group.
			annotations["agentregistry.dev/related-mcpserver"] = srv.Name
		}

		if hasPackages && upsertSeedMCPServer(ctx, mcpStore, srv, labels, annotations) {
			mcpRows++
		}

		for i := range srv.Remotes {
			if upsertSeedRemoteMCPServer(ctx, remoteStore, srv, i, labels, annotations) {
				remoteRows++
			}
		}
	}

	// Seal the pass with a log record so ops can diff count-on-boot.
	slog.Info("seed: builtin MCP/RemoteMCP import complete",
		"servers", len(servers), "mcp_rows", mcpRows, "remote_rows", remoteRows,
		"t", time.Now().Format(time.RFC3339))
	return nil
}

// remoteSiblingName derives a unique name when one upstream server splits
// into multiple RemoteMCPServer rows or stays alongside an MCPServer.
func remoteSiblingName(base string, idx, total int) string {
	if total <= 1 {
		return base + "-remote"
	}
	return fmt.Sprintf("%s-remote-%d", base, idx)
}

// upsertSeedMCPServer marshals a seedServerJSON's MCPServer projection and
// upserts it into mcpStore. Returns true on success (or "row already
// present", which is benign for seed re-runs).
func upsertSeedMCPServer(
	ctx context.Context,
	store *v1alpha1store.Store,
	srv *seedServerJSON,
	labels, annotations map[string]string,
) bool {
	spec, err := seedServerToMCPSpec(srv)
	if err != nil {
		slog.Warn("seed: failed to translate server", "name", srv.Name, "error", err)
		return false
	}
	specJSON, err := json.Marshal(spec)
	if err != nil {
		slog.Warn("seed: marshal spec", "name", srv.Name, "error", err)
		return false
	}
	if _, err = store.Upsert(ctx,
		v1alpha1.DefaultNamespace,
		srv.Name,
		srv.Version,
		specJSON,
		v1alpha1store.UpsertOpts{Labels: labels, Annotations: annotations},
	); err != nil {
		if errors.Is(err, pkgdb.ErrAlreadyExists) || errors.Is(err, pkgdb.ErrDuplicateVersion) {
			slog.Debug("seed: mcp row already present", "name", srv.Name, "version", srv.Version)
			return true
		}
		slog.Warn("seed: mcp upsert failed", "name", srv.Name, "version", srv.Version, "error", err)
		return false
	}
	return true
}

// upsertSeedRemoteMCPServer materializes one entry from srv.Remotes as a
// sibling RemoteMCPServer row. The naming rule is stable so re-runs land on
// the same identity.
func upsertSeedRemoteMCPServer(
	ctx context.Context,
	store *v1alpha1store.Store,
	srv *seedServerJSON,
	idx int,
	labels, annotations map[string]string,
) bool {
	r := srv.Remotes[idx]
	hasPackages := len(srv.Packages) > 0
	remoteName := srv.Name
	if hasPackages || len(srv.Remotes) > 1 {
		remoteName = remoteSiblingName(srv.Name, idx, len(srv.Remotes))
	}
	spec := v1alpha1.RemoteMCPServerSpec{
		Title:       srv.Title,
		Description: srv.Description,
		Remote: v1alpha1.MCPTransport{
			Type: r.Type,
			URL:  r.URL,
		},
	}
	specJSON, err := json.Marshal(spec)
	if err != nil {
		slog.Warn("seed: marshal remote spec", "name", remoteName, "error", err)
		return false
	}
	if _, err = store.Upsert(ctx,
		v1alpha1.DefaultNamespace,
		remoteName,
		srv.Version,
		specJSON,
		v1alpha1store.UpsertOpts{Labels: labels, Annotations: annotations},
	); err != nil {
		if errors.Is(err, pkgdb.ErrAlreadyExists) || errors.Is(err, pkgdb.ErrDuplicateVersion) {
			slog.Debug("seed: remote row already present", "name", remoteName, "version", srv.Version)
			return true
		}
		slog.Warn("seed: remote upsert failed", "name", remoteName, "version", srv.Version, "error", err)
		return false
	}
	return true
}

// seedServerToMCPSpec translates an upstream ServerJSON (as stored in
// seed.json) into the fields we carry on v1alpha1.MCPServerSpec. We
// intentionally drop fields that the v1alpha1 spec doesn't model yet
// ($schema, _meta.publisher-provided) — see REBUILD_TRACKER.md for the
// deferred-fields list.
//
// Remote endpoints are *not* projected here — they're materialized as
// sibling RemoteMCPServer rows by ImportBuiltinSeedData.
func seedServerToMCPSpec(s *seedServerJSON) (v1alpha1.MCPServerSpec, error) {
	spec := v1alpha1.MCPServerSpec{
		Description: s.Description,
		Title:       s.Title,
	}
	if s.Repository != nil {
		spec.Repository = &v1alpha1.Repository{
			URL:       s.Repository.URL,
			Subfolder: s.Repository.Subfolder,
		}
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
