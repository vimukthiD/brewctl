import { useEffect, useState } from "react";
import { Download, ExternalLink, KeyRound, X } from "lucide-react";
import { Button, CommandHint, JobProgress, Tag } from "./components";
import { Brew, errorMessage, openURL, plural, refKey, type Remote } from "./lib/api";
import { useBrew } from "./state";

/** Side panel for a catalogue package that isn't installed yet. */
export function RemoteDrawer() {
  const { remote: ref, showRemote, select, pkgs, byShort, busy, jobFor, install } = useBrew();
  const [info, setInfo] = useState<Remote | null>(null);
  const [error, setError] = useState<string | null>(null);
  const key = ref ? refKey(ref) : null;

  useEffect(() => {
    setInfo(null);
    setError(null);
    if (!ref) return;
    let live = true;
    Brew.Info({ name: ref.name, kind: ref.kind })
      .then((r) => {
        if (live && r) setInfo({ ...r, kind: ref.kind, deps: r.deps ?? [] });
      })
      .catch((e) => live && setError(errorMessage(e)));
    return () => {
      live = false;
    };
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps

  // Once it's installed, hand over to the regular details panel.
  const installedKey = key && pkgs.some((p) => p.key === key) ? key : null;
  useEffect(() => {
    if (installedKey) select(installedKey);
  }, [installedKey]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!ref) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") showRemote(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [ref, showRemote]);

  if (!ref || !key) return null;

  const flag = ref.kind === "cask" ? "--cask " : "";
  const isBusy = busy.has(key);
  const activeJob = jobFor(key);
  const missing = info ? info.deps.filter((d) => !byShort.has(d)) : [];
  const blocked = info?.disabled || info?.needsAdmin;

  return (
    <aside className="drawer" aria-label={`${ref.name} details`}>
      <header className="drawer-head">
        <div className="drawer-title">
          <h2>{ref.name}</h2>
          <div className="drawer-tags">
            {ref.kind === "cask" && <Tag tone="blue">app</Tag>}
            {info && (info.deprecated || info.disabled) && <Tag tone="red">{info.disabled ? "disabled" : "deprecated"}</Tag>}
          </div>
        </div>
        <button type="button" className="icon-btn" aria-label="Close details" onClick={() => showRemote(null)}>
          <X size={16} />
        </button>
      </header>

      <div className="drawer-scroll">
        {error ? (
          <p className="quiet">Couldn't load details: {error}</p>
        ) : !info ? (
          <div className="drawer-skel" aria-busy="true">
            <span className="skel" style={{ width: "80%" }} />
            <span className="skel" style={{ width: "45%" }} />
            <span className="skel skel-block" />
          </div>
        ) : (
          <>
            {info.kind === "cask" && info.displayName !== info.name && <p className="drawer-display">{info.displayName}</p>}
            <p className="drawer-desc">{info.desc || "No description."}</p>
            {info.homepage && (
              <button type="button" className="link" onClick={() => openURL(info.homepage)}>
                {info.homepage.replace(/^https?:\/\//, "").replace(/\/$/, "")}
                <ExternalLink size={12} />
              </button>
            )}

            <dl className="meta">
              <div>
                <dt>Version</dt>
                <dd className="ver">{info.version || "—"}</dd>
              </div>
              <div>
                <dt>Type</dt>
                <dd>{info.kind === "cask" ? "App" : "Command-line tool"}</dd>
              </div>
              {info.license && (
                <div>
                  <dt>License</dt>
                  <dd>{info.license}</dd>
                </div>
              )}
              {info.tap && (
                <div>
                  <dt>Source</dt>
                  <dd>{info.tap}</dd>
                </div>
              )}
            </dl>

            {info.disabled && <p className="note">Homebrew has disabled this package, so it can't be installed.</p>}
            {info.deprecated && !info.disabled && <p className="note">Homebrew has deprecated this package. It still installs, but it's on its way out.</p>}
            {info.kind === "cask" && info.autoUpdates && <p className="note">This app keeps itself up to date after installing.</p>}

            {info.needsAdmin && (
              <div className="callout callout-box">
                <KeyRound size={16} />
                <div>
                  <strong>Needs your admin password</strong>
                  <p>This app uses a system installer. Brewctl can't ask for your password yet, so install it from Terminal:</p>
                  <CommandHint command={`brew install ${flag}${info.name}`} />
                </div>
              </div>
            )}

            {info.kind === "formula" && (
              <>
                <h3 className="sub">Also installs</h3>
                {info.deps.length === 0 ? (
                  <p className="quiet">Nothing else. It has no dependencies.</p>
                ) : (
                  <>
                    <div className="chips">
                      {info.deps.map((d) => {
                        const have = byShort.get(d);
                        return (
                          <button key={d} type="button" className={`dep ${have ? "is-have" : ""}`} title={have ? "Already installed" : "Will be installed"} onClick={() => (have ? select(have.key) : showRemote({ name: d, kind: "formula" }))}>
                            {d}
                          </button>
                        );
                      })}
                    </div>
                    <p className="quiet dep-note">
                      {missing.length === 0
                        ? "You already have all of these."
                        : `Adds at least ${plural(missing.length, "more package")}${info.deps.length > missing.length ? `; you already have ${info.deps.length - missing.length} of these` : ""}.`}
                    </p>
                  </>
                )}
              </>
            )}

            {info.caveats && (
              <>
                <h3 className="sub">Notes from Homebrew</h3>
                <pre className="caveats">{info.caveats.trim()}</pre>
              </>
            )}

            {!info.needsAdmin && (
              <>
                <h3 className="sub">In Terminal</h3>
                <CommandHint command={`brew install ${flag}${info.name}`} />
              </>
            )}
          </>
        )}
      </div>

      {activeJob && <JobProgress job={activeJob} />}
      <footer className="drawer-foot">
        <span className="grow" />
        <Button variant="primary" icon={<Download size={15} />} busy={isBusy} disabled={!info || blocked} onClick={() => install(ref)}>
          {isBusy ? "Installing…" : "Install"}
        </Button>
      </footer>
    </aside>
  );
}
