package brew

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Package is one installed formula or cask.
type Package struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"` // "formula" or "cask"
	DisplayName string   `json:"displayName"`
	Desc        string   `json:"desc"`
	Homepage    string   `json:"homepage"`
	License     string   `json:"license"`
	Tap         string   `json:"tap"`
	Version     string   `json:"version"` // installed
	Latest      string   `json:"latest"`  // available
	Outdated    bool     `json:"outdated"`
	Pinned      bool     `json:"pinned"`
	OnRequest   bool     `json:"onRequest"`
	InstalledAt int64    `json:"installedAt"` // unix seconds, 0 if unknown
	Deps        []string `json:"deps"`        // direct runtime dependencies
	AllDeps     []string `json:"allDeps"`     // full runtime closure
	Caveats     string   `json:"caveats"`
	KegOnly     bool     `json:"kegOnly"`
	AutoUpdates bool     `json:"autoUpdates"`
	Deprecated  bool     `json:"deprecated"`
	Disabled    bool     `json:"disabled"`
	Apps        []string `json:"apps"` // cask .app artifacts
	Zap         []string `json:"zap"`  // paths a cask lists as its own files
	// ServiceRoot means the formula's service must run as root, so it is
	// started and stopped with `sudo brew services`.
	ServiceRoot bool `json:"serviceRoot"`
}

// Snapshot is the full installed inventory at a point in time.
type Snapshot struct {
	Packages  []Package `json:"packages"`
	FetchedAt int64     `json:"fetchedAt"`
}

type rawInfo struct {
	Formulae []struct {
		Name       string `json:"name"`
		FullName   string `json:"full_name"`
		Desc       string `json:"desc"`
		Homepage   string `json:"homepage"`
		License    string `json:"license"`
		Tap        string `json:"tap"`
		Revision   int    `json:"revision"`
		Outdated   bool   `json:"outdated"`
		Pinned     bool   `json:"pinned"`
		KegOnly    bool   `json:"keg_only"`
		Caveats    string `json:"caveats"`
		Deprecated bool   `json:"deprecated"`
		Disabled   bool   `json:"disabled"`
		Service    *struct {
			RequireRoot bool `json:"require_root"`
		} `json:"service"`
		Deps     []string `json:"dependencies"`
		Versions struct {
			Stable string `json:"stable"`
		} `json:"versions"`
		Installed []struct {
			Version     string `json:"version"`
			Time        int64  `json:"time"`
			OnRequest   bool   `json:"installed_on_request"`
			RuntimeDeps []struct {
				FullName string `json:"full_name"`
				Direct   *bool  `json:"declared_directly"`
			} `json:"runtime_dependencies"`
		} `json:"installed"`
	} `json:"formulae"`
	Casks []struct {
		Token       string   `json:"token"`
		Names       []string `json:"name"`
		Desc        string   `json:"desc"`
		Homepage    string   `json:"homepage"`
		Tap         string   `json:"tap"`
		Version     string   `json:"version"`
		Installed   string   `json:"installed"`
		InstalledAt int64    `json:"installed_time"`
		Outdated    bool     `json:"outdated"`
		Pinned      bool     `json:"pinned"`
		AutoUpdates bool     `json:"auto_updates"`
		Caveats     string   `json:"caveats"`
		Deprecated  bool     `json:"deprecated"`
		Disabled    bool     `json:"disabled"`
		DependsOn   struct {
			Formula json.RawMessage `json:"formula"`
		} `json:"depends_on"`
		Artifacts []json.RawMessage `json:"artifacts"`
	} `json:"casks"`
}

type rawOutdated struct {
	Formulae []rawOutdatedItem `json:"formulae"`
	Casks    []rawOutdatedItem `json:"casks"`
}

type rawOutdatedItem struct {
	Name    string `json:"name"`
	Current string `json:"current_version"`
}

// Inventory reads everything installed, plus what brew considers outdated.
func (c *Client) Inventory(ctx context.Context) (*Snapshot, error) {
	var (
		wg      sync.WaitGroup
		infoOut []byte
		infoErr error
		outOut  []byte
		outErr  error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		infoOut, infoErr = c.Run(ctx, "info", "--json=v2", "--installed")
	}()
	go func() {
		defer wg.Done()
		outOut, outErr = c.Run(ctx, "outdated", "--json=v2")
	}()
	wg.Wait()
	if infoErr != nil {
		return nil, infoErr
	}

	var info rawInfo
	if err := json.Unmarshal(infoOut, &info); err != nil {
		return nil, err
	}
	// `brew outdated` is the authority on what upgrade would act on; the
	// per-package "outdated" flag is only a fallback if that call failed.
	latest := map[string]string{}
	haveOutdated := false
	if outErr == nil {
		var od rawOutdated
		if json.Unmarshal(outOut, &od) == nil {
			haveOutdated = true
			for _, f := range od.Formulae {
				latest["formula:"+f.Name] = f.Current
			}
			for _, k := range od.Casks {
				latest["cask:"+k.Name] = k.Current
			}
		}
	}

	snap := &Snapshot{FetchedAt: time.Now().Unix()}
	for _, f := range info.Formulae {
		if len(f.Installed) == 0 {
			continue
		}
		inst := f.Installed[len(f.Installed)-1]
		p := Package{
			Name: f.Name, Kind: "formula", DisplayName: f.Name,
			Desc: f.Desc, Homepage: f.Homepage, License: f.License, Tap: f.Tap,
			Version: inst.Version, Latest: f.Versions.Stable,
			Pinned: f.Pinned, OnRequest: inst.OnRequest, InstalledAt: inst.Time,
			Caveats: f.Caveats, KegOnly: f.KegOnly,
			Deprecated: f.Deprecated, Disabled: f.Disabled,
			ServiceRoot: f.Service != nil && f.Service.RequireRoot,
			Deps:        []string{}, AllDeps: []string{}, Apps: []string{}, Zap: []string{},
		}
		if f.Tap != "" && f.Tap != "homebrew/core" && f.FullName != "" {
			p.Name = f.FullName
		}
		hasDirectFlag := false
		for _, d := range inst.RuntimeDeps {
			name := shortName(d.FullName)
			p.AllDeps = append(p.AllDeps, name)
			if d.Direct != nil {
				hasDirectFlag = true
				if *d.Direct {
					p.Deps = append(p.Deps, name)
				}
			}
		}
		if !hasDirectFlag {
			// Older install receipts don't record which deps are direct.
			if len(inst.RuntimeDeps) > 0 {
				in := map[string]bool{}
				for _, d := range p.AllDeps {
					in[d] = true
				}
				for _, d := range f.Deps {
					if in[d] {
						p.Deps = append(p.Deps, d)
					}
				}
			} else {
				p.Deps = append(p.Deps, f.Deps...)
			}
		}
		if haveOutdated {
			if v, ok := latest["formula:"+f.Name]; ok {
				p.Outdated, p.Latest = true, v
			} else if v, ok := latest["formula:"+f.FullName]; ok {
				p.Outdated, p.Latest = true, v
			}
		} else {
			p.Outdated = f.Outdated
		}
		snap.Packages = append(snap.Packages, p)
	}

	for _, k := range info.Casks {
		p := Package{
			Name: k.Token, Kind: "cask", DisplayName: k.Token,
			Desc: k.Desc, Homepage: k.Homepage, Tap: k.Tap,
			Version: k.Installed, Latest: k.Version,
			Pinned: k.Pinned, OnRequest: true, InstalledAt: k.InstalledAt,
			Caveats: k.Caveats, AutoUpdates: k.AutoUpdates,
			Deprecated: k.Deprecated, Disabled: k.Disabled,
			Deps: []string{}, AllDeps: []string{}, Apps: caskApps(k.Artifacts),
			Zap: caskZap(k.Artifacts),
		}
		// Tools an app needs from Homebrew, given as one name or a list.
		var many []string
		var one string
		if json.Unmarshal(k.DependsOn.Formula, &many) == nil {
			p.Deps = append(p.Deps, many...)
		} else if json.Unmarshal(k.DependsOn.Formula, &one) == nil && one != "" {
			p.Deps = append(p.Deps, one)
		}
		for i, d := range p.Deps {
			p.Deps[i] = shortName(d)
		}
		p.AllDeps = append(p.AllDeps, p.Deps...)
		if len(k.Names) > 0 && k.Names[0] != "" {
			p.DisplayName = k.Names[0]
		}
		if haveOutdated {
			if v, ok := latest["cask:"+k.Token]; ok {
				p.Outdated, p.Latest = true, v
			}
		} else {
			p.Outdated = k.Outdated
		}
		snap.Packages = append(snap.Packages, p)
	}

	sort.Slice(snap.Packages, func(i, j int) bool {
		return strings.ToLower(snap.Packages[i].Name) < strings.ToLower(snap.Packages[j].Name)
	})
	return snap, nil
}

func shortName(full string) string {
	if i := strings.LastIndexByte(full, '/'); i >= 0 {
		return full[i+1:]
	}
	return full
}

// caskApps pulls the .app bundle names out of a cask's artifact list, which
// looks like [{"app": ["Foo.app", {"target": "Bar.app"}]}, {"zap": ...}].
func caskApps(artifacts []json.RawMessage) []string {
	apps := []string{}
	for _, raw := range artifacts {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		// Apps installed by a system installer (.pkg) aren't listed as "app"
		// artifacts; the cask names them in its uninstall "delete" list instead.
		if un, ok := m["uninstall"]; ok {
			var stanzas []map[string]json.RawMessage
			if json.Unmarshal(un, &stanzas) == nil {
				for _, st := range stanzas {
					var many []string
					var one string
					if json.Unmarshal(st["delete"], &many) != nil && json.Unmarshal(st["delete"], &one) == nil {
						many = []string{one}
					}
					for _, d := range many {
						if filepath.Dir(d) == "/Applications" && strings.HasSuffix(d, ".app") {
							apps = append(apps, filepath.Base(d))
						}
					}
				}
			}
		}
		entry, ok := m["app"]
		if !ok {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(entry, &items) != nil {
			continue
		}
		name := ""
		for _, it := range items {
			var s string
			if json.Unmarshal(it, &s) == nil {
				name = filepath.Base(s)
				continue
			}
			var opts struct {
				Target string `json:"target"`
			}
			if json.Unmarshal(it, &opts) == nil && opts.Target != "" {
				name = filepath.Base(opts.Target)
			}
		}
		if name != "" {
			apps = append(apps, name)
		}
	}
	return apps
}

// caskZap collects the files a cask declares as its own in its "zap" stanza:
// [{"zap": [{"trash": ["~/Library/..."], "delete": "/path"}]}]. Only "trash"
// and "delete" count; "rmdir" names shared folders.
func caskZap(artifacts []json.RawMessage) []string {
	paths := []string{}
	for _, raw := range artifacts {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		entry, ok := m["zap"]
		if !ok {
			continue
		}
		var stanzas []map[string]json.RawMessage
		if json.Unmarshal(entry, &stanzas) != nil {
			continue
		}
		for _, st := range stanzas {
			for _, key := range []string{"trash", "delete"} {
				v, ok := st[key]
				if !ok {
					continue
				}
				var many []string
				var one string
				if json.Unmarshal(v, &many) == nil {
					paths = append(paths, many...)
				} else if json.Unmarshal(v, &one) == nil {
					paths = append(paths, one)
				}
			}
		}
	}
	return paths
}
