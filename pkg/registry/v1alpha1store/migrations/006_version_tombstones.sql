-- Tracks the highest version ever assigned per identity, even after the
-- live row(s) are deleted. Read on apply when the live table has no rows
-- for (namespace, name); written on every successful INSERT in
-- upsertVersioned. Never decremented or removed.
--
-- Why: content-registry versions are referenced by deployments as
-- immutable pins ("agents/foo:1"). After DeleteAllVersions frees the name,
-- a re-apply must NOT recycle "1" — that would silently change what an
-- old pin points at. Resuming from max_assigned + 1 preserves the
-- monotonic + immutable contract across delete cycles.
--
-- Keying on the schema-qualified table_name ("v1alpha1.agents", etc.)
-- mirrors what the Store already uses internally and keeps each kind's
-- number space independent without coupling to Kind strings.
CREATE TABLE IF NOT EXISTS v1alpha1.version_tombstones (
    table_name        TEXT        NOT NULL,
    namespace         TEXT        NOT NULL,
    name              TEXT        NOT NULL,
    max_assigned      INTEGER     NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (table_name, namespace, name)
);
