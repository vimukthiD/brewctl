package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Preferences are small settings the app remembers across launches.
type Preferences struct {
	// CleanupAfterUpdate makes "Update everything" finish with `brew cleanup`.
	CleanupAfterUpdate bool `json:"cleanupAfterUpdate"`
	// AutoUpdate lets the app run `brew update` on its own when the package
	// lists get old.
	AutoUpdate bool `json:"autoUpdate"`
	// AutoUpdateForcedOff is set when noAutoUpdateEnv turns it off regardless.
	AutoUpdateForcedOff bool `json:"autoUpdateForcedOff"`
}

// prefsFile is what's on disk; a missing value means the default.
type prefsFile struct {
	CleanupAfterUpdate *bool `json:"cleanupAfterUpdate,omitempty"`
	AutoUpdate         *bool `json:"autoUpdate,omitempty"`
}

// prefsStore keeps Preferences in a small JSON file.
type prefsStore struct {
	path string

	mu     sync.Mutex
	loaded bool
	f      prefsFile
}

func openPrefs(path string) *prefsStore { return &prefsStore{path: path} }

func (p *prefsStore) load() {
	if p.loaded {
		return
	}
	p.loaded = true
	if b, err := os.ReadFile(p.path); err == nil {
		_ = json.Unmarshal(b, &p.f)
	}
}

func (p *prefsStore) save() error {
	b, err := json.MarshalIndent(p.f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o700); err != nil {
		return err
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p.path)
}

// Get returns the preferences, with defaults for anything never set.
func (p *prefsStore) Get() Preferences {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	out := Preferences{CleanupAfterUpdate: true, AutoUpdate: true}
	if p.f.CleanupAfterUpdate != nil {
		out.CleanupAfterUpdate = *p.f.CleanupAfterUpdate
	}
	if p.f.AutoUpdate != nil {
		out.AutoUpdate = *p.f.AutoUpdate
	}
	return out
}

// set changes one remembered setting, keeping the old value if saving fails.
func (p *prefsStore) set(field **bool, on bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	old := *field
	*field = &on
	if err := p.save(); err != nil {
		*field = old
		return err
	}
	return nil
}

// SetCleanupAfterUpdate remembers the "Clean up afterwards" choice.
func (p *prefsStore) SetCleanupAfterUpdate(on bool) error {
	return p.set(&p.f.CleanupAfterUpdate, on)
}

// SetAutoUpdate remembers whether the app checks for updates on its own.
func (p *prefsStore) SetAutoUpdate(on bool) error {
	return p.set(&p.f.AutoUpdate, on)
}

func (s *BrewService) prefsStore() *prefsStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prefs == nil {
		dir, err := os.UserConfigDir()
		if err != nil {
			dir = os.TempDir()
		}
		s.prefs = openPrefs(filepath.Join(dir, "dev.vimukthid.brewctl", "prefs.json"))
	}
	return s.prefs
}

// Preferences returns the app's remembered settings.
func (s *BrewService) Preferences(ctx context.Context) Preferences {
	return s.preferences()
}

func (s *BrewService) preferences() Preferences {
	p := s.prefsStore().Get()
	p.AutoUpdateForcedOff = os.Getenv(noAutoUpdateEnv) != ""
	return p
}

// SetCleanupAfterUpdate remembers whether "Update everything" also cleans up.
func (s *BrewService) SetCleanupAfterUpdate(ctx context.Context, on bool) (Preferences, error) {
	err := s.prefsStore().SetCleanupAfterUpdate(on)
	return s.preferences(), err
}

// SetAutoUpdate turns the automatic update check on or off. The running app
// picks it up at once: turning it on checks straight away.
func (s *BrewService) SetAutoUpdate(ctx context.Context, on bool) (Preferences, error) {
	if err := s.prefsStore().SetAutoUpdate(on); err != nil {
		return s.preferences(), err
	}
	if on && s.wantsAutoUpdate() {
		go s.autoUpdateTick(time.Now())
	}
	return s.preferences(), nil
}
