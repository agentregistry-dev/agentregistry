DO $$ BEGIN
    RAISE EXCEPTION 'migration 009_controller_foundations is not reversible (up-only)';
END $$;
