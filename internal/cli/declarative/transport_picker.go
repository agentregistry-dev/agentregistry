package declarative

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// errTransportPickCancelled is returned when the user aborts the transport
// picker (Esc / Ctrl+C). Distinct from a normal selection.
var errTransportPickCancelled = errors.New("transport selection cancelled")

// transportRow describes one selectable transport option.
type transportRow struct {
	value string // "http" or "stdio" — what goes into spec.source.package.transport.type
	label string // human-readable line shown in the TUI
}

// runTransportPicker prompts the user to choose an MCP transport. Returns
// the chosen transport string ("http" or "stdio"), or
// errTransportPickCancelled if the user pressed esc/ctrl+c.
func runTransportPicker() (string, error) {
	m := transportPickerModel{
		rows: []transportRow{
			{"http", "http — Streamable HTTP (listens on a port; default 3000)"},
			{"stdio", "stdio — stdin/stdout (parent process spawns the binary)"},
		},
		cursor: 0,
	}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return "", fmt.Errorf("transport picker: %w", err)
	}
	res := final.(transportPickerModel)
	if res.cancelled {
		return "", errTransportPickCancelled
	}
	return res.rows[res.cursor].value, nil
}

type transportPickerModel struct {
	rows      []transportRow
	cursor    int
	cancelled bool
	done      bool
}

func (m transportPickerModel) Init() tea.Cmd { return nil }

func (m transportPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	transHeaderStyle = lipgloss.NewStyle().Bold(true)
	transCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	transDimStyle    = lipgloss.NewStyle().Faint(true)
)

func (m transportPickerModel) View() string {
	if m.done || m.cancelled {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n\n", transHeaderStyle.Render("?"), transHeaderStyle.Render("Pick a transport:"))
	for i, row := range m.rows {
		if i == m.cursor {
			fmt.Fprintf(&b, "%s %s\n", transCursorStyle.Render("❯"), transCursorStyle.Render(row.label))
		} else {
			fmt.Fprintf(&b, "  %s\n", row.label)
		}
	}
	fmt.Fprintf(&b, "\n%s\n", transDimStyle.Render("↑/↓ to move • enter to select • esc to cancel"))
	return b.String()
}
