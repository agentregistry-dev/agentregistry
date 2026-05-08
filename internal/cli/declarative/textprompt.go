package declarative

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// errTooManyAttempts is returned by the bufio fallback after 3 invalid
// attempts (caller piped input that loops on validation).
var errTooManyAttempts = errors.New("too many invalid attempts; aborting")

// errTextPromptCancelled is returned when the user presses esc/ctrl+c in
// the TUI text prompt.
var errTextPromptCancelled = errors.New("text input cancelled")

// validator checks user input. Return nil for ok, or an error whose message
// is shown back to the user before the re-prompt.
type validator func(s string) error

// promptText asks the user for a single-line value. When run in a TTY, uses
// a bubbletea text input with the default pre-filled and editable inline.
// When stdin is not a TTY (tests, pipes), falls back to bufio with
// "(default):" parenthetical, EOF accepts the default, three failed
// validations abort.
//
// out / in are only used by the bufio fallback path; the TUI path manages
// its own terminal IO via /dev/tty.
func promptText(label, defaultValue string, validate validator, out io.Writer, in io.Reader) (string, error) {
	if isatty() {
		v, err := promptTextTUI(label, defaultValue, validate)
		// If the TUI couldn't open /dev/tty (sandboxed test envs), fall
		// through to the bufio path. Don't swallow user-cancel.
		if err == nil || errors.Is(err, errTextPromptCancelled) {
			return v, err
		}
	}
	return promptTextFallback(label, defaultValue, validate, out, in)
}

// promptTextFallback is the non-TTY path: bufio + parenthetical default.
func promptTextFallback(label, defaultValue string, validate validator, out io.Writer, in io.Reader) (string, error) {
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

// promptTextTUI runs a bubbletea text input with the default pre-filled.
// User edits inline, presses Enter to accept, esc/ctrl+c to cancel.
// Validation errors stay in-place; user keeps editing until valid.
func promptTextTUI(label, defaultValue string, validate validator) (string, error) {
	m := newTextinputModel(label, defaultValue, validate)
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return "", fmt.Errorf("text prompt: %w", err)
	}
	res := final.(textinputModel)
	if res.cancelled {
		return "", errTextPromptCancelled
	}
	return strings.TrimSpace(res.input.Value()), nil
}

type textinputModel struct {
	input     textinput.Model
	label     string
	validate  validator
	err       string
	done      bool
	cancelled bool
}

func newTextinputModel(label, defaultValue string, v validator) textinputModel {
	ti := textinput.New()
	ti.SetValue(defaultValue)
	ti.CursorEnd()
	ti.Focus()
	ti.CharLimit = 512
	ti.Width = 50
	return textinputModel{input: ti, label: label, validate: v}
}

func (m textinputModel) Init() tea.Cmd { return textinput.Blink }

func (m textinputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			value := strings.TrimSpace(m.input.Value())
			if m.validate != nil {
				if err := m.validate(value); err != nil {
					m.err = err.Error()
					return m, nil
				}
			}
			m.done = true
			return m, tea.Quit
		}
	}
	// Any other key updates the textinput; clear stale error on edit.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.err = ""
	return m, cmd
}

var (
	tiHeaderStyle = lipgloss.NewStyle().Bold(true)
	tiErrorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	tiDimStyle    = lipgloss.NewStyle().Faint(true)
)

func (m textinputModel) View() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n%s\n",
		tiHeaderStyle.Render("?"),
		tiHeaderStyle.Render(m.label+":"),
		m.input.View())
	if m.err != "" {
		fmt.Fprintf(&b, "%s\n", tiErrorStyle.Render("✗ "+m.err))
	}
	fmt.Fprintf(&b, "%s\n", tiDimStyle.Render("enter to accept • esc to cancel"))
	return b.String()
}
