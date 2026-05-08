package declarative

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// errProviderPickCancelled is returned when the user aborts the picker.
var errProviderPickCancelled = errors.New("model provider selection cancelled")

// modelProviderRow lists one selectable provider.
type modelProviderRow struct {
	name        string
	description string
}

// providerRows is the canonical order shown in the picker. Same set as
// providerEnvKeys (see modelenv.go); kept in lockstep manually because the
// description text is presentation-only.
var providerRows = []modelProviderRow{
	{"gemini", "Google Gemini (default)"},
	{"openai", "OpenAI (gpt-*)"},
	{"anthropic", "Anthropic (Claude)"},
	{"bedrock", "AWS Bedrock (set AWS_* in your shell)"},
	{"agentgateway", "Routes through a local agentgateway proxy (default http://host.docker.internal:4000/v1)"},
}

// runModelProviderPicker presents the closed enum of supported providers
// and returns the chosen one. Cancel via esc / ctrl+c / q.
func runModelProviderPicker() (string, error) {
	m := modelProviderModel{cursor: 0}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return "", fmt.Errorf("picker: %w", err)
	}
	res := final.(modelProviderModel)
	if res.cancelled {
		return "", errProviderPickCancelled
	}
	return providerRows[res.cursor].name, nil
}

type modelProviderModel struct {
	cursor    int
	cancelled bool
	done      bool
}

func (m modelProviderModel) Init() tea.Cmd { return nil }

func (m modelProviderModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.cursor < len(providerRows)-1 {
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
	mpHeaderStyle = lipgloss.NewStyle().Bold(true)
	mpCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	mpDimStyle    = lipgloss.NewStyle().Faint(true)
)

func (m modelProviderModel) View() string {
	if m.done || m.cancelled {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n\n", mpHeaderStyle.Render("?"), mpHeaderStyle.Render("Pick a model provider:"))
	for i, row := range providerRows {
		label := fmt.Sprintf("%s — %s", row.name, row.description)
		if i == m.cursor {
			fmt.Fprintf(&b, "%s %s\n", mpCursorStyle.Render("❯"), mpCursorStyle.Render(label))
		} else {
			fmt.Fprintf(&b, "  %s\n", label)
		}
	}
	fmt.Fprintf(&b, "\n%s\n", mpDimStyle.Render("↑/↓ to move • enter to select • esc to cancel"))
	return b.String()
}
