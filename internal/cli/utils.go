package cli

import (
	"log"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/database"
	"github.com/agentregistry-dev/agentregistry/internal/models"
)

// findServersByName finds servers by name, checking full name first, then partial name
func findServersByName(searchName string) []*models.ServerDetail {
	servers, err := database.GetServers()
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
