import { useEffect, useState } from "react";
import { ArrowUpCircle, ExternalLink, Pin, PinOff, Trash2, X } from "lucide-react";
import { Button, CommandHint, PkgTags } from "./components";
import { formatBytes, formatDate, openURL, timeAgo } from "./lib/api";
import { useBrew } from "./state";

function DepChips({ names, empty }: { names: string[]; empty: string }) {
  const { byShort, select } = useBrew();
  if (names.length === 0) return <p className="quiet">{empty}</p>;
  return (
    <div className="chips">
      {names.map((n) => {
        const p = byShort.get(n);
        return (
          <button key={n} type="button" className="dep" disabled={!p} title={p?.desc} onClick={() => p && select(p.key)}>
            {n}
          </button>
        );
      })}
    </div>
  );
}

export function Drawer() {
  const { selected: p, select, sizes, usedBy, upgrade, setPinned, busy, confirming, uninstallTarget, requestUninstall } = useBrew();
  const [showAll, setShowAll] = useState(false);

  useEffect(() => setShowAll(false), [p?.key]);

  useEffect(() => {
    if (!p || confirming || uninstallTarget) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") select(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [p, select, confirming, uninstallTarget]);

  if (!p) return null;

  const dependents = p.kind === "formula" ? usedBy.get(p.short) ?? [] : [];
  const indirect = p.allDeps.filter((d) => !p.deps.includes(d));
  const isBusy = busy.has(p.key);
  const flag = p.kind === "cask" ? "--cask " : "";

  return (
    <aside className="drawer" aria-label={`${p.short} details`}>
      <header className="drawer-head">
        <div className="drawer-title">
          <h2>{p.short}</h2>
          <div className="drawer-tags">
            <PkgTags p={p} />
          </div>
        </div>
        <button type="button" className="icon-btn" aria-label="Close details" onClick={() => select(null)}>
          <X size={16} />
        </button>
      </header>

      <div className="drawer-scroll">
        {p.kind === "cask" && p.displayName !== p.name && <p className="drawer-display">{p.displayName}</p>}
        <p className="drawer-desc">{p.desc || "No description."}</p>
        {p.homepage && (
          <button type="button" className="link" onClick={() => openURL(p.homepage)}>
            {p.homepage.replace(/^https?:\/\//, "").replace(/\/$/, "")}
            <ExternalLink size={12} />
          </button>
        )}

        <dl className="meta">
          <div>
            <dt>Installed</dt>
            <dd className="ver">{p.version}</dd>
          </div>
          <div>
            <dt>Latest</dt>
            <dd className="ver">{p.latest || "—"}</dd>
          </div>
          <div>
            <dt>Size</dt>
            <dd>{sizes ? formatBytes(sizes[p.key] ?? 0) : "…"}</dd>
          </div>
          <div>
            <dt>Added</dt>
            <dd title={formatDate(p.installedAt)}>{timeAgo(p.installedAt)}</dd>
          </div>
          {p.license && (
            <div>
              <dt>License</dt>
              <dd>{p.license}</dd>
            </div>
          )}
          {p.tap && (
            <div>
              <dt>Source</dt>
              <dd>{p.tap}</dd>
            </div>
          )}
        </dl>

        <div className={`why ${p.onRequest ? "why-you" : ""}`}>
          {p.kind === "cask"
            ? "You installed this app."
            : p.onRequest
              ? "You installed this yourself."
              : dependents.length
                ? "Installed automatically, because other packages need it."
                : "Installed automatically, and nothing needs it any more."}
        </div>

        {p.outdated && p.pinned && <p className="note">Pinned at {p.version}. Unpin it to upgrade to {p.latest}.</p>}
        {p.kind === "cask" && p.autoUpdates && <p className="note">This app updates itself, so Homebrew doesn't upgrade it.</p>}

        {p.kind === "formula" && (
          <>
            <h3 className="sub">Needs</h3>
            <DepChips names={showAll ? p.allDeps : p.deps} empty="No dependencies." />
            {indirect.length > 0 && (
              <button type="button" className="more" onClick={() => setShowAll(!showAll)}>
                {showAll ? "Show direct only" : `Show ${indirect.length} indirect`}
              </button>
            )}

            <h3 className="sub">Needed by</h3>
            <DepChips names={dependents} empty="Nothing installed depends on this." />
          </>
        )}

        {p.caveats && (
          <>
            <h3 className="sub">Notes from Homebrew</h3>
            <pre className="caveats">{p.caveats.trim()}</pre>
          </>
        )}

        <h3 className="sub">In Terminal</h3>
        <CommandHint command={`brew info ${flag}${p.name}`} />
      </div>

      <footer className="drawer-foot">
        {p.outdated && !p.pinned && (
          <Button variant="primary" icon={<ArrowUpCircle size={15} />} busy={isBusy} onClick={() => upgrade([p])}>
            Upgrade to {p.latest}
          </Button>
        )}
        {p.kind === "formula" && (
          <Button icon={p.pinned ? <PinOff size={14} /> : <Pin size={14} />} disabled={isBusy} onClick={() => setPinned(p, !p.pinned)} title={p.pinned ? "Allow upgrades again" : "Keep this version; upgrades will skip it"}>
            {p.pinned ? "Unpin" : "Pin"}
          </Button>
        )}
        <span className="grow" />
        <Button
          variant="danger"
          icon={<Trash2 size={14} />}
          disabled={isBusy || dependents.length > 0}
          title={dependents.length ? `Needed by ${dependents.join(", ")}` : undefined}
          onClick={() => requestUninstall(p)}
        >
          Uninstall
        </Button>
      </footer>
      {dependents.length > 0 && <p className="drawer-warn">Can't uninstall while {dependents.length === 1 ? `${dependents[0]} needs` : `${dependents.length} packages need`} it.</p>}
    </aside>
  );
}
