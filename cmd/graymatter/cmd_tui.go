package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	graymatter "github.com/angelnicolasc/graymatter"
	"github.com/angelnicolasc/graymatter/pkg/memory"
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#5F5FFF")).
			Padding(0, 1)

	styleBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#5F5FFF"))

	styleSelected = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#5F5FFF"))

	styleDim = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#777777"))

	styleHelp = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#444444")).
			Padding(0, 1)
)

// ── List item types ───────────────────────────────────────────────────────────

type agentItem struct{ id string; count int }

func (a agentItem) Title() string       { return a.id }
func (a agentItem) Description() string { return fmt.Sprintf("%d facts", a.count) }
func (a agentItem) FilterValue() string { return a.id }

type factItem struct{ fact memory.Fact }

func (f factItem) Title() string {
	preview := f.fact.Text
	if len(preview) > 72 {
		preview = preview[:69] + "..."
	}
	return preview
}
func (f factItem) Description() string {
	return fmt.Sprintf("weight: %.3f · %s", f.fact.Weight, f.fact.CreatedAt.Format("2006-01-02"))
}
func (f factItem) FilterValue() string { return f.fact.Text }

// ── TUI model ─────────────────────────────────────────────────────────────────

type pane int

const (
	paneAgents pane = iota
	paneFacts
	paneDetail
)

type tuiModel struct {
	store     *memory.Store
	agentList list.Model
	factList  list.Model
	detail    viewport.Model
	active    pane
	width     int
	height    int
	err       error
	status    string
}

type agentsLoadedMsg struct{ agents []agentItem }
type factsLoadedMsg struct{ facts []factItem }
type errMsg struct{ err error }

func (m tuiModel) Init() tea.Cmd {
	return m.loadAgents()
}

func (m tuiModel) loadAgents() tea.Cmd {
	return func() tea.Msg {
		agents, err := m.store.ListAgents()
		if err != nil {
			return errMsg{err}
		}
		items := make([]agentItem, 0, len(agents))
		for _, a := range agents {
			st, _ := m.store.Stats(a)
			items = append(items, agentItem{id: a, count: st.FactCount})
		}
		return agentsLoadedMsg{items}
	}
}

func (m tuiModel) loadFacts(agentID string) tea.Cmd {
	return func() tea.Msg {
		facts, err := m.store.List(agentID)
		if err != nil {
			return errMsg{err}
		}
		items := make([]factItem, len(facts))
		for i, f := range facts {
			items[i] = factItem{f}
		}
		return factsLoadedMsg{items}
	}
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab", "right", "l":
			if m.active < paneDetail {
				m.active++
			}
		case "shift+tab", "left", "h":
			if m.active > paneAgents {
				m.active--
			}
		case "enter":
			if m.active == paneAgents {
				if sel, ok := m.agentList.SelectedItem().(agentItem); ok {
					m.active = paneFacts
					return m, m.loadFacts(sel.id)
				}
			} else if m.active == paneFacts {
				m.active = paneDetail
				if sel, ok := m.factList.SelectedItem().(factItem); ok {
					m.detail.SetContent(formatFactDetail(sel.fact))
				}
			}
		case "d":
			if m.active == paneFacts {
				if sel, ok := m.factList.SelectedItem().(factItem); ok {
					if agSel, ok2 := m.agentList.SelectedItem().(agentItem); ok2 {
						_ = m.store.Delete(agSel.id, sel.fact.ID)
						m.status = fmt.Sprintf("Deleted fact %s", sel.fact.ID[:8])
						return m, m.loadFacts(agSel.id)
					}
				}
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateSizes()

	case agentsLoadedMsg:
		items := make([]list.Item, len(msg.agents))
		for i, a := range msg.agents {
			items[i] = a
		}
		m.agentList.SetItems(items)

	case factsLoadedMsg:
		items := make([]list.Item, len(msg.facts))
		for i, f := range msg.facts {
			items[i] = f
		}
		m.factList.SetItems(items)

	case errMsg:
		m.err = msg.err
	}

	// Route key events to active pane.
	switch m.active {
	case paneAgents:
		var cmd tea.Cmd
		m.agentList, cmd = m.agentList.Update(msg)
		cmds = append(cmds, cmd)
	case paneFacts:
		var cmd tea.Cmd
		m.factList, cmd = m.factList.Update(msg)
		cmds = append(cmds, cmd)
	case paneDetail:
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m tuiModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err)
	}

	colW := m.width / 3

	// Agent pane.
	agentBorder := styleBorder
	if m.active == paneAgents {
		agentBorder = agentBorder.BorderForeground(lipgloss.Color("#FF875F"))
	}
	agentPane := agentBorder.Width(colW - 2).Height(m.height - 4).Render(m.agentList.View())

	// Fact pane.
	factBorder := styleBorder
	if m.active == paneFacts {
		factBorder = factBorder.BorderForeground(lipgloss.Color("#FF875F"))
	}
	factPane := factBorder.Width(colW - 2).Height(m.height - 4).Render(m.factList.View())

	// Detail pane.
	detailBorder := styleBorder
	if m.active == paneDetail {
		detailBorder = detailBorder.BorderForeground(lipgloss.Color("#FF875F"))
	}
	detailPane := detailBorder.Width(m.width - colW*2 - 6).Height(m.height - 4).Render(m.detail.View())

	body := lipgloss.JoinHorizontal(lipgloss.Top, agentPane, factPane, detailPane)

	title := styleTitle.Render(" GrayMatter ")
	help := styleHelp.Render("tab/←→: panes  j/k: navigate  enter: select  d: delete  q: quit")
	status := ""
	if m.status != "" {
		status = styleDim.Render("  " + m.status)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Center, title, status),
		body,
		help,
	)
}

func (m *tuiModel) updateSizes() {
	colW := m.width / 3
	listH := m.height - 6
	m.agentList.SetSize(colW-4, listH)
	m.factList.SetSize(colW-4, listH)
	m.detail.Width = m.width - colW*2 - 8
	m.detail.Height = listH
}

func formatFactDetail(f memory.Fact) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ID:      %s\n", f.ID))
	sb.WriteString(fmt.Sprintf("Agent:   %s\n", f.AgentID))
	sb.WriteString(fmt.Sprintf("Created: %s\n", f.CreatedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("Weight:  %.4f\n", f.Weight))
	sb.WriteString(fmt.Sprintf("Access:  %d times\n", f.AccessCount))
	sb.WriteString("\n─── Text ─────────────────────────────────\n\n")
	sb.WriteString(f.Text)
	sb.WriteString("\n")
	return sb.String()
}

// ── Command wiring ─────────────────────────────────────────────────────────────

func tuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Browse and manage memories in a terminal UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := graymatter.DefaultConfig()
			cfg.DataDir = dataDir

			mem, err := graymatter.NewWithConfig(cfg)
			if err != nil {
				return err
			}
			defer mem.Close()

			store := mem.Store()
			if store == nil {
				return fmt.Errorf("store not initialised")
			}

			agentDelegate := list.NewDefaultDelegate()
			factDelegate := list.NewDefaultDelegate()

			agentList := list.New(nil, agentDelegate, 30, 20)
			agentList.Title = "Agents"
			agentList.SetShowStatusBar(false)
			agentList.SetFilteringEnabled(true)

			factList := list.New(nil, factDelegate, 40, 20)
			factList.Title = "Facts"
			factList.SetShowStatusBar(false)
			factList.SetFilteringEnabled(true)

			vp := viewport.New(40, 20)

			m := tuiModel{
				store:     store,
				agentList: agentList,
				factList:  factList,
				detail:    vp,
			}

			p := tea.NewProgram(m, tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	}
}
