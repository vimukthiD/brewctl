package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"dev.vimukthid/brewctl/internal/brew"
)

func TestAutoUpdateDue(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	job := func(command, status string, ended time.Time) brew.Job {
		j := brew.Job{Command: command, Status: status}
		if !ended.IsZero() {
			j.EndedAt = ended.Unix()
		}
		return j
	}

	cases := []struct {
		name  string
		lists time.Time
		jobs  []brew.Job
		want  bool
	}{
		{"fresh lists", ago(time.Hour), nil, false},
		{"just under the threshold", ago(autoUpdateStale - time.Minute), nil, false},
		{"stale lists", ago(autoUpdateStale + time.Minute), nil, true},
		{"never updated", time.Time{}, nil, true},
		{"update already queued", ago(48 * time.Hour), []brew.Job{job("brew update", brew.StatusQueued, time.Time{})}, false},
		{"update already running", ago(48 * time.Hour), []brew.Job{job("brew update", brew.StatusRunning, time.Time{})}, false},
		{"an unrelated job is running", ago(48 * time.Hour), []brew.Job{job("brew upgrade", brew.StatusRunning, time.Time{})}, true},
		{"recent update succeeded but files unreadable", time.Time{}, []brew.Job{job("brew update", brew.StatusDone, ago(time.Hour))}, false},
		{"old update succeeded", ago(48 * time.Hour), []brew.Job{job("brew update", brew.StatusDone, ago(7*time.Hour))}, true},
		{"recent update failed: try again", ago(48 * time.Hour), []brew.Job{job("brew update", brew.StatusFailed, ago(time.Hour))}, true},
		{"recent update cancelled: try again", ago(48 * time.Hour), []brew.Job{job("brew update", brew.StatusCancelled, ago(time.Hour))}, true},
	}
	for _, c := range cases {
		if got := autoUpdateDue(now, c.lists, c.jobs); got != c.want {
			t.Errorf("%s: autoUpdateDue = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestWantsAutoUpdate(t *testing.T) {
	t.Setenv(noAutoUpdateEnv, "")
	if !(&BrewService{}).wantsAutoUpdate() {
		t.Error("the app should update automatically")
	}
	if (&BrewService{background: true}).wantsAutoUpdate() {
		t.Error("the background check must never update")
	}
	t.Setenv(noAutoUpdateEnv, "1")
	if (&BrewService{}).wantsAutoUpdate() {
		t.Errorf("%s=1 should turn it off", noAutoUpdateEnv)
	}
}

// fakeQueue records jobs instead of running brew.
type fakeQueue struct {
	jobs  []brew.Job
	after []int
}

func (q *fakeQueue) EnqueueWith(title string, o brew.Opts, args ...string) brew.Job {
	j := brew.Job{ID: len(q.jobs) + 1, Title: title, Command: "brew " + strings.Join(args, " "), Status: brew.StatusQueued, Mutates: true}
	q.jobs = append(q.jobs, j)
	q.after = append(q.after, o.After)
	return j
}

func TestUpdateEverything(t *testing.T) {
	for _, cleanup := range []bool{true, false} {
		q := &fakeQueue{}
		got := enqueueUpdateEverything(q, cleanup)
		wantCmds := []string{"brew update", "brew upgrade"}
		wantAfter := []int{0, 1}
		if cleanup {
			wantCmds = append(wantCmds, "brew cleanup")
			wantAfter = append(wantAfter, 2)
		}
		var cmds []string
		for _, j := range q.jobs {
			cmds = append(cmds, j.Command)
		}
		if !reflect.DeepEqual(cmds, wantCmds) {
			t.Errorf("cleanup=%v: commands %q, want %q", cleanup, cmds, wantCmds)
		}
		// A linear chain: each step waits for the one before it.
		if !reflect.DeepEqual(q.after, wantAfter) {
			t.Errorf("cleanup=%v: prerequisites %v, want %v", cleanup, q.after, wantAfter)
		}
		if !reflect.DeepEqual(got, q.jobs) {
			t.Errorf("cleanup=%v: returned %+v, want every queued job", cleanup, got)
		}
	}
}

func TestPrefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brewctl", "prefs.json")
	if !openPrefs(path).Get().CleanupAfterUpdate {
		t.Error("cleanup should default to on")
	}
	if err := openPrefs(path).SetCleanupAfterUpdate(false); err != nil {
		t.Fatal(err)
	}
	if openPrefs(path).Get().CleanupAfterUpdate {
		t.Error("turning cleanup off wasn't remembered")
	}
	if err := openPrefs(path).SetCleanupAfterUpdate(true); err != nil {
		t.Fatal(err)
	}
	if !openPrefs(path).Get().CleanupAfterUpdate {
		t.Error("turning cleanup back on wasn't remembered")
	}
}

// The loop reads the saved preference on every tick: turning automatic
// checks off (or back on) applies without a relaunch.
func TestAutoUpdateFollowsSavedPref(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-2 * autoUpdateStale)

	st := openPrefs(path)
	if !st.Get().AutoUpdate {
		t.Fatal("automatic checks should default to on")
	}
	if !shouldAutoUpdate(st.Get(), now, stale, nil) {
		t.Error("on with stale lists: should update")
	}
	if err := st.SetAutoUpdate(false); err != nil {
		t.Fatal(err)
	}
	// Same store, as the running loop sees it: no relaunch.
	if shouldAutoUpdate(st.Get(), now, stale, nil) {
		t.Error("switched off: should not update")
	}
	// And from disk, as the next launch sees it.
	if openPrefs(path).Get().AutoUpdate {
		t.Error("switching automatic checks off wasn't remembered")
	}
	if !openPrefs(path).Get().CleanupAfterUpdate {
		t.Error("changing one preference reset another")
	}
	if err := st.SetAutoUpdate(true); err != nil {
		t.Fatal(err)
	}
	if !shouldAutoUpdate(openPrefs(path).Get(), now, stale, nil) {
		t.Error("switched back on: should update")
	}
	if shouldAutoUpdate(st.Get(), now, now.Add(-time.Hour), nil) {
		t.Error("on with fresh lists: should not update")
	}
}

func TestPreferencesReportForcedOff(t *testing.T) {
	s := &BrewService{prefs: openPrefs(filepath.Join(t.TempDir(), "prefs.json"))}
	t.Setenv(noAutoUpdateEnv, "")
	if s.preferences().AutoUpdateForcedOff {
		t.Error("not forced off without the env var")
	}
	t.Setenv(noAutoUpdateEnv, "1")
	if p := s.preferences(); !p.AutoUpdateForcedOff || !p.AutoUpdate {
		t.Errorf("env var: %+v, want forced off with the saved choice kept", p)
	}
}
