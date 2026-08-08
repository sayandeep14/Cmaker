package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"cmaker/internal/templates"
)

// newTestModel builds a minimal model good enough to drive the New Project
// form directly, without a real terminal/pty - handleFormKey and
// newProjectArgs only touch the fields set here.
func newTestModel(compilers []string) *model {
	ti := textinput.New()
	ti.SetValue("myproj")
	return &model{
		state:     viewNewForm,
		nameInput: ti,
		templates: []templates.Meta{
			{Name: "default", Description: "d"},
			{Name: "raylib", Description: "r"},
		},
		compilers: compilers,
	}
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestNewProjectArgsNoCompilerSelected(t *testing.T) {
	m := newTestModel([]string{"/usr/bin/clang++", "/usr/bin/g++"})
	m.templateSel = 0
	m.compilerSel = 0 // "(default)"

	args := m.newProjectArgs()
	if strings.Contains(strings.Join(args, " "), "--compiler") {
		t.Errorf("newProjectArgs() = %v, want no --compiler flag when \"(default)\" is selected", args)
	}
	if m.pendingProjectDir != "myproj" {
		t.Errorf("pendingProjectDir = %q, want %q", m.pendingProjectDir, "myproj")
	}
}

func TestNewProjectArgsWithCompilerSelected(t *testing.T) {
	m := newTestModel([]string{"/usr/bin/clang++", "/usr/bin/g++"})
	m.templateSel = 1 // raylib
	m.compilerSel = 2 // compilers[1] = /usr/bin/g++

	args := m.newProjectArgs()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--compiler /usr/bin/g++") {
		t.Errorf("newProjectArgs() = %v, want --compiler /usr/bin/g++", args)
	}
	if !strings.Contains(joined, "--template raylib") {
		t.Errorf("newProjectArgs() = %v, want --template raylib", args)
	}
}

func TestFormStepSkipsCompilerPickerWithOneOrNoCompilers(t *testing.T) {
	for _, compilers := range [][]string{nil, {"/usr/bin/clang++"}} {
		m := newTestModel(compilers)
		m.formStep = 1
		newM, cmd := m.handleFormKey(keyMsg("enter"))
		got := newM.(*model)
		if got.formStep == 2 {
			t.Errorf("with %d compiler(s), formStep = 2, want to skip the picker entirely", len(compilers))
		}
		if got.state != viewRunning {
			t.Errorf("with %d compiler(s), state = %v, want viewRunning (should start the command directly)", len(compilers), got.state)
		}
		if cmd == nil {
			t.Error("expected a non-nil tea.Cmd to start the new-project command")
		}
	}
}

func TestFormStepShowsCompilerPickerWithMultipleCompilers(t *testing.T) {
	m := newTestModel([]string{"/usr/bin/clang++", "/usr/bin/g++"})
	m.formStep = 1

	newM, _ := m.handleFormKey(keyMsg("enter"))
	got := newM.(*model)
	if got.formStep != 2 {
		t.Errorf("formStep = %d, want 2 (compiler picker) with multiple compilers detected", got.formStep)
	}
	if got.state != viewNewForm {
		t.Errorf("state = %v, want to stay on viewNewForm until a compiler is picked", got.state)
	}
}

func TestCompilerPickerNavigationBounds(t *testing.T) {
	m := newTestModel([]string{"/usr/bin/clang++", "/usr/bin/g++"})
	m.formStep = 2
	m.compilerSel = 0

	// Up from 0 stays at 0.
	newM, _ := m.handleFormKey(keyMsg("up"))
	if got := newM.(*model).compilerSel; got != 0 {
		t.Errorf("compilerSel after up at 0 = %d, want 0", got)
	}

	// Down twice reaches the max index (len(compilers), i.e. 2 for 2 compilers).
	newM, _ = newM.(*model).handleFormKey(keyMsg("down"))
	newM, _ = newM.(*model).handleFormKey(keyMsg("down"))
	if got := newM.(*model).compilerSel; got != 2 {
		t.Errorf("compilerSel after two downs = %d, want 2", got)
	}

	// One more down must not overflow past len(compilers).
	newM, _ = newM.(*model).handleFormKey(keyMsg("down"))
	if got := newM.(*model).compilerSel; got != 2 {
		t.Errorf("compilerSel after overflow down = %d, want clamped at 2", got)
	}
}

func TestCompilerPickerBackspaceReturnsToTemplateStep(t *testing.T) {
	m := newTestModel([]string{"/usr/bin/clang++", "/usr/bin/g++"})
	m.formStep = 2

	newM, _ := m.handleFormKey(keyMsg("backspace"))
	got := newM.(*model)
	if got.formStep != 1 {
		t.Errorf("formStep after backspace = %d, want 1 (template picker)", got.formStep)
	}
	if got.state != viewNewForm {
		t.Errorf("state after backspace = %v, want viewNewForm", got.state)
	}
}

func TestCompilerPickerEnterStartsCommand(t *testing.T) {
	m := newTestModel([]string{"/usr/bin/clang++", "/usr/bin/g++"})
	m.formStep = 2
	m.compilerSel = 1

	newM, cmd := m.handleFormKey(keyMsg("enter"))
	got := newM.(*model)
	if got.state != viewRunning {
		t.Errorf("state = %v, want viewRunning", got.state)
	}
	if cmd == nil {
		t.Error("expected a non-nil tea.Cmd to start the new-project command")
	}
	if !strings.Contains(strings.Join(got.activeArgs, " "), "--compiler /usr/bin/clang++") {
		t.Errorf("activeArgs = %v, want --compiler /usr/bin/clang++", got.activeArgs)
	}
}
