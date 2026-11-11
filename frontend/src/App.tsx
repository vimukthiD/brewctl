import { useEffect, useRef, useState } from "react";
import { ArrowUpCircle, Beer, Compass, LayoutDashboard, Server, Package, RefreshCw, Search, ShieldAlert, Sparkles, TerminalSquare, X } from "lucide-react";
import { Button, CommandHint, ConfirmDialog, Spinner, Toasts } from "./components";
import { DepMap } from "./DepMap";
import { Drawer } from "./Drawer";
import { UninstallDialog } from "./UninstallDialog";
import { isActive, openURL, timeAgo } from "./lib/api";
import { useBrew, type View } from "./state";
import { Activity } from "./views/Activity";
import { Cleanup } from "./views/Cleanup";
import { Discover } from "./views/Discover";
import { Security } from "./views/Security";
import { Services } from "./views/Services";
import { RemoteDrawer } from "./RemoteDrawer";
import { Installed } from "./views/Installed";
import { Overview } from "./views/Overview";
import { Updates } from "./views/Updates";

const NAV: { id: View; label: string; icon: typeof Package }[] = [
  { id: "overview", label: "Overview", icon: LayoutDashboard },
  { id: "installed", label: "Installed", icon: Package },
  { id: "discover", label: "Discover", icon: Compass },
  { id: "updates", label: "Updates", icon: ArrowUpCircle },
  { id: "security", label: "Security", icon: ShieldAlert },
  { id: "services", label: "Services", icon: Server },
  { id: "cleanup", label: "Cleanup", icon: Sparkles },
  { id: "activity", label: "Activity", icon: TerminalSquare },
];

const TITLES: Record<View, string> = {
  overview: "Overview",
  installed: "Installed",
  discover: "Discover",
  updates: "Updates",
  security: "Security",
  services: "Services",
  cleanup: "Cleanup",
  activity: "Activity",
};

export default function App() {
  const b = useBrew();
  const [installedQuery, setInstalledQuery] = useState("");
  const [discoverQuery, setDiscoverQuery] = useState("");
  const searching = b.view === "discover" ? "discover" : "installed";
  const query = searching === "discover" ? discoverQuery : installedQuery;
  const setQuery = searching === "discover" ? setDiscoverQuery : setInstalledQuery;
  const searchRef = useRef<HTMLInputElement>(null);
  const [, setTick] = useState(0);

  // Keep "updated 3 minutes ago" honest.
  useEffect(() => {
    const t = window.setInterval(() => setTick((n) => n + 1), 30000);
    return () => window.clearInterval(t);
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.altKey) return;
      const k = e.key.toLowerCase();
      if (k === "k" || k === "f") {
        e.preventDefault();
        if (b.view !== "discover") b.setView("installed");
        requestAnimationFrame(() => searchRef.current?.select());
      } else if (k === "r") {
        e.preventDefault();
        b.refresh();
      } else if (k >= "1" && k <= "8") {
        e.preventDefault();
        b.setView(NAV[Number(k) - 1].id);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [b]);

  const nUpdates = b.pkgs.filter((p) => p.outdated && !p.pinned).length;
  const nRunning = (b.services ?? []).filter((s) => s.running).length;
  const nNewVulns = b.vulnFindings.reduce((n, f) => n + (f.vulns ?? []).filter((v) => v.new).length, 0);
  const active = b.jobs.filter(isActive);
  const running = b.jobs.find((j) => j.status === "running");
  const lastLine = running?.output[running.output.length - 1];

  const badge = (id: View) => {
    if (id === "installed" && b.pkgs.length) return <span className="nav-n">{b.pkgs.length}</span>;
    if (id === "updates" && nUpdates) return <span className="nav-n is-accent">{nUpdates}</span>;
    if (id === "security" && b.vulnFindings.length) return <span className="nav-n is-alert" title={nNewVulns ? `${nNewVulns} new` : undefined}>{nNewVulns ? `${nNewVulns} new` : b.vulnFindings.length}</span>;
    if (id === "services" && nRunning) return <span className="nav-n is-live">{nRunning}</span>;
    if (id === "activity" && active.length) return <Spinner size={13} />;
    return null;
  };

  const missing = b.status && !b.status.found;

  return (
    <div className="app">
      <nav className="sidebar" aria-label="Sections">
        <div className="brand">
          <span className="brand-mark">
            <Beer size={16} strokeWidth={2.2} />
          </span>
          <span className="brand-name">brewctl</span>
        </div>

        <ul className="nav">
          {NAV.map(({ id, label, icon: Icon }, i) => (
            <li key={id}>
              <button type="button" className={`nav-item ${b.view === id ? "is-on" : ""}`} aria-current={b.view === id ? "page" : undefined} title={`${label}  ⌘${i + 1}`} onClick={() => b.setView(id)}>
                <Icon size={16} />
                <span>{label}</span>
                {badge(id)}
              </button>
            </li>
          ))}
        </ul>

        <div className="sidebar-foot">
          {running && (
            <button type="button" className="now" onClick={() => b.showJob(running.id)}>
              <span className="now-head">
                <Spinner size={12} />
                <span>{running.title}</span>
                {active.length > 1 && <span className="now-more">+{active.length - 1}</span>}
              </span>
              <span className="now-line">{lastLine ?? "Starting…"}</span>
              <span className="progress-steps" aria-hidden>
                <span className="is-now" />
              </span>
            </button>
          )}
          {b.status?.found && (
            <div className="env">
              <span className="env-dot" aria-hidden />
              <span>Homebrew {b.status.version}</span>
            </div>
          )}
          <button type="button" className="site" onClick={() => openURL("https://vimukthid.dev")}>
            vimukthid.dev
          </button>
        </div>
      </nav>

      <main className="main">
        <div className="main-col">
        <header className="topbar">
          <h1>{TITLES[b.view]}</h1>
          <span className="grow" />
          {(b.view === "installed" || b.view === "discover") && (
            <label className="search">
              <Search size={14} aria-hidden />
              <input
                ref={searchRef}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Escape") {
                    e.stopPropagation();
                    if (query) setQuery("");
                    else e.currentTarget.blur();
                  }
                }}
                placeholder={searching === "discover" ? "Search Homebrew" : "Search installed"}
                aria-label={searching === "discover" ? "Search all Homebrew packages" : "Search installed packages"}
                spellCheck={false}
                autoComplete="off"
              />
              {query ? (
                <button type="button" className="search-x" aria-label="Clear search" onClick={() => setQuery("")}>
                  <X size={12} />
                </button>
              ) : (
                <kbd>⌘K</kbd>
              )}
            </label>
          )}
          {b.fetchedAt && !b.refreshing && <span className="stamp">Updated {timeAgo(b.fetchedAt)}</span>}
          <button type="button" className="icon-btn" aria-label="Refresh" title="Refresh  ⌘R" disabled={b.refreshing} onClick={() => b.refresh()}>
            <RefreshCw size={15} className={b.refreshing ? "spin" : ""} />
          </button>
        </header>

        <div className="content">
          {missing ? (
            <div className="page">
              <div className="empty">
                <div className="empty-icon">
                  <Beer size={28} />
                </div>
                <div className="empty-title">Homebrew isn't installed</div>
                <div className="empty-body">
                  Brewctl looked in /opt/homebrew and /usr/local and found nothing. Install Homebrew from Terminal, then try again.
                  <CommandHint command={'/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"'} />
                  <div className="empty-actions">
                    <Button onClick={() => openURL("https://brew.sh")}>Open brew.sh</Button>
                    <Button variant="primary" onClick={() => b.refresh()}>
                      Try again
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          ) : b.error && b.pkgs.length === 0 ? (
            <div className="page">
              <div className="empty">
                <div className="empty-title">Couldn't read from Homebrew</div>
                <div className="empty-body">
                  <pre className="caveats">{b.error}</pre>
                  <div className="empty-actions">
                    <Button variant="primary" onClick={() => b.refresh()}>
                      Try again
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          ) : (
            <>
              {b.error && (
                <div className="banner" role="alert">
                  Couldn't refresh: {b.error}
                </div>
              )}
              {b.view === "overview" && <Overview />}
              {b.view === "installed" && <Installed query={installedQuery} />}
              {b.view === "discover" && <Discover query={discoverQuery} setQuery={setDiscoverQuery} focusSearch={() => searchRef.current?.focus()} />}
              {b.view === "updates" && <Updates />}
              {b.view === "security" && <Security />}
              {b.view === "services" && (
                <Services
                  onDiscover={(q) => {
                    setDiscoverQuery(q);
                    b.setView("discover");
                  }}
                />
              )}
              {b.view === "cleanup" && <Cleanup />}
              {b.view === "activity" && <Activity />}
            </>
          )}
        </div>
        </div>
        <Drawer />
        <RemoteDrawer />
        <DepMap />
      </main>

      <Toasts />
      <ConfirmDialog />
      {b.uninstallTarget && <UninstallDialog key={b.uninstallTarget.key} p={b.uninstallTarget} />}
    </div>
  );
}
