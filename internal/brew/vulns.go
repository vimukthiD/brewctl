package brew

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Vuln is one known vulnerability affecting an installed formula.
type Vuln struct {
	ID       string   `json:"id"`
	Severity string   `json:"severity"` // low, medium, high, critical, or "" when unrated
	Summary  string   `json:"summary"`
	Aliases  []string `json:"aliases"`
	// Fixed lists where the fix landed: release versions, or commit hashes
	// for a fix that hasn't been released yet. Empty when there is no fix.
	Fixed     []string `json:"fixed"`
	FirstSeen int64    `json:"firstSeen"` // when a scan first reported it
	New       bool     `json:"new"`       // reported since the user last looked
}

// Finding groups the vulnerabilities of one formula.
type Finding struct {
	Formula string `json:"formula"`
	Version string `json:"version"`
	RepoURL string `json:"repoUrl"`
	Vulns   []Vuln `json:"vulns"`
	// Patched counts vulnerabilities Homebrew's own patches already resolve.
	Patched int `json:"patched"`
}

// VulnReport is the result of one `brew vulns` scan.
type VulnReport struct {
	CheckedAt int64     `json:"checkedAt"`
	Findings  []Finding `json:"findings"`
	Checked   int       `json:"checked"` // formulae looked up
	// Skipped are formulae with no source repository on record, which the
	// database can't look up. They are unknown, not safe.
	Skipped  []string `json:"skipped"`
	Patched  int      `json:"patched"`
	Complete bool     `json:"complete"`
}

type rawVuln struct {
	ID       string   `json:"id"`
	Severity string   `json:"severity"`
	Summary  string   `json:"summary"`
	Aliases  []string `json:"aliases"`
	Fixed    []string `json:"fixed_versions"`
}

type rawVulnReport struct {
	Findings []struct {
		Formula string    `json:"formula"`
		Version string    `json:"version"`
		RepoURL string    `json:"repo_url"`
		Vulns   []rawVuln `json:"vulnerabilities"`
		Patched []rawVuln `json:"patched"`
	} `json:"findings"`
	Skipped  []string `json:"skipped_formulae"`
	Checked  *int     `json:"checked"`
	Complete bool     `json:"complete"`
}

var severityRank = map[string]int{"critical": 4, "high": 3, "medium": 2, "low": 1}

// SeverityRank orders severities, highest first; unrated is 0.
func SeverityRank(s string) int { return severityRank[s] }

func parseVulns(out []byte) (*VulnReport, error) {
	var raw rawVulnReport
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	if raw.Checked == nil && raw.Findings == nil {
		return nil, fmt.Errorf("unexpected output")
	}
	rep := &VulnReport{Findings: []Finding{}, Skipped: raw.Skipped, Complete: raw.Complete}
	if raw.Checked != nil {
		rep.Checked = *raw.Checked
	}
	if rep.Skipped == nil {
		rep.Skipped = []string{}
	}
	sort.Strings(rep.Skipped)
	for _, f := range raw.Findings {
		rep.Patched += len(f.Patched)
		if len(f.Vulns) == 0 {
			continue
		}
		fd := Finding{Formula: f.Formula, Version: f.Version, RepoURL: f.RepoURL, Patched: len(f.Patched)}
		for _, v := range f.Vulns {
			sev := strings.ToLower(v.Severity)
			if _, ok := severityRank[sev]; !ok {
				sev = ""
			}
			vv := Vuln{ID: v.ID, Severity: sev, Summary: strings.TrimSpace(v.Summary), Aliases: v.Aliases, Fixed: v.Fixed}
			if vv.Aliases == nil {
				vv.Aliases = []string{}
			}
			if vv.Fixed == nil {
				vv.Fixed = []string{}
			}
			fd.Vulns = append(fd.Vulns, vv)
		}
		sort.SliceStable(fd.Vulns, func(i, j int) bool {
			return severityRank[fd.Vulns[i].Severity] > severityRank[fd.Vulns[j].Severity]
		})
		rep.Findings = append(rep.Findings, fd)
	}
	sort.SliceStable(rep.Findings, func(i, j int) bool {
		a, b := rep.Findings[i], rep.Findings[j]
		if ra, rb := severityRank[a.Vulns[0].Severity], severityRank[b.Vulns[0].Severity]; ra != rb {
			return ra > rb
		}
		return a.Formula < b.Formula
	})
	return rep, nil
}

// Vulns looks up installed formulae in the OSV.dev database through
// `brew vulns`. This sends their names and versions over the network.
func (c *Client) Vulns(ctx context.Context) (*VulnReport, error) {
	cmd := c.command(ctx, "vulns", "--json", "--max-summary", "0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// brew exits 1 when it finds anything, so judge by the output instead.
	out, runErr := cmd.Output()
	rep, err := parseVulns(out)
	if err != nil {
		msg := lastLine(stderr.String())
		if msg == "" && runErr != nil {
			msg = runErr.Error()
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("brew vulns: %s", msg)
	}
	rep.CheckedAt = time.Now().Unix()
	return rep, nil
}

// SecurityStore keeps the last scan and what the user has already been
// shown, so the monitor can tell new vulnerabilities from known ones.
//
// The app and the background check (a separate process) share the file, so
// every call re-reads it if it changed on disk.
type SecurityStore struct {
	mu      sync.Mutex
	path    string
	data    securityData
	modTime time.Time
	size    int64
}

type securityData struct {
	Monitor   bool             `json:"monitor"`
	Report    *VulnReport      `json:"report"`
	FirstSeen map[string]int64 `json:"firstSeen"`
	Unseen    []string         `json:"unseen"`
	// LastError is why the last check didn't finish; empty when it did.
	LastError string `json:"lastError,omitempty"`
}

func vulnKey(formula, id string) string { return formula + "|" + id }

// OpenSecurityStore loads the store at path; a missing or unreadable file
// starts empty.
func OpenSecurityStore(path string) *SecurityStore {
	s := &SecurityStore{path: path}
	s.load()
	return s
}

// load re-reads the file if another process changed it.
func (s *SecurityStore) load() {
	if st, err := os.Stat(s.path); err == nil && (!st.ModTime().Equal(s.modTime) || st.Size() != s.size) {
		if b, err := os.ReadFile(s.path); err == nil {
			var d securityData
			if json.Unmarshal(b, &d) == nil {
				s.data = d
			}
		}
		s.modTime, s.size = st.ModTime(), st.Size()
	}
	if s.data.FirstSeen == nil {
		s.data.FirstSeen = map[string]int64{}
	}
}

// TryLock takes the check lock, shared with other processes, so the app and
// the background check never run a check at the same time. It returns false
// if a check is already running elsewhere.
func (s *SecurityStore) TryLock() (unlock func(), ok bool) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return nil, false
	}
	f, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, false
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, true
}

// SetError records why the last check failed, or clears it.
func (s *SecurityStore) SetError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if s.data.LastError == msg {
		return
	}
	s.data.LastError = msg
	s.save()
}

// LastError is why the last check didn't finish, if it didn't.
func (s *SecurityStore) LastError() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	return s.data.LastError
}

func (s *SecurityStore) save() {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil && os.Rename(tmp, s.path) == nil {
		if st, err := os.Stat(s.path); err == nil {
			s.modTime, s.size = st.ModTime(), st.Size()
		}
	}
}

// Monitor reports whether automatic checks are on.
func (s *SecurityStore) Monitor() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	return s.data.Monitor
}

// SetMonitor turns automatic checks on or off.
func (s *SecurityStore) SetMonitor(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	s.data.Monitor = on
	s.save()
}

// CheckedAt is when the last scan finished, or 0.
func (s *SecurityStore) CheckedAt() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if s.data.Report == nil {
		return 0
	}
	return s.data.Report.CheckedAt
}

// Record stores a new scan and returns the findings that no earlier scan
// reported, reduced to their new vulnerabilities. The first scan ever is the
// baseline: nothing in it counts as new.
func (s *SecurityStore) Record(rep *VulnReport) []Finding {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	baseline := s.data.Report == nil
	present := map[string]bool{}
	var fresh []Finding
	for _, f := range rep.Findings {
		var add []Vuln
		for _, v := range f.Vulns {
			k := vulnKey(f.Formula, v.ID)
			present[k] = true
			if _, known := s.data.FirstSeen[k]; known {
				continue
			}
			s.data.FirstSeen[k] = rep.CheckedAt
			if !baseline {
				s.data.Unseen = append(s.data.Unseen, k)
				add = append(add, v)
			}
		}
		if len(add) > 0 {
			fresh = append(fresh, Finding{Formula: f.Formula, Version: f.Version, Vulns: add})
		}
	}
	// Forget what's gone, so a vulnerability that returns counts as new again.
	for k := range s.data.FirstSeen {
		if !present[k] {
			delete(s.data.FirstSeen, k)
		}
	}
	kept := s.data.Unseen[:0]
	for _, k := range s.data.Unseen {
		if present[k] {
			kept = append(kept, k)
		}
	}
	s.data.Unseen = kept
	s.data.Report = rep
	s.data.LastError = ""
	s.save()
	return fresh
}

// Acknowledge marks everything reported so far as seen by the user.
func (s *SecurityStore) Acknowledge() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if len(s.data.Unseen) == 0 {
		return
	}
	s.data.Unseen = nil
	s.save()
}

// Report returns a copy of the last scan with FirstSeen and New filled in,
// or nil if there has been none.
func (s *SecurityStore) Report() *VulnReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if s.data.Report == nil {
		return nil
	}
	unseen := map[string]bool{}
	for _, k := range s.data.Unseen {
		unseen[k] = true
	}
	rep := *s.data.Report
	rep.Findings = make([]Finding, len(s.data.Report.Findings))
	for i, f := range s.data.Report.Findings {
		f.Vulns = append([]Vuln(nil), f.Vulns...)
		for j := range f.Vulns {
			k := vulnKey(f.Formula, f.Vulns[j].ID)
			f.Vulns[j].FirstSeen = s.data.FirstSeen[k]
			f.Vulns[j].New = unseen[k]
		}
		rep.Findings[i] = f
	}
	return &rep
}

// Notify posts a macOS notification. The text is passed as arguments, never
// spliced into the script.
func Notify(ctx context.Context, title, body string) {
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript",
		"-e", "on run argv",
		"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
		"-e", "end run",
		"--", title, body)
	_ = cmd.Run()
}
