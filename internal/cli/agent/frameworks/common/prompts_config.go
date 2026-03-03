package common

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

// PythonPrompt represents the JSON structure written to prompts.json for the Python agent.
// Each prompt is a named text blob (the instruction content).
type PythonPrompt struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// PythonPromptFromResponse extracts the text content from a PromptJSON.
func PythonPromptFromResponse(name string, prompt *models.PromptJSON) PythonPrompt {
	return PythonPrompt{
		Name:    name,
		Content: prompt.Content,
	}
}

// ComputePromptsConfigPath returns the config directory and file path for prompts.
// If BaseDir or AgentName is empty, both returned paths are empty.
func ComputePromptsConfigPath(target *MCPConfigTarget) (configDir string, configPath string) {
	if target.BaseDir == "" || target.AgentName == "" {
		return "", ""
	}

	configDir = filepath.Join(target.BaseDir, target.AgentName)
	if target.Version != "" {
		configDir = filepath.Join(configDir, utils.SanitizeVersion(target.Version))
	}
	configPath = filepath.Join(configDir, "prompts.json")
	return configDir, configPath
}

// RefreshPromptsConfig cleans any existing prompts config for the target and optionally writes a new one.
// If prompts is empty or nil, it performs cleanup only.
func RefreshPromptsConfig(target *MCPConfigTarget, prompts []PythonPrompt, verbose bool) error {
	if target == nil {
		return fmt.Errorf("target is required")
	}

	configDir, configPath := ComputePromptsConfigPath(target)
	if configDir == "" {
		return nil
	}

	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove prompts config file: %w", err)
	}

	if len(prompts) == 0 {
		return nil
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create agent config directory: %w", err)
	}

	configData, err := json.MarshalIndent(prompts, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal prompts config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("failed to write prompts config file: %w", err)
	}

	if verbose {
		fmt.Printf("Wrote prompts config for agent %s version %s to %s\n", target.AgentName, target.Version, configPath)
	}

	return nil
}
