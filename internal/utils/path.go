package utils

import (
	"os"
	"path/filepath"
)

// IsLocalPath checks if the given string is a path to a local directory
func IsLocalPath(path string) bool {
	// Check for common path indicators
	if path == "." || path == ".." || filepath.IsAbs(path) {
		return true
	}
	// Check for relative paths
	if len(path) > 0 && (path[0] == '.' || path[0] == '/') {
		return true
	}
	// Check if the path exists as a directory
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return true
	}
	return false
}
