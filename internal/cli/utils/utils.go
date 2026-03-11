package utils

import (
	"fmt"
	"strings"
)

// ParseEnvFlags parses KEY=VALUE strings into a map. Returns an error on invalid format.
func ParseEnvFlags(envFlags []string) (map[string]string, error) {
	envMap := make(map[string]string)
	for _, e := range envFlags {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid env format (expected KEY=VALUE): %s", e)
		}
		envMap[parts[0]] = parts[1]
	}
	return envMap, nil
}
