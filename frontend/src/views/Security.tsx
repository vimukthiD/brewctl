import { useState } from "react";
import { ChevronDown, ExternalLink, ShieldAlert, ShieldCheck } from "lucide-react";
import { Button, CommandHint, Empty, Tag } from "../components";
import { openURL, plural, timeAgo, type Finding, type Vuln } from "../lib/api";
import { useBrew } from "../state";

const SEVERITY: Record<string, { label: string; tone: "red" | "accent" | "neutral" }> = {
  critical: { label: "critical", tone: "red" },
  high: { label: "high", tone: "red" },
  medium: { label: "medium", tone: "accent" },
  low: { label: "low", tone: "neutral" },
};

const isCommit = (s: string) => /^[0-9a-f]{40}$/i.test(s);

/** Where the fix stands, in words. */
function fixText(v: Vuln): string {
  const fixed = v.fixed ?? [];
  if (!fixed.length) return "No fix yet";
  const releases = fixed.filter((f) => !isCommit(f));
  if (releases.length) return `Fixed in ${releases.join(", ")}`;
  return "Fixed in the source code, not in a release yet";
}

const FIRST = 6;

function Row({ f }: { f: Finding }) {
  const { byShort, select, upgrade, busy } = useBrew();
  const [open, setOpen] = useState(false);
  const [all, setAll] = useState(false);

  const pkg = byShort.get(f.formula.slice(f.formula.lastIndexOf("/") + 1));
  const vulns = f.vulns ?? [];
  const fresh = vulns.filter((v) => v.new).length;
  const top = SEVERITY[vulns[0]?.severity ?? ""];
  const canUpgrade = !!pkg && pkg.outdated && !pkg.pinned;
  const shown = all ? vulns : vulns.slice(0, FIRST);

  return (
    <li className={`svc ${open ? "is-open" : ""}`}>
      <div className="svc-head">
        <button type="button" className="svc-toggle" aria-expanded={open} onClick={() => setOpen(!open)}>
          <ShieldAlert size={16} className="vuln-icon" aria-hidden />
          <span className="svc-main">
            <span className="svc-title">
              <span className="name">{f.formula}</span>
              <span className="ver">{f.version}</span>
              {fresh > 0 && <Tag tone="red">{fresh} new</Tag>}
              {top && <Tag tone={top.tone}>{top.label}</Tag>}
            </span>
            <span className="cell-desc">
              {plural(vulns.length, "known vulnerability", "known vulnerabilities")}
              {canUpgrade ? ` · update to ${pkg.latest} available` : ""}
            </span>
          </span>
          <ChevronDown size={14} className="svc-chev" />
        </button>
        {canUpgrade && (
          <div className="svc-actions">
            <Button size="sm" variant="primary" busy={busy.has(pkg.key)} onClick={() => upgrade([pkg])}>
              Upgrade
            </Button>
          </div>
        )}
      </div>

      {open && (
        <div className="svc-body">
          <p className="svc-desc">
            {canUpgrade
              ? `Homebrew has ${pkg.latest}. Upgrading may fix some of these; check again afterwards.`
              : pkg?.pinned && pkg.outdated
                ? `Pinned at ${pkg.version}. Unpin it to upgrade to ${pkg.latest}.`
                : "This is the newest version Homebrew has. Fixes arrive when the project releases them and Homebrew picks them up."}{" "}
            {pkg && (
              <button type="button" className="more more-inline" onClick={() => select(pkg.key)}>
                Package details
              </button>
            )}
          </p>
          <ul className="vulns">
            {shown.map((v) => {
              const sev = SEVERITY[v.severity];
              return (
                <li key={v.id}>
                  <button type="button" className="vuln-id" title="Open on osv.dev" onClick={() => openURL(`https://osv.dev/vulnerability/${encodeURIComponent(v.id)}`)}>
                    {v.id}
                    <ExternalLink size={11} aria-hidden />
                  </button>
                  <span className="vuln-text">
                    <span>
                      {v.summary || "No description."} {v.new && <Tag tone="red">new</Tag>} {sev && <Tag tone={sev.tone}>{sev.label}</Tag>}
                    </span>
                    <span className="quiet">{fixText(v)}</span>
                  </span>
                </li>
              );
            })}
          </ul>
          {vulns.length > FIRST && (
            <button type="button" className="more" onClick={() => setAll(!all)}>
              {all ? "Show fewer" : `Show all ${vulns.length}`}
            </button>
          )}
          {f.patched > 0 && <p className="quiet vuln-foot">{plural(f.patched, "more")} already fixed by Homebrew's own patches.</p>}
        </div>
      )}
    </li>
  );
}

function MonitorSwitch() {
  const { security, setSecurityMonitor } = useBrew();
  const on = !!security?.monitor;
  return (
    <button type="button" role="switch" aria-checked={on} className={`switch ${on ? "is-on" : ""}`} title="Checks once a day while Brewctl is open, and notifies you about new vulnerabilities" onClick={() => setSecurityMonitor(!on)}>
      <span className="switch-track" aria-hidden>
        <span className="switch-knob" />
      </span>
      <span>Check daily</span>
    </button>
  );
}

export function Security() {
  const { security, vulnFindings, scanSecurity, pkgs } = useBrew();
  const [showSkipped, setShowSkipped] = useState(false);

  if (security === null) {
    return (
      <div className="page" aria-busy="true">
        <div className="card is-skeleton" style={{ height: 58 }} />
      </div>
    );
  }

  const { report, scanning, error } = security;

  if (!report) {
    return (
      <div className="page">
        <Empty icon={<ShieldCheck size={28} />} title="Check your tools for known vulnerabilities">
          Homebrew looks up each installed tool in the public OSV.dev vulnerability database. The names and versions of your tools are sent to OSV.dev, so nothing is checked until you ask.
          {error && <pre className="caveats">{error}</pre>}
          <CommandHint command="brew vulns" />
          <div className="empty-actions">
            <Button variant="primary" busy={scanning} onClick={scanSecurity}>
              {scanning ? "Checking…" : "Check now"}
            </Button>
          </div>
          <div className="empty-actions">
            <MonitorSwitch />
          </div>
        </Empty>
      </div>
    );
  }

  const nVulns = vulnFindings.reduce((n, f) => n + (f.vulns ?? []).length, 0);
  const nNew = vulnFindings.reduce((n, f) => n + (f.vulns ?? []).filter((v) => v.new).length, 0);
  const skipped = report.skipped ?? [];
  const nCasks = pkgs.filter((p) => p.kind === "cask").length;

  return (
    <div className="page">
      <div className="bar">
        <div className="bar-text">
          <strong>
            {vulnFindings.length
              ? `${plural(vulnFindings.length, "tool has", "tools have")} known vulnerabilities`
              : "No known vulnerabilities found"}
          </strong>
          <span>
            {scanning ? "Checking now…" : `Checked ${timeAgo(report.checkedAt)}`}
            {vulnFindings.length ? ` · ${nVulns} in total` : ""}
            {nNew ? ` · ${nNew} new` : ""}
          </span>
        </div>
        <MonitorSwitch />
        <Button busy={scanning} onClick={scanSecurity}>
          Check now
        </Button>
      </div>

      {error && (
        <div className="banner" role="alert">
          The last check didn't finish: {error}
        </div>
      )}

      {vulnFindings.length > 0 ? (
        <ul className="svcs">
          {vulnFindings.map((f) => (
            <Row key={f.formula} f={f} />
          ))}
        </ul>
      ) : (
        <div className="allclear">
          <ShieldCheck size={18} />
          <div>
            <strong>Nothing reported for the {plural(report.checked, "tool")} that could be checked</strong>
            <span>That isn't proof they're safe, only that the database lists nothing for these versions.</span>
          </div>
        </div>
      )}

      <section className="card">
        <header className="card-head">
          <h2>What was checked</h2>
        </header>
        <ul className="facts">
          <li>
            <span>Tools checked</span>
            <b>{report.checked}</b>
          </li>
          <li>
            <span>Tools that couldn't be checked</span>
            <b>{skipped.length}</b>
          </li>
          <li>
            <span>Apps, which Homebrew doesn't check</span>
            <b>{nCasks}</b>
          </li>
          {report.patched > 0 && (
            <li>
              <span>Vulnerabilities already fixed by Homebrew's patches</span>
              <b>{report.patched}</b>
            </li>
          )}
        </ul>
        {skipped.length > 0 && (
          <>
            <p className="note">
              Homebrew has no source repository on record for {plural(skipped.length, "tool")}, so the database can't look {skipped.length === 1 ? "it" : "them"} up. Treat {skipped.length === 1 ? "it" : "them"} as unknown, not as safe.{" "}
              <button type="button" className="more more-inline" onClick={() => setShowSkipped(!showSkipped)}>
                {showSkipped ? "Hide the list" : "Show which"}
              </button>
            </p>
            {showSkipped && (
              <div className="chips">
                {skipped.map((n) => (
                  <span key={n} className="dep is-static">
                    {n}
                  </span>
                ))}
              </div>
            )}
          </>
        )}
        <p className="note">Most entries have no severity rating in the database, so read the description before deciding how much one matters to you. Data from OSV.dev.</p>
        <CommandHint command="brew vulns" />
      </section>
    </div>
  );
}
