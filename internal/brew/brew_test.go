package brew

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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

func TestNeedsSudo(t *testing.T) {
	if !needsSudo("sudo: a terminal is required to read the password; either use the -S option") {
		t.Error("terminal-required message not detected")
	}
	if needsSudo("==> Running sudo-free install") {
		t.Error("false positive")
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
	raw := `[{"app":["Foo.app"]},{"app":["Sub/Bar.app",{"target":"Baz.app"}]},{"zap":[{"trash":"~/x"}]},{"binary":["x"]}]`
	if err := json.Unmarshal([]byte(raw), &arts); err != nil {
		t.Fatal(err)
	}
	if got, want := caskApps(arts), []string{"Foo.app", "Baz.app"}; !reflect.DeepEqual(got, want) {
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
