-- Auto-publish all existing entries.
-- After this migration, the publish/unpublish workflow is removed:
-- every create inserts with published=true immediately.

UPDATE servers SET published = true, published_date = COALESCE(published_date, NOW()) WHERE published = false;
UPDATE agents SET published = true, published_date = COALESCE(published_date, NOW()) WHERE published = false;
UPDATE skills SET published = true, published_date = COALESCE(published_date, NOW()) WHERE published = false;
