package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dev.vimukthid/brewctl/internal/assist"
	"dev.vimukthid/brewctl/internal/brew"
)

// suggestAction checks a suggested change against this Mac's current state
// and, if it fits, shows it to the user as a card. It never runs anything:
// the card's Approve button does, through the same jobs as the rest of the
// app. A suggestion that doesn't fit comes back to Claude as an error.
func (a *AssistantService) suggestAction(ctx context.Context, in toolInput) (any, error) {
	// Read fresh: the conversation may have changed things since it began.
	snap, err := a.brew.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	g := assist.NewGraph(snap)
	installed := func(p PkgRef) (brew.Package, bool) {
		pkg, ok := g.Package(p.Name)
		if ok && p.Kind != "" && pkg.Kind != p.Kind {
			return brew.Package{}, false
		}
		return pkg, ok
	}

	card := ActionCard{ID: in.callID, Action: in.Action, Reason: in.Reason}
	needs := func(min, max int) error {
		n := len(in.Packages)
		switch {
		case n < min && min == 1:
			return errors.New("name the package to act on")
		case n > max && max == 0:
			return fmt.Errorf("%s doesn't take packages", in.Action)
		case n > max && max == 1:
			return fmt.Errorf("%s takes one package per card", in.Action)
		}
		return nil
	}

	switch in.Action {
	case "install":
		if err := needs(1, 10); err != nil {
			return nil, err
		}
		for _, p := range in.Packages {
			if pkg, ok := installed(p); ok {
				return nil, fmt.Errorf("%s is already installed (version %s)", pkg.Name, pkg.Version)
			}
			r, kind := a.lookup(ctx, p)
			if r == nil {
				return nil, fmt.Errorf("there's no package called %q in the Homebrew catalogue", p.Name)
			}
			if r.Disabled {
				return nil, fmt.Errorf("%s is disabled in Homebrew and can't be installed", r.Name)
			}
			card.Packages = append(card.Packages, ActionPkg{Name: r.Name, Kind: kind, Version: r.Version, Desc: r.Desc})
		}
		refs := make([]PkgRef, 0, len(card.Packages))
		for _, p := range card.Packages {
			refs = append(refs, PkgRef{Name: p.Name, Kind: p.Kind})
		}
		a.send(in.turn, AssistantEvent{Kind: "refs", Refs: refs})

	case "upgrade":
		if err := needs(1, 10); err != nil {
			return nil, err
		}
		for _, p := range in.Packages {
			pkg, ok := installed(p)
			switch {
			case !ok:
				return nil, fmt.Errorf("%s isn't installed", p.Name)
			case pkg.Pinned:
				return nil, fmt.Errorf("%s is pinned, so Homebrew won't upgrade it; it has to be unpinned first (in its details panel)", pkg.Name)
			case !pkg.Outdated:
				return nil, fmt.Errorf("%s is already up to date (%s)", pkg.Name, pkg.Version)
			}
			card.Packages = append(card.Packages, ActionPkg{Name: pkg.Name, Kind: pkg.Kind, Version: pkg.Version, Latest: pkg.Latest, Desc: pkg.Desc})
		}

	case "uninstall":
		if err := needs(1, 1); err != nil {
			return nil, err
		}
		pkg, ok := installed(in.Packages[0])
		if !ok {
			return nil, fmt.Errorf("%s isn't installed", in.Packages[0].Name)
		}
		if users := g.NeededBy(pkg.Name); len(users) > 0 {
			return nil, fmt.Errorf("can't uninstall %s: %s still need it", pkg.Name, strings.Join(users, ", "))
		}
		card.Packages = []ActionPkg{{Name: pkg.Name, Kind: pkg.Kind, Version: pkg.Version, Desc: pkg.Desc}}

	case "start_service", "stop_service", "restart_service":
		if err := needs(1, 1); err != nil {
			return nil, err
		}
		name := assist.Short(in.Packages[0].Name)
		sv, ok := a.brew.service(ctx, name)
		if !ok {
			return nil, fmt.Errorf("%s has no background service", name)
		}
		switch {
		case in.Action == "start_service" && sv.Running:
			return nil, fmt.Errorf("%s is already running", name)
		case in.Action == "stop_service" && !sv.Running && !sv.Registered:
			return nil, fmt.Errorf("%s isn't running or set to start at login", name)
		}
		card.Packages = []ActionPkg{{Name: name, Kind: "formula"}}

	case "autoremove":
		if err := needs(0, 0); err != nil {
			return nil, err
		}
		pv, err := a.brew.CleanupPreview(ctx)
		if err != nil {
			return nil, err
		}
		if len(pv.Orphans) == 0 {
			return nil, errors.New("there are no unneeded dependencies to remove")
		}
		for _, o := range pv.Orphans {
			card.Packages = append(card.Packages, ActionPkg{Name: o, Kind: "formula"})
		}

	case "cleanup":
		if err := needs(0, 0); err != nil {
			return nil, err
		}
	}

	card.Title, card.Command = describeAction(in.Action, card.Packages)
	a.send(in.turn, AssistantEvent{Kind: "action", Action: &card})
	return "Shown to the user as a card with Approve and Decline. Nothing has run yet. Their decision, and the " +
		"outcome if they approve, will come in a Brewctl note with their next message. Don't suggest the same " +
		"change again, and don't describe it as done.", nil
}

// lookup finds a catalogue entry, trying formula then cask unless the kind
// is given.
func (a *AssistantService) lookup(ctx context.Context, p PkgRef) (*brew.Remote, string) {
	kinds := []string{"formula", "cask"}
	if p.Kind != "" {
		kinds = []string{p.Kind}
	}
	for _, k := range kinds {
		if r, err := a.brew.Info(ctx, PkgRef{Name: p.Name, Kind: k}); err == nil && r != nil {
			return r, k
		}
	}
	return nil, ""
}

// describeAction gives a card its title and the equivalent Terminal command.
func describeAction(action string, pkgs []ActionPkg) (title, command string) {
	names := make([]string, len(pkgs))
	for i, p := range pkgs {
		names[i] = assist.Short(p.Name)
	}
	byKind := func(verb string) string {
		var lines []string
		for _, kind := range []string{"formula", "cask"} {
			var ns []string
			for _, p := range pkgs {
				if p.Kind == kind {
					ns = append(ns, p.Name)
				}
			}
			if len(ns) == 0 {
				continue
			}
			flag := ""
			if kind == "cask" {
				flag = " --cask"
			}
			lines = append(lines, "brew "+verb+flag+" "+strings.Join(ns, " "))
		}
		return strings.Join(lines, "\n")
	}
	switch action {
	case "install", "upgrade", "uninstall":
		return actionVerbs[action] + " " + listNames(names), byKind(action)
	case "start_service", "stop_service", "restart_service":
		verb := strings.TrimSuffix(action, "_service")
		return actionVerbs[action] + " the " + names[0] + " service", "brew services " + verb + " " + names[0]
	case "autoremove":
		return actionVerbs[action], "brew autoremove"
	default:
		return actionVerbs[action], "brew cleanup"
	}
}

// listNames joins names for a title: "a", "a and b", "a, b and 3 more".
func listNames(names []string) string {
	switch n := len(names); {
	case n == 0:
		return ""
	case n == 1:
		return names[0]
	case n <= 3:
		return strings.Join(names[:n-1], ", ") + " and " + names[n-1]
	default:
		return strings.Join(names[:2], ", ") + fmt.Sprintf(" and %d more", n-2)
	}
}
