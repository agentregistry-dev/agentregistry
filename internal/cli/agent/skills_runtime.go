package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	agentmanifest "github.com/agentregistry-dev/agentregistry/internal/cli/agent/manifest"
	"github.com/agentregistry-dev/agentregistry/internal/cli/common/docker"
	"github.com/agentregistry-dev/agentregistry/internal/cli/common/gitutil"
	arclient "github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

type resolvedSkillRef struct {
	name    string
	image   string // Docker/OCI image ref (mutually exclusive with repoURL)
	repoURL string // Git repository URL (mutually exclusive with image)
}

func resolveSkillsForRuntime(skills []v1alpha1.ResourceRef) ([]resolvedSkillRef, error) {
	if len(skills) == 0 {
		return nil, nil
	}

	resolved := make([]resolvedSkillRef, 0, len(skills))
	for _, skill := range skills {
		ref, err := resolveSkillSource(skill)
		if err != nil {
			return nil, fmt.Errorf("resolve skill %q: %w", skill.Name, err)
		}
		resolved = append(resolved, ref)
	}
	slices.SortFunc(resolved, func(a, b resolvedSkillRef) int {
		return strings.Compare(a.name, b.name)
	})

	return resolved, nil
}

// resolveSkillSource resolves a v1alpha1 skill ResourceRef to either a
// Docker image or a git repository URL. The skill is fetched from the
// configured registry; OCI packages are preferred, with the skill's git
// repository as a fallback. The local-side identifier is the basename
// of ref.Name (e.g. "summarize" for "acme/summarize").
func resolveSkillSource(skill v1alpha1.ResourceRef) (resolvedSkillRef, error) {
	registrySkillName := strings.TrimSpace(skill.Name)
	if registrySkillName == "" {
		return resolvedSkillRef{}, fmt.Errorf("skill ref has empty name")
	}

	localName := agentmanifest.RefBasename(registrySkillName)
	version := strings.TrimSpace(skill.Version)
	if version == "" {
		version = "latest"
	}

	skillResp, err := fetchSkillFromRegistry(registrySkillName, version)
	if err != nil {
		return resolvedSkillRef{}, err
	}
	if skillResp == nil {
		return resolvedSkillRef{}, fmt.Errorf("skill not found: %s (version %s)", registrySkillName, version)
	}

	// Prefer Docker/OCI image if available.
	imageRef, err := extractSkillImageRef(skillResp)
	if err == nil {
		return resolvedSkillRef{name: localName, image: imageRef}, nil
	}

	// Fall back to git repository.
	repoURL, err := extractSkillRepoURL(skillResp)
	if err != nil {
		return resolvedSkillRef{}, fmt.Errorf("skill %s (version %s): no docker/oci package or git repository found", registrySkillName, version)
	}
	return resolvedSkillRef{name: localName, repoURL: repoURL}, nil
}

// extractSkillRepoURL extracts a git repository URL from a skill response.
func extractSkillRepoURL(skillResp *v1alpha1.Skill) (string, error) {
	if skillResp == nil {
		return "", fmt.Errorf("skill response is required")
	}
	if skillResp.Spec.Repository != nil &&
		strings.TrimSpace(skillResp.Spec.Repository.URL) != "" {
		return strings.TrimSpace(skillResp.Spec.Repository.URL), nil
	}
	return "", fmt.Errorf("no git repository found")
}

func fetchSkillFromRegistry(skillName, version string) (*v1alpha1.Skill, error) {
	if apiClient == nil {
		return nil, fmt.Errorf("API client not initialized")
	}
	targetVersion := strings.TrimSpace(version)
	if strings.EqualFold(targetVersion, "latest") {
		targetVersion = ""
	}
	return arclient.GetTyped(
		context.Background(),
		apiClient,
		v1alpha1.KindSkill,
		v1alpha1.DefaultNamespace,
		skillName,
		targetVersion,
		func() *v1alpha1.Skill { return &v1alpha1.Skill{} },
	)
}

func extractSkillImageRef(skillResp *v1alpha1.Skill) (string, error) {
	if skillResp == nil {
		return "", fmt.Errorf("skill response is required")
	}
	// TODO: add support for git-based skill fetching. Requires
	// https://github.com/kagent-dev/kagent/pull/1365.
	for _, pkg := range skillResp.Spec.Packages {
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
		switch {
		case skill.image != "":
			if err := extractSkillImage(skill.image, targetDir, verbose); err != nil {
				return fmt.Errorf("materialize skill %q from image %q: %w", skill.name, skill.image, err)
			}
		case skill.repoURL != "":
			if err := gitutil.CloneAndCopy(skill.repoURL, targetDir, verbose); err != nil {
				return fmt.Errorf("materialize skill %q from repo %q: %w", skill.name, skill.repoURL, err)
			}
		default:
			return fmt.Errorf("skill %q has no image or repository URL", skill.name)
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
