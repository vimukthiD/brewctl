package brew

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// appDirs are where Homebrew puts cask apps: the default appdir first, then
// the per-user folder some people point --appdir at.
func appDirs() []string {
	dirs := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	return dirs
}

// findApp returns the first existing bundle for an app name such as
// "Firefox.app", or "" if it isn't in any of the dirs.
func findApp(dirs []string, app string) string {
	for _, d := range dirs {
		p := filepath.Join(d, app)
		if st, err := os.Stat(filepath.Join(p, "Contents", "Info.plist")); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// plistString reads one string key from a plist (XML or binary) with plutil.
func plistString(ctx context.Context, plist, key string) string {
	out, err := exec.CommandContext(ctx, "/usr/bin/plutil", "-extract", key, "raw", "-o", "-", plist).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// bundleVersion is the version an app bundle reports about itself: the
// marketing version, falling back to the build number when that is missing.
func bundleVersion(ctx context.Context, bundle string) string {
	plist := filepath.Join(bundle, "Contents", "Info.plist")
	if v := plistString(ctx, plist, "CFBundleShortVersionString"); v != "" {
		return v
	}
	return plistString(ctx, plist, "CFBundleVersion")
}

// fillAppVersions sets AppVersion on casks that update themselves. Homebrew's
// record for those stays at the version it installed, so the bundle is the
// only place that says what is actually on disk.
func fillAppVersions(ctx context.Context, pkgs []Package, dirs []string) {
	var wg sync.WaitGroup
	for i := range pkgs {
		p := &pkgs[i]
		if p.Kind != "cask" || !p.AutoUpdates || len(p.Apps) == 0 {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, app := range p.Apps {
				if b := findApp(dirs, app); b != "" {
					p.AppVersion = bundleVersion(ctx, b)
					return
				}
			}
		}()
	}
	wg.Wait()
}
