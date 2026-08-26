package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sigs.k8s.io/yaml"
)

func main() {
	inputDir := flag.String("input-dir", "", "Directory containing generated CRDs")
	outputDir := flag.String("output-dir", "", "Directory for extracted spec schemas")
	flag.Parse()
	if *inputDir == "" || *outputDir == "" {
		panic("input-dir and output-dir are required")
	}
	if err := extract(*inputDir, *outputDir); err != nil {
		panic(err)
	}
}

func extract(inputDir, outputDir string) error {
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(inputDir, entry.Name()))
		if err != nil {
			return err
		}
		var crd map[string]any
		if err := yaml.Unmarshal(data, &crd); err != nil {
			return err
		}
		spec, ok := nestedMap(crd, "spec")
		if !ok {
			return fmt.Errorf("%s: missing spec", entry.Name())
		}
		names, ok := nestedMap(spec, "names")
		if !ok {
			return fmt.Errorf("%s: missing spec.names", entry.Name())
		}
		kind, ok := names["kind"].(string)
		if !ok {
			return fmt.Errorf("%s: missing spec.names.kind", entry.Name())
		}
		versions, ok := spec["versions"].([]any)
		if !ok || len(versions) != 1 {
			return fmt.Errorf("%s: expected one version", entry.Name())
		}
		version, ok := versions[0].(map[string]any)
		if !ok {
			return fmt.Errorf("%s: invalid version", entry.Name())
		}
		schema, ok := nestedMap(version, "schema", "openAPIV3Schema", "properties", "spec")
		if !ok {
			return fmt.Errorf("%s: missing spec schema", entry.Name())
		}
		output, err := yaml.Marshal(schema)
		if err != nil {
			return err
		}
		name := strings.ToLower(kind) + ".yaml"
		if err := os.WriteFile(filepath.Join(outputDir, name), output, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func nestedMap(value map[string]any, keys ...string) (map[string]any, bool) {
	current := value
	for _, key := range keys {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}
