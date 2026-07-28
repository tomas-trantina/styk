package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styly
var (
	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")).
			Bold(true).
			Padding(0, 1)

	activeTabStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color("81")).
			Foreground(lipgloss.Color("81")).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, true, false).
				BorderForeground(lipgloss.Color("240")).
				Foreground(lipgloss.Color("240")).
				Padding(0, 1)

	windowStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(1)

	activeWindowStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("81")).
				Padding(1)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("248")).
			Italic(true)

	diffAddedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("84"))

	diffRemovedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("203"))

	diffHeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("81")).
			Bold(true)
)

type tuiState int

const (
	stateLoading tuiState = iota
	stateProject
	stateGlobal
	stateInput
	stateError
)

type model struct {
	state       tuiState
	prevState   tuiState
	err         error
	width       int
	height      int
	config      Config
	projectName string

	// Komponenty pro projekt
	historyList list.Model
	diffView    viewport.Model

	// Komponenty pro globální pohled
	projectList list.Model

	// Společné komponenty
	textInput   textinput.Model
	inputLabel  string
	inputAction func(string) tea.Cmd

	loading       bool
	spinner       spinner.Model
	activePane    int // 0: vlevo (list), 1: vpravo (view/detaily)
	lastActionMsg string
}

type historyItem struct {
	commit Commit
}

func (i historyItem) Title() string {
	return fmt.Sprintf("v%d: %s", i.commit.Version, i.commit.Message)
}
func (i historyItem) Description() string {
	return fmt.Sprintf("%s • %s", i.commit.Time, i.commit.Author)
}
func (i historyItem) FilterValue() string { return i.commit.Message }

type projectItem struct {
	name string
}

func (i projectItem) Title() string       { return i.name }
func (i projectItem) Description() string { return "Projekt na serveru" }
func (i projectItem) FilterValue() string { return i.name }

func initialModel(cfg Config, projName string) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))

	ti := textinput.New()
	ti.Placeholder = "Zadej text..."
	ti.Focus()

	// Inicializace prázdných modelů, aby SetSize neselhalo na nil pointeru
	hList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	pList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	dv := viewport.New(0, 0)

	return model{
		state:       stateLoading,
		config:      cfg,
		projectName: projName,
		spinner:     s,
		textInput:   ti,
		historyList: hList,
		projectList: pList,
		diffView:    dv,
		loading:     true,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.loadDataCmd(),
	)
}

// Zprávy pro asynchronní operace
type dataLoadedMsg struct {
	history  []Commit
	projects []string
}

type actionSuccessMsg string
type clearActionMsg struct{}
type errMsg struct{ err error }

func (m model) loadDataCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := connectSSH(m.config)
		if err != nil {
			return errMsg{err}
		}
		defer client.Close()

		if m.projectName != "" {
			history, err := getHistory(client, m.config, m.projectName)
			if err != nil {
				return errMsg{err}
			}
			return dataLoadedMsg{history: history}
		} else {
			out, err := runCmdOutput(client, fmt.Sprintf("ls -1 '%s' 2>/dev/null", getActiveProfile(m.config).RemotePath))
			if err != nil {
				return errMsg{err}
			}
			projects := strings.Split(strings.TrimSpace(string(out)), "\n")
			return dataLoadedMsg{projects: projects}
		}
	}
}

type diffLoadedMsg struct {
	content string
}

func colorizeDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	var colored []string
	for _, line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			colored = append(colored, diffAddedStyle.Render(line))
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			colored = append(colored, diffRemovedStyle.Render(line))
		} else if strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			colored = append(colored, diffHeaderStyle.Render(line))
		} else {
			colored = append(colored, line)
		}
	}
	return strings.Join(colored, "\n")
}

func (m model) loadDiffCmd(ver int) tea.Cmd {
	return func() tea.Msg {
		client, err := connectSSH(m.config)
		if err != nil {
			return errMsg{err}
		}
		defer client.Close()

		history, _ := getHistory(client, m.config, m.projectName)
		if ver > 0 && ver <= len(history) {
			c := history[ver-1]
			header := fmt.Sprintf("%sVerze: %sv%d\n%sZpráva: %s%s\n%sAutor: %s%s\n%sČas: %s%s\n%sVelikost: %s%s\n%sTyp: %s%s\n",
				Gray, White+Bold, c.Version,
				Gray, White, c.Message,
				Gray, White, c.Author,
				Gray, White, c.Time,
				Gray, White, formatSizeInt(c.Size),
				Gray, White, c.Type)

			content := header

			if c.Type == "patch" {
				patchZstd, _ := downloadData(client, fmt.Sprintf("%s/%s/versions/v%d.patch.zst", getActiveProfile(m.config).RemotePath, m.projectName, c.Version))
				if patchZstd != nil {
					patchData, _ := decompressZstd(patchZstd)
					if patchData != nil {
						content += "\n" + diffHeaderStyle.Render("--- ROZDÍLY (PATCH) ---") + "\n" + colorizeDiff(string(patchData))
					}
				}
			} else {
				content += "\n" + infoStyle.Render("(Plný snapshot - detaily souborů nejsou v tomto zobrazení zatím dostupné)")
			}
			return diffLoadedMsg{content: content}
		}
		return diffLoadedMsg{content: "Detaily nedostupné"}
	}
}

func (m model) addCmd(msg string) tea.Cmd {
	return func() tea.Msg {
		cmdAddSilent(m.config, msg, false)
		return actionSuccessMsg("Verze uložena: " + msg)
	}
}

func (m model) cloneCmd(name string) tea.Cmd {
	return func() tea.Msg {
		cmdCloneSilent(m.config, name)
		return actionSuccessMsg("Projekt naklonován: " + name)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.state == stateInput {
			switch msg.String() {
			case "enter":
				val := m.textInput.Value()
				if val != "" && m.inputAction != nil {
					cmds = append(cmds, m.inputAction(val))
					m.state = stateLoading
					m.loading = true
				} else {
					m.state = m.prevState
				}
			case "esc":
				m.state = m.prevState
			}
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.activePane = (m.activePane + 1) % 2
		case "r":
			m.state = stateLoading
			m.loading = true
			return m, m.loadDataCmd()
		case "c":
			if m.state == stateGlobal {
				if m.projectList.SelectedItem() != nil {
					item := m.projectList.SelectedItem().(projectItem)
					m.state = stateLoading
					m.loading = true
					return m, m.cloneCmd(item.name)
				}
			}
		case "a":
			if m.state == stateProject {
				m.prevState = m.state
				m.state = stateInput
				m.inputLabel = "Zpráva pro novou verzi:"
				m.textInput.SetValue("")
				m.inputAction = m.addCmd
				return m, textinput.Blink
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()

	case dataLoadedMsg:
		m.loading = false
		if m.projectName != "" {
			m.state = stateProject
			items := make([]list.Item, len(msg.history))
			for i, c := range msg.history {
				items[len(msg.history)-1-i] = historyItem{commit: c}
			}
			m.historyList.SetItems(items)
			m.historyList.Title = "Historie verzí"
			m.historyList.SetShowHelp(false)

			if len(msg.history) > 0 {
				cmds = append(cmds, m.loadDiffCmd(msg.history[len(msg.history)-1].Version))
			}
		} else {
			m.state = stateGlobal
			items := make([]list.Item, 0)
			for _, p := range msg.projects {
				if p == "" {
					continue
				}
				items = append(items, projectItem{name: p})
			}
			m.projectList.SetItems(items)
			m.projectList.Title = "Projekty na serveru"
		}
		m.updateLayout()

	case diffLoadedMsg:
		m.diffView.SetContent(msg.content)
		m.diffView.GotoTop()

	case actionSuccessMsg:
		m.lastActionMsg = string(msg)
		return m, tea.Batch(
			m.loadDataCmd(),
			tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
				return clearActionMsg{}
			}),
		)

	case clearActionMsg:
		m.lastActionMsg = ""
		return m, nil

	case errMsg:
		m.state = stateError
		m.err = msg.err
		m.loading = false

	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	if m.state == stateProject {
		if m.activePane == 0 {
			oldIdx := m.historyList.Index()
			m.historyList, cmd = m.historyList.Update(msg)
			cmds = append(cmds, cmd)
			if m.historyList.Index() != oldIdx && m.historyList.SelectedItem() != nil {
				if item, ok := m.historyList.SelectedItem().(historyItem); ok {
					cmds = append(cmds, m.loadDiffCmd(item.commit.Version))
				}
			}
		} else {
			m.diffView, cmd = m.diffView.Update(msg)
			cmds = append(cmds, cmd)
		}
	} else if m.state == stateGlobal {
		m.projectList, cmd = m.projectList.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) updateLayout() {
	listWidth := m.width / 3
	h := maxInt(0, m.height-12)

	// Komponenty uvnitř oken (odečteme padding pro lepší vzhled)
	m.historyList.SetSize(maxInt(0, listWidth-6), h-2)
	m.projectList.SetSize(maxInt(0, m.width/2-6), h-2)

	m.diffView.Width = maxInt(0, m.width-listWidth-6)
	m.diffView.Height = h - 2
}

func (m model) View() string {
	if m.width < 60 || m.height < 15 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true).Render("⚠️  TERMINÁL JE PŘÍLIŠ MALÝ\nProsím zvětši okno."))
	}

	if m.loading {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			fmt.Sprintf("%s\n\nNačítám data ze serveru...", m.spinner.View()))
	}

	if m.state == stateError {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			fmt.Sprintf("❌ %s\n\nStiskni 'q' pro ukončení.", m.err))
	}

	if m.state == stateInput {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.JoinVertical(lipgloss.Center,
				m.inputLabel,
				"\n",
				m.textInput.View(),
				"\n\n[Enter] Potvrdit • [Esc] Zpět",
			))
	}

	header := headerStyle.Render("🚀 STYK CONTROL CENTER")
	if m.projectName != "" {
		header += " • Projekt: " + lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true).Render(m.projectName)
	} else {
		header += " • " + lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Render("Globální režim")
	}

	tabs := m.renderTabs()

	var content string
	if m.state == stateProject {
		content = m.renderProjectView()
	} else {
		content = m.renderGlobalView()
	}

	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, tabs, content, footer)
}

func (m model) renderFooter() string {
	info := m.lastActionMsg
	if info != "" {
		info = lipgloss.NewStyle().Foreground(lipgloss.Color("84")).Bold(true).Render(" ✔ " + info)
	} else {
		keys := []string{"q: Konec", "tab: Přepnout okno", "a: Add", "c: Clone", "r: Refresh"}
		info = strings.Join(keys, "  •  ")
	}

	style := lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("250")).
		Padding(0, 1).
		Width(m.width)

	return style.Render(info)
}

func (m model) renderTabs() string {
	var tabs []string
	if m.projectName != "" {
		tabs = append(tabs, activeTabStyle.Render("Půdorys projektu"))
		tabs = append(tabs, inactiveTabStyle.Render("Globální přehled"))
	} else {
		tabs = append(tabs, activeTabStyle.Render("Globální přehled"))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

func (m model) renderProjectView() string {
	listStyle := windowStyle
	viewStyle := windowStyle
	if m.activePane == 0 {
		listStyle = activeWindowStyle
	} else {
		viewStyle = activeWindowStyle
	}

	listWidth := m.width / 3
	h := maxInt(0, m.height-12)

	// Odečteme 4 pro okraje a padding (2+2)
	l := listStyle.Width(maxInt(0, listWidth-4)).Height(h).Render(m.historyList.View())
	v := viewStyle.Width(maxInt(0, m.width-listWidth-4)).Height(h).Render(m.diffView.View())
	return lipgloss.JoinHorizontal(lipgloss.Top, l, v)
}

func (m model) renderGlobalView() string {
	listStyle := activeWindowStyle
	h := maxInt(0, m.height-12)

	// Odečteme 4 pro okraje a padding (2+2)
	l := listStyle.Width(maxInt(0, m.width/2-4)).Height(h).Render(m.projectList.View())

	details := windowStyle.Width(maxInt(0, m.width/2-4)).Height(h).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Bold(true).Render("Detaily serveru"),
			"\n",
			fmt.Sprintf("Adresa:   %s", getActiveProfile(m.config).ServerIP),
			fmt.Sprintf("Uživatel: %s", getActiveProfile(m.config).Username),
			fmt.Sprintf("Cesta:    %s", getActiveProfile(m.config).RemotePath),
			"\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Vyber projekt a stiskni 'c' pro klonování."),
		),
	)

	return lipgloss.JoinHorizontal(lipgloss.Top, l, details)
}

func runTUI(cfg Config, projName string) error {
	p := tea.NewProgram(initialModel(cfg, projName), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
