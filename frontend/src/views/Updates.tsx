import { CheckCircle2, RefreshCw } from "lucide-react";
import { Button, CommandHint, Empty, PkgTags } from "../components";
import { atLatest, caskVer, isActive, onDisk, timeAgo, updatedItself, type Pkg } from "../lib/api";
import { useBrew } from "../state";

export function Updates() {
  const { pkgs, upgrade, updateIndex, busy, select, jobs, fetchedAt, loading } = useBrew();

  const outdated = pkgs.filter((p) => p.outdated);
  const upgradable = outdated.filter((p) => !p.pinned);
  // Apps that update themselves and already have: Homebrew's record is old,
  // but nothing needs doing, so they stay out of the update count.
  const selfUpdated = pkgs.filter(updatedItself);
  const checking = jobs.some((j) => isActive(j) && j.command === "brew update");
  const anyBusy = upgradable.some((p) => busy.has(p.key));

  const main = (p: Pkg) => (
    <button type="button" className="up-main" onClick={() => select(p.key)}>
      <span className="up-title">
        <span className="name">{p.short}</span>
        <PkgTags p={{ ...p, outdated: false }} />
      </span>
      <span className="cell-desc">{p.desc}</span>
    </button>
  );

  const row = (p: Pkg) => (
    <li key={p.key} className="up-row">
      {main(p)}
      <span className="ver up-ver" title={p.autoUpdates && p.appVersion && p.appVersion !== p.version ? `Homebrew's record says ${p.version}` : undefined}>
        {onDisk(p)} <span className="arrow">→</span> <b>{p.latest}</b>
      </span>
      {p.pinned ? (
        <span className="up-note">Pinned</span>
      ) : (
        <Button size="sm" busy={busy.has(p.key)} onClick={() => upgrade([p])}>
          Upgrade
        </Button>
      )}
    </li>
  );

  const selfRow = (p: Pkg) => (
    <li key={p.key} className="up-row">
      {main(p)}
      <span className="up-self">
        {p.appVersion ? (
          <>
            <span className={`up-state ${atLatest(p) ? "is-ok" : ""}`}>
              {atLatest(p) ? <CheckCircle2 size={13} aria-hidden /> : null}
              {atLatest(p) ? "Up to date on its own" : "Updated on its own"}
              <b className="ver">{p.appVersion}</b>
            </span>
            <span className="up-record">Homebrew's record: {caskVer(p.version)}</span>
          </>
        ) : (
          <>
            <span className="up-state">Can't find the app to check</span>
            <span className="up-record">Homebrew's record: {caskVer(p.version)}</span>
          </>
        )}
      </span>
      <Button
        size="sm"
        variant="ghost"
        busy={busy.has(p.key)}
        title={`Reinstall ${p.latest} through Homebrew so its record matches. Quit the app first.`}
        onClick={() => upgrade([p])}
      >
        Upgrade with Homebrew anyway
      </Button>
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
          <ul className="up-list">{outdated.map(row)}</ul>
        </section>
      )}

      {selfUpdated.length > 0 && (
        <>
          <h3 className="sub sub-page">
            Apps that update themselves <span className="sub-count">{selfUpdated.length}</span>
          </h3>
          <p className="note">
            These kept themselves up to date, so there's nothing to do. Homebrew still lists the version it installed; upgrading with Homebrew reinstalls the app to bring its record in line.
          </p>
          <section className="card card-flush">
            <ul className="up-list">{selfUpdated.map(selfRow)}</ul>
          </section>
        </>
      )}
    </div>
  );
}
