package brew

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Service is a background service defined by an installed formula.
type Service struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Status     string `json:"status"`     // none, started, scheduled, stopped, error, unknown
	Running    bool   `json:"running"`    // a process is alive right now
	Registered bool   `json:"registered"` // starts by itself at login (or boot)
	Pid        int    `json:"pid"`
	ExitCode   *int   `json:"exitCode"` // last exit status, when it stopped on its own
	User       string `json:"user"`     // who it runs as; "root" for system services
	Command    string `json:"command"`
	File       string `json:"file"`
	LogPath    string `json:"logPath"`
	ErrorLog   string `json:"errorLog"`
	Scheduled  bool   `json:"scheduled"` // runs on a timer rather than staying up
}

type rawService struct {
	Name        string  `json:"name"`
	ServiceName string  `json:"service_name"`
	Running     bool    `json:"running"`
	Loaded      bool    `json:"loaded"`
	Schedulable bool    `json:"schedulable"`
	Pid         *int    `json:"pid"`
	ExitCode    *int    `json:"exit_code"`
	User        *string `json:"user"`
	Status      string  `json:"status"`
	File        string  `json:"file"`
	Registered  bool    `json:"registered"`
	Command     *string `json:"command"`
	LogPath     *string `json:"log_path"`
	ErrorLog    *string `json:"error_log_path"`
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func parseServices(out []byte) ([]Service, error) {
	var raw []rawService
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	services := make([]Service, 0, len(raw))
	for _, r := range raw {
		s := Service{
			Name: r.Name, Label: r.ServiceName, Status: r.Status,
			Running: r.Running, Registered: r.Registered, ExitCode: r.ExitCode,
			User: str(r.User), Command: str(r.Command), File: r.File,
			LogPath: str(r.LogPath), ErrorLog: str(r.ErrorLog), Scheduled: r.Schedulable,
		}
		if r.Pid != nil {
			s.Pid = *r.Pid
		}
		// brew reports "started" for a registered service whose process has
		// since died; the exit code is what tells them apart.
		if s.Status == "" {
			s.Status = "unknown"
		}
		services = append(services, s)
	}
	sort.SliceStable(services, func(i, j int) bool {
		if services[i].Running != services[j].Running {
			return services[i].Running
		}
		return services[i].Name < services[j].Name
	})
	return services, nil
}

// Services lists every service the user's installed formulae provide.
func (c *Client) Services(ctx context.Context) ([]Service, error) {
	out, err := c.Run(ctx, "services", "info", "--all", "--json")
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return []Service{}, nil
	}
	return parseServices(out)
}

// ServiceActions maps what the UI may ask for onto `brew services` verbs.
var ServiceActions = map[string]string{
	"start":   "Start",   // run now and at every login
	"run":     "Run",     // run now only
	"stop":    "Stop",    // stop and don't start at login
	"restart": "Restart", // stop, then start
}

// Active reports whether the service is running or set to start by itself,
// either of which outlives `brew uninstall`.
func (s Service) Active() bool {
	return s.Running || s.Registered || s.Pid != 0
}

const logTailBytes = 48 * 1024

// ServiceLog returns the end of a service's log files. The paths come from
// brew's own description of the service, never from the caller.
func (c *Client) ServiceLog(ctx context.Context, name string) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("invalid service name %q", name)
	}
	services, err := c.Services(ctx)
	if err != nil {
		return "", err
	}
	for _, s := range services {
		if s.Name != name {
			continue
		}
		var b strings.Builder
		seen := map[string]bool{}
		for _, p := range []string{s.LogPath, s.ErrorLog} {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			if tail, err := tailFile(p, logTailBytes); err == nil && tail != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				fmt.Fprintf(&b, "==> %s\n%s", p, tail)
			}
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("%s has no service", name)
}

func tailFile(path string, max int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("not a file")
	}
	start := info.Size() - max
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(f, max))
	if err != nil {
		return "", err
	}
	text := strings.ToValidUTF8(string(data), "")
	if start > 0 {
		// Drop the partial first line.
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
	}
	return strings.TrimRight(stripANSI(text), "\n"), nil
}
