package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentPlist(t *testing.T) {
	exe := "/Applications/Tom & Jerry's <Apps>/Brewctl.app/Contents/MacOS/brewctl"
	p := filepath.Join(t.TempDir(), "agent.plist")
	if err := os.WriteFile(p, []byte(agentPlist(exe)), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/usr/bin/plutil", "-lint", p).CombinedOutput(); err != nil {
		t.Fatalf("plutil: %s", out)
	}
	out, err := exec.Command("/usr/bin/plutil", "-extract", "ProgramArguments.0", "raw", p).Output()
	if err != nil || strings.TrimSpace(string(out)) != exe {
		t.Errorf("program = %q, %v", out, err)
	}
	if agentInstalled("") {
		t.Error("an empty path is never installed")
	}
}
