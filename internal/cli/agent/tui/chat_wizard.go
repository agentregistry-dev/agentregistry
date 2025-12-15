package tui

import (
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/tui/theme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type chatWizardStep int

const (
	stepGatewayURL chatWizardStep = iota
	stepSelectAgent
	stepSelectVersion
)

// ChatWizard provides a wizard for chatting with deployed agents.
type ChatWizard struct {
	id     string
	width  int
	height int

	step   chatWizardStep
	result ChatResult
	ok     bool
	errMsg string

	// UI components
	gatewayURLInput textinput.Model
	agentList       list.Model
	versionList     list.Model

	// State
	apiClient         *client.Client
	selectedAgentName string
	selectedVersion   string
	gatewayURL        string
}

type ChatResult struct {
	AgentName  string
	Version    string
	GatewayURL string
}

// Async message types for fetching data
type fetchDeployedAgentsMsg struct {
	agents []client.DeploymentResponse
	err    error
}

type fetchAgentVersionsMsg struct {
	agentName string
	versions  []string
	err       error
}

// NewChatWizard creates a new chat wizard instance.
func NewChatWizard(apiClient *client.Client) *ChatWizard {
	if apiClient == nil {
		return nil
	}

	// Gateway URL input
	gatewayInput := textinput.New()
	gatewayInput.Placeholder = "http://localhost:21212" // default gateway URL
	gatewayInput.Width = 50

	// Agent list
	agentList := list.New([]list.Item{}, choiceDelegate{}, 50, 12)
	agentList.Title = "Select deployed agent"
	agentList.SetShowStatusBar(false)
	agentList.SetFilteringEnabled(true)
	agentList.Styles.Title = lipgloss.NewStyle().Bold(true)
	agentList.Styles.PaginationStyle = list.DefaultStyles().PaginationStyle.PaddingLeft(2)

	// Version list
	versionList := list.New([]list.Item{}, choiceDelegate{}, 50, 12)
	versionList.Title = "Select version"
	versionList.SetShowStatusBar(false)
	versionList.SetFilteringEnabled(false)
	versionList.Styles.Title = lipgloss.NewStyle().Bold(true)
	versionList.Styles.PaginationStyle = list.DefaultStyles().PaginationStyle.PaddingLeft(2)

	w := &ChatWizard{
		id:              "chat_wizard",
		apiClient:       apiClient,
		step:            stepGatewayURL,
		gatewayURLInput: gatewayInput,
		agentList:       agentList,
		versionList:     versionList,
		gatewayURL:      "http://localhost:21212", // default gateway URL
	}

	// Set default value for gateway URL and focus it
	w.gatewayURLInput.SetValue(w.gatewayURL)
	w.gatewayURLInput.Focus()

	return w
}

func (w *ChatWizard) ID() string         { return w.id }
func (w *ChatWizard) Fullscreen() bool   { return true }
func (w *ChatWizard) Ok() bool           { return w.ok }
func (w *ChatWizard) Result() ChatResult { return w.result }

func (w *ChatWizard) Init() tea.Cmd {
	return nil
}

// Update handles Bubble Tea messages and routes to the current step's components.
func (w *ChatWizard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		w.width, w.height = m.Width, m.Height
		// Pass sizing into active list
		switch w.step {
		case stepSelectAgent:
			w.agentList.SetSize(maxInt(50, m.Width-20), maxInt(12, m.Height-10))
		case stepSelectVersion:
			w.versionList.SetSize(maxInt(50, m.Width-20), maxInt(12, m.Height-10))
		}
		return w, nil
	case fetchDeployedAgentsMsg:
		if m.err != nil {
			w.errMsg = fmt.Sprintf("Failed to fetch deployed agents: %v", m.err)
			return w, nil
		}

		// Create list items from deployed agents
		items := make([]list.Item, len(m.agents))
		for i, agent := range m.agents {
			items[i] = choiceItem{agent.ServerName}
		}
		w.agentList.SetItems(items)
		w.step = stepSelectAgent
		return w, nil
	case fetchAgentVersionsMsg:
		if m.err != nil {
			w.errMsg = fmt.Sprintf("Failed to fetch versions for agent %s: %v", m.agentName, m.err)
			return w, nil
		}

		// Create list items from versions
		items := make([]list.Item, len(m.versions))
		for i, version := range m.versions {
			items[i] = choiceItem{version}
		}
		w.versionList.SetItems(items)
		w.step = stepSelectVersion
		return w, nil
	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			if w.step == stepGatewayURL {
				return w, tea.Quit
			}
			w.errMsg = ""
			w.prevStep()
			return w, nil
		case "q", "ctrl+c":
			return w, tea.Quit
		case "enter":
			return w, w.onEnter()
		}
	}

	// Delegate updates to current step
	switch w.step {
	case stepGatewayURL:
		var cmd tea.Cmd
		w.gatewayURLInput, cmd = w.gatewayURLInput.Update(msg)
		return w, cmd
	case stepSelectAgent:
		var cmd tea.Cmd
		w.agentList, cmd = w.agentList.Update(msg)
		return w, cmd
	case stepSelectVersion:
		var cmd tea.Cmd
		w.versionList, cmd = w.versionList.Update(msg)
		return w, cmd
	}

	return w, nil
}

// fetchDeployedAgents performs the async operation to fetch deployed agents
func (w *ChatWizard) fetchDeployedAgents() tea.Cmd {
	return func() tea.Msg {
		deployments, err := w.apiClient.GetDeployedServers()
		if err != nil {
			return fetchDeployedAgentsMsg{
				agents: nil,
				err:    err,
			}
		}

		// Filter for agents only
		var agents []client.DeploymentResponse
		for _, dep := range deployments {
			if dep.ResourceType == "agent" {
				agents = append(agents, *dep)
			}
		}

		return fetchDeployedAgentsMsg{
			agents: agents,
			err:    nil,
		}
	}
}

// fetchAgentVersions performs the async operation to fetch versions for a specific agent
func (w *ChatWizard) fetchAgentVersions(agentName string) tea.Cmd {
	return func() tea.Msg {
		deployments, err := w.apiClient.GetDeployedServers()
		if err != nil {
			return fetchAgentVersionsMsg{
				agentName: agentName,
				versions:  nil,
				err:       err,
			}
		}

		// Collect unique versions for this agent
		versionMap := make(map[string]bool)
		for _, dep := range deployments {
			if dep.ServerName == agentName && dep.ResourceType == "agent" {
				versionMap[dep.Version] = true
			}
		}

		var versions []string
		for version := range versionMap {
			versions = append(versions, version)
		}

		return fetchAgentVersionsMsg{
			agentName: agentName,
			versions:  versions,
			err:       nil,
		}
	}
}

// onEnter handles the Enter key by delegating to a step-specific handler.
func (w *ChatWizard) onEnter() tea.Cmd {
	w.errMsg = ""
	switch w.step {
	case stepGatewayURL:
		return w.enterGatewayURL()
	case stepSelectAgent:
		return w.enterSelectAgent()
	case stepSelectVersion:
		return w.enterSelectVersion()
	}
	return nil
}

// enterGatewayURL validates and stores the gateway URL, then fetches deployed agents.
func (w *ChatWizard) enterGatewayURL() tea.Cmd {
	url := strings.TrimSpace(w.gatewayURLInput.Value())
	if url == "" {
		url = "http://localhost:21212" // use default
	}

	// Basic URL validation
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		w.errMsg = "Gateway URL must start with http:// or https://"
		return nil
	}

	w.gatewayURL = url
	return w.fetchDeployedAgents()
}

// enterSelectAgent processes the selected agent and fetches its versions.
func (w *ChatWizard) enterSelectAgent() tea.Cmd {
	if it, ok := w.agentList.SelectedItem().(choiceItem); ok {
		// Extract agent name from the display text (handle "Title (Name)" format)
		displayText := it.Title()
		agentName := displayText
		if strings.Contains(displayText, " (") && strings.HasSuffix(displayText, ")") {
			// Extract name from "Title (Name)" format
			start := strings.LastIndex(displayText, " (")
			end := len(displayText) - 1
			if start >= 0 && end > start {
				agentName = displayText[start+2 : end]
			}
		}
		w.selectedAgentName = agentName
		return w.fetchAgentVersions(agentName)
	}
	return nil
}

// enterSelectVersion processes the selected version and starts the chat.
func (w *ChatWizard) enterSelectVersion() tea.Cmd {
	if it, ok := w.versionList.SelectedItem().(choiceItem); ok {
		w.selectedVersion = it.Title()
		w.result = ChatResult{
			AgentName:  w.selectedAgentName,
			Version:    w.selectedVersion,
			GatewayURL: w.gatewayURL,
		}
		w.ok = true
		return tea.Quit
	}
	return nil
}

// View renders the current step of the wizard.
func (w *ChatWizard) View() string {
	header := w.renderHeader()
	body := ""
	switch w.step {
	case stepGatewayURL:
		body = w.labeled("Gateway URL", w.gatewayURLInput.View()) + w.errorView()
	case stepSelectAgent:
		body = w.agentList.View() + w.errorView()
	case stepSelectVersion:
		body = w.versionList.View() + w.errorView()
	}

	// Fixed content area height so header stays at same line and help at bottom
	contentTarget := maxInt(12, w.height-10) // target content height inside the box
	headerLines := lineCount(header)
	bodyTarget := maxInt(3, contentTarget-headerLines)
	bodyPadded := lipgloss.NewStyle().Height(bodyTarget).Render(body)

	inner := lipgloss.JoinVertical(lipgloss.Left, header, bodyPadded)

	// Calculate box width: aim for 80% of screen width with reasonable min/max bounds
	boxWidth := maxInt(60, (w.width*8)/10)
	if boxWidth > w.width-10 {
		boxWidth = w.width - 10
	}

	box := lipgloss.NewStyle().
		Width(boxWidth).
		Height(contentTarget).
		Padding(1, 2).
		Render(inner)
	return lipgloss.Place(w.width, w.height, lipgloss.Center, lipgloss.Center, box)
}

// prevStep moves the wizard back by one logical step based on current state.
func (w *ChatWizard) prevStep() {
	switch w.step {
	case stepGatewayURL:
		// Can't go back from first step
	case stepSelectAgent:
		w.step = stepGatewayURL
	case stepSelectVersion:
		w.step = stepSelectAgent
	}
}

// renderHeader shows the current step progress.
func (w *ChatWizard) renderHeader() string {
	stepNum := 1
	totalSteps := 3

	switch w.step {
	case stepGatewayURL:
		stepNum = 1
	case stepSelectAgent:
		stepNum = 2
	case stepSelectVersion:
		stepNum = 3
	}

	title := fmt.Sprintf("Chat with Agent  —  Step %d/%d", stepNum, totalSteps)
	return theme.HeadingStyle().Render(title)
}

// errorView shows error messages.
func (w *ChatWizard) errorView() string {
	if strings.TrimSpace(w.errMsg) == "" {
		return ""
	}
	return theme.ErrorStyle().Render("\nError: " + w.errMsg)
}

// labeled creates a labeled input display.
func (w *ChatWizard) labeled(label, view string) string {
	return lipgloss.JoinHorizontal(lipgloss.Left, theme.StatusStyle().Render(label+": "), view)
}
