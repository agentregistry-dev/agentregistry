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

// ResolveManifestPrompts fetches prompts referenced in the agent manifest from the registry
// and returns them as PythonPrompt structs ready to be written to prompts.json.
func ResolveManifestPrompts(manifest *agentmanifest.AgentManifest, verbose bool) ([]common.PythonPrompt, error) {
	if manifest == nil || len(manifest.Prompts) == 0 {
		return nil, nil
	}

	if verbose {
		fmt.Printf("[prompt-resolver] Processing %d prompts from manifest\n", len(manifest.Prompts))
	}

	clients := make(map[string]*client.Client)

	var resolved []common.PythonPrompt
	for i, ref := range manifest.Prompts {
		registryURL := ref.RegistryURL
		if registryURL == "" {
			registryURL = defaultRegistryURL
		}

		promptName := ref.RegistryPromptName
		promptVersion := ref.RegistryPromptVersion

		if verbose {
			fmt.Printf("[prompt-resolver] [%d] Resolving prompt %q (registryPromptName=%q version=%q registryURL=%q)\n",
				i, ref.Name, promptName, promptVersion, registryURL)
		}

		apiClient, ok := clients[registryURL]
		if !ok {
			apiClient = client.NewClient(registryURL, "")
			clients[registryURL] = apiClient
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
			return nil, fmt.Errorf("prompt %q not found in registry at %s", promptName, registryURL)
		}

		if verbose {
			fmt.Printf("[prompt-resolver] [%d] Successfully resolved prompt %q (version=%q, content length=%d)\n",
				i, ref.Name, promptResp.Metadata.Version, len(promptResp.Spec.Content))
		}

		resolved = append(resolved, common.PythonPrompt{
			Name:    ref.Name,
			Content: promptResp.Spec.Content,
		})
	}

	if verbose {
		fmt.Printf("[prompt-resolver] Resolved %d prompts total\n", len(resolved))
	}

	return resolved, nil
}
