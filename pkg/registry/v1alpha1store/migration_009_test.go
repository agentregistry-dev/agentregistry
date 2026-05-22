//go:build integration

package v1alpha1store

import (
	"context"
	_ "embed"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

//go:embed migrations/009_mcp_dns_label.sql
var migration009SQL string

// runMigration009 executes the 009 SQL inside a single transaction,
// matching how applyMigration wraps each migration file in production. The
// migration is idempotent, so re-applying it to the already-migrated
// template DB is safe. Non-compliant rows seeded by the test get rewritten.
//
// The transaction wrap is load-bearing: 009 contains a pre-flight DO block
// that may RAISE EXCEPTION followed by cascade UPDATEs. Without an explicit
// transaction the abort only rolls back the DO block and the cascades would
// still run, which doesn't match production behavior.
func runMigration009(t *testing.T, pool *pgxpool.Pool) error {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, migration009SQL); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// TestMigration009_SanitizeOrdering pins the lower()/regexp_replace
// ordering: regexp_replace MUST run on the lowercased input, otherwise
// uppercase letters get eaten by `[^a-z0-9-]+` (POSIX regex is
// case-sensitive).
func TestMigration009_SanitizeOrdering(t *testing.T) {
	pool := NewTestPool(t)
	ctx := context.Background()

	cases := []struct {
		original, want string
	}{
		{"MyServer", "myserver"},
		{"io.github.user/Server", "io-github-user-server"},
		{"Snake_Case_Name", "snake-case-name"},
		{"ALLCAPS", "allcaps"},
		{"already-ok", "already-ok"},
	}
	for _, c := range cases {
		insertRemoteRow(t, pool, c.original, tagFor(c.original))
	}

	// run the migration script which auto-sanitizes the pre-existing servers (e.g. an upgrade)
	require.NoError(t, runMigration009(t, pool))

	for _, c := range cases {
		var got string
		err := pool.QueryRow(ctx,
			`SELECT name FROM v1alpha1.mcp_servers
			  WHERE namespace = 'default' AND tag = $1`,
			tagFor(c.original),
		).Scan(&got)
		require.NoError(t, err, "lookup for %q", c.original)
		require.Equal(t, c.want, got,
			"sanitize ordering wrong for %q (got %q, want %q)", c.original, got, c.want)
	}
}

// TestMigration009_ConditionalMcpNamePreservation verifies that mcpName is
// only populated when the original name matches the upstream catalogue
// pattern (`namespace/name`). Originals that don't match (e.g.
// "Snake_Case_Name") still get the annotation but not mcpName.
func TestMigration009_ConditionalMcpNamePreservation(t *testing.T) {
	pool := NewTestPool(t)
	ctx := context.Background()

	// upstream-shaped + package -> mcpName preserved
	insertPackageRow(t, pool, "io.github.user/Server", "v1")
	// non-upstream-shape + package -> mcpName SKIPPED, annotation only
	insertPackageRow(t, pool, "Snake_Case_Name", "v1")

	require.NoError(t, runMigration009(t, pool))

	// upstream-shaped row has mcpName populated with the original.
	var mcpName string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT spec->'source'->'package'->>'mcpName'
		   FROM v1alpha1.mcp_servers
		  WHERE namespace='default' AND name='io-github-user-server'`,
	).Scan(&mcpName))
	require.Equal(t, "io.github.user/Server", mcpName)

	// non-upstream-shape row does NOT have mcpName but DOES have the annotation.
	var hasMcpName bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT spec->'source'->'package' ? 'mcpName'
		   FROM v1alpha1.mcp_servers
		  WHERE namespace='default' AND name='snake-case-name'`,
	).Scan(&hasMcpName))
	require.False(t, hasMcpName,
		"mcpName must NOT be set for non-upstream-shaped originals (they would fail validateMCPPackageName on next write)")

	var ann string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT annotations->>'agentregistry.dev/migrated-from-name'
		   FROM v1alpha1.mcp_servers
		  WHERE namespace='default' AND name='snake-case-name'`,
	).Scan(&ann))
	require.Equal(t, "Snake_Case_Name", ann,
		"annotation must retain the original name even when mcpName is skipped")
}

// TestMigration009_ExistingMcpNamePreserved verifies that rows with an
// already-set mcpName are not overwritten by the preservation UPDATE.
// Note: This is very unlikely to occur, because as of migration 009, this field
// was non-existent, but this acts as a guard.
func TestMigration009_ExistingMcpNamePreserved(t *testing.T) {
	pool := NewTestPool(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO v1alpha1.mcp_servers (namespace, name, tag, spec, content_hash) VALUES
		  ('default', 'has.existing/MCPName', 'v1',
		   '{"source":{"package":{"registryType":"npm","identifier":"w","mcpName":"keep.me/original","transport":{"type":"stdio"}}}}'::jsonb,
		   repeat('a', 64))`)
	require.NoError(t, err)

	require.NoError(t, runMigration009(t, pool))

	var mcpName string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT spec->'source'->'package'->>'mcpName'
		   FROM v1alpha1.mcp_servers
		  WHERE namespace='default' AND name='has-existing-mcpname'`,
	).Scan(&mcpName))
	require.Equal(t, "keep.me/original", mcpName, "existing mcpName must not be overwritten")
}

// TestMigration009_TagsAllowedSameOriginal verifies that two rows sharing
// the same original name but different tags coexist after rewrite. The
// pre-flight allows count(distinct original)=1 even when final_name
// collides across tags.
func TestMigration009_TagsAllowedSameOriginal(t *testing.T) {
	pool := NewTestPool(t)
	ctx := context.Background()

	insertRemoteRow(t, pool, "MyServer", "v1")
	insertRemoteRow(t, pool, "MyServer", "v2")
	require.NoError(t, runMigration009(t, pool))

	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM v1alpha1.mcp_servers
		  WHERE namespace='default' AND name='myserver'`,
	).Scan(&n))
	require.Equal(t, 2, n, "both tag versions should survive rewrite")
}

// TestMigration009_CollisionPreflightAborts verifies that two DIFFERENT
// originals sanitizing to the same final raise the collision exception and
// roll back the transaction cleanly, no partial rewrites should leak.
func TestMigration009_CollisionPreflightAborts(t *testing.T) {
	pool := NewTestPool(t)
	ctx := context.Background()

	// "My_Server" + "my/server" collides with the compliant "myserver".
	// A similar case occurs if one was named the already-compliant "myserver"
	insertRemoteRow(t, pool, "My_Server", "v1")
	insertRemoteRow(t, pool, "my/server", "v1")

	err := runMigration009(t, pool)
	require.Error(t, err)
	require.Contains(t, err.Error(), "collisions detected")

	// Rollback must have reverted any in-progress work. MyServer is still here.
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM v1alpha1.mcp_servers
		  WHERE namespace='default' AND name='My_Server'`,
	).Scan(&n))
	require.Equal(t, 1, n, "collision abort must roll back; original row should remain")

	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM v1alpha1.mcp_servers
		  WHERE namespace='default' AND name='my/server'`,
	).Scan(&n))
	require.Equal(t, 1, n, "collision abort must roll back; original row should remain")
}

// TestMigration009_EmptySanitizationAborts covers the second pre-flight:
// a name composed entirely of non-DNS-label characters sanitizes to empty
// and must abort the migration with a clear error.
func TestMigration009_EmptySanitizationAborts(t *testing.T) {
	pool := NewTestPool(t)

	insertRemoteRow(t, pool, "%%%", "v1")

	err := runMigration009(t, pool)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no DNS-label-compatible characters")
}

// TestMigration009_AgentMcpServersOrderingPreserved verifies that the
// Agent.spec.mcpServers[] cascade preserves element order via
// WITH ORDINALITY + ORDER BY ord. Also incidentally confirms that
// MCPServer entries get rewritten and non-MCPServer entries (Skill) are
// left untouched.
func TestMigration009_AgentMcpServersOrderingPreserved(t *testing.T) {
	pool := NewTestPool(t)
	ctx := context.Background()

	insertRemoteRow(t, pool, "MyServer", "v1")
	insertRemoteRow(t, pool, "already-ok", "v1")
	_, err := pool.Exec(ctx, `
		INSERT INTO v1alpha1.agents (namespace, name, tag, spec, content_hash) VALUES
		  ('default', 'agent-1', 'v1',
		   '{"mcpServers":[
		       {"kind":"MCPServer","name":"MyServer"},
		       {"kind":"MCPServer","name":"already-ok"},
		       {"kind":"Skill","name":"SomeSkill"},
		       {"kind":"MCPServer","name":"Another-Bad/Name"}
		   ]}'::jsonb,
		   repeat('a', 64))`)
	require.NoError(t, err)

	require.NoError(t, runMigration009(t, pool))

	var names []string
	rows, err := pool.Query(ctx,
		`SELECT elem->>'name'
		   FROM v1alpha1.agents,
		        jsonb_array_elements(spec->'mcpServers') WITH ORDINALITY AS t(elem, ord)
		  WHERE namespace='default' AND name='agent-1'
		  ORDER BY ord`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		names = append(names, s)
	}
	require.NoError(t, rows.Err())
	require.Equal(t,
		[]string{"myserver", "already-ok", "SomeSkill", "another-bad-name"},
		names,
		"WITH ORDINALITY should pin element order through the rebuild")
}

// TestMigration009_RemoteRowsLeftAsRemote pins the contract that the
// mcpName-preservation UPDATE only touches package-bearing rows. A
// Remote MCPServer (spec.remote, no spec.source) gets its name sanitized
// like everything else, but the spec body stays purely remote: no
// spec.source key is introduced as a side effect.
func TestMigration009_RemoteRowsLeftAsRemote(t *testing.T) {
	pool := NewTestPool(t)
	ctx := context.Background()

	insertRemoteRow(t, pool, "MyServer", "v1")
	require.NoError(t, runMigration009(t, pool))

	var hasSource bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT spec ? 'source' FROM v1alpha1.mcp_servers
		  WHERE namespace='default' AND name='myserver'`,
	).Scan(&hasSource))
	require.False(t, hasSource,
		"migration must not introduce spec.source on a remote-only MCPServer row")
}

// TestMigration009_DeploymentTargetRefCascade verifies the
// Deployment.spec.targetRef.name cascade:
//   - kind=MCPServer + non-compliant name -> rewritten, generation bumped
//   - kind=MCPServer + compliant name     -> untouched (generation stays 1)
//   - kind=Agent (non-MCPServer)          -> untouched even if name is non-compliant
//   - kind=MCPServer + dangling ref       -> sanitized anyway, per the migration's
//     explicit design (sanitization is
//     deterministic so resolution outcome
//     is preserved).
func TestMigration009_DeploymentTargetRefCascade(t *testing.T) {
	pool := NewTestPool(t)
	ctx := context.Background()

	// Real MCPServer the rewritten-targetRef deployment will resolve to.
	insertRemoteRow(t, pool, "MyServer", "v1")

	insertDeployment(t, pool, "dep-rewrite", "MCPServer", "MyServer")
	insertDeployment(t, pool, "dep-compliant", "MCPServer", "already-ok")
	insertDeployment(t, pool, "dep-other-kind", "Agent", "SomeAgent")
	insertDeployment(t, pool, "dep-dangling", "MCPServer", "DanglingRef")

	require.NoError(t, runMigration009(t, pool))

	type row struct {
		refName string
		gen     int64
	}
	get := func(name string) row {
		var r row
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT spec->'targetRef'->>'name', generation
			   FROM v1alpha1.deployments
			  WHERE namespace='default' AND name=$1`, name,
		).Scan(&r.refName, &r.gen))
		return r
	}

	rewritten := get("dep-rewrite")
	require.Equal(t, "myserver", rewritten.refName,
		"MCPServer targetRef with non-compliant name must be rewritten")
	require.EqualValues(t, 2, rewritten.gen,
		"rewritten targetRef row must have generation bumped")

	compliant := get("dep-compliant")
	require.Equal(t, "already-ok", compliant.refName,
		"compliant MCPServer targetRef must be left as-is")
	require.EqualValues(t, 1, compliant.gen,
		"compliant targetRef row must not be touched")

	otherKind := get("dep-other-kind")
	require.Equal(t, "SomeAgent", otherKind.refName,
		"non-MCPServer targetRef must not be rewritten")
	require.EqualValues(t, 1, otherKind.gen,
		"non-MCPServer targetRef row must not be touched")

	dangling := get("dep-dangling")
	require.Equal(t, "danglingref", dangling.refName,
		"dangling MCPServer targetRef is still sanitized (per the migration's documented design)")
	require.EqualValues(t, 2, dangling.gen,
		"dangling-ref row that got sanitized must have generation bumped")
}

// TestMigration009_Idempotent re-runs the migration against the
// already-rewritten state and asserts no rows are touched a second time.
func TestMigration009_Idempotent(t *testing.T) {
	pool := NewTestPool(t)
	ctx := context.Background()

	insertRemoteRow(t, pool, "MyServer", "v1")
	require.NoError(t, runMigration009(t, pool))

	var gen1 int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT generation FROM v1alpha1.mcp_servers
		  WHERE namespace='default' AND name='myserver'`,
	).Scan(&gen1))

	require.NoError(t, runMigration009(t, pool))

	var gen2 int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT generation FROM v1alpha1.mcp_servers
		  WHERE namespace='default' AND name='myserver'`,
	).Scan(&gen2))
	require.Equal(t, gen1, gen2, "re-running the migration must not bump generation")
}

// --- fixture helpers -------------------------------------------------------

// tagFor derives a deterministic tag from the original name so the test can
// look the rewritten row back up. Fixture rows are seeded one per original
// name within a test, so a truncation of the original suffices as the tag.
func tagFor(originalName string) string {
	if len(originalName) > 32 {
		return originalName[:32]
	}
	return originalName
}

func insertRemoteRow(t *testing.T, pool *pgxpool.Pool, name, tag string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO v1alpha1.mcp_servers (namespace, name, tag, spec, content_hash) VALUES
		  ($1, $2, $3,
		   '{"remote":{"type":"http","url":"https://x"}}'::jsonb,
		   repeat('a', 64))`,
		"default", name, tag)
	require.NoError(t, err)
}

func insertPackageRow(t *testing.T, pool *pgxpool.Pool, name, tag string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO v1alpha1.mcp_servers (namespace, name, tag, spec, content_hash) VALUES
		  ($1, $2, $3,
		   '{"source":{"package":{"registryType":"npm","identifier":"x","transport":{"type":"stdio"}}}}'::jsonb,
		   repeat('a', 64))`,
		"default", name, tag)
	require.NoError(t, err)
}

func insertDeployment(t *testing.T, pool *pgxpool.Pool, name, refKind, refName string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO v1alpha1.deployments (namespace, name, spec) VALUES
		  ($1, $2, jsonb_build_object('targetRef', jsonb_build_object('kind', $3::text, 'name', $4::text)))`,
		"default", name, refKind, refName)
	require.NoError(t, err)
}
