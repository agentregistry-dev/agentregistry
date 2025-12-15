package utils

import (
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// RegistryConfig holds the configuration for a registry type
type RegistryConfig struct {
	Image   string
	Command string
}

// ProcessArguments processes model.Argument slices into []string args, allowing for overrides.
// It processes positional arguments first, then named arguments.
func ProcessArguments(
	args []string,
	modelArgs []model.Argument,
	argOverrides map[string]string,
) []string {
	getArgValue := func(arg model.Argument) string {
		// Check for override first if provided
		if argOverrides != nil {
			if v, exists := argOverrides[arg.Name]; exists {
				return v
			}
		}
		// Use value if set
		if arg.Value != "" {
			return arg.Value
		}
		// Fall back to default
		return arg.Default
	}

	// Process positional arguments first
	for _, arg := range modelArgs {
		if arg.Type == model.ArgumentTypePositional {
			value := getArgValue(arg)
			if value != "" {
				args = append(args, value)
			}
		}
	}

	// Then process named arguments
	for _, arg := range modelArgs {
		if arg.Type == model.ArgumentTypeNamed {
			// Always add the argument name (e.g., "--rm", "-e")
			args = append(args, arg.Name)

			// Add value if present (not all named args have values)
			value := getArgValue(arg)
			if value != "" {
				args = append(args, value)
			}
		}
	}

	return args
}

// ProcessEnvironmentVariables validates and processes environment variables
// into a map[string]string, allowing for overrides.
func ProcessEnvironmentVariables(
	envVars []model.KeyValueInput,
	overrides map[string]string,
) (map[string]string, error) {
	result := make(map[string]string)
	var missingRequired []string

	for _, env := range envVars {
		var value string

		// Check if override provided
		if override, exists := overrides[env.Name]; exists {
			value = override
		} else if env.Value != "" {
			// Use value if set
			value = env.Value
		} else if env.Default != "" {
			// Use default if available
			value = env.Default
		}

		// Track missing required vars
		if env.IsRequired && value == "" {
			missingRequired = append(missingRequired, env.Name)
		}

		// Only add to result if value is not empty
		if value != "" {
			result[env.Name] = value
		}
	}

	if len(missingRequired) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missingRequired, ", "))
	}

	// Add any override vars that weren't in the spec
	for key, value := range overrides {
		found := false
		for _, env := range envVars {
			if env.Name == key {
				found = true
				break
			}
		}
		if !found {
			result[key] = value
		}
	}

	return result, nil
}

// ProcessHeaders validates and processes headers into a map[string]string, allowing for overrides.
func ProcessHeaders(
	headers []model.KeyValueInput,
	headerOverrides map[string]string,
) (map[string]string, error) {
	result := make(map[string]string)
	var missingRequired []string

	for _, h := range headers {
		var value string

		// Check if override provided
		if headerOverrides != nil {
			if override, exists := headerOverrides[h.Name]; exists {
				value = override
			}
		}

		// Use value if not overridden
		if value == "" {
			value = h.Value
		}

		// Fall back to default if value still empty
		if value == "" {
			value = h.Default
		}

		// Track missing required headers
		if h.IsRequired && value == "" {
			missingRequired = append(missingRequired, h.Name)
		}

		// Only add to result if value is not empty
		if value != "" {
			result[h.Name] = value
		}
	}

	if len(missingRequired) > 0 {
		return nil, fmt.Errorf("missing required headers: %s", strings.Join(missingRequired, ", "))
	}

	return result, nil
}

// GetRegistryConfig returns the image and command configuration for a given registry type.
func GetRegistryConfig(
	packageInfo model.Package,
	args []string,
) (RegistryConfig, []string, error) {
	var config RegistryConfig

	// Normalize registry type to handle both constant and string cases
	normalizedType := strings.ToLower(string(packageInfo.RegistryType))

	switch normalizedType {
	case strings.ToLower(string(model.RegistryTypeNPM)):
		config.Image = "node:24-alpine3.21"
		config.Command = packageInfo.RunTimeHint
		if config.Command == "" {
			config.Command = "npx"
		}
		if !slices.Contains(args, "-y") {
			args = append(args, "-y")
		}

		// Append identifier with version, if specified
		if packageInfo.Version != "" {
			args = append(args, packageInfo.Identifier+"@"+packageInfo.Version)
		} else {
			args = append(args, packageInfo.Identifier)
		}

	case strings.ToLower(string(model.RegistryTypePyPI)):
		config.Image = "ghcr.io/astral-sh/uv:debian"
		config.Command = packageInfo.RunTimeHint
		if config.Command == "" {
			config.Command = "uvx"
		}

		// Append identifier with version, if specified
		if packageInfo.Version != "" {
			args = append(args, packageInfo.Identifier+"=="+packageInfo.Version)
		} else {
			args = append(args, packageInfo.Identifier)
		}

	case strings.ToLower(string(model.RegistryTypeOCI)):
		config.Image = packageInfo.Identifier

	default:
		return RegistryConfig{}, nil, fmt.Errorf("unsupported package registry type: %s", string(packageInfo.RegistryType))
	}

	return config, args, nil
}

// EnvMapToStringSlice converts a map[string]string to []string in "KEY=VALUE" format
// for docker-compose compatibility.
func EnvMapToStringSlice(envMap map[string]string) []string {
	result := make([]string, 0, len(envMap))
	for key, value := range envMap {
		result = append(result, fmt.Sprintf("%s=%s", key, value))
	}
	return result
}
