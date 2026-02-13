package common

import (
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/stoewer/go-strcase"
)

const DefaultUserName = "user"

// BuildLocalImageName constructs a local Docker image name from a project name and version.
// Returns format: "kebab-case-name:version"
func BuildLocalImageName(name, version string) string {
	if version == "" {
		version = "latest"
	}
	return fmt.Sprintf("%s:%s", strcase.KebabCase(name), version)
}

// BuildRegistryImageName constructs a full Docker registry image reference.
// Returns format: "registry-url/kebab-case-name:version"
func BuildRegistryImageName(registryURL, name, version string) string {
	return fmt.Sprintf("%s/%s", strings.TrimSuffix(registryURL, "/"), BuildLocalImageName(name, version))
}

// ValidateProjectDir checks if the provided project directory exists and is a directory.
func ValidateProjectDir(projectDir string) error {
	info, err := os.Stat(projectDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("project directory does not exist: %s", projectDir)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("project directory is not a directory: %s", projectDir)
	}
	return nil
}

// GetImageNameFromManifest loads the project manifest and
// constructs a Docker image name in the format "kebab-case-name:version".
func GetImageNameFromManifest(loader manifest.ManifestLoader) (string, error) {
	if !loader.Exists() {
		return "", fmt.Errorf(
			"manifest not found")
	}

	projectManifest, err := loader.Load()
	if err != nil {
		return "", fmt.Errorf("failed to load project manifest: %w", err)
	}

	return BuildLocalImageName(projectManifest.Name, projectManifest.Version), nil
}

func BuildMCPServerRegistryName(author, name string) string {
	if author == "" {
		printer.PrintInfo(fmt.Sprintf("Author not specified, defaulting to '%s'", DefaultUserName))
		author = DefaultUserName
	}
	return fmt.Sprintf("%s/%s", strings.ToLower(author), strings.ToLower(name))
}

// ResolveVersion returns the version to use based on priority: flag > manifest > "latest".
func ResolveVersion(flagVersion, manifestVersion string) string {
	if flagVersion != "" {
		return flagVersion
	}
	if manifestVersion != "" {
		return manifestVersion
	}
	return "latest"
}
