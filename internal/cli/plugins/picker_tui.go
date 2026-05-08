package plugins

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// errPickCancelled is returned when the user aborts the picker (Esc / Ctrl+C).
var errPickCancelled = errors.New("plugin selection cancelled")

// runPickerTUI presents candidates as a single selectable list and returns
// the chosen plugin. The picker is intentionally always shown, even with
// one candidate, for consistency and discoverability (per design).
func runPickerTUI(candidates []*Plugin) (*Plugin, error) {
	sorted := append([]*Plugin(nil), candidates...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Framework != sorted[j].Framework {
			return sorted[i].Framework < sorted[j].Framework
		}
		return sorted[i].Language < sorted[j].Language
	})

	m := pickerModel{
		items:  sorted,
		cursor: 0,
	}
	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("picker: %w", err)
	}
	res := final.(pickerModel)
	if res.cancelled {
		return nil, errPickCancelled
	}
	return res.items[res.cursor], nil
}

type pickerModel struct {
	items     []*Plugin
	cursor    int
	cancelled bool
	done      bool
}

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.cursor < len(m.items)-1 {
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
	pickerHeaderStyle = lipgloss.NewStyle().Bold(true)
	pickerCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	pickerDimStyle    = lipgloss.NewStyle().Faint(true)
)

func (m pickerModel) View() string {
	if m.done || m.cancelled {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n\n", pickerHeaderStyle.Render("?"), pickerHeaderStyle.Render("Pick a framework:"))
	frameworkCount := map[string]int{}
	for _, p := range m.items {
		frameworkCount[p.Framework]++
	}
	for i, p := range m.items {
		label := p.Framework
		if frameworkCount[p.Framework] > 1 {
			label = fmt.Sprintf("%s (%s)", p.Framework, p.Language)
		}
		if p.Description != "" {
			label = fmt.Sprintf("%s — %s", label, p.Description)
		}
		if i == m.cursor {
			fmt.Fprintf(&b, "%s %s\n", pickerCursorStyle.Render("❯"), pickerCursorStyle.Render(label))
		} else {
			fmt.Fprintf(&b, "  %s\n", label)
		}
	}
	fmt.Fprintf(&b, "\n%s\n", pickerDimStyle.Render("↑/↓ to move • enter to select • esc to cancel"))
	return b.String()
}
