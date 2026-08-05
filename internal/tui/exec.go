package tui

import (
	"context"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// The TUI reuses the exact same headless commands (build/run/clean/doctor/
// watch/new) by re-exec'ing this same binary as a subprocess, rather than
// calling their handler functions in-process. Several of those handlers call
// os.Exit on failure (see cmd/*.go) - fine for a one-shot CLI invocation,
// fatal if called directly from inside the TUI process. Running them as a
// child process contains that blast radius to the child and lets us stream
// its stdout/stderr into the log viewport live.

// logChunkMsg carries a raw chunk of subprocess output to append to the log.
type logChunkMsg string

// cmdDoneMsg is sent once the subprocess exits (successfully, with a
// non-zero exit code, or because it was cancelled).
type cmdDoneMsg struct {
	err error
}

// teaWriter forwards each Write to the bubbletea program as a logChunkMsg,
// which is how output produced in the subprocess-running goroutine gets back
// into the Update loop safely.
type teaWriter struct {
	send func(tea.Msg)
}

func (w *teaWriter) Write(p []byte) (int, error) {
	b := make([]byte, len(p))
	copy(b, p)
	w.send(logChunkMsg(string(b)))
	return len(p), nil
}

// runSubcommand builds a tea.Cmd that re-execs this binary with the given
// subcommand args, in dir (relative to the TUI's own working directory, or
// "" / "." for the current directory), streaming combined output to send.
// Cancelling ctx kills the subprocess - this is how the TUI stops `watch`.
//
// done wraps the subprocess error into the tea.Msg that bubbletea will
// deliver to Update when the returned tea.Cmd's function returns - bubbletea
// dispatches a tea.Cmd's return value as a message on its own, independently
// of anything sent via `send` mid-flight. Callers that don't want the
// generic cmdDoneMsg handling (e.g. a background probe that shouldn't flip
// the UI into the "done" view) must pass a different wrapper type here.
func runSubcommand(ctx context.Context, dir string, args []string, send func(tea.Msg), done func(error) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		exePath, err := os.Executable()
		if err != nil {
			exePath = os.Args[0]
		}

		cmd := exec.CommandContext(ctx, exePath, args...)
		if dir != "" && dir != "." {
			cmd.Dir = dir
		}
		w := &teaWriter{send: send}
		cmd.Stdout = w
		cmd.Stderr = w

		err = cmd.Run()
		return done(err)
	}
}
