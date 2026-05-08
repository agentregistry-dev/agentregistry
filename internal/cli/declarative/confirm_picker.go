package declarative

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// errConfirmCancelled is returned when the user presses esc/ctrl+c at the
// confirm picker. Distinct from declining (which returns false, nil).
var errConfirmCancelled = errors.New("confirmation cancelled")

// confirmRow describes one selectable answer (No / Yes).
type confirmRow struct {
	label string
}

// runConfirmPicker shows a two-row Yes/No picker with cursor on No by
// default. Returns true on Yes, false on No, errConfirmCancelled if the
// user pressed esc/ctrl+c.
func runConfirmPicker(prompt string) (bool, error) {
	m := confirmPickerModel{
		prompt: prompt,
		rows: []confirmRow{
			{"No, leave it (default)"},
			{"Yes, wipe and recreate"},
		},
		cursor: 0,
	}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return false, fmt.Errorf("confirm picker: %w", err)
	}
	res := final.(confirmPickerModel)
	if res.cancelled {
		return false, errConfirmCancelled
	}
	return res.cursor == 1, nil
}

type confirmPickerModel struct {
	prompt    string
	rows      []confirmRow
	cursor    int
	cancelled bool
	done      bool
}

func (m confirmPickerModel) Init() tea.Cmd { return nil }

func (m confirmPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case "enter":
			m.done = true
			return m, tea.Quit
		}
	}
	return m, nil
}

var (
	cpHeaderStyle = lipgloss.NewStyle().Bold(true)
	cpCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	cpDimStyle    = lipgloss.NewStyle().Faint(true)
)

func (m confirmPickerModel) View() string {
	if m.done || m.cancelled {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n\n", cpHeaderStyle.Render("?"), cpHeaderStyle.Render(m.prompt))
	for i, row := range m.rows {
		if i == m.cursor {
			fmt.Fprintf(&b, "%s %s\n", cpCursorStyle.Render("❯"), cpCursorStyle.Render(row.label))
		} else {
			fmt.Fprintf(&b, "  %s\n", row.label)
		}
	}
	fmt.Fprintf(&b, "\n%s\n", cpDimStyle.Render("↑/↓ to move • enter to select • esc to cancel"))
	return b.String()
}
