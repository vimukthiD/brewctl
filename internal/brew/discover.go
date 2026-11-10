package brew

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// SearchResult is one hit from the Homebrew catalogue.
type SearchResult struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	DisplayName string `json:"displayName"`
	Desc        string `json:"desc"`
}

const maxResults = 120

// Search looks through the names and descriptions of everything Homebrew can
// install. A query wrapped in slashes is treated by brew as a regex.
func (c *Client) Search(ctx context.Context, query string) ([]SearchResult, error) {
	q := strings.TrimSpace(query)
	if len(q) < 2 {
		return []SearchResult{}, nil
	}
	if len(q) > 80 || strings.HasPrefix(q, "-") || strings.ContainsAny(q, "\n\r\x00") {
		return nil, errors.New("that search can't be used")
	}
	out, err := c.Run(ctx, "desc", "--search", q)
	if err != nil {
		return nil, err
	}
	results := parseDesc(string(out))
	rankResults(results, q)
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

// parseDesc reads `brew desc --search` output:
//
//	==> Formulae
//	ripgrep: Search tool like grep
//	==> Casks
//	visual-studio-code: (Microsoft Visual Studio Code, VS Code) Open-source code editor
func parseDesc(out string) []SearchResult {
	results := []SearchResult{}
	kind := "formula"
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "==>"):
			if strings.Contains(line, "Cask") {
				kind = "cask"
			} else {
				kind = "formula"
			}
			continue
		}
		name, desc, ok := strings.Cut(line, ": ")
		if !ok {
			name, desc = strings.TrimSuffix(line, ":"), ""
		}
		if !ValidName(name) {
			continue
		}
		r := SearchResult{Name: name, Kind: kind, DisplayName: name, Desc: desc}
		if kind == "cask" && strings.HasPrefix(desc, "(") {
			if end := strings.Index(desc, ") "); end > 0 {
				names := desc[1:end]
				r.Desc = desc[end+2:]
				if first, _, _ := strings.Cut(names, ", "); first != "" {
					r.DisplayName = first
				}
			} else if strings.HasSuffix(desc, ")") {
				r.DisplayName, r.Desc = strings.Trim(desc, "()"), ""
			}
		}
		if r.Desc == "[no description]" {
			r.Desc = ""
		}
		results = append(results, r)
	}
	return results
}

// rankResults puts the hits a person most likely meant first: exact name,
// then names starting with the query, then names containing it, then
// description-only matches.
func rankResults(rs []SearchResult, query string) {
	q := strings.ToLower(query)
	score := func(r SearchResult) int {
		n, d := strings.ToLower(shortName(r.Name)), strings.ToLower(r.DisplayName)
		switch {
		case n == q || d == q:
			return 0
		case strings.HasPrefix(n, q) || strings.HasPrefix(d, q):
			return 1
		case strings.Contains(n, q) || strings.Contains(d, q):
			return 2
		}
		return 3
	}
	sort.SliceStable(rs, func(i, j int) bool {
		si, sj := score(rs[i]), score(rs[j])
		if si != sj {
			return si < sj
		}
		if len(rs[i].Name) != len(rs[j].Name) && si < 3 {
			return len(rs[i].Name) < len(rs[j].Name)
		}
		return rs[i].Name < rs[j].Name
	})
}

// Remote describes a package that may not be installed yet.
type Remote struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	DisplayName string   `json:"displayName"`
	Desc        string   `json:"desc"`
	Homepage    string   `json:"homepage"`
	License     string   `json:"license"`
	Tap         string   `json:"tap"`
	Version     string   `json:"version"`
	Deps        []string `json:"deps"`
	Caveats     string   `json:"caveats"`
	Deprecated  bool     `json:"deprecated"`
	Disabled    bool     `json:"disabled"`
	AutoUpdates bool     `json:"autoUpdates"`
	// NeedsAdmin is set for casks that run a system installer (.pkg), which
	// asks for an administrator password.
	NeedsAdmin bool `json:"needsAdmin"`
}

// Info fetches details for one formula or cask from the catalogue.
func (c *Client) Info(ctx context.Context, name, kind string) (*Remote, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("invalid package name %q", name)
	}
	flag := "--formula"
	if kind == "cask" {
		flag = "--cask"
	}
	out, err := c.Run(ctx, "info", "--json=v2", flag, name)
	if err != nil {
		return nil, err
	}
	var info rawInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, err
	}
	if kind == "cask" {
		if len(info.Casks) == 0 {
			return nil, fmt.Errorf("%s was not found", name)
		}
		k := info.Casks[0]
		r := &Remote{
			Name: k.Token, Kind: "cask", DisplayName: k.Token, Desc: k.Desc,
			Homepage: k.Homepage, Tap: k.Tap, Version: k.Version, Deps: []string{},
			Caveats: k.Caveats, Deprecated: k.Deprecated, Disabled: k.Disabled,
			AutoUpdates: k.AutoUpdates, NeedsAdmin: caskHasArtifact(k.Artifacts, "pkg"),
		}
		if len(k.Names) > 0 && k.Names[0] != "" {
			r.DisplayName = k.Names[0]
		}
		return r, nil
	}
	if len(info.Formulae) == 0 {
		return nil, fmt.Errorf("%s was not found", name)
	}
	f := info.Formulae[0]
	r := &Remote{
		Name: f.Name, Kind: "formula", DisplayName: f.Name, Desc: f.Desc,
		Homepage: f.Homepage, License: f.License, Tap: f.Tap, Version: f.Versions.Stable,
		Deps: f.Deps, Caveats: f.Caveats, Deprecated: f.Deprecated, Disabled: f.Disabled,
	}
	if f.Tap != "" && f.Tap != "homebrew/core" && f.FullName != "" {
		r.Name = f.FullName
	}
	if r.Deps == nil {
		r.Deps = []string{}
	}
	return r, nil
}

func caskHasArtifact(artifacts []json.RawMessage, key string) bool {
	for _, raw := range artifacts {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) == nil {
			if _, ok := m[key]; ok {
				return true
			}
		}
	}
	return false
}
