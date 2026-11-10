package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"dev.vimukthid/brewctl/internal/brew"
)

// BrewService is the API the frontend calls.
type BrewService struct {
	mu     sync.Mutex
	client *brew.Client
	runner *brew.Runner
	snap   *brew.Snapshot
	// leftovers remembers each scan, so removal can only touch paths the
	// scan itself produced, never arbitrary paths from the caller.
	leftovers map[string][]brew.Leftover
}

// Status describes the Homebrew installation the app is driving.
type Status struct {
	Found   bool   `json:"found"`
	Error   string `json:"error"`
	Path    string `json:"path"`
	Prefix  string `json:"prefix"`
	Version string `json:"version"`
}

// PkgRef names a package; Kind is "formula" or "cask".
type PkgRef struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// bundleIcon finds the app icon inside the .app bundle, if running from one.
func bundleIcon() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	icon := filepath.Join(filepath.Dir(filepath.Dir(exe)), "Resources", "icons.icns")
	if _, err := os.Stat(icon); err != nil {
		return ""
	}
	return icon
}

func emit(event string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(event, data)
	}
}

// ready lazily detects brew, so a Homebrew installed after launch is picked
// up by the next refresh without restarting the app.
func (s *BrewService) ready(ctx context.Context) (*brew.Client, *brew.Runner, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client, s.runner, nil
	}
	c, err := brew.Detect(ctx)
	if err != nil {
		return nil, nil, err
	}
	c.IconPath = bundleIcon()
	s.client = c
	s.runner = brew.NewRunner(c, emit)
	return s.client, s.runner, nil
}

// Status reports whether Homebrew was found and where.
func (s *BrewService) Status(ctx context.Context) Status {
	c, _, err := s.ready(ctx)
	if err != nil {
		return Status{Error: err.Error()}
	}
	return Status{Found: true, Path: c.Path, Prefix: c.Prefix, Version: c.Version}
}

// Snapshot returns the installed inventory, freshly read from brew.
func (s *BrewService) Snapshot(ctx context.Context) (*brew.Snapshot, error) {
	c, _, err := s.ready(ctx)
	if err != nil {
		return nil, err
	}
	snap, err := c.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
	return snap, nil
}

// Sizes measures disk usage for the packages in the latest snapshot.
func (s *BrewService) Sizes(ctx context.Context) (*brew.Sizes, error) {
	c, _, err := s.ready(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	snap := s.snap
	s.mu.Unlock()
	if snap == nil {
		if snap, err = s.Snapshot(ctx); err != nil {
			return nil, err
		}
	}
	return c.DiskUsage(ctx, snap), nil
}

// CleanupPreview reports what autoremove and cleanup would delete.
func (s *BrewService) CleanupPreview(ctx context.Context) (*brew.CleanupPreview, error) {
	c, _, err := s.ready(ctx)
	if err != nil {
		return nil, err
	}
	return c.PreviewCleanup(ctx)
}

// Jobs lists queued, running and recent jobs with their output.
func (s *BrewService) Jobs(ctx context.Context) []brew.Job {
	_, r, err := s.ready(ctx)
	if err != nil {
		return []brew.Job{}
	}
	return r.List()
}

// CancelJob stops a queued or running job.
func (s *BrewService) CancelJob(ctx context.Context, id int) {
	if _, r, err := s.ready(ctx); err == nil {
		r.Cancel(id)
	}
}

func kindFlag(kind string) (string, error) {
	switch kind {
	case "formula":
		return "--formula", nil
	case "cask":
		return "--cask", nil
	}
	return "", fmt.Errorf("unknown package kind %q", kind)
}

func checkRef(p PkgRef) error {
	if !brew.ValidName(p.Name) {
		return fmt.Errorf("invalid package name %q", p.Name)
	}
	_, err := kindFlag(p.Kind)
	return err
}

// UpdateIndex runs `brew update` to fetch the latest package definitions.
func (s *BrewService) UpdateIndex(ctx context.Context) (brew.Job, error) {
	_, r, err := s.ready(ctx)
	if err != nil {
		return brew.Job{}, err
	}
	return r.Enqueue("Check for updates", true, "update"), nil
}

// Upgrade upgrades the given packages: one job for formulae, one for casks.
func (s *BrewService) Upgrade(ctx context.Context, pkgs []PkgRef) ([]brew.Job, error) {
	_, r, err := s.ready(ctx)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, errors.New("nothing to upgrade")
	}
	byKind := map[string][]string{}
	for _, p := range pkgs {
		if err := checkRef(p); err != nil {
			return nil, err
		}
		byKind[p.Kind] = append(byKind[p.Kind], p.Name)
	}
	jobs := []brew.Job{}
	for _, kind := range []string{"formula", "cask"} {
		names := byKind[kind]
		if len(names) == 0 {
			continue
		}
		flag, _ := kindFlag(kind)
		title := "Upgrade " + names[0]
		if len(names) > 1 {
			title = fmt.Sprintf("Upgrade %d %ss", len(names), kind)
			if kind == "formula" {
				title = fmt.Sprintf("Upgrade %d formulae", len(names))
			}
		}
		args := append([]string{"upgrade", flag}, names...)
		jobs = append(jobs, r.Enqueue(title, true, args...))
	}
	return jobs, nil
}

// Uninstall removes a package. brew itself refuses when other installed
// packages still depend on it.
func (s *BrewService) Uninstall(ctx context.Context, p PkgRef) (brew.Job, error) {
	_, r, err := s.ready(ctx)
	if err != nil {
		return brew.Job{}, err
	}
	if err := checkRef(p); err != nil {
		return brew.Job{}, err
	}
	flag, _ := kindFlag(p.Kind)
	return r.Enqueue("Uninstall "+p.Name, true, "uninstall", flag, p.Name), nil
}

// SetPinned pins or unpins a formula so upgrades skip it.
func (s *BrewService) SetPinned(ctx context.Context, name string, pinned bool) (brew.Job, error) {
	_, r, err := s.ready(ctx)
	if err != nil {
		return brew.Job{}, err
	}
	if !brew.ValidName(name) {
		return brew.Job{}, fmt.Errorf("invalid package name %q", name)
	}
	if pinned {
		return r.Enqueue("Pin "+name, true, "pin", name), nil
	}
	return r.Enqueue("Unpin "+name, true, "unpin", name), nil
}

// Autoremove uninstalls dependencies nothing needs any more.
func (s *BrewService) Autoremove(ctx context.Context) (brew.Job, error) {
	_, r, err := s.ready(ctx)
	if err != nil {
		return brew.Job{}, err
	}
	return r.Enqueue("Remove unused dependencies", true, "autoremove"), nil
}

// Cleanup deletes old versions and stale downloads. With deep set it also
// empties the whole download cache.
func (s *BrewService) Cleanup(ctx context.Context, deep bool) (brew.Job, error) {
	_, r, err := s.ready(ctx)
	if err != nil {
		return brew.Job{}, err
	}
	if deep {
		return r.Enqueue("Deep clean", true, "cleanup", "--prune=all", "-s"), nil
	}
	return r.Enqueue("Clean up", true, "cleanup"), nil
}

// Doctor runs `brew doctor`; the findings land in the job output.
func (s *BrewService) Doctor(ctx context.Context) (brew.Job, error) {
	_, r, err := s.ready(ctx)
	if err != nil {
		return brew.Job{}, err
	}
	return r.EnqueueReport("Health check", "doctor"), nil
}

// Leftovers lists what uninstalling a package would leave behind.
func (s *BrewService) Leftovers(ctx context.Context, p PkgRef) ([]brew.Leftover, error) {
	c, _, err := s.ready(ctx)
	if err != nil {
		return nil, err
	}
	if err := checkRef(p); err != nil {
		return nil, err
	}
	s.mu.Lock()
	snap := s.snap
	s.mu.Unlock()
	var pkg *brew.Package
	if snap != nil {
		for i := range snap.Packages {
			if snap.Packages[i].Name == p.Name && snap.Packages[i].Kind == p.Kind {
				pkg = &snap.Packages[i]
				break
			}
		}
	}
	if pkg == nil {
		return nil, fmt.Errorf("%s is not installed", p.Name)
	}
	found := c.Leftovers(ctx, *pkg)
	s.mu.Lock()
	if s.leftovers == nil {
		s.leftovers = map[string][]brew.Leftover{}
	}
	s.leftovers[p.Kind+":"+p.Name] = found
	s.mu.Unlock()
	return found, nil
}

// RemoveLeftovers clears paths from the package's last Leftovers scan. It
// refuses while the package is still installed.
func (s *BrewService) RemoveLeftovers(ctx context.Context, p PkgRef, paths []string) (brew.RemoveResult, error) {
	c, _, err := s.ready(ctx)
	if err != nil {
		return brew.RemoveResult{}, err
	}
	if err := checkRef(p); err != nil {
		return brew.RemoveResult{}, err
	}
	s.mu.Lock()
	allowed := s.leftovers[p.Kind+":"+p.Name]
	s.mu.Unlock()
	if len(allowed) == 0 {
		return brew.RemoveResult{}, errors.New("no leftover scan for this package")
	}
	flag, _ := kindFlag(p.Kind)
	if _, err := c.Run(ctx, "list", flag, "--versions", p.Name); err == nil {
		return brew.RemoveResult{}, fmt.Errorf("%s is still installed", p.Name)
	}
	return c.RemoveLeftovers(ctx, allowed, paths), nil
}

// Search finds installable packages by name or description.
func (s *BrewService) Search(ctx context.Context, query string) ([]brew.SearchResult, error) {
	c, _, err := s.ready(ctx)
	if err != nil {
		return nil, err
	}
	return c.Search(ctx, query)
}

// Info describes a package from the catalogue, installed or not.
func (s *BrewService) Info(ctx context.Context, p PkgRef) (*brew.Remote, error) {
	c, _, err := s.ready(ctx)
	if err != nil {
		return nil, err
	}
	if err := checkRef(p); err != nil {
		return nil, err
	}
	return c.Info(ctx, p.Name, p.Kind)
}

// Install installs a formula or cask.
func (s *BrewService) Install(ctx context.Context, p PkgRef) (brew.Job, error) {
	_, r, err := s.ready(ctx)
	if err != nil {
		return brew.Job{}, err
	}
	if err := checkRef(p); err != nil {
		return brew.Job{}, err
	}
	flag, _ := kindFlag(p.Kind)
	return r.Enqueue("Install "+p.Name, true, "install", flag, p.Name), nil
}
