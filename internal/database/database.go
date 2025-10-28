package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

var DB *sql.DB

// Initialize sets up the SQLite database
func Initialize() error {
	// Get home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// Create .arctl directory if it doesn't exist
	arctlDir := filepath.Join(homeDir, ".arctl")
	if err := os.MkdirAll(arctlDir, 0755); err != nil {
		return fmt.Errorf("failed to create .arctl directory: %w", err)
	}

	// Open database connection
	dbPath := filepath.Join(arctlDir, "arctl.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	DB = db

	// Enable foreign key constraints (disabled by default in SQLite)
	if _, err := DB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Create tables
	if err := createTables(); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	return nil
}

func createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS registries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT UNIQUE NOT NULL,
		url TEXT NOT NULL,
		type TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS servers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		registry_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		title TEXT,
		description TEXT NOT NULL,
		version TEXT NOT NULL,
		website_url TEXT,
		installed BOOLEAN DEFAULT 0,
		data TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (registry_id) REFERENCES registries(id) ON DELETE CASCADE,
		UNIQUE(registry_id, name, version)
	);

	CREATE TABLE IF NOT EXISTS skills (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		registry_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT NOT NULL,
		version TEXT NOT NULL,
		installed BOOLEAN DEFAULT 0,
		data TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (registry_id) REFERENCES registries(id) ON DELETE CASCADE,
		UNIQUE(registry_id, name, version)
	);

	CREATE TABLE IF NOT EXISTS installations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		resource_type TEXT NOT NULL,
		resource_id INTEGER NOT NULL,
		resource_name TEXT NOT NULL,
		version TEXT NOT NULL,
		config TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(resource_type, resource_name)
	);

	CREATE INDEX IF NOT EXISTS idx_servers_registry ON servers(registry_id);
	CREATE INDEX IF NOT EXISTS idx_skills_registry ON skills(registry_id);
	CREATE INDEX IF NOT EXISTS idx_servers_installed ON servers(installed);
	CREATE INDEX IF NOT EXISTS idx_skills_installed ON skills(installed);
	`

	_, err := DB.Exec(schema)
	return err
}

// Close closes the database connection
func Close() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}

// GetRegistries returns all connected registries
func GetRegistries() ([]models.Registry, error) {
	if DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	rows, err := DB.Query(`
		SELECT id, name, url, type, created_at, updated_at 
		FROM registries 
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query registries: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var registries []models.Registry
	for rows.Next() {
		var r models.Registry
		if err := rows.Scan(&r.ID, &r.Name, &r.URL, &r.Type, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan registry: %w", err)
		}
		registries = append(registries, r)
	}

	return registries, nil
}

// GetServers returns all MCP servers from connected registries
func GetServers() ([]models.ServerDetail, error) {
	if DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	rows, err := DB.Query(`
		SELECT id, registry_id, name, title, description, version, website_url, installed, data, created_at, updated_at 
		FROM servers 
		ORDER BY name, version DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query servers: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var servers []models.ServerDetail
	for rows.Next() {
		var s models.ServerDetail
		if err := rows.Scan(&s.ID, &s.RegistryID, &s.Name, &s.Title, &s.Description, &s.Version, &s.WebsiteURL, &s.Installed, &s.Data, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan server: %w", err)
		}
		servers = append(servers, s)
	}

	return servers, nil
}

// GetSkills returns all skills from connected registries
func GetSkills() ([]models.Skill, error) {
	if DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	rows, err := DB.Query(`
		SELECT id, registry_id, name, description, version, installed, data, created_at, updated_at 
		FROM skills 
		ORDER BY name, version DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query skills: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var skills []models.Skill
	for rows.Next() {
		var s models.Skill
		if err := rows.Scan(&s.ID, &s.RegistryID, &s.Name, &s.Description, &s.Version, &s.Installed, &s.Data, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan skill: %w", err)
		}
		skills = append(skills, s)
	}

	return skills, nil
}

// GetInstallations returns all installed resources
func GetInstallations() ([]models.Installation, error) {
	if DB == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	rows, err := DB.Query(`
		SELECT id, resource_type, resource_id, resource_name, version, config, created_at, updated_at 
		FROM installations 
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query installations: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var installations []models.Installation
	for rows.Next() {
		var i models.Installation
		if err := rows.Scan(&i.ID, &i.ResourceType, &i.ResourceID, &i.ResourceName, &i.Version, &i.Config, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan installation: %w", err)
		}
		installations = append(installations, i)
	}

	return installations, nil
}

// AddRegistry adds a new registry
func AddRegistry(name, url, registryType string) error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := DB.Exec(`
		INSERT INTO registries (name, url, type) 
		VALUES (?, ?, ?)
	`, name, url, registryType)
	if err != nil {
		return fmt.Errorf("failed to add registry: %w", err)
	}

	return nil
}

// RemoveRegistry removes a registry
func RemoveRegistry(name string) error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}

	result, err := DB.Exec(`DELETE FROM registries WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("failed to remove registry: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("registry not found: %s", name)
	}

	return nil
}

// AddOrUpdateServer adds or updates a server in the database
func AddOrUpdateServer(registryID int, name, title, description, version, websiteURL, data string) error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}

	// Use INSERT OR REPLACE to handle duplicates
	_, err := DB.Exec(`
		INSERT INTO servers (registry_id, name, title, description, version, website_url, data, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(registry_id, name, version) DO UPDATE SET
			title = excluded.title,
			description = excluded.description,
			website_url = excluded.website_url,
			data = excluded.data,
			updated_at = CURRENT_TIMESTAMP
	`, registryID, name, title, description, version, websiteURL, data)
	if err != nil {
		return fmt.Errorf("failed to add/update server: %w", err)
	}

	return nil
}

// ClearRegistryServers removes all servers for a specific registry
func ClearRegistryServers(registryID int) error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := DB.Exec(`DELETE FROM servers WHERE registry_id = ?`, registryID)
	if err != nil {
		return fmt.Errorf("failed to clear servers: %w", err)
	}

	return nil
}
