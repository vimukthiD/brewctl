package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

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

	security    *brew.SecurityStore
	scanning    bool
	monitorOnce sync.Once
	background  bool   // running as the login agent's check, not the app
	agentErr    string // why setting up the login agent failed

	notes       *notifications.NotificationService // nil when unavailable
	notesOn     string                             // "allowed", "off" or "unavailable"
	pendingView string                             // a view a clicked notification asked for
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
	if !s.background {
		s.monitorOnce.Do(func() { go s.monitor() })
	}
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
	// `brew uninstall` leaves a formula's service running and registered at
	// login, pointing at files that no longer exist. Stop it first; jobs run
	// in order, so the uninstall follows.
	// A service that runs as root needs `sudo brew services stop`. If the
	// stop doesn't succeed (say the password prompt is cancelled), the
	// uninstall is skipped rather than leaving the service orphaned.
	var after int
	plan := s.uninstallPlan(ctx, p)
	short := p.Name[strings.LastIndex(p.Name, "/")+1:]
	switch {
	case plan.StopService && plan.RootService:
		// One password stops it and hands its files back.
		after = r.EnqueueWith("Stop "+short, brew.Opts{Admin: true, Reclaim: plan.Reclaim}, "services", "stop", short).ID
	case plan.StopService:
		after = r.EnqueueWith("Stop "+short, brew.Opts{}, "services", "stop", short).ID
		if plan.Reclaim != "" {
			after = r.EnqueueWith("Reclaim "+short+"'s files", brew.Opts{Admin: true, Reclaim: plan.Reclaim, After: after}).ID
		}
	case plan.Reclaim != "":
		after = r.EnqueueWith("Reclaim "+short+"'s files", brew.Opts{Admin: true, Reclaim: plan.Reclaim}).ID
	}
	return r.EnqueueWith("Uninstall "+p.Name, brew.Opts{After: after}, "uninstall", flag, p.Name), nil
}

// UninstallPlan is what uninstalling a package involves beyond `brew uninstall`.
type UninstallPlan struct {
	StopService bool `json:"stopService"` // its service is running or registered
	RootService bool `json:"rootService"` // ...as root, so stopping it needs the password
	Running     bool `json:"running"`
	// Reclaim is its Cellar folder when root owns files in it (left by
	// running its service as root); the password hands them back first.
	Reclaim string `json:"reclaim"`
	Command string `json:"command"` // the equivalent Terminal commands
}

// UninstallPlan says what uninstalling p will do, for the confirmation.
func (s *BrewService) UninstallPlan(ctx context.Context, p PkgRef) (UninstallPlan, error) {
	if err := checkRef(p); err != nil {
		return UninstallPlan{}, err
	}
	return s.uninstallPlan(ctx, p), nil
}

func (s *BrewService) uninstallPlan(ctx context.Context, p PkgRef) UninstallPlan {
	flag, _ := kindFlag(p.Kind)
	plan := UninstallPlan{}
	var steps []string
	if p.Kind == "formula" {
		short := p.Name[strings.LastIndex(p.Name, "/")+1:]
		if sv, ok := s.service(ctx, short); ok && sv.Active() {
			plan.StopService, plan.RootService, plan.Running = true, s.asRoot(sv), sv.Running
			stop := "brew services stop " + short
			if plan.RootService {
				stop = "sudo " + stop
			}
			steps = append(steps, stop)
		}
		if c, _, err := s.ready(ctx); err == nil {
			plan.Reclaim = c.RootOwnedRack(p.Name)
		}
		if plan.Reclaim != "" {
			steps = append(steps, "sudo chown -R $USER "+plan.Reclaim)
		}
	}
	plan.Command = strings.Join(append(steps, "brew uninstall "+flag+" "+p.Name), " && ")
	return plan
}

// service looks up one background service by name.
func (s *BrewService) service(ctx context.Context, name string) (brew.Service, bool) {
	c, _, err := s.ready(ctx)
	if err != nil {
		return brew.Service{}, false
	}
	services, err := c.Services(ctx)
	if err != nil {
		return brew.Service{}, false
	}
	for _, sv := range services {
		if sv.Name == name {
			return sv, true
		}
	}
	return brew.Service{}, false
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
	return c.RemoveLeftovers(ctx, allowed, paths, "Clear leftovers of "+p.Name), nil
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

// Services lists background services provided by installed formulae.
func (s *BrewService) Services(ctx context.Context) ([]brew.Service, error) {
	c, _, err := s.ready(ctx)
	if err != nil {
		return nil, err
	}
	return c.Services(ctx)
}

// ServiceAction starts, stops, restarts, runs or pauses a service.
func (s *BrewService) ServiceAction(ctx context.Context, name, action string) (brew.Job, error) {
	_, r, err := s.ready(ctx)
	if err != nil {
		return brew.Job{}, err
	}
	if !brew.ValidName(name) {
		return brew.Job{}, fmt.Errorf("invalid service name %q", name)
	}
	verb, ok := brew.ServiceActions[action]
	if !ok {
		return brew.Job{}, fmt.Errorf("unknown service action %q", action)
	}
	// Services that run as root are managed with `sudo brew services`.
	sv, _ := s.service(ctx, name)
	return r.EnqueueWith(verb+" "+name, brew.Opts{Admin: s.asRoot(sv)}, "services", action, name), nil
}

// asRoot reports whether a service is, or must be, run as root: either it
// already runs as root, or its formula requires root to start it.
func (s *BrewService) asRoot(sv brew.Service) bool {
	if sv.User == "root" {
		return true
	}
	if sv.User != "" {
		return false // running as this user
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snap != nil {
		for _, p := range s.snap.Packages {
			if p.Kind == "formula" && p.Name[strings.LastIndex(p.Name, "/")+1:] == sv.Name {
				return p.ServiceRoot
			}
		}
	}
	return false
}

// ServiceLog returns the tail of a service's log files, if it has any.
func (s *BrewService) ServiceLog(ctx context.Context, name string) (string, error) {
	c, _, err := s.ready(ctx)
	if err != nil {
		return "", err
	}
	return c.ServiceLog(ctx, name)
}
