package plugins

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

// RenderArgs runs Go text/template substitution on each arg independently.
// Missing values cause an error.
func RenderArgs(args []string, vars map[string]any) ([]string, error) {
	out := make([]string, len(args))
	for i, raw := range args {
		t, err := template.New("arg").Option("missingkey=error").Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse arg %q: %w", raw, err)
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, vars); err != nil {
			return nil, fmt.Errorf("substitute arg %q: %w", raw, err)
		}
		out[i] = buf.String()
	}
	return out, nil
}

// ExecCapture runs a Command (inline command or script) in workDir, with vars
// substituted into argv. Captures combined stdout+stderr and returns it.
//
// Commands run via os/exec with an arg list — never through a shell.
func ExecCapture(cmd Command, workDir string, vars map[string]any) (string, error) {
	argv, err := resolveArgv(cmd, vars)
	if err != nil {
		return "", err
	}
	c := exec.Command(argv[0], argv[1:]...)
	c.Dir = workDir
	c.Env = append(os.Environ(), envFromVars(vars)...)
	out, err := c.CombinedOutput()
	return string(out), err
}

// ExecForeground runs a Command with stdout/stderr forwarded to the current process.
// Used by arctl run / build for live output.
func ExecForeground(cmd Command, workDir string, vars map[string]any, extraEnv []string) error {
	argv, err := resolveArgv(cmd, vars)
	if err != nil {
		return err
	}
	c := exec.Command(argv[0], argv[1:]...)
	c.Dir = workDir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	c.Env = append(os.Environ(), append(envFromVars(vars), extraEnv...)...)
	return c.Run()
}

func resolveArgv(cmd Command, vars map[string]any) ([]string, error) {
	if cmd.Script != "" {
		path := cmd.Script
		if !filepath.IsAbs(path) {
			// Script paths are relative to the plugin's SourceDir, which the
			// caller must include as PluginDir in vars.
			raw, ok := vars["PluginDir"]
			if !ok {
				return nil, fmt.Errorf("plugin script %q resolution requires PluginDir var", path)
			}
			pluginDir, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("plugin script %q: PluginDir var must be string", path)
			}
			path = filepath.Join(pluginDir, path)
		}
		return []string{path}, nil
	}
	if len(cmd.Command) == 0 {
		return nil, fmt.Errorf("plugin command is empty")
	}
	return RenderArgs(cmd.Command, vars)
}

func envFromVars(vars map[string]any) []string {
	out := make([]string, 0, len(vars))
	for k, v := range vars {
		out = append(out, fmt.Sprintf("ARCTL_VAR_%s=%v", k, v))
	}
	return out
}

// RenderTemplates walks the plugin's templates directory and writes each file
// to dst. Files ending in `.tmpl` get text/template substitution applied AND
// the `.tmpl` extension stripped on output. Other files are copied verbatim.
func RenderTemplates(p *Plugin, dst string, vars map[string]any) error {
	if p.TemplatesDir == "" {
		return fmt.Errorf("plugin %q: templatesDir not set", p.Name)
	}
	srcRoot := p.TemplatesDir
	if !filepath.IsAbs(srcRoot) {
		srcRoot = filepath.Join(p.SourceDir, srcRoot)
	}

	return filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0755)
		}

		outPath := filepath.Join(dst, rel)
		isTpl := filepath.Ext(rel) == ".tmpl"
		if isTpl {
			outPath = outPath[:len(outPath)-len(".tmpl")]
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !isTpl {
			return os.WriteFile(outPath, data, 0644)
		}

		t, err := template.New(rel).Option("missingkey=error").Parse(string(data))
		if err != nil {
			return fmt.Errorf("parse template %q: %w", rel, err)
		}
		f, err := os.Create(outPath)
		if err != nil {
			return err
		}
		defer f.Close()
		return t.Execute(f, vars)
	})
}
