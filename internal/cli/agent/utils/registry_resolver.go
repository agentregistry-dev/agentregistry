package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	agentmanifest "github.com/agentregistry-dev/agentregistry/internal/cli/agent/manifest"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

var defaultRegistryURL = "http://127.0.0.1:12121"

// SetDefaultRegistryURL overrides the fallback registry URL used when manifests omit registry_url.
func SetDefaultRegistryURL(url string) {
	if strings.TrimSpace(url) == "" {
		return
	}
	defaultRegistryURL = url
}

// GetDefaultRegistryURL returns the current default registry URL (without /v0 suffix).
// This is the form stored in agent.yaml manifest entries.
func GetDefaultRegistryURL() string {
	return strings.TrimSuffix(strings.TrimSuffix(defaultRegistryURL, "/"), "/v0")
}

// ResolvePromptRefs fetches each v1alpha1 prompt ResourceRef from the
// configured registry and returns the resolved content as PythonPrompt
// entries ready to be written to prompts.json. The local prompts.json
// key is the basename of ref.Name (e.g. "system" for "acme/system"),
// so the agent runtime can address the prompt by a short identifier.
func ResolvePromptRefs(prompts []v1alpha1.ResourceRef, verbose bool) ([]common.PythonPrompt, error) {
	if len(prompts) == 0 {
		return nil, nil
	}

	if verbose {
		fmt.Printf("[prompt-resolver] Processing %d prompts from manifest\n", len(prompts))
	}

	apiClient := client.NewClient(defaultRegistryURL, "")

	var resolved []common.PythonPrompt
	for i, ref := range prompts {
		promptName := strings.TrimSpace(ref.Name)
		promptVersion := strings.TrimSpace(ref.Version)
		if strings.EqualFold(promptVersion, "latest") {
			promptVersion = ""
		}
		localName := agentmanifest.RefBasename(promptName)

		if verbose {
			fmt.Printf("[prompt-resolver] [%d] Resolving prompt %q (registryPromptName=%q version=%q)\n",
				i, localName, promptName, promptVersion)
		}

		promptResp, err := client.GetTyped(
			context.Background(),
			apiClient,
			v1alpha1.KindPrompt,
			v1alpha1.DefaultNamespace,
			promptName,
			promptVersion,
			func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} },
		)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch prompt %q from registry: %w", promptName, err)
		}
		if promptResp == nil {
			return nil, fmt.Errorf("prompt %q not found in registry at %s", promptName, defaultRegistryURL)
		}

		if verbose {
			fmt.Printf("[prompt-resolver] [%d] Successfully resolved prompt %q (version=%q, content length=%d)\n",
				i, localName, promptResp.Metadata.Version, len(promptResp.Spec.Content))
		}

		resolved = append(resolved, common.PythonPrompt{
			Name:    localName,
			Content: promptResp.Spec.Content,
		})
	}

	if verbose {
		fmt.Printf("[prompt-resolver] Resolved %d prompts total\n", len(resolved))
	}

	return resolved, nil
}
