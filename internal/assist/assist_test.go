package assist

import (
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"dev.vimukthid/brewctl/internal/brew"
)

// app -> curl -> openssl, app -> libpng; tool -> openssl; orphan stands alone.
func testSnap() *brew.Snapshot {
	return &brew.Snapshot{Packages: []brew.Package{
		{Name: "app", Kind: "formula", Version: "1.0", OnRequest: true, Deps: []string{"curl", "libpng"}},
		{Name: "curl", Kind: "formula", Version: "8.0", Deps: []string{"openssl@3"}},
		{Name: "openssl@3", Kind: "formula", Version: "3.4", Outdated: true, Latest: "3.5"},
		{Name: "libpng", Kind: "formula", Version: "1.6"},
		{Name: "me/tap/tool", Kind: "formula", Version: "2.0", OnRequest: true, Deps: []string{"openssl@3"}, Pinned: true},
		{Name: "orphan", Kind: "formula", Version: "0.1"},
		{Name: "firefox", Kind: "cask", Version: "130.0", AppVersion: "131.0", AutoUpdates: true, OnRequest: true},
	}}
}

func TestInventory(t *testing.T) {
	inv := Inventory(testSnap())
	for _, want := range []string{
		"Formulae (6):",
		"openssl@3 3.4 (dependency, update to 3.5)",
		"me/tap/tool 2.0 (pinned)",
		"Apps / casks (1):\nfirefox 131.0 (updates itself)",
	} {
		if !strings.Contains(inv, want) {
			t.Errorf("inventory missing %q:\n%s", want, inv)
		}
	}
	if Inventory(testSnap()) != inv {
		t.Error("inventory is not stable")
	}
}

func TestReasons(t *testing.T) {
	g := NewGraph(testSnap())
	got := g.Reasons("openssl@3")
	want := [][]string{{"tool", "openssl@3"}, {"app", "curl", "openssl@3"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Reasons(openssl@3) = %v, want %v", got, want)
	}
	if got := g.Reasons("app"); !reflect.DeepEqual(got, [][]string{{"app"}}) {
		t.Errorf("Reasons(app) = %v", got)
	}
	if got := g.Reasons("orphan"); len(got) != 0 {
		t.Errorf("Reasons(orphan) = %v, want none", got)
	}
	if got := g.Reasons("missing"); got != nil {
		t.Errorf("Reasons(missing) = %v", got)
	}
	if got := g.NeededBy("openssl@3"); !reflect.DeepEqual(got, []string{"curl", "tool"}) {
		t.Errorf("NeededBy = %v", got)
	}
}

func TestOnlyFor(t *testing.T) {
	g := NewGraph(testSnap())
	// openssl stays: tool still needs it.
	if got := g.OnlyFor("app"); !reflect.DeepEqual(got, []string{"curl", "libpng"}) {
		t.Errorf("OnlyFor(app) = %v", got)
	}
	if got := g.OnlyFor("tool"); len(got) != 0 {
		t.Errorf("OnlyFor(tool) = %v, want none", got)
	}
}

func TestCost(t *testing.T) {
	u := Usage{Input: 1000, CacheWrite: 4000, CacheRead: 10000, Output: 500}.Add(Usage{Output: 500})
	// 1000*4 + 4000*5 + 10000*0.2 + 1000*20 = 46000 per million.
	if got := u.Cost(Price{Input: 4, CacheWrite: 5, CacheRead: 0.20, Output: 20}); math.Abs(got-0.046) > 1e-9 {
		t.Errorf("cost = %v, want 0.046", got)
	}
}

func TestRequestCostTiers(t *testing.T) {
	haiku, _ := FindModel("claude-haiku-5-5")
	short := Usage{Input: 50_000, Output: 1_000}
	if got := haiku.RequestCost(short); math.Abs(got-(50_000*0.10+1_000*0.50)/1e6) > 1e-12 {
		t.Errorf("short prompt cost = %v", got)
	}
	long := Usage{Input: 20_000, CacheRead: 90_000, Output: 1_000}
	if got := haiku.RequestCost(long); math.Abs(got-(20_000*0.50+90_000*0.05+1_000*2.50)/1e6) > 1e-12 {
		t.Errorf("long prompt cost = %v", got)
	}
	opus, _ := FindModel("claude-opus-5-5")
	if got := opus.RequestCost(long); math.Abs(got-(20_000*4+90_000*0.20+1_000*20)/1e6) > 1e-12 {
		t.Errorf("opus cost = %v", got)
	}
}

func TestStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "assistant.json")
	st := OpenStore(path)
	if st.Model().ID != DefaultModel {
		t.Errorf("default model = %s", st.Model().ID)
	}
	if err := st.SetModel("gpt-4"); err == nil {
		t.Error("unknown model accepted")
	}
	if err := st.SetModel("claude-haiku-5-5"); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 11, 13, 15, 0, 0, 0, time.Local)
	_ = st.Record(now.AddDate(0, 0, -500), "claude-sonnet-5-5", Usage{Input: 10}, 9, true) // pruned by the next write
	_ = st.Record(now, "claude-opus-5-5", Usage{Input: 1000, Output: 100}, 0.02, true)
	_ = st.Record(now, "claude-opus-5-5", Usage{CacheRead: 5000, Output: 50}, 0.01, false)
	_ = st.Record(now.AddDate(0, 0, -3), "claude-haiku-5-5", Usage{Input: 800}, 0.001, true)
	_ = st.Record(now.AddDate(0, -1, 0), "claude-sonnet-5-5", Usage{Input: 10}, 0.5, true)

	// Everything survives a reopen.
	st = OpenStore(path)
	if st.Model().ID != "claude-haiku-5-5" {
		t.Errorf("model after reopen = %s", st.Model().ID)
	}
	s := st.Summary(now)
	if s.Today.Questions != 1 || math.Abs(s.Today.Cost-0.03) > 1e-9 || s.Today.InputTokens != 6000 || s.Today.OutputTokens != 150 {
		t.Errorf("today = %+v", s.Today)
	}
	if s.Month.Questions != 2 || math.Abs(s.Month.Cost-0.031) > 1e-9 {
		t.Errorf("month = %+v", s.Month)
	}
	if s.All.Questions != 3 || math.Abs(s.All.Cost-0.531) > 1e-9 {
		t.Errorf("all = %+v (the 500-day-old record should be gone)", s.All)
	}
	if s.Since != now.AddDate(0, -1, 0).Format("2006-01-02") {
		t.Errorf("since = %s", s.Since)
	}
	if len(s.ByModel) != 2 || s.ByModel[0].Model != "claude-opus-5-5" || s.ByModel[0].Name != "Claude Opus 5.5" {
		t.Errorf("by model = %+v", s.ByModel)
	}
	if len(s.Days) != 30 || s.Days[29].Date != "2026-11-13" || math.Abs(s.Days[29].Cost-0.03) > 1e-9 || s.Days[26].Questions != 1 {
		t.Errorf("days = %+v", s.Days)
	}
}
