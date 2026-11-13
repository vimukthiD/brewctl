// Package assist holds the parts of the AI assistant that don't talk to the
// model: what it is told about this Mac, and how its cost is counted.
package assist

import (
	"fmt"
	"sort"
	"strings"

	"dev.vimukthid/brewctl/internal/brew"
)

// Short is a package's name without its tap, as dependencies refer to it.
func Short(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// Inventory is a compact, stable listing of what is installed: one line per
// package, sorted, so the same Mac gives the same text (and a cache hit).
func Inventory(snap *brew.Snapshot) string {
	if snap == nil || len(snap.Packages) == 0 {
		return "Nothing is installed with Homebrew."
	}
	var formulae, casks []string
	for _, p := range snap.Packages {
		var b strings.Builder
		b.WriteString(p.Name)
		b.WriteByte(' ')
		v := p.Version
		if p.AutoUpdates && p.AppVersion != "" {
			v = p.AppVersion
		}
		b.WriteString(v)
		var tags []string
		if p.Kind == "formula" && !p.OnRequest {
			tags = append(tags, "dependency")
		}
		if p.Outdated {
			tags = append(tags, "update to "+p.Latest)
		}
		if p.Pinned {
			tags = append(tags, "pinned")
		}
		if p.AutoUpdates {
			tags = append(tags, "updates itself")
		}
		if p.Deprecated || p.Disabled {
			tags = append(tags, "deprecated")
		}
		if len(tags) > 0 {
			b.WriteString(" (" + strings.Join(tags, ", ") + ")")
		}
		if p.Kind == "cask" {
			casks = append(casks, b.String())
		} else {
			formulae = append(formulae, b.String())
		}
	}
	sort.Strings(formulae)
	sort.Strings(casks)
	var out strings.Builder
	fmt.Fprintf(&out, "Formulae (%d):\n%s\n\nApps / casks (%d):\n%s", len(formulae), strings.Join(formulae, "\n"), len(casks), strings.Join(casks, "\n"))
	return out.String()
}

// Graph is the installed dependency graph, by short name.
type Graph struct {
	pkgs     map[string]brew.Package
	needs    map[string][]string // direct dependencies that are installed
	neededBy map[string][]string // installed packages that need it directly
}

// NewGraph indexes a snapshot.
func NewGraph(snap *brew.Snapshot) *Graph {
	g := &Graph{pkgs: map[string]brew.Package{}, needs: map[string][]string{}, neededBy: map[string][]string{}}
	if snap == nil {
		return g
	}
	for _, p := range snap.Packages {
		g.pkgs[Short(p.Name)] = p
	}
	for _, p := range snap.Packages {
		name := Short(p.Name)
		for _, d := range p.Deps {
			d = Short(d)
			if _, ok := g.pkgs[d]; !ok {
				continue
			}
			g.needs[name] = append(g.needs[name], d)
			g.neededBy[d] = append(g.neededBy[d], name)
		}
	}
	for _, m := range []map[string][]string{g.needs, g.neededBy} {
		for k := range m {
			sort.Strings(m[k])
		}
	}
	return g
}

// Package looks up an installed package by full or short name.
func (g *Graph) Package(name string) (brew.Package, bool) {
	p, ok := g.pkgs[Short(name)]
	return p, ok
}

// NeededBy lists installed packages that depend on name directly.
func (g *Graph) NeededBy(name string) []string { return g.neededBy[Short(name)] }

// Reasons explains why a package is installed: for each package the user
// installed themselves that (indirectly) needs it, the shortest chain from
// that package down to this one. A package the user installed is its own
// reason. Sorted by chain length, then name.
func (g *Graph) Reasons(name string) [][]string {
	target := Short(name)
	if _, ok := g.pkgs[target]; !ok {
		return nil
	}
	// Breadth-first from the target up through "needed by", remembering
	// the step back down, gives the shortest chain to every root.
	next := map[string]string{target: ""}
	queue := []string{target}
	var roots []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if p := g.pkgs[cur]; p.OnRequest || p.Kind == "cask" {
			roots = append(roots, cur)
		}
		for _, up := range g.neededBy[cur] {
			if _, seen := next[up]; !seen {
				next[up] = cur
				queue = append(queue, up)
			}
		}
	}
	chains := make([][]string, 0, len(roots))
	for _, r := range roots {
		chain := []string{r}
		for c := next[r]; c != ""; c = next[c] {
			chain = append(chain, c)
		}
		chains = append(chains, chain)
	}
	sort.SliceStable(chains, func(i, j int) bool {
		if len(chains[i]) != len(chains[j]) {
			return len(chains[i]) < len(chains[j])
		}
		return chains[i][0] < chains[j][0]
	})
	return chains
}

// OnlyFor lists the dependencies that would no longer be needed by anything
// if name were removed: its whole dependency closure, minus whatever another
// remaining package (or the user's own request) still holds on to.
func (g *Graph) OnlyFor(name string) []string {
	target := Short(name)
	// Everything reachable from the target.
	closure := map[string]bool{}
	stack := append([]string(nil), g.needs[target]...)
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if closure[cur] || cur == target {
			continue
		}
		closure[cur] = true
		stack = append(stack, g.needs[cur]...)
	}
	// Everything still reachable from the other roots stays.
	kept := map[string]bool{}
	var walk func(string)
	walk = func(n string) {
		if kept[n] || n == target {
			return
		}
		kept[n] = true
		for _, d := range g.needs[n] {
			walk(d)
		}
	}
	for n, p := range g.pkgs {
		if n != target && (p.OnRequest || p.Kind == "cask") {
			walk(n)
		}
	}
	var out []string
	for n := range closure {
		if !kept[n] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Usage is the token count of one or more model calls.
type Usage struct {
	Input      int64 `json:"input"`      // uncached input
	CacheWrite int64 `json:"cacheWrite"` // input written to the cache
	CacheRead  int64 `json:"cacheRead"`  // input read from the cache
	Output     int64 `json:"output"`
}

// Add sums two usages.
func (u Usage) Add(o Usage) Usage {
	return Usage{u.Input + o.Input, u.CacheWrite + o.CacheWrite, u.CacheRead + o.CacheRead, u.Output + o.Output}
}

// Price is US dollars per million tokens.
type Price struct{ Input, CacheWrite, CacheRead, Output float64 }

// Cost of a usage at a price, in dollars.
func (u Usage) Cost(p Price) float64 {
	return (float64(u.Input)*p.Input + float64(u.CacheWrite)*p.CacheWrite + float64(u.CacheRead)*p.CacheRead + float64(u.Output)*p.Output) / 1e6
}
