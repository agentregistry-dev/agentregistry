package runtime

import (
	"fmt"
	"slices"
)

// RuntimeValidator is a function that validates if a runtime value is supported
type RuntimeValidator func(runtime string) error

var (
	// SupportedRuntimes defines the available runtimes
	SupportedRuntimes = []string{"local", "kubernetes"}

	// CustomRuntimeValidator allows extending the runtimes
	CustomRuntimeValidator RuntimeValidator
)

// ValidateRuntime checks if a runtime is valid
func ValidateRuntime(runtime string) error {
	// Check base runtimes
	if slices.Contains(SupportedRuntimes, runtime) {
		return nil
	}

	if CustomRuntimeValidator != nil {
		return CustomRuntimeValidator(runtime)
	}

	return fmt.Errorf("unsupported runtime %q, supported values: %v", runtime, SupportedRuntimes)
}
