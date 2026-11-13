// Package keychain stores one secret per service in the macOS login
// keychain, through the system `security` tool.
package keychain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNotFound means there is no item for the service and account.
var ErrNotFound = errors.New("not in the keychain")

const tool = "/usr/bin/security"

// Item names one generic-password entry.
type Item struct {
	Service string // e.g. "dev.vimukthid.brewctl.anthropic"
	Account string
}

// Get reads the secret.
func (it Item) Get(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, tool, "find-generic-password", "-s", it.Service, "-a", it.Account, "-w").Output()
	if err != nil {
		var ee *exec.ExitError
		// 44 is errSecItemNotFound.
		if errors.As(err, &ee) && ee.ExitCode() == 44 {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("reading the keychain: %w", err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// Set stores or replaces the secret. The command goes to `security -i` on
// stdin, so the secret never appears in the process list.
func (it Item) Set(ctx context.Context, secret string) error {
	if !safe(secret) || !safe(it.Service) || !safe(it.Account) {
		return errors.New("unexpected characters in the value")
	}
	cmd := exec.CommandContext(ctx, tool, "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %q -a %q -w %q\n", it.Service, it.Account, secret))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	// `security -i` exits 0 even when a command fails; it prints the error.
	if msg := strings.TrimSpace(stderr.String() + string(out)); err != nil || msg != "" {
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("saving to the keychain: %s", msg)
	}
	return nil
}

// Delete removes the secret; a missing item is not an error.
func (it Item) Delete(ctx context.Context) error {
	err := exec.CommandContext(ctx, tool, "delete-generic-password", "-s", it.Service, "-a", it.Account).Run()
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 44) {
		return fmt.Errorf("removing from the keychain: %w", err)
	}
	return nil
}

// safe allows the characters API keys and identifiers use, so quoting for
// `security -i` can't be broken out of.
func safe(s string) bool {
	if s == "" || len(s) > 512 {
		return false
	}
	for _, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.", r)
		if !ok {
			return false
		}
	}
	return true
}
