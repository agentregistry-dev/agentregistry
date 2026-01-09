package config

import "fmt"

// Validate performs runtime validations on the loaded configuration.
// It is intentionally strict for embeddings to avoid runtime pgvector errors.
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.Embeddings.Enabled {
		if cfg.Embeddings.Dimensions <= 0 {
			return fmt.Errorf("embeddings dimensions must be positive (got %d)", cfg.Embeddings.Dimensions)
		}
		// Database schema currently provisions vector(1536). Reject mismatches early.
		if cfg.Embeddings.Dimensions != 1536 {
			return fmt.Errorf("embeddings dimensions must equal 1536 to match database schema (got %d)", cfg.Embeddings.Dimensions)
		}
		if cfg.Embeddings.Model == "" {
			return fmt.Errorf("embeddings model must be specified when embeddings are enabled")
		}
		if cfg.Embeddings.Provider == "" {
			return fmt.Errorf("embeddings provider must be specified when embeddings are enabled")
		}
	}
	return nil
}
