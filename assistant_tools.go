package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"dev.vimukthid/brewctl/internal/assist"
	"dev.vimukthid/brewctl/internal/brew"
)

// tool is one function the assistant may call. Every tool here only reads;
// suggest_action shows a change for the user to approve, it doesn't run it.
type tool struct {
	name     string
	desc     string
	props    map[string]any
	required []string
	// label says what the call does, for the chat ("Looked up wget").
	label func(in toolInput) string
	run   func(a *AssistantService, ctx context.Context, in toolInput) (any, error)
}

// toolInput is the union of every tool's parameters.
type toolInput struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Query    string   `json:"query"`
	ID       int      `json:"id"`
	Action   string   `json:"action"`
	Packages []PkgRef `json:"packages"`
	Reason   string   `json:"reason"`

	// Set by callTool, not by the model.
	turn   int
	callID string
}

// actionVerbs are what suggest_action may propose, with the card's verb.
var actionVerbs = map[string]string{
	"install":         "Install",
	"upgrade":         "Upgrade",
	"uninstall":       "Uninstall",
	"start_service":   "Start",
	"stop_service":    "Stop",
	"restart_service": "Restart",
	"autoremove":      "Remove unneeded dependencies",
	"cleanup":         "Clean up old versions and downloads",
}

var errBadInput = errors.New("missing or invalid parameters")

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

var assistantTools = []tool{
	{
		name: "package_details",
		desc: "Look up one package: what it is, its installed and latest versions, its size on disk, " +
			"why it is installed (the chain from a package the user installed), what needs it, what would " +
			"become unneeded if it were removed, known vulnerabilities and its background service. Works for " +
			"packages that are not installed too (then it describes the catalogue entry). Call this whenever " +
			"the user asks about a specific package, whether it is safe to remove, or why it is there.",
		props: map[string]any{
			"name": str("Package name, e.g. \"wget\", \"openssl@3\" or \"firefox\"."),
			"kind": map[string]any{"type": "string", "enum": []string{"formula", "cask"}, "description": "Only needed when a formula and a cask share the name."},
		},
		required: []string{"name"},
		label:    func(in toolInput) string { return "Looked up " + in.Name },
		run:      (*AssistantService).packageDetails,
	},
	{
		name: "search_catalog",
		desc: "Search the Homebrew catalogue (formulae and apps) by name and description. Call this when the " +
			"user is looking for software to do something, or asks whether Homebrew has a package.",
		props:    map[string]any{"query": str("A few keywords, e.g. \"image resize\" or \"postgres gui\".")},
		required: []string{"query"},
		label:    func(in toolInput) string { return fmt.Sprintf("Searched the catalogue for “%s”", in.Query) },
		run: func(a *AssistantService, ctx context.Context, in toolInput) (any, error) {
			res, err := a.brew.Search(ctx, in.Query)
			if err != nil {
				return nil, err
			}
			if len(res) > 25 {
				res = res[:25]
			}
			refs := make([]PkgRef, 0, len(res))
			for _, r := range res {
				refs = append(refs, PkgRef{Name: r.Name, Kind: r.Kind})
			}
			a.send(in.turn, AssistantEvent{Kind: "refs", Refs: refs})
			return res, nil
		},
	},
	{
		name: "security_findings",
		desc: "Get the latest vulnerability scan of the installed formulae: which have known vulnerabilities, " +
			"their severity and whether a fixed version exists. Call this for any question about security, " +
			"CVEs or whether something is safe to keep.",
		props: map[string]any{},
		label: func(toolInput) string { return "Checked security findings" },
		run:   (*AssistantService).securityFindings,
	},
	{
		name: "list_services",
		desc: "List Homebrew background services and their state (running, stopped, error, starts at login). " +
			"Call this for questions about services, daemons, or what is running in the background.",
		props: map[string]any{},
		label: func(toolInput) string { return "Checked background services" },
		run: func(a *AssistantService, ctx context.Context, _ toolInput) (any, error) {
			return a.brew.Services(ctx)
		},
	},
	{
		name:     "service_log",
		desc:     "Read the end of a background service's log. Call this when a service stopped with an error or the user asks what it is doing.",
		props:    map[string]any{"name": str("The formula whose service to read, e.g. \"postgresql@17\".")},
		required: []string{"name"},
		label:    func(in toolInput) string { return "Read the log of " + in.Name },
		run: func(a *AssistantService, ctx context.Context, in toolInput) (any, error) {
			return a.brew.ServiceLog(ctx, in.Name)
		},
	},
	{
		name:  "recent_activity",
		desc:  "List the Homebrew commands Brewctl ran recently (installs, upgrades, cleanups) with their status. Call this when the user asks what happened, or about something that failed.",
		props: map[string]any{},
		label: func(toolInput) string { return "Checked recent activity" },
		run:   (*AssistantService).recentActivity,
	},
	{
		name:     "activity_output",
		desc:     "Read the output of one recent command by its id (from recent_activity). Call this to explain why a command failed.",
		props:    map[string]any{"id": map[string]any{"type": "integer", "description": "The command's id."}},
		required: []string{"id"},
		label:    func(in toolInput) string { return "Read the output of a recent command" },
		run:      (*AssistantService).activityOutput,
	},
	{
		name:  "cleanup_preview",
		desc:  "See what Homebrew's cleanup would remove: unneeded dependencies and old downloads, with the space freed. Call this when the user wants to free space or tidy up.",
		props: map[string]any{},
		label: func(toolInput) string { return "Checked what cleanup would remove" },
		run: func(a *AssistantService, ctx context.Context, _ toolInput) (any, error) {
			return a.brew.CleanupPreview(ctx)
		},
	},
	{
		name:  "disk_usage",
		desc:  "Measure disk space: the largest installed packages and Homebrew's download cache. Call this for questions about space or size. Takes a few seconds.",
		props: map[string]any{},
		label: func(toolInput) string { return "Measured disk usage" },
		run:   (*AssistantService).diskUsage,
	},
	{
		name: "suggest_action",
		desc: "Suggest a change for the user to approve. Brewctl shows it as a card with Approve and Decline; " +
			"nothing runs until the user approves, and you'll hear the outcome in a Brewctl note on their next " +
			"message. Actions: install (packages not installed yet), upgrade (outdated installed packages), " +
			"uninstall (one installed package nothing else needs), start_service / stop_service / restart_service " +
			"(one formula with a background service), autoremove (remove dependencies nothing needs any more) and " +
			"cleanup (delete old versions and cached downloads). The call fails with a reason if the change " +
			"doesn't fit this Mac's current state.",
		props: map[string]any{
			"action": map[string]any{"type": "string", "enum": []string{"install", "upgrade", "uninstall", "start_service", "stop_service", "restart_service", "autoremove", "cleanup"}},
			"packages": map[string]any{
				"type":        "array",
				"description": "The packages to act on (none for autoremove and cleanup). At most 10.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": str("Package name as Homebrew knows it."),
						"kind": map[string]any{"type": "string", "enum": []string{"formula", "cask"}, "description": "formula, or cask for Mac apps."},
					},
					"required": []string{"name"},
				},
			},
			"reason": str("One short sentence the user sees on the card: why this helps."),
		},
		required: []string{"action", "reason"},
		label:    func(in toolInput) string { return "Suggested a change" },
		run:      (*AssistantService).suggestAction,
	},
}

func findTool(name string) (tool, bool) {
	for _, t := range assistantTools {
		if t.name == name {
			return t, true
		}
	}
	return tool{}, false
}

// parseInput checks a tool call's input before anything runs: the model's
// JSON is streamed unvalidated, so it may be cut off or malformed.
func parseInput(t tool, raw []byte) (toolInput, error) {
	var in toolInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, errBadInput
	}
	for _, r := range t.required {
		switch r {
		case "name":
			if !brew.ValidName(in.Name) {
				return in, errBadInput
			}
		case "query":
			if strings.TrimSpace(in.Query) == "" {
				return in, errBadInput
			}
		case "id":
			if in.ID <= 0 {
				return in, errBadInput
			}
		case "action":
			if _, ok := actionVerbs[in.Action]; !ok {
				return in, errBadInput
			}
		case "reason":
			in.Reason = strings.TrimSpace(in.Reason)
			if in.Reason == "" {
				return in, errBadInput
			}
		}
	}
	if len(in.Packages) > 10 {
		return in, errBadInput
	}
	for _, p := range in.Packages {
		if !brew.ValidName(p.Name) || (p.Kind != "" && p.Kind != "formula" && p.Kind != "cask") {
			return in, errBadInput
		}
	}
	if in.Kind != "" && in.Kind != "formula" && in.Kind != "cask" {
		return in, errBadInput
	}
	return in, nil
}

func (a *AssistantService) snapshot(ctx context.Context) (*brew.Snapshot, error) {
	a.brew.mu.Lock()
	snap := a.brew.snap
	a.brew.mu.Unlock()
	if snap != nil {
		return snap, nil
	}
	return a.brew.Snapshot(ctx)
}

func (a *AssistantService) packageDetails(ctx context.Context, in toolInput) (any, error) {
	snap, err := a.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	g := assist.NewGraph(snap)
	p, ok := g.Package(in.Name)
	if ok && in.Kind != "" && p.Kind != in.Kind {
		ok = false
	}
	if !ok {
		// Not installed: describe the catalogue entry.
		kinds := []string{"formula", "cask"}
		if in.Kind != "" {
			kinds = []string{in.Kind}
		}
		for _, k := range kinds {
			if r, err := a.brew.Info(ctx, PkgRef{Name: in.Name, Kind: k}); err == nil && r != nil {
				a.send(in.turn, AssistantEvent{Kind: "refs", Refs: []PkgRef{{Name: r.Name, Kind: k}}})
				return map[string]any{"installed": false, "catalogue": r}, nil
			}
		}
		return nil, fmt.Errorf("no package called %q is installed or in the catalogue", in.Name)
	}

	out := map[string]any{
		"installed":    true,
		"name":         p.Name,
		"kind":         p.Kind,
		"display_name": p.DisplayName,
		"description":  p.Desc,
		"homepage":     p.Homepage,
		"version":      p.Version,
		"latest":       p.Latest,
		"outdated":     p.Outdated,
		"pinned":       p.Pinned,
		"installed_by": map[bool]string{true: "the user", false: "Homebrew, as a dependency"}[p.OnRequest || p.Kind == "cask"],
		"installed_at": time.Unix(p.InstalledAt, 0).Format("2006-01-02"),
		"depends_on":   p.Deps,
		"needed_by":    g.NeededBy(p.Name),
	}
	if p.AutoUpdates {
		out["updates_itself"] = true
		if p.AppVersion != "" {
			out["version_on_disk"] = p.AppVersion
		}
	}
	if p.Deprecated || p.Disabled {
		out["deprecated"] = true
	}
	if p.Caveats != "" {
		out["caveats"] = strings.TrimSpace(p.Caveats)
	}
	if reasons := g.Reasons(p.Name); len(reasons) > 0 {
		var lines []string
		for _, c := range reasons {
			lines = append(lines, strings.Join(c, " → "))
		}
		if len(lines) > 8 {
			lines = append(lines[:8], fmt.Sprintf("…and %d more", len(lines)-8))
		}
		out["why_installed"] = lines
	} else {
		// No package lists it, but brew may still keep it (a build
		// dependency, say); only autoremove's own answer settles it.
		out["why_installed"] = "It wasn't installed on request and no installed package lists it as a runtime dependency."
		if pv, err := a.brew.CleanupPreview(ctx); err == nil {
			orphan := false
			for _, o := range pv.Orphans {
				if assist.Short(o) == assist.Short(p.Name) {
					orphan = true
				}
			}
			if orphan {
				out["removed_by_cleanup"] = "Yes: Homebrew's cleanup (brew autoremove) would remove it as no longer needed."
			} else {
				out["removed_by_cleanup"] = "No: Homebrew's cleanup keeps it, so something may still need it (for example as a build dependency). Don't call it safe to remove without saying this."
			}
		}
	}
	if only := g.OnlyFor(p.Name); len(only) > 0 {
		out["unneeded_if_removed"] = only
	}
	if c, _, err := a.brew.ready(ctx); err == nil {
		sz := c.DiskUsage(ctx, &brew.Snapshot{Packages: []brew.Package{p}})
		for _, n := range sz.Packages {
			out["size"] = humanBytes(n)
		}
	}
	if p.Kind == "formula" {
		if st := a.brew.securityState(); st.Report != nil {
			for _, f := range st.Report.Findings {
				if f.Formula == assist.Short(p.Name) {
					out["vulnerabilities"] = f.Vulns
				}
			}
		}
		if sv, ok := a.brew.service(ctx, assist.Short(p.Name)); ok {
			out["service"] = map[string]any{"status": sv.Status, "running": sv.Running, "starts_at_login": sv.Registered}
		}
	}
	return out, nil
}

func (a *AssistantService) securityFindings(ctx context.Context, _ toolInput) (any, error) {
	st := a.brew.securityState()
	if st.Report == nil {
		return "No vulnerability scan has run yet. The user can run one from the Security view.", nil
	}
	r := st.Report
	type finding struct {
		Formula string      `json:"formula"`
		Version string      `json:"version"`
		Vulns   []brew.Vuln `json:"vulnerabilities"`
	}
	out := map[string]any{
		"checked_at":        time.Unix(r.CheckedAt, 0).Format("2006-01-02 15:04"),
		"formulae_checked":  r.Checked,
		"fixed_by_homebrew": r.Patched,
	}
	var fs []finding
	for _, f := range r.Findings {
		fs = append(fs, finding{f.Formula, f.Version, f.Vulns})
	}
	out["findings"] = fs
	if len(r.Skipped) > 0 {
		out["not_checkable"] = len(r.Skipped)
	}
	return out, nil
}

func (a *AssistantService) recentActivity(ctx context.Context, _ toolInput) (any, error) {
	jobs := a.brew.Jobs(ctx)
	type row struct {
		ID      int    `json:"id"`
		Title   string `json:"title"`
		Command string `json:"command"`
		Status  string `json:"status"`
		When    string `json:"when"`
	}
	out := []row{}
	for i := len(jobs) - 1; i >= 0 && len(out) < 20; i-- {
		j := jobs[i]
		t := j.QueuedAt
		if j.EndedAt > 0 {
			t = j.EndedAt
		}
		out = append(out, row{j.ID, j.Title, j.Command, j.Status, time.Unix(t, 0).Format("15:04")})
	}
	return out, nil
}

func (a *AssistantService) activityOutput(ctx context.Context, in toolInput) (any, error) {
	for _, j := range a.brew.Jobs(ctx) {
		if j.ID == in.ID {
			lines := j.Output
			if len(lines) > 150 {
				lines = append([]string{fmt.Sprintf("(%d earlier lines left out)", len(lines)-150)}, lines[len(lines)-150:]...)
			}
			return map[string]any{"title": j.Title, "command": j.Command, "status": j.Status, "output": strings.Join(lines, "\n")}, nil
		}
	}
	return nil, fmt.Errorf("no recent command with id %d", in.ID)
}

func (a *AssistantService) diskUsage(ctx context.Context, _ toolInput) (any, error) {
	sz, err := a.brew.Sizes(ctx)
	if err != nil {
		return nil, err
	}
	type row struct {
		Package string `json:"package"`
		Size    string `json:"size"`
		bytes   int64
	}
	var rows []row
	var total int64
	for k, n := range sz.Packages {
		total += n
		rows = append(rows, row{Package: k[strings.IndexByte(k, ':')+1:], Size: humanBytes(n), bytes: n})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].bytes > rows[j].bytes })
	if len(rows) > 20 {
		rows = rows[:20]
	}
	return map[string]any{"total_installed": humanBytes(total), "download_cache": humanBytes(sz.Cache), "largest": rows}, nil
}

func humanBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}
