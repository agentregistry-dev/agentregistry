package plugins

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

// RenderArgs runs Go text/template substitution on each arg independently.
// Missing values cause an error.
func RenderArgs(args []string, vars map[string]string) ([]string, error) {
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
func ExecCapture(cmd Command, workDir string, vars map[string]string) (string, error) {
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
func ExecForeground(cmd Command, workDir string, vars map[string]string, extraEnv []string) error {
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

func resolveArgv(cmd Command, vars map[string]string) ([]string, error) {
	if cmd.Script != "" {
		path := cmd.Script
		if !filepath.IsAbs(path) {
			// Script paths are relative to the plugin's SourceDir, which the
			// caller must include as PluginDir in vars.
			pluginDir, ok := vars["PluginDir"]
			if !ok {
				return nil, fmt.Errorf("plugin script %q resolution requires PluginDir var", path)
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

func envFromVars(vars map[string]string) []string {
	out := make([]string, 0, len(vars))
	for k, v := range vars {
		out = append(out, fmt.Sprintf("ARCTL_VAR_%s=%s", k, v))
	}
	return out
}
