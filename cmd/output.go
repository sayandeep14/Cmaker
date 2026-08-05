package cmd

import (
	"fmt"
	"os"
)

// --- Colored, level-aware output helpers ---
// A stopgap for real UI polish - the TUI owns presentation long-term, but
// headless/CI usage needs sane, scriptable output today.

const (
	ansiReset  = "\x1b[0m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
	ansiBold   = "\x1b[1m"
)

func colorize(code, s string) string {
	if flagNoColor {
		return s
	}
	return code + s + ansiReset
}

// infof prints normal progress output. Suppressed by --quiet.
func infof(format string, a ...any) {
	if flagQuiet {
		return
	}
	fmt.Println(colorize(ansiCyan, "-- "+fmt.Sprintf(format, a...)))
}

// okf prints a success message. Suppressed by --quiet.
func okf(format string, a ...any) {
	if flagQuiet {
		return
	}
	fmt.Println(colorize(ansiGreen, "-- "+fmt.Sprintf(format, a...)))
}

// warnf prints a warning to stderr. Always shown, even with --quiet.
func warnf(format string, a ...any) {
	fmt.Fprintln(os.Stderr, colorize(ansiYellow, "-- Warning: "+fmt.Sprintf(format, a...)))
}

// errorf prints an error to stderr. Always shown, even with --quiet.
func errorf(format string, a ...any) {
	fmt.Fprintln(os.Stderr, colorize(ansiRed, "-- Error: "+fmt.Sprintf(format, a...)))
}

// debugf prints diagnostic output, only when --verbose is set.
func debugf(format string, a ...any) {
	if !flagVerbose {
		return
	}
	fmt.Println(colorize(ansiBold, "-- [debug] "+fmt.Sprintf(format, a...)))
}
