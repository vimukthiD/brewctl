package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The login agent runs `Brewctl --background-check` every few hours, so the
// daily vulnerability check happens even while the app is closed. Each run
// exits straight away unless a check is due.

const (
	agentLabel    = "dev.vimukthid.brewctl.monitor"
	agentFlag     = "--background-check"
	agentInterval = 3 * 60 * 60 // seconds between wake-ups
)

func agentPlistPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist")
}

// agentExecutable is the app's own binary, if it runs from inside an .app
// bundle. Development and browser builds return "": notifications need the
// bundle, and an agent pointing into a build folder would go stale.
func agentExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if !strings.Contains(exe, ".app/Contents/MacOS/") {
		return ""
	}
	return exe
}

func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func agentPlist(exe string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>StartInterval</key>
	<integer>%d</integer>
	<key>ProcessType</key>
	<string>Background</string>
	<key>LowPriorityIO</key>
	<true/>
</dict>
</plist>
`, agentLabel, xmlText(exe), agentFlag, agentInterval)
}

// agentInstalled reports whether the agent is set up for this copy of the app.
func agentInstalled(exe string) bool {
	b, err := os.ReadFile(agentPlistPath())
	return err == nil && exe != "" && string(b) == agentPlist(exe)
}

func launchctl(args ...string) error {
	out, err := exec.Command("/bin/launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return nil
}

func agentDomain() string { return fmt.Sprintf("gui/%d", os.Getuid()) }

// installAgent writes the agent and loads it, replacing an older copy (say,
// one pointing at where the app used to be).
func installAgent(exe string) error {
	path := agentPlistPath()
	if path == "" || exe == "" {
		return fmt.Errorf("the background check needs Brewctl to run from its app")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	_ = launchctl("bootout", agentDomain()+"/"+agentLabel)
	if err := os.WriteFile(path, []byte(agentPlist(exe)), 0o644); err != nil {
		return err
	}
	return launchctl("bootstrap", agentDomain(), path)
}

// removeAgent unloads and deletes the agent, if there is one.
func removeAgent() error {
	path := agentPlistPath()
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	// Delete first: when the agent removes itself, bootout ends this process.
	err := os.Remove(path)
	_ = launchctl("bootout", agentDomain()+"/"+agentLabel)
	return err
}
