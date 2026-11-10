import { CheckCircle2, RefreshCw } from "lucide-react";
import { Button, CommandHint, Empty, PkgTags } from "../components";
import { isActive, timeAgo, type Pkg } from "../lib/api";
import { useBrew } from "../state";

export function Updates() {
  const { pkgs, upgrade, updateIndex, busy, select, jobs, fetchedAt, loading } = useBrew();

  const outdated = pkgs.filter((p) => p.outdated);
  const upgradable = outdated.filter((p) => !p.pinned);
  // Apps that update themselves: brew deliberately leaves these alone.
  const selfUpdating = pkgs.filter((p) => p.kind === "cask" && p.autoUpdates && !p.outdated && p.latest && p.latest !== p.version);
  const checking = jobs.some((j) => isActive(j) && j.command === "brew update");
  const anyBusy = upgradable.some((p) => busy.has(p.key));

  const row = (p: Pkg, action: boolean) => (
    <li key={p.key} className="up-row">
      <button type="button" className="up-main" onClick={() => select(p.key)}>
        <span className="up-title">
          <span className="name">{p.short}</span>
          <PkgTags p={{ ...p, outdated: false }} />
        </span>
        <span className="cell-desc">{p.desc}</span>
      </button>
      <span className="ver up-ver">
        {p.version} <span className="arrow">→</span> <b>{p.latest}</b>
      </span>
      {action &&
        (p.pinned ? (
          <span className="up-note">Pinned</span>
        ) : (
          <Button size="sm" busy={busy.has(p.key)} onClick={() => upgrade([p])}>
            Upgrade
          </Button>
        ))}
    </li>
  );

  return (
    <div className="page">
      <div className="bar">
        <div className="bar-text">
          <strong>{loading ? "Checking…" : upgradable.length ? `${upgradable.length} update${upgradable.length === 1 ? "" : "s"} available` : outdated.length ? "Nothing to upgrade" : "Everything is up to date"}</strong>
          <span>
            {!upgradable.length && outdated.length ? `${outdated.length} pinned package${outdated.length === 1 ? " is" : "s are"} held back · ` : ""}
            {fetchedAt ? `Read from Homebrew ${timeAgo(fetchedAt)}` : ""}
          </span>
        </div>
        <Button icon={<RefreshCw size={14} />} busy={checking} onClick={updateIndex}>
          Check for updates
        </Button>
        {upgradable.length > 0 && (
          <Button variant="primary" busy={anyBusy} onClick={() => upgrade(upgradable)}>
            Upgrade all
          </Button>
        )}
      </div>

      {!loading && outdated.length === 0 && (
        <Empty icon={<CheckCircle2 size={28} />} title="You're up to date">
          “Check for updates” asks Homebrew for the latest package list.
          <CommandHint command="brew update && brew outdated" />
        </Empty>
      )}

      {outdated.length > 0 && (
        <section className="card card-flush">
          <ul className="up-list">{outdated.map((p) => row(p, true))}</ul>
        </section>
      )}

      {selfUpdating.length > 0 && (
        <>
          <h3 className="sub sub-page">Apps that update themselves</h3>
          <p className="note">These have a newer version in Homebrew, but they manage their own updates, so Homebrew leaves them alone.</p>
          <section className="card card-flush">
            <ul className="up-list">{selfUpdating.map((p) => row(p, false))}</ul>
          </section>
        </>
      )}
    </div>
  );
}
