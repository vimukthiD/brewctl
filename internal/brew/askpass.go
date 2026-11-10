package brew

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Homebrew runs a few steps of some cask installs through sudo. When the
// SUDO_ASKPASS variable names a program, brew calls `sudo -A` and sudo runs
// that program to get the password. Ours shows the standard macOS password
// dialog: the password goes from that dialog straight to sudo, and never
// passes through this app, its UI or its logs.

// askpassGiveUp is how long the dialog waits before the job is failed, so an
// unattended prompt can't hold Homebrew's lock forever.
const askpassGiveUp = 300

// askpassScript is the AppleScript the helper runs. The message arrives as an
// argument, so nothing from a package name is ever compiled as script.
func askpassScript(iconPath string, giveUpSeconds int) string {
	icon := "caution"
	if iconPath != "" {
		icon = `POSIX file "` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(iconPath) + `"`
	}
	return fmt.Sprintf(`on run argv
	activate
	set r to display dialog (item 1 of argv) default answer "" with hidden answer ¬
		with title "Brewctl" buttons {"Cancel", "OK"} default button "OK" ¬
		with icon %s giving up after %d
	if gave up of r then error number -128
	return text returned of r
end run
`, icon, giveUpSeconds)
}

// shellQuote wraps s in single quotes for /bin/sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// newAskpass writes a private, single-use password helper for one job and
// returns its path and a function that deletes it again.
func newAskpass(reason, iconPath string, giveUpSeconds int) (string, func(), error) {
	dir, err := os.MkdirTemp("", "brewctl-askpass-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	scpt := filepath.Join(dir, "prompt.applescript")
	if err := os.WriteFile(scpt, []byte(askpassScript(iconPath, giveUpSeconds)), 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	msg := fmt.Sprintf("“%s” needs your Mac login password to continue.\n\nIt goes straight to macOS and is not saved.", reason)
	helper := filepath.Join(dir, "askpass")
	body := "#!/bin/sh\nexec /usr/bin/osascript " + shellQuote(scpt) + " " + shellQuote(msg) + " 2>/dev/null\n"
	if err := os.WriteFile(helper, []byte(body), 0o700); err != nil {
		cleanup()
		return "", nil, err
	}
	return helper, cleanup, nil
}

// Why a job that needed sudo didn't get it.
const (
	SudoCancelled   = "cancelled"   // the password dialog was dismissed or timed out
	SudoWrong       = "wrong"       // the password was rejected
	SudoNotAdmin    = "notadmin"    // this account can't administer the Mac
	SudoUnavailable = "unavailable" // no way to ask (helper missing)
)

// sudoIssue classifies a line of sudo's own error output.
func sudoIssue(line string) string {
	if !strings.Contains(line, "sudo") && !strings.Contains(line, "Sorry, try again") {
		return ""
	}
	switch {
	case strings.Contains(line, "no password was provided"):
		return SudoCancelled
	case strings.Contains(line, "incorrect password attempt"):
		return SudoWrong
	case strings.Contains(line, "is not in the sudoers file"), strings.Contains(line, "is not allowed to run sudo"),
		strings.Contains(line, "may not run sudo"):
		return SudoNotAdmin
	case strings.Contains(line, "a terminal is required"), strings.Contains(line, "a password is required"),
		strings.Contains(line, "no askpass program"), strings.Contains(line, "no tty present"),
		strings.Contains(line, "unable to run"):
		return SudoUnavailable
	}
	return ""
}
