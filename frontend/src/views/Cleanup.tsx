import { Button, CommandHint, Spinner } from "../components";
import { formatBytes, isActive } from "../lib/api";
import { useBrew } from "../state";

export function Cleanup() {
  const { cleanup, cacheSize, autoremove, cleanupNow, confirm, jobs, byShort, select } = useBrew();

  const running = (cmd: string) => jobs.some((j) => isActive(j) && j.command.startsWith(cmd));
  const orphans = cleanup?.orphans ?? [];
  const items = cleanup?.items ?? [];

  const doAutoremove = async () => {
    const ok = await confirm({
      title: `Remove ${orphans.length} unused ${orphans.length === 1 ? "dependency" : "dependencies"}?`,
      body: (
        <>
          Nothing you installed needs {orphans.length === 1 ? "it" : "them"} any more: <b>{orphans.join(", ")}</b>.
        </>
      ),
      confirmLabel: "Remove",
      danger: true,
      command: "brew autoremove",
    });
    if (ok) autoremove();
  };

  const doCleanup = async () => {
    const ok = await confirm({
      title: "Clear old versions?",
      body: <>Deletes outdated versions of packages and stale downloads{cleanup?.summary ? `, freeing about ${cleanup.summary}` : ""}. Your current versions are untouched.</>,
      confirmLabel: "Clean up",
      command: "brew cleanup",
    });
    if (ok) cleanupNow(false);
  };

  const doDeep = async () => {
    const ok = await confirm({
      title: "Empty the download cache?",
      body: <>Deletes every cached download ({formatBytes(cacheSize)}) along with old versions. Nothing installed is removed; Homebrew downloads again when it needs to.</>,
      confirmLabel: "Empty cache",
      danger: true,
      command: "brew cleanup --prune=all -s",
    });
    if (ok) cleanupNow(true);
  };

  return (
    <div className="page">
      <section className="card">
        <header className="card-head">
          <div>
            <h2>Unused dependencies</h2>
            <p className="card-sub">Packages that were installed for something you've since removed.</p>
          </div>
          <Button variant={orphans.length ? "primary" : "default"} disabled={!orphans.length} busy={running("brew autoremove")} onClick={doAutoremove}>
            Remove {orphans.length || ""}
          </Button>
        </header>
        {!cleanup ? (
          <div className="pending">
            <Spinner /> Asking Homebrew…
          </div>
        ) : orphans.length === 0 ? (
          <p className="quiet">None. Every dependency is still needed.</p>
        ) : (
          <div className="chips">
            {orphans.map((o) => {
              const p = byShort.get(o);
              return (
                <button key={o} type="button" className="dep" disabled={!p} onClick={() => p && select(p.key)}>
                  {o}
                </button>
              );
            })}
          </div>
        )}
      </section>

      <section className="card">
        <header className="card-head">
          <div>
            <h2>Old versions and stale downloads</h2>
            <p className="card-sub">Leftovers from upgrades that Homebrew no longer uses.</p>
          </div>
          <Button variant={items.length ? "primary" : "default"} disabled={!items.length} busy={running("brew cleanup") && !running("brew cleanup --prune")} onClick={doCleanup}>
            {cleanup?.summary ? `Free ${cleanup.summary}` : "Clean up"}
          </Button>
        </header>
        {!cleanup ? (
          <div className="pending">
            <Spinner /> Asking Homebrew…
          </div>
        ) : items.length === 0 ? (
          <p className="quiet">Nothing to clear.</p>
        ) : (
          <ul className="paths">
            {items.slice(0, 40).map((it) => (
              <li key={it.path}>
                <span className="path" title={it.path}>
                  {it.path}
                </span>
                <span className="path-size">{it.size}</span>
              </li>
            ))}
            {items.length > 40 && <li className="quiet">…and {items.length - 40} more</li>}
          </ul>
        )}
      </section>

      <section className="card">
        <header className="card-head">
          <div>
            <h2>Download cache</h2>
            <p className="card-sub">
              Installers Homebrew keeps in case it needs them again: <b>{formatBytes(cacheSize)}</b>.
            </p>
          </div>
          <Button variant="danger" disabled={!cacheSize} busy={running("brew cleanup --prune")} onClick={doDeep}>
            Empty cache
          </Button>
        </header>
        <CommandHint command="brew cleanup --prune=all -s" />
      </section>
    </div>
  );
}
