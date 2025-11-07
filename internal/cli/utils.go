package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/models"
)

// findServersByName finds servers by name, checking full name first, then partial name
func findServersByName(searchName string) []*models.ServerDetail {
	servers, err := APIClient.GetServers()
	if err != nil {
		log.Fatalf("Failed to get servers: %v", err)
	}

	// First, try exact match with full name
	for _, s := range servers {
		if s.Name == searchName {
			return []*models.ServerDetail{&s}
		}
	}

	// If no exact match, search for name part (after /)
	var matches []*models.ServerDetail
	searchLower := strings.ToLower(searchName)

	for _, s := range servers {
		// Extract name part (after /)
		parts := strings.Split(s.Name, "/")
		var namePart string
		if len(parts) == 2 {
			namePart = strings.ToLower(parts[1])
		} else {
			namePart = strings.ToLower(s.Name)
		}

		if namePart == searchLower {
			serverCopy := s
			matches = append(matches, &serverCopy)
		}
	}

	return matches
}

// splitServerName splits a server name into namespace and name parts
func splitServerName(fullName string) (namespace, name string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", fullName
}

// parseKeyValuePairs parses key=value pairs from command line flags
func parseKeyValuePairs(pairs []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, pair := range pairs {
		idx := findFirstEquals(pair)
		if idx == -1 {
			return nil, fmt.Errorf("invalid key=value pair (missing =): %s", pair)
		}
		key := pair[:idx]
		value := pair[idx+1:]
		result[key] = value
	}
	return result, nil
}

// findFirstEquals finds the first = character in a string
func findFirstEquals(s string) int {
	for i, c := range s {
		if c == '=' {
			return i
		}
	}
	return -1
}

// generateRandomName generates a random hex string for use in naming
func generateRandomName() (string, error) {
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random name: %w", err)
	}
	return hex.EncodeToString(randomBytes), nil
}

// generateRuntimePaths generates random names and paths for runtime directories
// Returns projectName, runtimeDir, and any error encountered
func generateRuntimePaths(prefix string) (projectName string, runtimeDir string, err error) {
	// Generate a random name
	randomName, err := generateRandomName()
	if err != nil {
		return "", "", err
	}

	// Create project name with prefix
	projectName = prefix + randomName

	// Get home directory and construct runtime directory path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("failed to get home directory: %w", err)
	}
	baseRuntimeDir := filepath.Join(homeDir, ".arctl", "runtime")
	runtimeDir = filepath.Join(baseRuntimeDir, prefix+randomName)

	return projectName, runtimeDir, nil
}
