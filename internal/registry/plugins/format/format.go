// Package format detects a plugin bundle's layout with the rules the kagent
// harnesses apply at startup (loadManifest in kagent
// go/core/pkg/agentplugins/materialize.go), so a Plugin the registry accepts is
// one the harnesses accept. Raise v1alpha1.PluginScanVersion with any change to
// these rules.
package format

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

const (
	agentPluginsManifest = "plugin.json"
	agentPluginsSchema   = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	maxNameLength        = 64
)

var namePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`)

// manifestKeys are the only manifest keys the rules type-check. kagent drops
// every other key without error.
var manifestKeys = map[string]bool{
	"$schema": true, "name": true, "version": true, "description": true, "author": true,
	"homepage": true, "repository": true, "license": true, "keywords": true, "extensions": true,
}

// manifest holds the kept keys with the JSON types kagent requires of them.
type manifest struct {
	Schema      string                     `json:"$schema"`
	Name        string                     `json:"name"`
	Version     string                     `json:"version,omitempty"`
	Description string                     `json:"description,omitempty"`
	Author      *manifestAuthor            `json:"author,omitempty"`
	Homepage    string                     `json:"homepage,omitempty"`
	Repository  string                     `json:"repository,omitempty"`
	License     string                     `json:"license,omitempty"`
	Keywords    []string                   `json:"keywords,omitempty"`
	Extensions  map[string]json.RawMessage `json:"extensions,omitempty"`
}

// manifestAuthor is the object form of author, the only form kagent accepts.
type manifestAuthor struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// Detect returns the format of b and the path of the manifest the rules
// selected. Every rejection wraps bundle.ErrInvalidBundle.
func Detect(b *bundle.CanonicalBundle) (v1alpha1.PluginFormat, string, error) {
	path, format, err := selectManifest(b)
	if err != nil {
		return "", "", err
	}
	if err := checkManifest(b.Files[path], format); err != nil {
		return "", "", fmt.Errorf("%w: %s: %w", bundle.ErrInvalidBundle, path, err)
	}
	return format, path, nil
}

// selectManifest picks the root plugin.json when present, and the Claude
// manifest only when it is absent. kagent refuses a manifest directory.
func selectManifest(b *bundle.CanonicalBundle) (string, v1alpha1.PluginFormat, error) {
	if _, ok := b.Files[agentPluginsManifest]; ok {
		return agentPluginsManifest, v1alpha1.PluginFormatAgentPlugins, nil
	}
	if b.HasDir(agentPluginsManifest) {
		return "", "", manifestDirectoryError(agentPluginsManifest)
	}
	if _, ok := b.Files[bundle.ManifestPath]; ok {
		return bundle.ManifestPath, v1alpha1.PluginFormatClaudePlugin, nil
	}
	if b.HasDir(bundle.ManifestPath) {
		return "", "", manifestDirectoryError(bundle.ManifestPath)
	}
	return "", "", fmt.Errorf("%w: no %s or %s manifest", bundle.ErrInvalidBundle, agentPluginsManifest, bundle.ManifestPath)
}

func manifestDirectoryError(path string) error {
	return fmt.Errorf("%w: %s is a directory", bundle.ErrInvalidBundle, path)
}

// checkManifest applies the schema and name rules to the selected manifest.
func checkManifest(raw []byte, format v1alpha1.PluginFormat) error {
	m, err := decodeManifest(raw)
	if err != nil {
		return err
	}
	if format == v1alpha1.PluginFormatAgentPlugins && m.Schema != agentPluginsSchema {
		return fmt.Errorf("unsupported plugin schema %q", m.Schema)
	}
	if !validName(m.Name) {
		return fmt.Errorf("invalid plugin name %q", m.Name)
	}
	return nil
}

// decodeManifest keeps the known keys, then decodes them strictly, so a kept
// key with the wrong JSON type fails as it does in kagent.
func decodeManifest(raw []byte) (manifest, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return manifest{}, err
	}
	for name := range fields {
		if !manifestKeys[name] {
			delete(fields, name)
		}
	}
	if ext := fields["extensions"]; len(ext) > 0 && ext[0] != '{' {
		delete(fields, "extensions")
	}
	kept, err := json.Marshal(fields)
	if err != nil {
		return manifest{}, err
	}
	var m manifest
	decoder := json.NewDecoder(bytes.NewReader(kept))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return manifest{}, err
	}
	return m, nil
}

// validName reports whether name is lowercase letters, digits, dots, and
// dashes, at most 64 characters, with no "--" or "..".
func validName(name string) bool {
	return len(name) <= maxNameLength && namePattern.MatchString(name) &&
		!strings.Contains(name, "--") && !strings.Contains(name, "..")
}
