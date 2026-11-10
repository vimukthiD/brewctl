import { useMemo } from "react";
import { ArrowUpCircle, CheckCircle2, ChevronRight, Stethoscope } from "lucide-react";
import { Button, Spinner, useCountUp } from "../components";
import { formatBytes, type Pkg } from "../lib/api";
import { useBrew } from "../state";

function Stat({ label, value, hint, tone, onClick }: { label: string; value: string; hint: string; tone?: "accent" | "green"; onClick?: () => void }) {
  return (
    <button type="button" className={`stat ${tone ? `stat-${tone}` : ""}`} onClick={onClick}>
      <span className="stat-label">{label}</span>
      <span className="stat-value">{value}</span>
      <span className="stat-hint">{hint}</span>
    </button>
  );
}

function CountStat(props: { label: string; n: number; hint: string; tone?: "accent" | "green"; onClick?: () => void }) {
  const v = useCountUp(props.n);
  return <Stat {...props} value={Math.round(v).toLocaleString()} />;
}

export function Overview() {
  const { pkgs, sizes, cacheSize, cleanup, setView, select, upgrade, busy, doctor, loading } = useBrew();

  const groups = useMemo(() => {
    const requested = pkgs.filter((p) => p.kind === "formula" && p.onRequest);
    const deps = pkgs.filter((p) => p.kind === "formula" && !p.onRequest);
    const casks = pkgs.filter((p) => p.kind === "cask");
    const outdated = pkgs.filter((p) => p.outdated && !p.pinned);
    return { requested, deps, casks, outdated };
  }, [pkgs]);

  const sum = (list: Pkg[]) => (sizes ? list.reduce((n, p) => n + (sizes[p.key] ?? 0), 0) : null);
  const bytes = {
    requested: sum(groups.requested),
    deps: sum(groups.deps),
    casks: sum(groups.casks),
    cache: cacheSize,
  };
  const total = sizes ? (bytes.requested ?? 0) + (bytes.deps ?? 0) + (bytes.casks ?? 0) + (bytes.cache ?? 0) : null;

  const segments = [
    { id: "req", label: "Installed by you", n: bytes.requested },
    { id: "dep", label: "Dependencies", n: bytes.deps },
    { id: "app", label: "Apps", n: bytes.casks },
    { id: "cache", label: "Download cache", n: bytes.cache },
  ];

  const largest = useMemo(() => {
    if (!sizes) return [];
    return pkgs
      .map((p) => ({ p, n: sizes[p.key] ?? 0 }))
      .sort((a, b) => b.n - a.n)
      .slice(0, 8);
  }, [pkgs, sizes]);
  const maxSize = largest[0]?.n || 1;

  const nUpdates = groups.outdated.length;
  const orphans = cleanup?.orphans.length ?? 0;
  const anyBusy = groups.outdated.some((p) => busy.has(p.key));

  if (loading) return <OverviewSkeleton />;

  return (
    <div className="page">
      <div className="stats">
        <CountStat label="Installed by you" n={groups.requested.length + groups.casks.length} hint={`${groups.requested.length} tools · ${groups.casks.length} ${groups.casks.length === 1 ? "app" : "apps"}`} onClick={() => setView("installed")} />
        <CountStat label="Dependencies" n={groups.deps.length} hint="pulled in automatically" onClick={() => setView("installed")} />
        <CountStat
          label="Updates"
          n={nUpdates}
          hint={nUpdates ? "ready to install" : "everything is current"}
          tone={nUpdates ? "accent" : "green"}
          onClick={() => setView("updates")}
        />
        <Stat label="Disk used" value={formatBytes(total)} hint={total == null ? "measuring…" : "packages + cache"} onClick={() => setView("cleanup")} />
      </div>

      <div className="grid-2">
        <section className="card">
          <header className="card-head">
            <h2>Disk usage</h2>
            <span className="card-meta">{total == null ? <Spinner size={13} /> : formatBytes(total)}</span>
          </header>
          <div className="strip" role="img" aria-label="Disk usage by category">
            {segments.map(
              (s) =>
                total != null &&
                (s.n ?? 0) > 0 && <span key={s.id} className={`strip-seg seg-${s.id}`} style={{ flexGrow: s.n ?? 0 }} title={`${s.label}: ${formatBytes(s.n)}`} />,
            )}
          </div>
          <ul className="legend">
            {segments.map((s) => (
              <li key={s.id}>
                <span className={`legend-dot seg-${s.id}`} aria-hidden />
                <span className="legend-label">{s.label}</span>
                <span className="legend-value">{formatBytes(s.n)}</span>
              </li>
            ))}
          </ul>

          <h3 className="sub">Largest packages</h3>
          <ul className="rank">
            {largest.length === 0 &&
              Array.from({ length: 6 }, (_, i) => (
                <li key={i} className="rank-row is-skeleton">
                  <span className="skel" style={{ width: `${70 - i * 8}%` }} />
                </li>
              ))}
            {largest.map(({ p, n }) => (
              <li key={p.key}>
                <button type="button" className="rank-row" onClick={() => select(p.key)}>
                  <span className="rank-name">{p.short}</span>
                  <span className="rank-bar">
                    <span className={p.kind === "cask" ? "seg-app" : p.onRequest ? "seg-req" : "seg-dep"} style={{ width: `${Math.max(2, (n / maxSize) * 100)}%` }} />
                  </span>
                  <span className="rank-size">{formatBytes(n)}</span>
                </button>
              </li>
            ))}
          </ul>
        </section>

        <div className="stack">
          <section className="card">
            <header className="card-head">
              <h2>Updates</h2>
              {nUpdates > 0 && (
                <Button size="sm" variant="primary" busy={anyBusy} onClick={() => upgrade(groups.outdated)}>
                  Upgrade all
                </Button>
              )}
            </header>
            {nUpdates === 0 ? (
              <div className="allclear">
                <CheckCircle2 size={18} />
                <div>
                  <strong>You're up to date</strong>
                  <span>Check again from the Updates tab.</span>
                </div>
              </div>
            ) : (
              <ul className="mini-list">
                {groups.outdated.slice(0, 5).map((p) => (
                  <li key={p.key}>
                    <button type="button" className="mini-row" onClick={() => select(p.key)}>
                      <ArrowUpCircle size={15} className="mini-icon" />
                      <span className="mini-name">{p.short}</span>
                      <span className="ver">
                        {p.version} <span className="arrow">→</span> <b>{p.latest}</b>
                      </span>
                    </button>
                  </li>
                ))}
                {nUpdates > 5 && (
                  <li>
                    <button type="button" className="more" onClick={() => setView("updates")}>
                      {nUpdates - 5} more <ChevronRight size={13} />
                    </button>
                  </li>
                )}
              </ul>
            )}
          </section>

          <section className="card">
            <header className="card-head">
              <h2>Maintenance</h2>
            </header>
            <ul className="facts">
              <li>
                <span>Unused dependencies</span>
                <b>{cleanup ? orphans : "…"}</b>
              </li>
              <li>
                <span>Old versions to clear</span>
                <b>{cleanup ? cleanup.summary || "None" : "…"}</b>
              </li>
              <li>
                <span>Download cache</span>
                <b>{formatBytes(cacheSize)}</b>
              </li>
            </ul>
            <div className="card-actions">
              <Button size="sm" onClick={() => setView("cleanup")}>
                Review cleanup
              </Button>
              <Button size="sm" variant="ghost" icon={<Stethoscope size={14} />} onClick={doctor}>
                Run health check
              </Button>
            </div>
          </section>
        </div>
      </div>
    </div>
  );
}

function OverviewSkeleton() {
  return (
    <div className="page" aria-busy="true">
      <div className="stats">
        {Array.from({ length: 4 }, (_, i) => (
          <div key={i} className="stat is-skeleton">
            <span className="skel" style={{ width: "50%" }} />
            <span className="skel skel-lg" style={{ width: "35%" }} />
            <span className="skel" style={{ width: "65%" }} />
          </div>
        ))}
      </div>
      <div className="grid-2">
        <div className="card is-skeleton" style={{ height: 380 }} />
        <div className="stack">
          <div className="card is-skeleton" style={{ height: 150 }} />
          <div className="card is-skeleton" style={{ height: 200 }} />
        </div>
      </div>
    </div>
  );
}
