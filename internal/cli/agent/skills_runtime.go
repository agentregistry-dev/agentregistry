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
	"github.com/agentregistry-dev/agentregistry/internal/cli/common/gitutil"
	arclient "github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

type resolvedSkillRef struct {
	name    string
	repoURL string // Git repository URL
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

// resolveSkillSource resolves a v1alpha1 skill ResourceRef to a git
// repository URL. The skill is fetched from the configured registry.
// The local-side identifier is the basename of ref.Name
// (e.g. "summarize" for "acme/summarize").
func resolveSkillSource(skill v1alpha1.ResourceRef) (resolvedSkillRef, error) {
	registrySkillName := strings.TrimSpace(skill.Name)
	if registrySkillName == "" {
		return resolvedSkillRef{}, fmt.Errorf("skill ref has empty name")
	}

	localName := agentmanifest.RefBasename(registrySkillName)
	tag := strings.TrimSpace(skill.Tag)
	if tag == "" {
		tag = "latest"
	}

	skillResp, err := fetchSkillFromRegistry(registrySkillName, tag)
	if err != nil {
		return resolvedSkillRef{}, err
	}
	if skillResp == nil {
		return resolvedSkillRef{}, fmt.Errorf("skill not found: %s (tag %s)", registrySkillName, tag)
	}

	repoURL, err := extractSkillRepoURL(skillResp)
	if err != nil {
		return resolvedSkillRef{}, fmt.Errorf("skill %s (tag %s): no git repository found", registrySkillName, tag)
	}
	return resolvedSkillRef{name: localName, repoURL: repoURL}, nil
}

// extractSkillRepoURL extracts a git repository URL from a skill response.
func extractSkillRepoURL(skillResp *v1alpha1.Skill) (string, error) {
	if skillResp == nil {
		return "", fmt.Errorf("skill response is required")
	}
	if skillResp.Spec.Source != nil &&
		skillResp.Spec.Source.Repository != nil &&
		strings.TrimSpace(skillResp.Spec.Source.Repository.URL) != "" {
		return strings.TrimSpace(skillResp.Spec.Source.Repository.URL), nil
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
		if skill.repoURL == "" {
			return fmt.Errorf("skill %q has no repository URL", skill.name)
		}
		if err := gitutil.CloneAndCopy(skill.repoURL, targetDir, verbose); err != nil {
			return fmt.Errorf("materialize skill %q from repo %q: %w", skill.name, skill.repoURL, err)
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
