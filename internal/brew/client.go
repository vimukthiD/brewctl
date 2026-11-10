// Package brew wraps the Homebrew CLI: detection, inventory, disk usage and
// a serial job runner for mutating commands.
package brew

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Client is a located Homebrew installation.
type Client struct {
	Path     string `json:"path"`
	Prefix   string `json:"prefix"`
	Cellar   string `json:"cellar"`
	Caskroom string `json:"caskroom"`
	Cache    string `json:"cache"`
	Version  string `json:"version"`
	// IconPath is an .icns shown in the password dialog; empty for a generic one.
	IconPath string `json:"-"`
}

// ErrNotFound is returned when no brew executable can be located.
var ErrNotFound = errors.New("Homebrew was not found on this Mac")

var candidates = []string{
	"/opt/homebrew/bin/brew",
	"/usr/local/bin/brew",
	"/home/linuxbrew/.linuxbrew/bin/brew",
}

// Detect locates brew. Apps launched from Finder get a minimal PATH, so the
// well-known install locations are checked before falling back to PATH.
func Detect(ctx context.Context) (*Client, error) {
	path := ""
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			path = c
			break
		}
	}
	if path == "" {
		p, err := exec.LookPath("brew")
		if err != nil {
			return nil, ErrNotFound
		}
		path = p
	}
	c := &Client{Path: path}

	var wg sync.WaitGroup
	get := func(dst *string, arg string) {
		defer wg.Done()
		if out, err := c.Run(ctx, arg); err == nil {
			*dst = strings.TrimSpace(string(out))
		}
	}
	wg.Add(5)
	go get(&c.Prefix, "--prefix")
	go get(&c.Cellar, "--cellar")
	go get(&c.Caskroom, "--caskroom")
	go get(&c.Cache, "--cache")
	go get(&c.Version, "--version")
	wg.Wait()

	if c.Prefix == "" {
		c.Prefix = filepath.Dir(filepath.Dir(path))
	}
	if c.Cellar == "" {
		c.Cellar = filepath.Join(c.Prefix, "Cellar")
	}
	if c.Caskroom == "" {
		c.Caskroom = filepath.Join(c.Prefix, "Caskroom")
	}
	if i := strings.IndexByte(c.Version, '\n'); i >= 0 {
		c.Version = c.Version[:i]
	}
	c.Version = strings.TrimPrefix(c.Version, "Homebrew ")
	return c, nil
}

// env is the environment every brew invocation runs with. Auto-update is off
// because the app updates explicitly, and nothing may prompt for input.
func (c *Client) env() []string {
	bin := filepath.Dir(c.Path)
	path := strings.Join([]string{bin, filepath.Join(filepath.Dir(bin), "sbin"),
		"/usr/bin", "/bin", "/usr/sbin", "/sbin"}, ":")
	env := []string{
		"PATH=" + path,
		"HOMEBREW_NO_AUTO_UPDATE=1",
		"HOMEBREW_NO_ENV_HINTS=1",
		"HOMEBREW_NO_COLOR=1",
		"HOMEBREW_NO_ANALYTICS_MESSAGE_OUTPUT=1",
		"NONINTERACTIVE=1",
	}
	for _, k := range []string{"HOME", "USER", "LOGNAME", "TMPDIR", "LANG", "SHELL",
		"HOMEBREW_GITHUB_API_TOKEN", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "no_proxy"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func (c *Client) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Path, args...)
	cmd.Env = c.env()
	cmd.Dir = os.TempDir()
	return cmd
}

// Run executes a read-only brew command and returns its stdout.
func (c *Client) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := c.command(ctx, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := lastLine(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("brew %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@+._/-]*$`)

// ValidName reports whether s is safe to pass to brew as a package name. It
// can never be mistaken for a flag.
func ValidName(s string) bool {
	return len(s) <= 200 && nameRE.MatchString(s)
}
