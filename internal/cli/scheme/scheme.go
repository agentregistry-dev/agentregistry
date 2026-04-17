package scheme

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

const APIVersion = "ar.dev/v1alpha1"

// Resource is the universal declarative envelope for all registry resources.
type Resource struct {
	APIVersion string         `yaml:"apiVersion"`
	Kind       string         `yaml:"kind"`
	Metadata   Metadata       `yaml:"metadata"`
	Spec       map[string]any `yaml:"spec"`
}

// Metadata holds the resource identity and server-generated timestamps.
type Metadata struct {
	Name        string     `yaml:"name"`
	Version     string     `yaml:"version,omitempty"`
	PublishedAt *time.Time `yaml:"publishedAt,omitempty"`
	UpdatedAt   *time.Time `yaml:"updatedAt,omitempty"`
}

// DecodeBytes parses one or more YAML documents from b.
// Each document must have apiVersion, kind, and metadata.name.
// Returns an error if any document is invalid or if no documents are found.
func DecodeBytes(b []byte) ([]*Resource, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	var resources []*Resource

	for {
		var r Resource
		if err := dec.Decode(&r); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("parsing YAML: %w", err)
		}

		// Skip entirely empty documents (e.g. trailing ---)
		if r.Kind == "" && r.APIVersion == "" && r.Metadata.Name == "" {
			continue
		}

		if r.Kind == "" {
			return nil, fmt.Errorf("resource missing required field 'kind'")
		}
		if r.APIVersion == "" {
			return nil, fmt.Errorf("resource %q missing required field 'apiVersion'", r.Kind)
		}
		if r.APIVersion != APIVersion {
			return nil, fmt.Errorf("resource %s/%s: unsupported apiVersion %q; expected %q",
				r.Kind, r.Metadata.Name, r.APIVersion, APIVersion)
		}
		if r.Metadata.Name == "" {
			return nil, fmt.Errorf("resource %q missing required field 'metadata.name'", r.Kind)
		}

		resources = append(resources, &r)
	}

	if len(resources) == 0 {
		return nil, fmt.Errorf("no resources found in input")
	}

	return resources, nil
}

// DecodeFile reads a file at path and parses its YAML documents.
func DecodeFile(path string) ([]*Resource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return DecodeBytes(data)
}
