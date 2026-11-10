package brew

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Where a leftover was found, which is also how sure we are that it belongs
// to the package.
const (
	SourceCache    = "cache"    // Homebrew's download of this package: certain
	SourceHomebrew = "homebrew" // config/data under the Homebrew prefix: named after it
	SourceZap      = "zap"      // listed by the cask itself as its own files: certain
	SourceHome     = "home"     // a folder in the home directory with the same name: a guess
)

// Leftover is a file or folder `brew uninstall` does not remove.
type Leftover struct {
	Path   string `json:"path"`
	Label  string `json:"label"` // path with the home directory shortened to ~
	Size   int64  `json:"size"`
	Source string `json:"source"`
	Dir    bool   `json:"dir"`
}

// homeSpots are the places command-line tools conventionally keep their own
// files, relative to the home directory. %s is the package name.
var homeSpots = []string{
	".%s",
	".%src",
	".config/%s",
	".local/share/%s",
	".local/state/%s",
	".cache/%s",
	"Library/Application Support/%s",
	"Library/Caches/%s",
	"Library/Logs/%s",
}

// prefixSpots are where formulae keep config and data under the Homebrew
// prefix; uninstall deliberately leaves these in place.
var prefixSpots = []string{"etc/%s", "var/%s", "var/lib/%s", "var/log/%s", "var/run/%s"}

// baseName drops the tap and any @version: "hashicorp/tap/terraform" and
// "python@3.13" become "terraform" and "python".
func baseName(name string) string {
	s := shortName(name)
	if i := strings.IndexByte(s, '@'); i > 0 {
		s = s[:i]
	}
	return s
}

// Leftovers finds what uninstalling p would leave behind. Call it while the
// package is still installed: a cask's own list of files is gone afterwards.
func (c *Client) Leftovers(ctx context.Context, p Package) []Leftover {
	home, _ := os.UserHomeDir()
	seen := map[string]bool{}
	var out []Leftover

	add := func(path, source string) {
		path = filepath.Clean(path)
		if seen[path] || !c.removable(path, home) {
			return
		}
		info, err := os.Lstat(path)
		if err != nil {
			return
		}
		seen[path] = true
		l := Leftover{Path: path, Label: tilde(path, home), Source: source, Dir: info.IsDir()}
		if info.Mode()&os.ModeSymlink != 0 && source == SourceCache {
			// Cache entries are symlinks into downloads/; report the real size.
			if target, err := os.Stat(path); err == nil {
				l.Size = target.Size()
			}
		} else {
			l.Size = dirSize(ctx, path)
		}
		out = append(out, l)
	}

	short, base := shortName(p.Name), baseName(p.Name)
	names := []string{short}
	if base != short {
		names = append(names, base)
	}

	if c.Cache != "" {
		for _, dir := range []string{c.Cache, filepath.Join(c.Cache, "Cask")} {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				n := e.Name()
				if strings.HasPrefix(n, short+"--") || strings.HasPrefix(n, short+"_bottle_manifest--") {
					add(filepath.Join(dir, n), SourceCache)
				}
			}
		}
	}

	if p.Kind == "formula" {
		for _, n := range names {
			for _, spot := range prefixSpots {
				add(filepath.Join(c.Prefix, fmt.Sprintf(spot, n)), SourceHomebrew)
			}
		}
	}

	if home != "" {
		for _, z := range p.Zap {
			pattern := z
			if strings.HasPrefix(pattern, "~/") {
				pattern = filepath.Join(home, pattern[2:])
			}
			if !filepath.IsAbs(pattern) {
				continue
			}
			matches, _ := filepath.Glob(pattern)
			for _, m := range matches {
				add(m, SourceZap)
			}
		}
		// One- and two-letter names ("go", "gh") collide with too much.
		if len(base) >= 3 {
			for _, spot := range homeSpots {
				add(filepath.Join(home, fmt.Sprintf(spot, base)), SourceHome)
			}
		}
	}

	rank := map[string]int{SourceCache: 0, SourceHomebrew: 1, SourceZap: 2, SourceHome: 3}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Source] != rank[out[j].Source] {
			return rank[out[i].Source] < rank[out[j].Source]
		}
		return out[i].Size > out[j].Size
	})
	if out == nil {
		out = []Leftover{}
	}
	return out
}

// removable is the safety net: a leftover must sit inside the home directory,
// the Homebrew prefix or the Homebrew cache, at least two levels down, and
// must not be one of the shared folders everything else lives in.
func (c *Client) removable(path, home string) bool {
	if !filepath.IsAbs(path) || path != filepath.Clean(path) {
		return false
	}
	shared := map[string]bool{}
	if home != "" {
		for _, s := range []string{"", "Library", "Library/Application Support", "Library/Caches",
			"Library/Logs", "Library/Preferences", "Library/Containers", "Library/Group Containers",
			".config", ".local", ".local/share", ".local/state", ".cache", ".ssh", ".gnupg",
			"Documents", "Desktop", "Downloads", "Pictures", "Music", "Movies", "Applications"} {
			shared[filepath.Join(home, s)] = true
		}
	}
	for _, s := range []string{c.Prefix, c.Cellar, c.Caskroom, c.Cache,
		filepath.Join(c.Cache, "downloads"), filepath.Join(c.Cache, "Cask"),
		filepath.Join(c.Prefix, "etc"), filepath.Join(c.Prefix, "var"),
		filepath.Join(c.Prefix, "var/lib"), filepath.Join(c.Prefix, "var/log"),
		filepath.Join(c.Prefix, "var/run"), filepath.Join(c.Prefix, "bin"),
		filepath.Join(c.Prefix, "opt"), filepath.Join(c.Prefix, "lib"),
		filepath.Join(c.Prefix, "share"), filepath.Join(c.Prefix, "Library")} {
		shared[s] = true
	}
	if shared[path] {
		return false
	}
	for _, root := range []string{home, c.Prefix, c.Cache} {
		if root == "" || root == "/" {
			continue
		}
		if strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func tilde(path, home string) string {
	if home != "" && strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}

// RemoveResult summarises a cleanup of leftovers.
type RemoveResult struct {
	Removed int      `json:"removed"`
	Freed   int64    `json:"freed"`
	Failed  []string `json:"failed"` // labels of items that could not be removed
}

// RemoveLeftovers clears the given paths, but only those present in allowed
// (the result of an earlier Leftovers scan). Homebrew's cached downloads are
// deleted outright since brew can fetch them again; everything else goes to
// the Trash so it can be put back.
func (c *Client) RemoveLeftovers(ctx context.Context, allowed []Leftover, paths []string) RemoveResult {
	res := RemoveResult{Failed: []string{}}
	byPath := map[string]Leftover{}
	for _, l := range allowed {
		byPath[l.Path] = l
	}
	home, _ := os.UserHomeDir()

	var toTrash []Leftover
	for _, p := range paths {
		l, ok := byPath[p]
		if !ok || !c.removable(l.Path, home) {
			res.Failed = append(res.Failed, tilde(p, home))
			continue
		}
		if _, err := os.Lstat(l.Path); err != nil {
			continue // already gone
		}
		if l.Source != SourceCache {
			toTrash = append(toTrash, l)
			continue
		}
		// A cache entry is a symlink to the real file in downloads/.
		if target, err := os.Readlink(l.Path); err == nil {
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(l.Path), target)
			}
			target = filepath.Clean(target)
			if strings.HasPrefix(target, c.Cache+string(filepath.Separator)) {
				_ = os.Remove(target)
			}
		}
		if err := os.RemoveAll(l.Path); err != nil {
			res.Failed = append(res.Failed, l.Label)
			continue
		}
		res.Removed++
		res.Freed += l.Size
	}

	if len(toTrash) > 0 {
		trashed, err := moveToTrash(ctx, toTrash)
		for _, l := range toTrash {
			if trashed[l.Path] {
				res.Removed++
				res.Freed += l.Size
			} else {
				res.Failed = append(res.Failed, l.Label)
			}
		}
		_ = err
	}
	return res
}

// trashScript asks macOS to move each argument to the Trash and prints a JSON
// array of the paths that made it. Using the system API (rather than renaming
// into ~/.Trash) handles name clashes and other volumes.
const trashScript = `function run(argv) {
  ObjC.import("Foundation");
  var done = [];
  for (var i = 0; i < argv.length; i++) {
    var ok = $.NSFileManager.defaultManager.trashItemAtURLResultingItemURLError(
      $.NSURL.fileURLWithPath(argv[i]), null, null);
    if (ok) done.push(argv[i]);
  }
  return JSON.stringify(done);
}`

func moveToTrash(ctx context.Context, items []Leftover) (map[string]bool, error) {
	done := map[string]bool{}
	args := []string{"-l", "JavaScript", "-e", trashScript}
	for _, l := range items {
		args = append(args, l.Path)
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return done, errors.New(lastLine(stderr.String()))
	}
	var paths []string
	raw := strings.TrimSpace(string(out))
	if err := json.Unmarshal([]byte(raw), &paths); err != nil {
		// osascript may print the string quoted; try unquoting once.
		if s, uerr := strconv.Unquote(raw); uerr == nil {
			err = json.Unmarshal([]byte(s), &paths)
		}
		if err != nil {
			return done, err
		}
	}
	for _, p := range paths {
		done[p] = true
	}
	return done, nil
}
