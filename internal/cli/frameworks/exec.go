package frameworks

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

// renderPathComponents substitutes Go template syntax in each path component
// (split on os.PathSeparator). Components without `{{` are passed through
// untouched. Empty results after substitution are an error.
func renderPathComponents(rel string, vars map[string]any) (string, error) {
	parts := strings.Split(rel, string(os.PathSeparator))
	for i, part := range parts {
		if !strings.Contains(part, "{{") {
			continue
		}
		t, err := template.New("path").Option("missingkey=error").Parse(part)
		if err != nil {
			return "", fmt.Errorf("parse path component %q: %w", part, err)
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, vars); err != nil {
			return "", fmt.Errorf("substitute path component %q: %w", part, err)
		}
		out := buf.String()
		if out == "" {
			return "", fmt.Errorf("path component %q rendered empty", part)
		}
		parts[i] = out
	}
	return filepath.Join(parts...), nil
}

// RenderArgs runs Go text/template substitution on each arg independently.
// Missing values cause an error. Args that render to an empty string are
// dropped so framework commands can use `{{if .X}}--flag={{.X}}{{end}}` to
// emit-or-skip optional flags without breaking the argv.
func RenderArgs(args []string, vars map[string]any) ([]string, error) {
	out := make([]string, 0, len(args))
	for _, raw := range args {
		t, err := template.New("arg").Option("missingkey=error").Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse arg %q: %w", raw, err)
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, vars); err != nil {
			return nil, fmt.Errorf("substitute arg %q: %w", raw, err)
		}
		if buf.Len() == 0 {
			continue
		}
		out = append(out, buf.String())
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
		path, err := resolveScriptPath(cmd.Script, vars)
		if err != nil {
			return nil, err
		}
		return []string{path}, nil
	}
	if len(cmd.Command) == 0 {
		return nil, fmt.Errorf("framework command is empty")
	}
	return RenderArgs(cmd.Command, vars)
}

// resolveScriptPath returns an absolute path for a framework script reference.
// Relative paths are resolved against the FrameworkDir var supplied by the caller.
func resolveScriptPath(script string, vars map[string]any) (string, error) {
	if filepath.IsAbs(script) {
		return script, nil
	}
	raw, ok := vars["FrameworkDir"]
	if !ok {
		return "", fmt.Errorf("framework script %q resolution requires FrameworkDir var", script)
	}
	frameworkDir, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("framework script %q: FrameworkDir var must be string", script)
	}
	return filepath.Join(frameworkDir, script), nil
}

func envFromVars(vars map[string]any) []string {
	out := make([]string, 0, len(vars))
	for k, v := range vars {
		out = append(out, fmt.Sprintf("ARCTL_VAR_%s=%v", k, v))
	}
	return out
}

// RenderTemplates walks the framework's templates directory and writes each file
// to dst. Files ending in `.tmpl` get text/template substitution applied AND
// the `.tmpl` extension stripped on output. Other files are copied verbatim.
func RenderTemplates(p *Framework, dst string, vars map[string]any) error {
	if p.TemplatesDir == "" {
		return fmt.Errorf("framework %q: templatesDir not set", p.Name)
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
		// Substitute Go template syntax in each path component so directories
		// like `{{.Name}}/` resolve to e.g. `myagent/`. Standard cookiecutter
		// pattern; no special placeholder syntax — just `{{ }}` in the name.
		relRendered, err := renderPathComponents(rel, vars)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, relRendered), 0755)
		}

		outPath := filepath.Join(dst, relRendered)
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
