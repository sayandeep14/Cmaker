package logs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartTeeFinishCapturesOutput(t *testing.T) {
	dir := t.TempDir()

	s, err := Start(dir, "build", 5)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var terminal bytes.Buffer
	w := s.Tee(&terminal)
	w.Write([]byte("hello from a build\n"))

	s.Finish(nil)

	if terminal.String() != "hello from a build\n" {
		t.Errorf("Tee() should still write to the base writer, got %q", terminal.String())
	}

	names, err := List(dir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(names) != 1 || !strings.HasSuffix(names[0], "-ok.log") {
		t.Fatalf("List() = %v, want exactly one *-ok.log", names)
	}

	data, err := os.ReadFile(filepath.Join(dir, Dir, names[0]))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello from a build\n" {
		t.Errorf("log file content = %q, want %q", string(data), "hello from a build\n")
	}
}

func TestFinishMarksFailure(t *testing.T) {
	dir := t.TempDir()
	s, err := Start(dir, "run", 5)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	s.Finish(errors.New("boom"))

	names, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || !strings.HasSuffix(names[0], "-fail.log") {
		t.Fatalf("List() = %v, want exactly one *-fail.log", names)
	}
}

func TestNilSessionIsSafeNoOp(t *testing.T) {
	var s *Session
	var buf bytes.Buffer
	w := s.Tee(&buf)
	if w != &buf {
		t.Error("Tee() on a nil *Session should return the base writer unchanged")
	}
	w.Write([]byte("still works\n"))
	if buf.String() != "still works\n" {
		t.Errorf("got %q", buf.String())
	}
	s.Finish(nil) // must not panic
}

func TestPruneKeepsOnlyMostRecent(t *testing.T) {
	dir := t.TempDir()
	for i := range 8 {
		s, err := Start(dir, "build", 3)
		if err != nil {
			t.Fatal(err)
		}
		s.Finish(nil)
		_ = i
		time.Sleep(2 * time.Millisecond) // ensure distinct, increasing timestamps
	}

	names, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 {
		t.Fatalf("List() after pruning = %d entries, want 3: %v", len(names), names)
	}
}

func TestLatestFailureFiltersByKindAndStatus(t *testing.T) {
	dir := t.TempDir()

	s1, _ := Start(dir, "build", 5)
	s1.Finish(nil) // ok - should never be returned by LatestFailure
	time.Sleep(2 * time.Millisecond)

	s2, _ := Start(dir, "run", 5)
	s2.Finish(errors.New("run failed"))
	time.Sleep(2 * time.Millisecond)

	s3, _ := Start(dir, "build", 5)
	s3.Finish(errors.New("build failed"))

	path, err := LatestFailure(dir, "")
	if err != nil {
		t.Fatalf("LatestFailure() error = %v", err)
	}
	if !strings.HasSuffix(path, "-fail.log") || !strings.Contains(filepath.Base(path), "build-") {
		t.Errorf("LatestFailure(\"\") = %q, want the most recent failure (a build one)", path)
	}

	runPath, err := LatestFailure(dir, "run")
	if err != nil {
		t.Fatalf("LatestFailure(run) error = %v", err)
	}
	if !strings.Contains(filepath.Base(runPath), "run-") {
		t.Errorf("LatestFailure(run) = %q, want a run-*-fail.log", runPath)
	}
}

func TestLatestFailureNoFailures(t *testing.T) {
	dir := t.TempDir()
	s, _ := Start(dir, "build", 5)
	s.Finish(nil)

	if _, err := LatestFailure(dir, ""); err == nil {
		t.Error("expected an error when no failing logs exist")
	}
}

func TestListEmptyDirNoError(t *testing.T) {
	names, err := List(t.TempDir())
	if err != nil {
		t.Fatalf("List() on a project with no .cmaker/logs yet should not error, got %v", err)
	}
	if len(names) != 0 {
		t.Errorf("List() = %v, want empty", names)
	}
}
