package main

import (
	"context"
	"os"
	"time"

	"dev.vimukthid/brewctl/internal/brew"
)

// While the app is open it keeps Homebrew's package lists fresh, so the
// Updates view isn't showing what was available days ago. It runs the same
// `brew update` job as "Check for updates", so it shows up in Activity, and
// the refresh after it reloads the outdated list. The login agent's
// background check never does this.

const (
	// autoUpdateStale is how old the lists may get before Brewctl updates them.
	autoUpdateStale = 6 * time.Hour
	// autoUpdateEvery is how often the app looks, after the first look at launch.
	autoUpdateEvery = time.Hour
	// autoUpdateDelay lets launch finish reading the inventory first.
	autoUpdateDelay = 5 * time.Second
	autoUpdateTitle = "Check for updates (automatic)"
	// noAutoUpdateEnv, set to anything, turns the automatic update off.
	noAutoUpdateEnv = "BREWCTL_NO_AUTO_UPDATE"
)

// autoUpdateDue reports whether to run `brew update` now: the lists are
// older than autoUpdateStale (or were never updated), no update is queued or
// running, and none finished successfully within autoUpdateStale. That last
// check keeps a setup whose files Brewctl can't read from updating hourly.
func autoUpdateDue(now, listsUpdated time.Time, jobs []brew.Job) bool {
	for _, j := range jobs {
		if j.Command != "brew update" {
			continue
		}
		switch j.Status {
		case brew.StatusQueued, brew.StatusRunning:
			return false
		case brew.StatusDone:
			if now.Sub(time.Unix(j.EndedAt, 0)) < autoUpdateStale {
				return false
			}
		}
	}
	return listsUpdated.IsZero() || now.Sub(listsUpdated) >= autoUpdateStale
}

// shouldAutoUpdate is autoUpdateDue, if the user hasn't turned automatic
// checks off.
func shouldAutoUpdate(p Preferences, now, listsUpdated time.Time, jobs []brew.Job) bool {
	return p.AutoUpdate && autoUpdateDue(now, listsUpdated, jobs)
}

// wantsAutoUpdate is false in the login agent's background check and when
// noAutoUpdateEnv is set.
func (s *BrewService) wantsAutoUpdate() bool {
	return !s.background && os.Getenv(noAutoUpdateEnv) == ""
}

// autoUpdate looks at launch and then every autoUpdateEvery.
func (s *BrewService) autoUpdate() {
	time.Sleep(autoUpdateDelay)
	for {
		s.autoUpdateTick(time.Now())
		time.Sleep(autoUpdateEvery)
	}
}

// autoUpdateTick queues the automatic update if it's due. It reads the saved
// preference every time, so switching it off needs no relaunch.
func (s *BrewService) autoUpdateTick(now time.Time) bool {
	c, r, err := s.ready(context.Background())
	if err != nil {
		return false
	}
	if !shouldAutoUpdate(s.prefsStore().Get(), now, c.ListsUpdatedAt(), r.List()) {
		return false
	}
	r.Enqueue(autoUpdateTitle, true, "update")
	return true
}
