package cmd

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// commitPreviewModel is a minimal bubbletea model wrapping a single
// textarea, pre-filled with a proposed commit message - 'cmaker commit
// --preview's inline, in-terminal edit step. Deliberately not run with
// tea.WithAltScreen() (unlike internal/tui's full dashboard): this is a
// quick edit prompt, not a takeover of the whole terminal, so it renders
// inline in the normal scrollback the same way every other cmaker prompt
// does.
type commitPreviewModel struct {
	ta        textarea.Model
	confirmed bool
	cancelled bool
}

func newCommitPreviewModel(initial string) commitPreviewModel {
	ta := textarea.New()
	ta.SetValue(initial)
	ta.Focus()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.CharLimit = 0
	ta.SetWidth(88)
	ta.SetHeight(10)
	return commitPreviewModel{ta: ta}
}

func (m commitPreviewModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m commitPreviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyCtrlS:
			m.confirmed = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

func (m commitPreviewModel) View() string {
	return "Edit the commit message below - Ctrl+S to commit, Esc to cancel:\n\n" + m.ta.View() + "\n"
}

// previewAndEditMessage runs the inline editor pre-filled with message,
// returning the (possibly edited) final text and whether the user
// confirmed (Ctrl+S) rather than cancelled (Esc/Ctrl+C).
func previewAndEditMessage(message string) (edited string, ok bool, err error) {
	finalModel, err := tea.NewProgram(newCommitPreviewModel(message)).Run()
	if err != nil {
		return "", false, fmt.Errorf("preview editor failed: %w", err)
	}
	m, ok := finalModel.(commitPreviewModel)
	if !ok || !m.confirmed || m.cancelled {
		return "", false, nil
	}
	return m.ta.Value(), true, nil
}
