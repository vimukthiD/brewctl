package brew

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
)

// cacheInternals are parts of the cache folder that are Homebrew's own
// working data (the package index, Ruby bytecode), not downloads. No cleanup
// command removes them, so they don't count as reclaimable cache.
var cacheInternals = map[string]bool{"api": true, "bootsnap": true, "tool-selection": true}

// Sizes is on-disk usage in bytes. Packages is keyed by "kind:name".
type Sizes struct {
	Packages map[string]int64 `json:"packages"`
	Cache    int64            `json:"cache"`
}

// dirSize sums allocated blocks under root, like `du`: symlinks are not
// followed and hard-linked files are counted once.
func dirSize(ctx context.Context, root string) int64 {
	var total int64
	seen := map[uint64]struct{}{}
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if ctx.Err() != nil {
			return fs.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			total += info.Size()
			return nil
		}
		if st.Nlink > 1 && !d.IsDir() {
			if _, dup := seen[uint64(st.Ino)]; dup {
				return nil
			}
			seen[uint64(st.Ino)] = struct{}{}
		}
		total += int64(st.Blocks) * 512
		return nil
	})
	return total
}

// DiskUsage measures every package in the snapshot plus the download cache.
func (c *Client) DiskUsage(ctx context.Context, snap *Snapshot) *Sizes {
	res := &Sizes{Packages: make(map[string]int64, len(snap.Packages))}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, 8)
	)
	measure := func(key string, paths []string) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()
		var n int64
		for _, p := range paths {
			if _, err := os.Lstat(p); err == nil {
				n += dirSize(ctx, p)
			}
		}
		mu.Lock()
		res.Packages[key] = n
		mu.Unlock()
	}
	for _, p := range snap.Packages {
		var paths []string
		if p.Kind == "cask" {
			paths = append(paths, filepath.Join(c.Caskroom, p.Name))
			for _, app := range p.Apps {
				paths = append(paths, filepath.Join("/Applications", app))
			}
		} else {
			paths = append(paths, filepath.Join(c.Cellar, shortName(p.Name)))
		}
		wg.Add(1)
		go measure(p.Kind+":"+p.Name, paths)
	}
	if c.Cache != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var n int64
			entries, _ := os.ReadDir(c.Cache)
			for _, e := range entries {
				if !cacheInternals[e.Name()] {
					n += dirSize(ctx, filepath.Join(c.Cache, e.Name()))
				}
			}
			mu.Lock()
			res.Cache = n
			mu.Unlock()
		}()
	}
	wg.Wait()
	return res
}

// CleanupItem is one path `brew cleanup` would delete.
type CleanupItem struct {
	Path string `json:"path"`
	Size string `json:"size"`
}

// CleanupPreview is what autoremove and cleanup would do, without doing it.
type CleanupPreview struct {
	Orphans []string      `json:"orphans"`
	Items   []CleanupItem `json:"items"`
	Summary string        `json:"summary"` // e.g. "1.2GB", empty when nothing to free
}

var (
	wouldRemoveRE = regexp.MustCompile(`^Would remove: (.+?)(?: \((?:[^()]*, )?([^(),]+)\))?$`)
	wouldFreeRE   = regexp.MustCompile(`free approximately (.+?) of disk space`)
)

// PreviewCleanup dry-runs `brew autoremove` and `brew cleanup`.
func (c *Client) PreviewCleanup(ctx context.Context) (*CleanupPreview, error) {
	pv := &CleanupPreview{Orphans: []string{}, Items: []CleanupItem{}}
	var (
		wg             sync.WaitGroup
		autoOut, clOut []byte
		autoErr, clErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		autoOut, autoErr = c.Run(ctx, "autoremove", "--dry-run")
	}()
	go func() {
		defer wg.Done()
		clOut, clErr = c.Run(ctx, "cleanup", "--dry-run")
	}()
	wg.Wait()
	if autoErr != nil {
		return nil, autoErr
	}
	if clErr != nil {
		return nil, clErr
	}
	for _, l := range strings.Split(string(autoOut), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "==>") {
			continue
		}
		pv.Orphans = append(pv.Orphans, l)
	}
	for _, l := range strings.Split(string(clOut), "\n") {
		l = strings.TrimSpace(l)
		if m := wouldRemoveRE.FindStringSubmatch(l); m != nil {
			pv.Items = append(pv.Items, CleanupItem{Path: m[1], Size: m[2]})
		} else if m := wouldFreeRE.FindStringSubmatch(l); m != nil {
			pv.Summary = m[1]
		}
	}
	return pv, nil
}
