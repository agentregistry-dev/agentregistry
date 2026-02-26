package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common/docker"
	arclient "github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

type resolvedSkillRef struct {
	name  string
	image string
}

func resolveSkillsForRuntime(manifest *models.AgentManifest) ([]resolvedSkillRef, error) {
	if manifest == nil || len(manifest.Skills) == 0 {
		return nil, nil
	}

	resolved := make([]resolvedSkillRef, 0, len(manifest.Skills))
	for _, skill := range manifest.Skills {
		imageRef, err := resolveSkillImageRef(skill)
		if err != nil {
			return nil, fmt.Errorf("resolve skill %q: %w", skill.Name, err)
		}
		resolved = append(resolved, resolvedSkillRef{
			name:  skill.Name,
			image: imageRef,
		})
	}
	slices.SortFunc(resolved, func(a, b resolvedSkillRef) int {
		return strings.Compare(a.name, b.name)
	})

	return resolved, nil
}

func resolveSkillImageRef(skill models.SkillRef) (string, error) {
	image := strings.TrimSpace(skill.Image)
	registrySkillName := strings.TrimSpace(skill.RegistrySkillName)
	hasImage := image != ""
	hasRegistry := registrySkillName != ""

	if !hasImage && !hasRegistry {
		return "", fmt.Errorf("one of image or registrySkillName is required")
	}
	if hasImage && hasRegistry {
		return "", fmt.Errorf("only one of image or registrySkillName may be set")
	}
	if hasImage {
		return image, nil
	}

	version := strings.TrimSpace(skill.RegistrySkillVersion)
	if version == "" {
		version = "latest"
	}

	skillResp, err := fetchSkillFromRegistry(skill.RegistryURL, registrySkillName, version)
	if err != nil {
		return "", err
	}
	if skillResp == nil {
		return "", fmt.Errorf("skill not found: %s (version %s)", registrySkillName, version)
	}

	imageRef, err := extractSkillImageRef(skillResp)
	if err != nil {
		return "", fmt.Errorf("skill %s (version %s): %w", registrySkillName, version, err)
	}
	return imageRef, nil
}

func fetchSkillFromRegistry(registryURL, skillName, version string) (*models.SkillResponse, error) {
	// Use the default configured API client when registry URL is omitted.
	if strings.TrimSpace(registryURL) == "" {
		if apiClient == nil {
			return nil, fmt.Errorf("API client not initialized")
		}
		if strings.EqualFold(version, "latest") {
			return apiClient.GetSkillByName(skillName)
		}
		return apiClient.GetSkillByNameAndVersion(skillName, version)
	}

	baseURL, err := normalizeSkillRegistryURL(registryURL)
	if err != nil {
		return nil, err
	}

	// TODO: DI the client.
	client := arclient.NewClient(baseURL, "")
	if strings.EqualFold(version, "latest") {
		resp, err := client.GetSkillByName(skillName)
		if err != nil {
			return nil, fmt.Errorf("fetch skill %q from %s: %w", skillName, baseURL, err)
		}
		return resp, nil
	}

	resp, err := client.GetSkillByNameAndVersion(skillName, version)
	if err != nil {
		return nil, fmt.Errorf("fetch skill %q version %q from %s: %w", skillName, version, baseURL, err)
	}
	return resp, nil
}

func normalizeSkillRegistryURL(registryURL string) (string, error) {
	trimmed := strings.TrimSpace(registryURL)
	if trimmed == "" {
		return "", fmt.Errorf("registry URL is required")
	}

	trimmed = strings.TrimSuffix(trimmed, "/")
	if strings.HasSuffix(trimmed, "/v0") {
		return trimmed, nil
	}
	return trimmed + "/v0", nil
}

func extractSkillImageRef(skillResp *models.SkillResponse) (string, error) {
	if skillResp == nil {
		return "", fmt.Errorf("skill response is required")
	}
	// TODO: add support for git-based skill fetching. Requires
	// https://github.com/kagent-dev/kagent/pull/1365.
	for _, pkg := range skillResp.Skill.Packages {
		typ := strings.ToLower(strings.TrimSpace(pkg.RegistryType))
		if (typ == "docker" || typ == "oci") && strings.TrimSpace(pkg.Identifier) != "" {
			return strings.TrimSpace(pkg.Identifier), nil
		}
	}
	return "", fmt.Errorf("no docker/oci package found")
}

func materializeSkillsForRuntime(skills []resolvedSkillRef, skillsDir string, verbose bool) error {
	if strings.TrimSpace(skillsDir) == "" {
		if len(skills) == 0 {
			return nil
		}
		return fmt.Errorf("skills directory is required")
	}

	if err := os.RemoveAll(skillsDir); err != nil {
		return fmt.Errorf("clear skills directory %s: %w", skillsDir, err)
	}
	if len(skills) == 0 {
		return nil
	}
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf("create skills directory %s: %w", skillsDir, err)
	}

	usedDirs := make(map[string]int)
	for _, skill := range skills {
		dirName := sanitizeSkillDirName(skill.name)
		if count := usedDirs[dirName]; count > 0 {
			dirName += "-" + strconv.Itoa(count+1)
		}
		usedDirs[dirName]++

		targetDir := filepath.Join(skillsDir, dirName)
		if err := extractSkillImage(skill.image, targetDir, verbose); err != nil {
			return fmt.Errorf("materialize skill %q from %q: %w", skill.name, skill.image, err)
		}
	}
	return nil
}

func sanitizeSkillDirName(name string) string {
	out := strings.TrimSpace(strings.ToLower(name))
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		" ", "-",
		".", "-",
		"@", "-",
	)
	out = replacer.Replace(out)
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	out = strings.Trim(out, "-")
	if out == "" {
		return "skill"
	}
	return out
}

func extractSkillImage(imageRef, targetDir string, verbose bool) error {
	if strings.TrimSpace(imageRef) == "" {
		return fmt.Errorf("image reference is required")
	}

	exec := docker.NewExecutor(verbose, "")
	if !exec.ImageExistsLocally(imageRef) {
		if err := exec.Pull(imageRef); err != nil {
			return fmt.Errorf("pull image: %w", err)
		}
	}

	containerID, err := exec.CreateContainer(imageRef)
	if err != nil {
		return err
	}
	defer func() {
		_ = exec.RemoveContainer(containerID)
	}()

	tempDir, err := os.MkdirTemp("", "arctl-skill-extract-*")
	if err != nil {
		return fmt.Errorf("create temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	if err := exec.CopyFromContainer(containerID, "/.", tempDir); err != nil {
		return err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("create target skill directory: %w", err)
	}
	if err := docker.CopyNonEmptyContents(tempDir, targetDir); err != nil {
		return fmt.Errorf("copy extracted skill contents: %w", err)
	}
	return nil
}
