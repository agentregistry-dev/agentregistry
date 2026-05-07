package declarative

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

// LoadDotEnv reads .env from projectDir if present. Missing file is not an error.
func LoadDotEnv(projectDir string) (map[string]string, error) {
	path := filepath.Join(projectDir, ".env")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return godotenv.Parse(strings.NewReader(string(data)))
}

// ValidateRequiredEnv returns an error listing every required key not in env.
// A required key is also considered satisfied if it is set in the process env.
// Empty string values are treated as missing — `arctl init` writes a `.env`
// with empty placeholders, and an unfilled placeholder is not a satisfied var.
func ValidateRequiredEnv(env map[string]string, required []string) error {
	var missing []string
	for _, k := range required {
		if v, ok := env[k]; ok && v != "" {
			continue
		}
		if v := os.Getenv(k); v != "" {
			continue // process env satisfies it
		}
		missing = append(missing, k)
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("missing required env: %s (set in .env or pass --env)", strings.Join(missing, ", "))
}
