// Package tui implements cmaker's interactive dashboard (bubbletea).
package tui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cmaker/internal/config"
	"cmaker/internal/templates"
)

type viewState int

const (
	viewMenu viewState = iota
	viewNewForm
	viewRunning
	viewDone
)

type toolStatus int

const (
	toolUnknown toolStatus = iota
	toolChecking
	toolReady
	toolMissing
)

type menuItem struct {
	label       string
	description string
	args        []string // subcommand + flags to re-exec, nil for special items
	special     string   // "new" | "quit", empty for normal exec items
}

var menuItems = []menuItem{
	{label: "New Project", description: "Scaffold a project from a template", special: "new"},
	{label: "Build", description: "cmake configure + build (Debug)", args: []string{"build", "--quiet"}},
	{label: "Build (Release)", description: "cmake configure + build with -O3", args: []string{"build", "--release", "--quiet"}},
	{label: "Run", description: "Build if needed, then run the executable", args: []string{"run", "--quiet"}},
	{label: "Test", description: "Build, then run the project's ctest suite (needs 'testing: enabled' in cmaker.yaml)", args: []string{"test", "--quiet"}},
	{label: "Clean", description: "Remove and recreate build/", args: []string{"clean", "--quiet"}},
	{label: "Doctor", description: "Check the local toolchain", args: []string{"doctor"}},
	{label: "Watch", description: "Auto rebuild + rerun on file changes (esc to stop)", args: []string{"watch", "--quiet"}},
	{label: "Quit", description: "Exit cmaker", special: "quit"},
}

// items returns the sidebar menu: the static built-in commands plus any
// project-scoped named configs (see 'cmaker add config') found in the
// current project's cmaker.yaml, inserted before "Quit" - so configs saved
// on the CLI automatically show up here too, with no separate TUI-side
// storage to keep in sync.
func (m *model) items() []menuItem {
	configs := config.TryLoadConfigs(m.projectDir)
	if len(configs) == 0 {
		return menuItems
	}

	names := make([]string, 0, len(configs))
	for n := range configs {
		names = append(names, n)
	}
	sort.Strings(names)

	items := make([]menuItem, 0, len(menuItems)+len(names))
	items = append(items, menuItems[:len(menuItems)-1]...) // everything but "Quit"
	for _, n := range names {
		cmdLine := configs[n]
		items = append(items, menuItem{
			label:       n,
			description: "Saved config: cmaker " + cmdLine,
			args:        append(strings.Fields(cmdLine), "--quiet"),
		})
	}
	items = append(items, menuItems[len(menuItems)-1]) // "Quit"
	return items
}

type model struct {
	program *tea.Program

	width, height int
	state         viewState
	cursor        int

	// project context
	projectDir  string // "" or "." means "current directory"
	hasConfig   bool
	doctorState toolStatus

	// running/done state
	activeLabel string
	activeArgs  []string
	log         strings.Builder
	viewport    viewport.Model
	spin        spinner.Model
	running     bool
	lastErr     error
	cancel      context.CancelFunc

	// new-project form state
	formStep    int // 0 = name input, 1 = template picker
	nameInput   textinput.Model
	templates   []templates.Meta
	templateSel int

	// pendingProjectDir is set right before a "new" command runs so that, on
	// success, subsequent commands are run inside the freshly scaffolded
	// project directory instead of the TUI's original working directory.
	pendingProjectDir string
}

func newModel() *model {
	ti := textinput.New()
	ti.Placeholder = "MyProject"
	ti.Focus()
	ti.CharLimit = 64
	ti.Width = 40

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = styleSpinnerLabel

	vp := viewport.New(80, 15)

	projectDir := "."
	hasConfig := false
	if _, err := os.Stat("cmaker.yaml"); err == nil {
		hasConfig = true
	}

	tmpls, err := templates.List()
	if err != nil || len(tmpls) == 0 {
		tmpls = []templates.Meta{{Name: "default", Description: "Minimal hello-world C++ executable"}}
	}

	return &model{
		state:       viewMenu,
		projectDir:  projectDir,
		hasConfig:   hasConfig,
		doctorState: toolChecking,
		viewport:    vp,
		spin:        sp,
		nameInput:   ti,
		templates:   tmpls,
	}
}

// RunTUI launches the interactive dashboard. Called when cmaker is invoked
// with no subcommand on an interactive terminal, or via `cmaker tui`.
func RunTUI() error {
	m := newModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	m.program = p

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui exited with error: %w", err)
	}
	return nil
}

func (m *model) Init() tea.Cmd {
	ctx := context.Background()
	noop := func(tea.Msg) {} // discard streamed output from the background probe
	doneWrap := func(err error) tea.Msg { return doctorCheckDoneMsg{err: err} }
	return tea.Batch(
		m.spin.Tick,
		runSubcommand(ctx, m.projectDir, []string{"doctor"}, noop, doneWrap),
	)
}

// doctorCheckDoneMsg is the completion message for the background,
// non-interactive toolchain probe run at startup - distinct from cmdDoneMsg
// so it updates only the status-bar dot instead of switching views.
type doctorCheckDoneMsg struct {
	err error
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizeViewport()
		return m, nil

	case doctorCheckDoneMsg:
		if msg.err == nil {
			m.doctorState = toolReady
		} else {
			m.doctorState = toolMissing
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case logChunkMsg:
		m.log.WriteString(string(msg))
		m.viewport.SetContent(m.log.String())
		m.viewport.GotoBottom()
		return m, nil

	case cmdDoneMsg:
		m.running = false
		m.lastErr = msg.err
		m.state = viewDone
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		if msg.err == nil && m.pendingProjectDir != "" {
			m.projectDir = m.pendingProjectDir
			m.hasConfig = true
		}
		m.pendingProjectDir = ""
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *model) resizeViewport() {
	sidebarWidth := 32
	m.viewport.Width = max(m.width-sidebarWidth-6, 20)
	m.viewport.Height = max(m.height-8, 5)
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	}

	switch m.state {
	case viewMenu:
		return m.handleMenuKey(msg)
	case viewNewForm:
		return m.handleFormKey(msg)
	case viewRunning:
		if msg.String() == "esc" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, nil
		}
	case viewDone:
		if msg.String() == "enter" || msg.String() == "esc" {
			m.state = viewMenu
			return m, nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) handleMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items())-1 {
			m.cursor++
		}
	case "enter":
		item := m.items()[m.cursor]
		switch item.special {
		case "quit":
			return m, tea.Quit
		case "new":
			m.state = viewNewForm
			m.formStep = 0
			m.nameInput.SetValue("")
			m.nameInput.Focus()
			m.templateSel = 0
			return m, textinput.Blink
		default:
			return m.startCommand(item.label, item.args)
		}
	}
	return m, nil
}

func (m *model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.state = viewMenu
		return m, nil
	}

	switch m.formStep {
	case 0: // name input
		switch msg.String() {
		case "enter":
			if strings.TrimSpace(m.nameInput.Value()) == "" {
				return m, nil
			}
			m.formStep = 1
			m.nameInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.nameInput, cmd = m.nameInput.Update(msg)
		return m, cmd

	case 1: // template picker
		switch msg.String() {
		case "up", "k":
			if m.templateSel > 0 {
				m.templateSel--
			}
		case "down", "j":
			if m.templateSel < len(m.templates)-1 {
				m.templateSel++
			}
		case "backspace":
			m.formStep = 0
			m.nameInput.Focus()
			return m, textinput.Blink
		case "enter":
			name := strings.TrimSpace(m.nameInput.Value())
			tmpl := m.templates[m.templateSel].Name
			args := []string{"new", name, "--template", tmpl, "--quiet"}
			m.pendingProjectDir = name
			return m.startCommand("New Project: "+name, args)
		}
	}
	return m, nil
}

func (m *model) startCommand(label string, args []string) (tea.Model, tea.Cmd) {
	m.state = viewRunning
	m.activeLabel = label
	m.activeArgs = args
	m.log.Reset()
	m.viewport.SetContent("")
	m.running = true
	m.lastErr = nil

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	dir := m.projectDir
	send := func(msg tea.Msg) {
		if m.program != nil {
			m.program.Send(msg)
		}
	}
	doneWrap := func(err error) tea.Msg { return cmdDoneMsg{err: err} }
	return m, tea.Batch(m.spin.Tick, runSubcommand(ctx, dir, args, send, doneWrap))
}

func (m *model) View() string {
	switch m.state {
	case viewNewForm:
		return m.renderForm()
	case viewRunning, viewDone:
		return m.renderRun()
	default:
		return m.renderMenu()
	}
}

func (m *model) renderHeader() string {
	title := styleTitle.Render("cmaker")
	tagline := styleTagline.Render("C++/CMake project scaffolder")
	return lipgloss.JoinHorizontal(lipgloss.Bottom, title, "  ", tagline)
}

func (m *model) renderStatusBar() string {
	dot := styleDotWait.Render("●")
	label := "checking toolchain..."
	switch m.doctorState {
	case toolReady:
		dot = styleDotGood.Render("●")
		label = "toolchain ready"
	case toolMissing:
		dot = styleDotBad.Render("●")
		label = "toolchain incomplete (see Doctor)"
	}

	cfgLabel := "no cmaker.yaml here"
	if m.hasConfig {
		cfgLabel = "cmaker.yaml found"
	}

	return styleStatusBar.Render(fmt.Sprintf("%s %s  ·  dir: %s  ·  %s", dot, label, m.projectDir, cfgLabel))
}

func (m *model) renderMenu() string {
	items := m.items()
	var sb strings.Builder
	for i, item := range items {
		if i == m.cursor {
			sb.WriteString(styleMenuItemSelected.Render("❯ "+item.label) + "\n")
		} else {
			sb.WriteString(styleMenuItem.Render("  "+item.label) + "\n")
		}
	}
	sidebar := styleSidebar.Width(28).Render(sb.String())

	desc := items[m.cursor].description
	main := styleMainPane.Width(m.mainWidth()).Height(m.height - 8).Render(
		lipgloss.JoinVertical(lipgloss.Top,
			styleFormLabel.Render(items[m.cursor].label),
			"",
			desc,
		),
	)

	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", main)
	hint := styleHint.Render("↑/↓ navigate   enter select   q quit")

	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		m.renderStatusBar(),
		"",
		body,
		"",
		hint,
	)
}

func (m *model) mainWidth() int {
	return max(m.width-28-6, 20)
}

func (m *model) renderForm() string {
	var body string
	switch m.formStep {
	case 0:
		body = lipgloss.JoinVertical(lipgloss.Left,
			styleFormLabel.Render("Project name"),
			"",
			m.nameInput.View(),
			"",
			styleHint.Render("enter continue   esc cancel"),
		)
	case 1:
		var sb strings.Builder
		sb.WriteString(styleFormLabel.Render("Template") + "\n\n")
		for i, t := range m.templates {
			line := t.Name + " — " + t.Description
			if i == m.templateSel {
				sb.WriteString(styleMenuItemSelected.Render("❯ "+line) + "\n")
			} else {
				sb.WriteString(styleMenuItem.Render("  "+line) + "\n")
			}
		}
		sb.WriteString("\n" + styleHint.Render("↑/↓ choose   enter create   backspace back   esc cancel"))
		body = sb.String()
	}

	main := styleMainPane.Width(m.mainWidth() + 30).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		"",
		main,
	)
}

func (m *model) renderRun() string {
	var status string
	switch {
	case m.running:
		status = m.spin.View() + " " + styleSpinnerLabel.Render(m.activeLabel+"...")
	case m.lastErr == nil:
		status = styleSuccess.Render("✔ " + m.activeLabel + " succeeded")
	default:
		status = styleFailure.Render("✘ "+m.activeLabel+" failed") + "  " + styleHint.Render("("+m.lastErr.Error()+")")
	}

	hint := "esc stop"
	if m.state == viewDone {
		hint = "enter/esc back to menu   ↑/↓ scroll log"
	}

	logBox := styleMainPane.Width(m.viewport.Width + 2).Render(m.viewport.View())

	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeader(),
		"",
		status,
		"",
		logBox,
		"",
		styleHint.Render(hint),
	)
}
