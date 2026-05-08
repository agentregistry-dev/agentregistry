package declarative

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// errTypePickCancelled is returned when the user aborts the picker.
var errTypePickCancelled = errors.New("init type selection cancelled")

type typeRow struct {
	subcommand  string // matches the cobra subcommand name (agent, mcp, skill, prompt)
	display     string // shown to the user
	description string
}

// initTypeRows lists the four kinds the user can scaffold via `arctl init`.
var initTypeRows = []typeRow{
	{"agent", "Agent", "Conversational agent backed by an LLM"},
	{"mcp", "MCPServer", "Model Context Protocol server (provides tools to agents)"},
	{"skill", "Skill", "Anthropic-format skill module (content for agents to load)"},
	{"prompt", "Prompt", "Reusable system prompt definition"},
}

// runInitTypePicker presents the four kinds and returns the cobra subcommand
// name (e.g. "agent") matching the user's pick.
func runInitTypePicker() (string, error) {
	final, err := tea.NewProgram(initTypeModel{cursor: 0}).Run()
	if err != nil {
		return "", fmt.Errorf("picker: %w", err)
	}
	res := final.(initTypeModel)
	if res.cancelled {
		return "", errTypePickCancelled
	}
	return initTypeRows[res.cursor].subcommand, nil
}

type initTypeModel struct {
	cursor    int
	cancelled bool
	done      bool
}

func (m initTypeModel) Init() tea.Cmd { return nil }

func (m initTypeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.cursor < len(initTypeRows)-1 {
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
	tpHeaderStyle = lipgloss.NewStyle().Bold(true)
	tpCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	tpDimStyle    = lipgloss.NewStyle().Faint(true)
)

func (m initTypeModel) View() string {
	if m.done || m.cancelled {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n\n", tpHeaderStyle.Render("?"), tpHeaderStyle.Render("What kind of resource?"))
	for i, row := range initTypeRows {
		label := fmt.Sprintf("%s — %s", row.display, row.description)
		if i == m.cursor {
			fmt.Fprintf(&b, "%s %s\n", tpCursorStyle.Render("❯"), tpCursorStyle.Render(label))
		} else {
			fmt.Fprintf(&b, "  %s\n", label)
		}
	}
	fmt.Fprintf(&b, "\n%s\n", tpDimStyle.Render("↑/↓ to move • enter to select • esc to cancel"))
	return b.String()
}
