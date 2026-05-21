package config

import "fmt"

// Validate performs runtime validations on the loaded configuration.
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if cfg.ControllerEventRetention < 0 {
		return fmt.Errorf("controller event retention must be non-negative")
	}
	if cfg.ControllerEventKeepAfterRevision < 0 {
		return fmt.Errorf("controller event keep-after revision must be non-negative")
	}
	if cfg.ControllerWorkRetention < 0 {
		return fmt.Errorf("controller work retention must be non-negative")
	}
	if cfg.ControllerAttemptRetention < 0 {
		return fmt.Errorf("controller attempt retention must be non-negative")
	}
	if cfg.ControllerRetentionPruneBatchLimit < 0 {
		return fmt.Errorf("controller retention prune batch limit must be non-negative")
	}
	return nil
}
