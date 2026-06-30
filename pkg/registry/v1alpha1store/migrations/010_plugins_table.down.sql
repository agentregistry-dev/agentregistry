-- Reverses 010_plugins_table.up.sql. Dropping the table removes its indexes
-- and triggers; the shared trigger functions (set_updated_at,
-- notify_status_change, record_control_plane_event) are owned by earlier
-- migrations and left in place.
DROP INDEX IF EXISTS plugins_terminating;
DROP INDEX IF EXISTS plugins_tags_alive;
DROP INDEX IF EXISTS plugins_list_alive;
DROP INDEX IF EXISTS plugins_labels_gin;

DROP TABLE IF EXISTS plugins;
