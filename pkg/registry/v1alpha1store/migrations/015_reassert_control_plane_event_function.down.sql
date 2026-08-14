-- No-op. The up migration re-asserts record_control_plane_event() at the
-- definition migration 009 already describes, so on a database built from the
-- current migration set there is nothing to revert. Databases whose function
-- predates the 009 edit keep the repaired definition; restoring the stale
-- variant would reintroduce the missed Plugin/Skill resolvedSource events.
SELECT 1;
