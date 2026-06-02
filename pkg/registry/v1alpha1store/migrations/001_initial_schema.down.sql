DO $$ BEGIN
    RAISE EXCEPTION 'migration 001_initial_schema is not reversible (up-only)';
END $$;
