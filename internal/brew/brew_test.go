package brew

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{
		"wget":                true,
		"python@3.13":         true,
		"dart-lang/dart/dart": true,
		"gtk+3":               true,
		"":                    false,
		"--force":             false,
		"-f":                  false,
		"a b":                 false,
		"a;rm -rf":            false,
		"$(x)":                false,
	} {
		if got := ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestStripANSI(t *testing.T) {
	in := "\x1b[1;32m==>\x1b[0m Pouring \x1b]8;;https://x\x07link\x1b]8;;\x07 done"
	if got, want := stripANSI(in), "==> Pouring link done"; got != want {
		t.Errorf("stripANSI = %q, want %q", got, want)
	}
}

func TestSudoIssue(t *testing.T) {
	for line, want := range map[string]string{
		"sudo: a terminal is required to read the password; either use the -S option": SudoUnavailable,
		"sudo: no password was provided":                                              SudoCancelled,
		"sudo: 3 incorrect password attempts":                                         SudoWrong,
		"someone is not in the sudoers file.":                                         SudoNotAdmin,
		"sudo: someone is not allowed to run sudo on this Mac":                        SudoNotAdmin,
		"==> Running sudo-free install":                                               "",
		"==> Pouring hello":                                                           "",
	} {
		if got := sudoIssue(line); got != want {
			t.Errorf("sudoIssue(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestAskpassHelper(t *testing.T) {
	// A reason with quotes must survive both the shell and AppleScript.
	helper, cleanup, err := newAskpass(`Install it's "odd"`, `/tmp/My "App"/icons.icns`, 300)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(helper)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("helper mode = %v, err = %v", info.Mode().Perm(), err)
	}
	if dir, _ := os.Stat(filepath.Dir(helper)); dir.Mode().Perm() != 0o700 {
		t.Errorf("helper dir mode = %v", dir.Mode().Perm())
	}
	body, _ := os.ReadFile(helper)
	if !strings.Contains(string(body), `it'\''s`) {
		t.Errorf("reason not shell-quoted: %s", body)
	}
	scpt, _ := os.ReadFile(filepath.Join(filepath.Dir(helper), "prompt.applescript"))
	if !strings.Contains(string(scpt), `POSIX file "/tmp/My \"App\"/icons.icns"`) || strings.Contains(string(scpt), "odd") {
		t.Errorf("script: %s", scpt)
	}
	cleanup()
	if _, err := os.Stat(helper); err == nil {
		t.Error("helper not removed")
	}
}

func TestCleanupRegex(t *testing.T) {
	m := wouldRemoveRE.FindStringSubmatch("Would remove: /opt/homebrew/Cellar/go/1.26.0 (14,203 files, 251.2MB)")
	if m == nil || m[1] != "/opt/homebrew/Cellar/go/1.26.0" || m[2] != "251.2MB" {
		t.Errorf("keg line: %q", m)
	}
	m = wouldRemoveRE.FindStringSubmatch("Would remove: /Users/x/Library/Caches/Homebrew/wget--1.24.tar.gz (1.2MB)")
	if m == nil || m[2] != "1.2MB" {
		t.Errorf("cache line: %q", m)
	}
	m = wouldFreeRE.FindStringSubmatch("==> This operation would free approximately 1.4GB of disk space.")
	if m == nil || m[1] != "1.4GB" {
		t.Errorf("summary line: %q", m)
	}
}

func TestCaskApps(t *testing.T) {
	var arts []json.RawMessage
	raw := `[{"app":["Foo.app"]},{"app":["Sub/Bar.app",{"target":"Baz.app"}]},{"zap":[{"trash":"~/x"}]},{"binary":["x"]},{"uninstall":[{"pkgutil":"x","delete":["/Applications/Pkg.app","/Library/Other/thing"]}]}]`
	if err := json.Unmarshal([]byte(raw), &arts); err != nil {
		t.Fatal(err)
	}
	if got, want := caskApps(arts), []string{"Foo.app", "Baz.app", "Pkg.app"}; !reflect.DeepEqual(got, want) {
		t.Errorf("caskApps = %v, want %v", got, want)
	}
}

func TestCaskZap(t *testing.T) {
	var arts []json.RawMessage
	raw := `[{"app":["Foo.app"]},{"zap":[{"trash":["~/Library/Caches/foo","~/Library/Preferences/com.foo.plist"],"rmdir":"~/Library/Shared"},{"delete":"/Library/foo"}]}]`
	if err := json.Unmarshal([]byte(raw), &arts); err != nil {
		t.Fatal(err)
	}
	want := []string{"~/Library/Caches/foo", "~/Library/Preferences/com.foo.plist", "/Library/foo"}
	if got := caskZap(arts); !reflect.DeepEqual(got, want) {
		t.Errorf("caskZap = %v, want %v", got, want)
	}
}

func TestBaseName(t *testing.T) {
	for in, want := range map[string]string{"python@3.13": "python", "hashicorp/tap/terraform": "terraform", "wget": "wget"} {
		if got := baseName(in); got != want {
			t.Errorf("baseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLeftoversScanAndRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	prefix := filepath.Join(home, "brew")
	c := &Client{Prefix: prefix, Cellar: filepath.Join(prefix, "Cellar"),
		Caskroom: filepath.Join(prefix, "Caskroom"), Cache: filepath.Join(home, "Library/Caches/Homebrew")}

	mk := func(rel string) string {
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "f"), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg := mk(".config/demo")
	mk(".config/demolition") // similar name, must not match
	etc := mk("brew/etc/demo")
	mk("Library/Caches/Homebrew/downloads")
	dl := filepath.Join(c.Cache, "downloads", "abc--demo--1.0.tar.gz")
	os.WriteFile(dl, []byte("bottle"), 0o644)
	link := filepath.Join(c.Cache, "demo--1.0")
	os.Symlink(dl, link)

	found := c.Leftovers(context.Background(), Package{Name: "demo", Kind: "formula"})
	got := map[string]string{}
	for _, l := range found {
		got[l.Path] = l.Source
	}
	want := map[string]string{cfg: SourceHome, etc: SourceHomebrew, link: SourceCache}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Leftovers = %v, want %v", got, want)
	}

	// Paths outside the scan are refused; cache entries are deleted with their target.
	outside := mk("Documents/keep")
	res := c.RemoveLeftovers(context.Background(), found, []string{link, outside, filepath.Join(home, ".config")})
	if res.Removed != 1 || len(res.Failed) != 2 {
		t.Errorf("RemoveLeftovers = %+v", res)
	}
	for _, gone := range []string{link, dl} {
		if _, err := os.Lstat(gone); err == nil {
			t.Errorf("%s still exists", gone)
		}
	}
	for _, kept := range []string{outside, cfg, etc} {
		if _, err := os.Lstat(kept); err != nil {
			t.Errorf("%s was removed", kept)
		}
	}
}

func TestRemovableGuards(t *testing.T) {
	c := &Client{Prefix: "/opt/homebrew", Cellar: "/opt/homebrew/Cellar", Caskroom: "/opt/homebrew/Caskroom", Cache: "/Users/x/Library/Caches/Homebrew"}
	for path, want := range map[string]bool{
		"/Users/x/.config/demo":                true,
		"/opt/homebrew/etc/demo":               true,
		"/Users/x":                             false,
		"/Users/x/.config":                     false,
		"/Users/x/Library/Application Support": false,
		"/Users/x/Documents":                   false,
		"/opt/homebrew/etc":                    false,
		"/opt/homebrew":                        false,
		"/etc/demo":                            false,
		"/Users/other/.config/demo":            false,
		"/Users/x/.config/../../other":         false,
		"relative/path":                        false,
	} {
		if got := c.removable(path, "/Users/x"); got != want {
			t.Errorf("removable(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestParseDescAndRank(t *testing.T) {
	out := `==> Formulae
code-cli: Command-line interface built-in Visual Studio Code
ripgrep-all: Wrapper around ripgrep that adds multiple rich file types
ripgrep: Search tool like grep and The Silver Searcher
nodesc: [no description]

==> Casks
visual-studio-code: (Microsoft Visual Studio Code, VS Code) Open-source code editor
plain-cask: Just a description
--bad: flag-looking name is dropped
`
	rs := parseDesc(out)
	if len(rs) != 6 {
		t.Fatalf("got %d results: %+v", len(rs), rs)
	}
	vs := rs[4]
	if vs.Kind != "cask" || vs.DisplayName != "Microsoft Visual Studio Code" || vs.Desc != "Open-source code editor" {
		t.Errorf("cask parse: %+v", vs)
	}
	if rs[3].Desc != "" || rs[5].DisplayName != "plain-cask" {
		t.Errorf("edge cases: %+v %+v", rs[3], rs[5])
	}
	rankResults(rs, "ripgrep")
	if rs[0].Name != "ripgrep" || rs[1].Name != "ripgrep-all" {
		t.Errorf("rank: %s, %s", rs[0].Name, rs[1].Name)
	}
}

func TestParseServices(t *testing.T) {
	raw := `[
	 {"name":"zeta","service_name":"sh.brew.zeta","running":false,"loaded":false,"schedulable":false,"pid":null,"exit_code":null,"user":null,"status":"none","file":"/opt/homebrew/opt/zeta/sh.brew.zeta.plist","registered":false,"command":"/opt/homebrew/opt/zeta/bin/zeta","log_path":null,"error_log_path":null},
	 {"name":"alpha","service_name":"sh.brew.alpha","running":true,"loaded":true,"schedulable":false,"pid":4242,"exit_code":null,"user":"me","status":"started","file":"/Users/me/Library/LaunchAgents/sh.brew.alpha.plist","registered":true,"command":"alpha -d","log_path":"/opt/homebrew/var/log/alpha.log","error_log_path":null},
	 {"name":"broken","service_name":"sh.brew.broken","running":false,"loaded":true,"schedulable":false,"pid":null,"exit_code":78,"user":"me","status":"error","file":"x","registered":true,"command":null,"log_path":null,"error_log_path":null}
	]`
	ss, err := parseServices([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 3 || ss[0].Name != "alpha" || !ss[0].Running || ss[0].Pid != 4242 || ss[0].User != "me" || ss[0].LogPath == "" {
		t.Fatalf("running service should sort first: %+v", ss)
	}
	if ss[1].Name != "broken" || ss[1].ExitCode == nil || *ss[1].ExitCode != 78 || ss[1].Command != "" {
		t.Errorf("error service: %+v", ss[1])
	}
	if ss[2].Name != "zeta" || ss[2].Registered || ss[2].ExitCode != nil {
		t.Errorf("stopped service: %+v", ss[2])
	}
}

func TestTailFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	os.WriteFile(p, []byte("first line is long\nsecond\nthird\n"), 0o644)
	if got, _ := tailFile(p, 1024); got != "first line is long\nsecond\nthird" {
		t.Errorf("whole file: %q", got)
	}
	if got, _ := tailFile(p, 16); got != "second\nthird" {
		t.Errorf("tail drops the partial line: %q", got)
	}
}

func TestParseVulns(t *testing.T) {
	out := []byte(`{"findings":[
	  {"formula":"aaa","version":"1.0","repo_url":"https://example.com/aaa","vulnerabilities":[
	    {"id":"CVE-1","severity":"UNKNOWN","summary":" one ","aliases":[],"fixed_versions":["abc"]},
	    {"id":"CVE-2","severity":"HIGH","summary":"two","aliases":[],"fixed_versions":[]}],"patched":[]},
	  {"formula":"only-patched","version":"2.0","vulnerabilities":[],"patched":[{"id":"CVE-3","severity":"HIGH"}]},
	  {"formula":"zzz","version":"3.0","vulnerabilities":[{"id":"CVE-4","severity":"CRITICAL","summary":"four"}],"patched":[]}],
	  "skipped_formulae":["b","a"],"checked":7,"complete":false}`)
	rep, err := parseVulns(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Findings) != 2 || rep.Findings[0].Formula != "zzz" || rep.Findings[1].Formula != "aaa" {
		t.Fatalf("findings: %+v", rep.Findings)
	}
	a := rep.Findings[1]
	if a.Vulns[0].ID != "CVE-2" || a.Vulns[0].Severity != "high" || a.Vulns[1].Severity != "" || a.Vulns[1].Summary != "one" {
		t.Errorf("vulns not normalised or sorted: %+v", a.Vulns)
	}
	if rep.Checked != 7 || rep.Patched != 1 || !reflect.DeepEqual(rep.Skipped, []string{"a", "b"}) {
		t.Errorf("report: %+v", rep)
	}
	if _, err := parseVulns([]byte(`Error: no such command`)); err == nil {
		t.Error("garbage should not parse")
	}
	if _, err := parseVulns([]byte(`{}`)); err == nil {
		t.Error("an empty object is not a report")
	}
}

func TestSecurityStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "security.json")
	report := func(at int64, ids ...string) *VulnReport {
		f := Finding{Formula: "pkg", Version: "1"}
		for _, id := range ids {
			f.Vulns = append(f.Vulns, Vuln{ID: id})
		}
		return &VulnReport{CheckedAt: at, Findings: []Finding{f}}
	}
	s := OpenSecurityStore(path)
	if s.Report() != nil || s.Monitor() {
		t.Fatal("a new store should be empty with monitoring off")
	}
	if fresh := s.Record(report(100, "A")); len(fresh) != 0 {
		t.Errorf("the first scan is the baseline, got new: %+v", fresh)
	}
	fresh := s.Record(report(200, "A", "B"))
	if len(fresh) != 1 || len(fresh[0].Vulns) != 1 || fresh[0].Vulns[0].ID != "B" {
		t.Fatalf("want only B as new, got %+v", fresh)
	}
	s.SetMonitor(true)

	// Survives a restart.
	s = OpenSecurityStore(path)
	v := s.Report().Findings[0].Vulns
	if !s.Monitor() || v[0].New || !v[1].New || v[0].FirstSeen != 100 || v[1].FirstSeen != 200 {
		t.Errorf("after reload: monitor=%v vulns=%+v", s.Monitor(), v)
	}
	if fresh := s.Record(report(300, "A", "B")); len(fresh) != 0 {
		t.Errorf("nothing changed, got new: %+v", fresh)
	}
	if !s.Report().Findings[0].Vulns[1].New {
		t.Error("B should stay new until acknowledged")
	}
	s.Acknowledge()
	if s.Report().Findings[0].Vulns[1].New {
		t.Error("acknowledged vulnerabilities are no longer new")
	}
	// Fixed, then back: counts as new again.
	s.Record(report(400, "A"))
	if fresh := s.Record(report(500, "A", "B")); len(fresh) != 1 {
		t.Errorf("a returning vulnerability should be new, got %+v", fresh)
	}
}
