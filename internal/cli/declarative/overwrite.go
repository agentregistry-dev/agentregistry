package declarative

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// errOverwriteDeclined is returned when the user answers no (or EOF) to the
// overwrite prompt. The caller surfaces it as the command's exit error so
// scripts get a non-zero exit code; the message is friendly enough for users.
var errOverwriteDeclined = errors.New("aborted; nothing was changed")

// handleExistingProjectDir checks whether projectDir already exists; if it
// does, prompts y/N and either wipes the directory (yes) or returns
// errOverwriteDeclined (no/EOF/anything else). If the directory doesn't
// exist, returns nil and the caller proceeds normally.
func handleExistingProjectDir(projectDir string, out io.Writer, in io.Reader) error {
	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat project dir: %w", err)
	}
	ok, err := confirmOverwrite(projectDir, out, in)
	if err != nil {
		return err
	}
	if !ok {
		return errOverwriteDeclined
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
	ok, err := confirmOverwrite(path, out, in)
	if err != nil {
		return err
	}
	if !ok {
		return errOverwriteDeclined
	}
	fmt.Fprintf(out, "→ Removing %s...\n", path)
	return os.Remove(path)
}

// confirmOverwrite reads y/n from `in` and returns true on yes, false
// (with errOverwriteDeclined wrapping a friendly message) on no/EOF/anything
// else. The prompt is rendered to `out`; default is no, indicated by capital
// N in `(y/N)`.
func confirmOverwrite(target string, out io.Writer, in io.Reader) (bool, error) {
	fmt.Fprintf(out, "? %q already exists. Overwrite? (y/N): ", target)
	r := bufio.NewReader(in)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "y" || answer == "yes" {
		return true, nil
	}
	return false, errOverwriteDeclined
}
