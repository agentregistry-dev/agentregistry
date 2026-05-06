package declarative

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/cli/plugins"
	"github.com/fsnotify/fsnotify"
)

// runWithWatch runs the project under fsnotify; on file changes it restarts
// the child process after a short debounce. Ignores .git/, .gitignore, .env.
//
// When dryRun is true the watcher itself, the "Watching for changes…" line,
// and the "Change detected" line still print, but the underlying child
// process is never started. This is what `arctl run --watch --dry-run`
// surfaces to tests.
func runWithWatch(out io.Writer, projectDir string, p *plugins.Plugin, env []string, dryRun bool) error {
	var current *exec.Cmd
	startCmd := func() error {
		if current != nil {
			_ = current.Process.Kill()
			_ = current.Wait()
		}
		fmt.Fprintf(out, "→ %s: %s\n", p.Name, strings.Join(p.Run.Command, " "))
		argv, err := plugins.RenderArgs(p.Run.Command, map[string]any{
			"ProjectDir": projectDir,
			"PluginDir":  p.SourceDir,
		})
		if err != nil {
			return err
		}
		if dryRun {
			fmt.Fprintln(out, "(dry-run; skipping exec)")
			return nil
		}
		current = exec.Command(argv[0], argv[1:]...)
		current.Dir = projectDir
		current.Env = append(env, "ARCTL_RUN_WATCH=1")
		current.Stdout, current.Stderr = out, out
		return current.Start()
	}

	if err := startCmd(); err != nil {
		return err
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if err := addWatches(w, projectDir); err != nil {
		return err
	}

	fmt.Fprintln(out, "→ Watching for changes (Ctrl+C to stop)...")

	debounce := time.NewTimer(time.Hour)
	debounce.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		select {
		case e := <-w.Events:
			if shouldIgnore(e.Name) {
				continue
			}
			fmt.Fprintf(out, "→ Change detected: %s\n", filepath.Base(e.Name))
			debounce.Reset(200 * time.Millisecond)
		case <-debounce.C:
			if err := startCmd(); err != nil {
				return err
			}
			fmt.Fprintln(out, "✓ Restarted")
		case err := <-w.Errors:
			return err
		case <-ctx.Done():
			return nil
		}
	}
}

// addWatches recursively adds every directory and file under root to the
// watcher, skipping ignored paths (.git, .gitignore, .env).
func addWatches(w *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, _ os.FileInfo, err error) error {
		if err != nil || shouldIgnore(path) {
			return nil
		}
		return w.Add(path)
	})
}

// shouldIgnore reports whether path should be excluded from watch events.
func shouldIgnore(path string) bool {
	if strings.Contains(path, "/.git/") || strings.HasSuffix(path, "/.git") {
		return true
	}
	base := filepath.Base(path)
	if base == ".gitignore" || base == ".env" {
		return true
	}
	return false
}
