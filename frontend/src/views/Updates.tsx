import { ArrowUpCircle, CheckCircle2, RefreshCw, Zap } from "lucide-react";
import { Button, ChainProgress, CommandHint, Empty, PkgTags, Spinner, Switch } from "../components";
import { atLatest, caskVer, isActive, isAutomatic, onDisk, timeAgo, updatedItself, type Pkg } from "../lib/api";
import { useBrew } from "../state";

const longDate = (unix: number) => new Date(unix * 1000).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });

const NO_AUTO_ENV = "BREWCTL_NO_AUTO_UPDATE";

/** Whether Brewctl refreshes the package lists on its own. */
function AutoUpdateSwitch() {
  const { prefs, setAutoUpdate } = useBrew();
  const forced = !!prefs?.autoUpdateForcedOff;
  return (
    <Switch
      on={(prefs?.autoUpdate ?? true) && !forced}
      onChange={setAutoUpdate}
      disabled={forced || !prefs}
      label="Check automatically"
      title={
        forced
          ? `Turned off because ${NO_AUTO_ENV} is set`
          : "Check for updates automatically: while Brewctl is open, it fetches Homebrew's package lists once they're more than 6 hours old"
      }
    />
  );
}

/** The up-to-date message: when the lists were checked, and when they will be again. */
function UpToDateNote() {
  const { status, prefs } = useBrew();
  const listsAt = status?.listsUpdatedAt ?? 0;
  const checked = listsAt ? `Package lists checked ${timeAgo(listsAt)}.` : "Homebrew hasn't fetched its package lists yet.";
  const next = prefs?.autoUpdateForcedOff
    ? `Automatic checks are off because ${NO_AUTO_ENV} is set.`
    : prefs?.autoUpdate ?? true
      ? "Brewctl checks again on its own once they're 6 hours old."
      : "Automatic checks are off, so use “Check for updates” to look again.";
  return (
    <>
      {checked} {next}
    </>
  );
}

/** When Homebrew's package lists were last checked, and when Brewctl last read what's installed. */
export function Freshness() {
  const { status, fetchedAt, jobs, prefs } = useBrew();
  const update = jobs.find((j) => j.command === "brew update" && j.status === "running");
  const listsAt = status?.listsUpdatedAt ?? 0;
  if (update) {
    return (
      <span className="fresh is-checking">
        <Spinner size={11} />
        {isAutomatic(update) ? "Package lists are old, checking Homebrew for new versions…" : "Checking Homebrew for new versions…"}
      </span>
    );
  }
  return (
    <span className="fresh">
      <span title={listsAt ? `Homebrew last fetched its package lists ${longDate(listsAt)}, from Brewctl or Terminal` : "Homebrew hasn't fetched its package lists on this Mac yet"}>
        {listsAt ? `Package lists checked ${timeAgo(listsAt)}` : "Package lists not checked yet"}
      </span>
      {fetchedAt ? (
        <>
          <span className="fresh-sep" aria-hidden>
            ·
          </span>
          <span title={`Brewctl read what's installed ${longDate(fetchedAt)}`}>Read from Homebrew {timeAgo(fetchedAt)}</span>
        </>
      ) : null}
      {prefs?.autoUpdateForcedOff && (
        <>
          <span className="fresh-sep" aria-hidden>
            ·
          </span>
          <span title={`Turned off because ${NO_AUTO_ENV} is set`}>Automatic checks off</span>
        </>
      )}
    </span>
  );
}

/** One click for the usual Terminal routine: update, upgrade, and optionally clean up. */
function UpdateEverything() {
  const { updateEverything, everything, updatingEverything, prefs, setCleanupAfterUpdate, showJob } = useBrew();
  const cleanup = prefs?.cleanupAfterUpdate ?? true;
  const failed = !updatingEverything ? everything.find((j) => j.status === "failed") : undefined;
  const command = `brew update && brew upgrade${cleanup ? " && brew cleanup" : ""}`;

  return (
    <section className="card everything">
      <header className="card-head">
        <div>
          <h2>Update everything</h2>
          <p className="card-sub">
            Gets the latest package lists, then upgrades everything that isn't pinned{cleanup ? " and clears old versions" : ""}. Each step runs only if the one before it worked.
          </p>
        </div>
        <div className="everything-actions">
          <label className="check" title="Runs brew cleanup after upgrading, removing old versions and stale downloads">
            <input type="checkbox" checked={cleanup} disabled={updatingEverything} onChange={(e) => setCleanupAfterUpdate(e.target.checked)} />
            Clean up afterwards
          </label>
          <Button variant="primary" icon={<Zap size={14} />} busy={updatingEverything} onClick={updateEverything}>
            Update everything
          </Button>
        </div>
      </header>
      {updatingEverything ? (
        <ChainProgress jobs={everything} />
      ) : (
        <>
          {failed && (
            <p className="everything-failed" role="alert">
              The last run stopped at “{failed.title}”, so the steps after it were skipped.{" "}
              <button type="button" className="md-link" onClick={() => showJob(failed.id)}>
                Show output
              </button>
            </p>
          )}
          <CommandHint command={command} />
        </>
      )}
    </section>
  );
}

export function Updates() {
  const { pkgs, upgrade, updateIndex, busy, select, jobs, loading, updatingEverything } = useBrew();

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
        disabled={updatingEverything}
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
          {!upgradable.length && outdated.length ? <span>{`${outdated.length} pinned package${outdated.length === 1 ? " is" : "s are"} held back`}</span> : null}
          <Freshness />
        </div>
        <AutoUpdateSwitch />
        <Button icon={<RefreshCw size={14} />} busy={checking} disabled={updatingEverything} onClick={updateIndex}>
          Check for updates
        </Button>
        {upgradable.length > 0 && (
          <Button icon={<ArrowUpCircle size={14} />} busy={anyBusy && !updatingEverything} disabled={updatingEverything} onClick={() => upgrade(upgradable)}>
            Upgrade all <span className="btn-count">{upgradable.length}</span>
          </Button>
        )}
      </div>

      <UpdateEverything />

      {!loading && outdated.length === 0 && (
        <Empty icon={<CheckCircle2 size={28} />} title="You're up to date">
          <UpToDateNote />
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
