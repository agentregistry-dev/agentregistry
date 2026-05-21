package declarative

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"

	inspectorpkg "github.com/agentregistry-dev/agentregistry/internal/cli/declarative/inspector"
	"github.com/agentregistry-dev/agentregistry/internal/cli/frameworks"
)

// stopGracePeriod is how long the non-docker fallback in stopChild waits after
// SIGINT before escalating to SIGKILL. The docker path doesn't use this — it
// removes the container daemon-side, which doesn't depend on signal forwarding.
const stopGracePeriod = 2 * time.Second

// runWithWatch runs the project under fsnotify; on file changes it restarts
// the child process after a short debounce. Ignores .git/, .gitignore, .env.
//
// When dryRun is true the watcher itself, the "Watching for changes…" line,
// and the "Change detected" line still print, but the underlying child
// process is never started. This is what `arctl run --watch --dry-run`
// surfaces to tests.
func runWithWatch(ctx context.Context, out io.Writer, projectDir string, p *frameworks.Framework, image string, port int, env []string, dryRun, inspector bool) error {
	// Per-watch-session container name. The random suffix keeps two terminals
	// running `arctl run --watch` on the same project from stomping each
	// other's containers — `docker rm -f` on restart would otherwise kill the
	// sibling session's container silently. Within one session, every restart
	// reuses this name so `docker rm -f` is well-defined.
	containerName := fmt.Sprintf("arctl-watch-%s-%s", filepath.Base(projectDir), randSuffix(6))

	var current *exec.Cmd
	startCmd := func() error {
		if current != nil {
			stopChild(current, containerName)
		}
		argv, err := frameworks.RenderArgs(p.Run.Command, map[string]any{
			"ProjectDir":   projectDir,
			"FrameworkDir": p.SourceDir,
			"Image":        image,
			"Port":         port,
		})
		if err != nil {
			return err
		}
		argv = injectDockerName(argv, containerName)
		fmt.Fprintf(out, "→ %s: %s\n", p.Name, strings.Join(argv, " "))
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

	// Inspector launches ONCE for the whole watch session. The MCP container
	// restarts repeatedly behind a stable URL (same port, same path), and
	// the Inspector reconnects on its own across restarts — so we don't
	// relaunch per cycle. dryRun narrates without launching.
	if inspector {
		if dryRun {
			fmt.Fprintf(out, "→ would launch MCP Inspector against http://localhost:%d/mcp\n", port)
		} else {
			stop := launchInspector(out, port)
			defer stop()
		}
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

	// Editors typically write a file via several inode operations (atomic
	// rename, chmod, etc.), and macOS fsevents fans these out further. Print
	// "Change detected" only on the first event in a debounce window so a
	// single save doesn't spam the log; reset on restart so the next save
	// still gets a line.
	pending := false

	for {
		select {
		case e := <-w.Events:
			if shouldIgnore(e.Name) {
				continue
			}
			if !pending {
				fmt.Fprintf(out, "→ Change detected: %s\n", filepath.Base(e.Name))
				pending = true
			}
			debounce.Reset(200 * time.Millisecond)
		case <-debounce.C:
			pending = false
			if err := startCmd(); err != nil {
				return err
			}
			fmt.Fprintln(out, "✓ Restarted")
		case err := <-w.Errors:
			return err
		case <-ctx.Done():
			if current != nil {
				stopChild(current, containerName)
			}
			return nil
		}
	}
}

// launchInspector starts the MCP Inspector subprocess pointed at the local
// MCP and returns a cleanup function the caller defers. On launch failure
// (npx missing, etc.) the helper prints a warning and returns a no-op
// cleanup — Inspector is a debug tool and must not gate the dev loop.
func launchInspector(out io.Writer, port int) func() {
	inspectorURL := fmt.Sprintf("http://localhost:%d/mcp", port)
	insCmd, err := inspectorpkg.Launch(context.Background(), inspectorURL)
	if err != nil {
		fmt.Fprintf(out, "Warning: --inspector skipped: %v\n", err)
		fmt.Fprintf(out, "         MCP will still start on %s.\n", inspectorURL)
		return func() {}
	}
	fmt.Fprintf(out, "→ MCP Inspector launching (will connect to %s when ready)\n", inspectorURL)
	return func() {
		if insCmd.Process != nil {
			_ = insCmd.Process.Kill()
			_ = insCmd.Wait()
		}
	}
}

// injectDockerName inserts `--name <name>` after `docker run` so we can
// `docker rm -f <name>` on restart. If argv isn't `docker run …`, returns it
// unchanged — non-docker frameworks fall back to signal-based stop in
// stopChild, which is correct because they don't need the daemon-side
// cleanup.
func injectDockerName(argv []string, name string) []string {
	if len(argv) < 2 || argv[0] != "docker" || argv[1] != "run" {
		return argv
	}
	out := make([]string, 0, len(argv)+2)
	out = append(out, argv[0], argv[1], "--name", name)
	out = append(out, argv[2:]...)
	return out
}

// stopChild stops a running child process. For `docker run` children, signal
// propagation to the daemon-managed container is unreliable (the daemon runs
// its own 10s stop timer on SIGTERM, independent of the CLI process), so we
// rely on the daemon-side `docker rm -f <name>` to actually kill the
// container; the CLI subprocess exits on its own once the container is gone.
// For non-docker children we keep the SIGINT → wait → SIGKILL escalation as
// a generic graceful stop.
func stopChild(cmd *exec.Cmd, containerName string) {
	if cmd.Process == nil {
		return
	}
	isDocker := len(cmd.Args) > 1 && cmd.Args[0] == "docker" && cmd.Args[1] == "run"
	if isDocker && containerName != "" {
		// Blocking on `docker rm -f` is intentional: it returns only after
		// the container is gone AND the userland port mapping is released,
		// so the next `docker run` won't race the daemon's teardown.
		rm := exec.Command("docker", "rm", "-f", containerName)
		_ = rm.Run()
		_ = cmd.Wait()
		return
	}
	_ = cmd.Process.Signal(syscall.SIGINT)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(stopGracePeriod):
		_ = cmd.Process.Kill()
		<-done
	}
}

// randSuffix returns a lowercase hex string of n characters. Used to keep
// concurrent `arctl run --watch` sessions on the same project from sharing
// a container name.
func randSuffix(n int) string {
	b := make([]byte, (n+1)/2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
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
