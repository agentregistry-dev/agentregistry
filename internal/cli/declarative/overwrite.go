package declarative

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// errOverwriteDeclined is internal-only; the public surfaces print a
// friendly line and return nil so "user said no" exits 0 (it's not an
// error — they actively chose to keep their project).
var errOverwriteDeclined = errors.New("aborted; nothing was changed")

// errOverwriteHandled is a sentinel that means "we already handled this
// (user declined); caller should exit cleanly without further work."
// Cobra commands convert this to nil so the exit code is 0.
var errOverwriteHandled = errors.New("overwrite handled (no-op)")

// handleExistingProjectDir checks whether projectDir already exists; if it
// does, asks for confirmation (TUI Yes/No picker in TTY, y/N text fallback
// otherwise). On yes: wipes the dir. On no: prints a friendly line and
// returns errOverwriteHandled (caller converts to clean exit). On cancel:
// returns the cancel error.
func handleExistingProjectDir(projectDir string, out io.Writer, in io.Reader) error {
	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat project dir: %w", err)
	}
	ok, err := askOverwrite(projectDir, out, in)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintf(out, "✗ Leaving %s alone.\n", projectDir)
		return errOverwriteHandled
	}
	fmt.Fprintf(out, "→ Wiping %s/...\n", projectDir)
	return os.RemoveAll(projectDir)
}

// handleExistingFile is the same check for prompts (which write a single
// <name>.yaml file rather than a project directory).
func handleExistingFile(path string, out io.Writer, in io.Reader) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat output file: %w", err)
	}
	ok, err := askOverwrite(path, out, in)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintf(out, "✗ Leaving %s alone.\n", path)
		return errOverwriteHandled
	}
	fmt.Fprintf(out, "→ Removing %s...\n", path)
	return os.Remove(path)
}

// askOverwrite chooses between the TUI Yes/No picker (when a TTY is
// available) and the y/N text fallback. Returns true=overwrite, false=keep,
// or errConfirmCancelled if the user pressed esc/ctrl+c in the picker.
func askOverwrite(target string, out io.Writer, in io.Reader) (bool, error) {
	if isatty() {
		ok, err := runConfirmPicker(fmt.Sprintf("%q already exists. Overwrite?", target))
		if err == nil || errors.Is(err, errConfirmCancelled) {
			return ok, err
		}
		// /dev/tty unavailable (sandboxed test env) → fall through to bufio.
	}
	return confirmOverwrite(target, out, in)
}

// confirmOverwrite reads y/n from `in`. Returns (true, nil) on yes,
// (false, nil) on anything else (including EOF, empty input, or "n"). The
// caller decides what to do with a "no" answer — typically print a friendly
// line and exit 0.
//
// Used as the non-TTY fallback when the bubbletea picker can't open
// /dev/tty (sandboxed test envs, piped invocations).
func confirmOverwrite(target string, out io.Writer, in io.Reader) (bool, error) {
	fmt.Fprintf(out, "? %q already exists. Overwrite? (y/N, press Enter to keep): ", target)
	r := bufio.NewReader(in)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "y" || answer == "yes" {
		return true, nil
	}
	return false, nil
}
