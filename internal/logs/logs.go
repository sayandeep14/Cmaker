// Package logs implements cmaker's build/run log capture (§24, the
// foundation `cmaker heal` builds on, independent of any AI piece): every
// `cmaker build`/`cmaker run` writes its combined stdout/stderr to a
// rotating store under .cmaker/logs/, so a failure can be read back later
// - by a human via `cmaker logs`, or by `cmaker heal`.
package logs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Dir is where logs live, relative to a project root.
const Dir = ".cmaker/logs"

// DefaultKeep is how many completed logs are kept before older ones are
// pruned, when a project doesn't override it via cmaker.yaml's logs_keep.
const DefaultKeep = 5

// Session captures one `cmaker build`/`cmaker run` invocation's combined
// output into a single rotating log file.
type Session struct {
	file      *os.File
	root      string
	kind      string
	timestamp string
	keep      int
}

// Start begins a new log capture of the given kind ("build" or "run") under
// root/.cmaker/logs. A failure here (e.g. couldn't create the directory) is
// deliberately non-fatal to the caller: logging is a best-effort diagnostic
// aid, not a build-blocking requirement. Callers should still call Tee/
// Finish on the returned *Session even when err != nil - both are safe
// no-ops on a nil *Session, so logging just silently degrades for that one
// invocation instead of requiring an if-err branch at every call site.
func Start(root, kind string, keep int) (*Session, error) {
	dir := filepath.Join(root, Dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", dir, err)
	}

	ts := time.Now().UTC().Format("20060102T150405.000000000Z")
	tmpPath := filepath.Join(dir, fmt.Sprintf("%s-%s.inprogress.log", ts, kind))
	f, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %w", err)
	}

	if keep <= 0 {
		keep = DefaultKeep
	}
	return &Session{file: f, root: root, kind: kind, timestamp: ts, keep: keep}, nil
}

// Tee returns w wrapped so writes also go to the session's log file - or w
// itself, unchanged, if s is nil (Start failed for this invocation).
func (s *Session) Tee(w io.Writer) io.Writer {
	if s == nil {
		return w
	}
	return io.MultiWriter(w, s.file)
}

// Finish closes the log file, renames it to record whether the invocation
// succeeded or failed (the filename itself encodes this - see
// LatestFailure, which relies on it rather than reopening every log to
// check), and prunes old logs beyond keep. Safe to call on a nil *Session.
func (s *Session) Finish(runErr error) {
	if s == nil {
		return
	}
	s.file.Close()

	status := "ok"
	if runErr != nil {
		status = "fail"
	}
	dir := filepath.Join(s.root, Dir)
	tmpPath := filepath.Join(dir, fmt.Sprintf("%s-%s.inprogress.log", s.timestamp, s.kind))
	finalPath := filepath.Join(dir, fmt.Sprintf("%s-%s-%s.log", s.timestamp, s.kind, status))
	os.Rename(tmpPath, finalPath)

	prune(dir, s.keep)
}

// prune removes the oldest completed logs beyond keep. Filenames sort
// chronologically (the timestamp is a fixed-width ISO-8601-ish format), so
// a plain lexicographic sort is enough - no need to parse timestamps back
// out.
func prune(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && isCompletedLog(e.Name()) {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keep {
		return
	}
	sort.Strings(names)
	for _, n := range names[:len(names)-keep] {
		os.Remove(filepath.Join(dir, n))
	}
}

func isCompletedLog(name string) bool {
	return strings.HasSuffix(name, "-ok.log") || strings.HasSuffix(name, "-fail.log")
}

// List returns every completed log filename under root/.cmaker/logs, most
// recent first.
func List(root string) ([]string, error) {
	dir := filepath.Join(root, Dir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && isCompletedLog(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

// LatestFailure returns the path to the most recent failed log, optionally
// restricted to kind ("build"/"run"; "" for either) - what `cmaker heal`
// reads from.
func LatestFailure(root, kind string) (string, error) {
	names, err := List(root)
	if err != nil {
		return "", err
	}
	for _, n := range names {
		if !strings.HasSuffix(n, "-fail.log") {
			continue
		}
		// Filename is <timestamp>-<kind>-<status>.log; the timestamp
		// format itself contains no hyphens, so this substring check
		// unambiguously matches the kind segment.
		if kind == "" || strings.Contains(n, "-"+kind+"-") {
			return filepath.Join(root, Dir, n), nil
		}
	}
	if kind != "" {
		return "", fmt.Errorf("no failing %s logs found (see 'cmaker logs') - nothing for 'cmaker heal' to work from", kind)
	}
	return "", fmt.Errorf("no failing build/run logs found (see 'cmaker logs') - nothing for 'cmaker heal' to work from")
}
