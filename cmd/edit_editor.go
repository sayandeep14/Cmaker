package cmd

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// lineEditorModel is a small nano-like multi-line text editor: plain
// cursor movement/insertion/deletion over a []string buffer, syntax
// highlighted via chroma - re-highlighted every keystroke (see
// highlightLine) for every line except the one the cursor is currently
// on, which renders in plain text with a visible reverse-video cursor
// block instead. Splitting it that way sidesteps the much harder problem
// of positioning a movable cursor inside chroma's ANSI-escaped output
// (which isn't safe to index by character offset): every line that isn't
// being actively edited gets full, accurate highlighting, and the one
// line that is briefly loses it while the cursor sits on it.
type lineEditorModel struct {
	lines             []string
	cursorRow         int
	cursorCol         int
	filename          string
	highlightsEnabled bool
	lexer             chroma.Lexer
	style             *chroma.Style
	formatter         chroma.Formatter

	width, height int
	scrollTop     int
	ready         bool

	confirmed bool
	cancelled bool
}

func newLineEditorModel(content, filename string) lineEditorModel {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}

	m := lineEditorModel{lines: lines, filename: filename}
	if flagNoColor {
		return m
	}

	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Get("cpp")
	}
	if lexer == nil {
		return m
	}
	style := styles.Get(highlightStyle)
	if style == nil {
		style = styles.Fallback
	}
	formatter := formatters.Get("terminal16m")
	if formatter == nil {
		return m
	}
	m.lexer = chroma.Coalesce(lexer)
	m.style = style
	m.formatter = formatter
	m.highlightsEnabled = true
	return m
}

// highlightLine syntax-highlights a single line - tokenized independently
// of the rest of the buffer, so multi-line constructs (block comments,
// raw strings spanning lines) may render less accurately than
// cmd/highlight.go's whole-file highlightCode, a deliberate tradeoff for
// letting each line re-highlight cheaply on every keystroke. Falls back
// to the line unchanged on any failure - editing must never be blocked by
// a highlighting bug.
func (m lineEditorModel) highlightLine(line string) string {
	if !m.highlightsEnabled {
		return line
	}
	iterator, err := m.lexer.Tokenise(nil, line+"\n")
	if err != nil {
		return line
	}
	var buf bytes.Buffer
	if err := m.formatter.Format(&buf, m.style, iterator); err != nil {
		return line
	}
	return strings.TrimRight(buf.String(), "\n")
}

func (m lineEditorModel) Init() tea.Cmd {
	return nil
}

func (m lineEditorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m lineEditorModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		m.cancelled = true
		return m, tea.Quit
	case tea.KeyCtrlS:
		m.confirmed = true
		return m, tea.Quit
	case tea.KeyRunes:
		m.insertRunes(msg.Runes)
	case tea.KeySpace:
		m.insertRunes([]rune{' '})
	case tea.KeyTab:
		m.insertRunes([]rune{' ', ' ', ' ', ' '})
	case tea.KeyEnter:
		m.splitLine()
	case tea.KeyBackspace:
		m.backspace()
	case tea.KeyDelete:
		m.deleteForward()
	case tea.KeyLeft:
		m.moveLeft()
	case tea.KeyRight:
		m.moveRight()
	case tea.KeyUp:
		m.moveVertical(-1)
	case tea.KeyDown:
		m.moveVertical(1)
	case tea.KeyPgUp:
		m.moveVertical(-m.viewportHeight())
	case tea.KeyPgDown:
		m.moveVertical(m.viewportHeight())
	case tea.KeyHome:
		m.cursorCol = 0
	case tea.KeyEnd:
		m.cursorCol = len([]rune(m.lines[m.cursorRow]))
	}
	m.scrollToCursor()
	return m, nil
}

func (m *lineEditorModel) insertRunes(rs []rune) {
	line := []rune(m.lines[m.cursorRow])
	col := clampCol(m.cursorCol, len(line))
	next := make([]rune, 0, len(line)+len(rs))
	next = append(next, line[:col]...)
	next = append(next, rs...)
	next = append(next, line[col:]...)
	m.lines[m.cursorRow] = string(next)
	m.cursorCol = col + len(rs)
}

func (m *lineEditorModel) splitLine() {
	line := []rune(m.lines[m.cursorRow])
	col := clampCol(m.cursorCol, len(line))
	before := string(line[:col])
	after := string(line[col:])

	m.lines = append(m.lines, "")
	copy(m.lines[m.cursorRow+2:], m.lines[m.cursorRow+1:])
	m.lines[m.cursorRow] = before
	m.lines[m.cursorRow+1] = after
	m.cursorRow++
	m.cursorCol = 0
}

func (m *lineEditorModel) backspace() {
	if m.cursorCol > 0 {
		line := []rune(m.lines[m.cursorRow])
		col := clampCol(m.cursorCol, len(line))
		m.lines[m.cursorRow] = string(append(line[:col-1], line[col:]...))
		m.cursorCol = col - 1
		return
	}
	if m.cursorRow == 0 {
		return
	}
	prevLen := len([]rune(m.lines[m.cursorRow-1]))
	m.lines[m.cursorRow-1] += m.lines[m.cursorRow]
	m.lines = append(m.lines[:m.cursorRow], m.lines[m.cursorRow+1:]...)
	m.cursorRow--
	m.cursorCol = prevLen
}

func (m *lineEditorModel) deleteForward() {
	line := []rune(m.lines[m.cursorRow])
	col := clampCol(m.cursorCol, len(line))
	if col < len(line) {
		m.lines[m.cursorRow] = string(append(line[:col], line[col+1:]...))
		return
	}
	if m.cursorRow >= len(m.lines)-1 {
		return
	}
	m.lines[m.cursorRow] += m.lines[m.cursorRow+1]
	m.lines = append(m.lines[:m.cursorRow+1], m.lines[m.cursorRow+2:]...)
}

func (m *lineEditorModel) moveLeft() {
	if m.cursorCol > 0 {
		m.cursorCol--
		return
	}
	if m.cursorRow > 0 {
		m.cursorRow--
		m.cursorCol = len([]rune(m.lines[m.cursorRow]))
	}
}

func (m *lineEditorModel) moveRight() {
	lineLen := len([]rune(m.lines[m.cursorRow]))
	if m.cursorCol < lineLen {
		m.cursorCol++
		return
	}
	if m.cursorRow < len(m.lines)-1 {
		m.cursorRow++
		m.cursorCol = 0
	}
}

func (m *lineEditorModel) moveVertical(delta int) {
	m.cursorRow += delta
	if m.cursorRow < 0 {
		m.cursorRow = 0
	}
	if m.cursorRow > len(m.lines)-1 {
		m.cursorRow = len(m.lines) - 1
	}
	m.cursorCol = clampCol(m.cursorCol, len([]rune(m.lines[m.cursorRow])))
}

func clampCol(col, lineLen int) int {
	if col < 0 {
		return 0
	}
	if col > lineLen {
		return lineLen
	}
	return col
}

// viewportHeight is how many buffer lines are visible at once - the
// terminal height minus the header and footer rows this View() always
// renders.
func (m lineEditorModel) viewportHeight() int {
	h := m.height - 4
	if h < 1 {
		h = 20 // sensible default before the first WindowSizeMsg arrives
	}
	return h
}

func (m *lineEditorModel) scrollToCursor() {
	vh := m.viewportHeight()
	if m.cursorRow < m.scrollTop {
		m.scrollTop = m.cursorRow
	}
	if m.cursorRow >= m.scrollTop+vh {
		m.scrollTop = m.cursorRow - vh + 1
	}
}

var cursorBlockStyle = lipgloss.NewStyle().Reverse(true)
var gutterStyle = lipgloss.NewStyle().Faint(true)

func (m lineEditorModel) View() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Editing %s - line %d/%d - ^S save & exit, Esc cancel\n\n", m.filename, m.cursorRow+1, len(m.lines))

	vh := m.viewportHeight()
	end := min(m.scrollTop+vh, len(m.lines))
	gutterWidth := len(fmt.Sprintf("%d", len(m.lines)))

	for i := m.scrollTop; i < end; i++ {
		fmt.Fprintf(&b, "%s ", gutterStyle.Render(fmt.Sprintf("%*d", gutterWidth, i+1)))
		if i == m.cursorRow {
			b.WriteString(m.renderCursorLine())
		} else {
			b.WriteString(m.highlightLine(m.lines[i]))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderCursorLine renders the line the cursor is currently on in plain
// text, with a single reverse-video character marking the cursor's exact
// position - see lineEditorModel's own doc for why this line skips
// highlighting rather than the others.
func (m lineEditorModel) renderCursorLine() string {
	line := []rune(m.lines[m.cursorRow])
	col := clampCol(m.cursorCol, len(line))
	before := string(line[:col])
	after := ""
	cursorChar := " "
	if col < len(line) {
		cursorChar = string(line[col])
		after = string(line[col+1:])
	}
	return before + cursorBlockStyle.Render(cursorChar) + after
}

// runLineEditor runs the interactive editor pre-filled with content,
// returning the (possibly edited) final text and whether the user saved
// (Ctrl+S) rather than cancelled (Esc/Ctrl+C). filename is used only to
// guess the syntax-highlighting language, matching cmd/highlight.go.
func runLineEditor(content, filename string) (edited string, ok bool, err error) {
	finalModel, err := tea.NewProgram(newLineEditorModel(content, filename), tea.WithAltScreen()).Run()
	if err != nil {
		return "", false, fmt.Errorf("editor failed: %w", err)
	}
	m, isModel := finalModel.(lineEditorModel)
	if !isModel || !m.confirmed || m.cancelled {
		return "", false, nil
	}
	return strings.Join(m.lines, "\n"), true, nil
}
