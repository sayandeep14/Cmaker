package cmd

import (
	"runtime"
	"strconv"
	"testing"
)

func TestBuildCommandArgsDefaultsToNumCPU(t *testing.T) {
	got := buildCommandArgs("Debug", 0)
	want := []string{"--build", "build", "--config", "Debug", "-j", strconv.Itoa(runtime.NumCPU())}
	if len(got) != len(want) {
		t.Fatalf("buildCommandArgs(Debug, 0) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("buildCommandArgs(Debug, 0)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildCommandArgsExplicitJobs(t *testing.T) {
	got := buildCommandArgs("Release", 4)
	want := []string{"--build", "build", "--config", "Release", "-j", "4"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("buildCommandArgs(Release, 4)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildCommandArgsNegativeJobsFallsBackToNumCPU(t *testing.T) {
	got := buildCommandArgs("Debug", -1)
	want := strconv.Itoa(runtime.NumCPU())
	if got[len(got)-1] != want {
		t.Errorf("buildCommandArgs(Debug, -1) jobs = %q, want %q", got[len(got)-1], want)
	}
}
