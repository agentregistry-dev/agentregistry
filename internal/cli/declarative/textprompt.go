package declarative

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// errTooManyAttempts is returned by promptText after 3 invalid attempts.
var errTooManyAttempts = errors.New("too many invalid attempts; aborting")

// validator checks user input. Return nil for ok, or an error whose message
// is shown back to the user before the re-prompt.
type validator func(s string) error

// promptText prints `label (default):` to out, reads a line from in, and
// returns either the typed value or the default if the user pressed Enter.
//
// If validate is non-nil, the returned value is checked. Validation failure
// is shown to the user and the prompt repeats up to 3 times before erroring
// out with errTooManyAttempts. Validation is also applied to the default if
// the user accepts it (catches misconfigured callers).
//
// EOF (e.g. piped empty stdin) returns the default value and proceeds.
func promptText(label, defaultValue string, validate validator, out io.Writer, in io.Reader) (string, error) {
	r := bufio.NewReader(in)
	for attempt := 0; attempt < 3; attempt++ {
		if defaultValue != "" {
			fmt.Fprintf(out, "? %s (%s): ", label, defaultValue)
		} else {
			fmt.Fprintf(out, "? %s: ", label)
		}
		line, err := r.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("read input: %w", err)
		}
		value := strings.TrimSpace(line)
		if value == "" {
			value = defaultValue
		}
		if validate != nil {
			if verr := validate(value); verr != nil {
				fmt.Fprintf(out, "✗ %s\n", verr.Error())
				continue
			}
		}
		return value, nil
	}
	return "", errTooManyAttempts
}
