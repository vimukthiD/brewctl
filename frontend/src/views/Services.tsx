import { useEffect, useState } from "react";
import { ChevronDown, Play, RotateCw, Server, Square } from "lucide-react";
import { Button, CommandHint, Empty, JobProgress, Tag } from "../components";
import { Brew, errorMessage, plural, type Service } from "../lib/api";
import { useBrew } from "../state";

const IDEAS = ["postgresql", "redis", "nginx", "mysql", "ollama"];

function describe(s: Service): { tone: "running" | "error" | "idle" | "scheduled"; text: string } {
  if (s.running) return { tone: "running", text: s.pid ? `Running · pid ${s.pid}` : "Running" };
  if (s.status === "error" || (s.exitCode != null && s.exitCode !== 0)) {
    return { tone: "error", text: `Stopped with an error${s.exitCode != null ? ` (exit code ${s.exitCode})` : ""}` };
  }
  if (s.status === "scheduled") return { tone: "scheduled", text: "Scheduled · runs on a timer" };
  if (s.registered) return { tone: "idle", text: "Not running · starts at next login" };
  return { tone: "idle", text: "Not running" };
}

function Row({ s }: { s: Service }) {
  const { byShort, select, serviceAction, busy, jobFor, confirm } = useBrew();
  const [open, setOpen] = useState(false);
  const [log, setLog] = useState<string | null>(null);
  const [logError, setLogError] = useState<string | null>(null);

  const pkg = byShort.get(s.name);
  const state = describe(s);
  const key = `service:${s.name}`;
  const working = busy.has(key);
  const job = jobFor(key);
  const isRoot = s.user === "root";
  const hasLog = !!(s.logPath || s.errorLog);

  useEffect(() => {
    if (!open || !hasLog) return;
    let live = true;
    setLogError(null);
    Brew.ServiceLog(s.name)
      .then((t) => live && setLog(t ?? ""))
      .catch((e) => live && setLogError(errorMessage(e)));
    return () => {
      live = false;
    };
  }, [open, hasLog, s.name, s.running, s.pid]);

  const stop = async () => {
    const ok = await confirm({
      title: `Stop ${s.name}?`,
      body: <>Anything using it will lose its connection. It also won't start by itself at login until you start it again.</>,
      confirmLabel: "Stop",
      danger: true,
      command: `brew services stop ${s.name}`,
    });
    if (ok) serviceAction(s.name, "stop");
  };

  return (
    <li className={`svc ${open ? "is-open" : ""}`}>
      <div className="svc-head">
        <button type="button" className="svc-toggle" aria-expanded={open} onClick={() => setOpen(!open)}>
          <span className={`svc-dot is-${state.tone}`} aria-hidden />
          <span className="svc-main">
            <span className="svc-title">
              <span className="name">{s.name}</span>
              {s.registered && <Tag title="Starts by itself when you log in">at login</Tag>}
              {isRoot && <Tag tone="blue" title="Runs as the system administrator">system</Tag>}
            </span>
            <span className="cell-desc">{state.text}</span>
          </span>
          <ChevronDown size={14} className="svc-chev" />
        </button>
        {!isRoot && (
          <div className="svc-actions">
            {s.running ? (
              <>
                <Button size="sm" icon={<RotateCw size={13} />} disabled={working} onClick={() => serviceAction(s.name, "restart")}>
                  Restart
                </Button>
                <Button size="sm" variant="danger" icon={<Square size={12} />} disabled={working} onClick={stop}>
                  Stop
                </Button>
              </>
            ) : (
              <Button size="sm" variant="primary" icon={<Play size={13} />} busy={working} onClick={() => serviceAction(s.name, "start")}>
                Start
              </Button>
            )}
          </div>
        )}
      </div>

      {job && <JobProgress job={job} />}

      {open && (
        <div className="svc-body">
          {pkg?.desc && (
            <p className="svc-desc">
              {pkg.desc}{" "}
              <button type="button" className="more more-inline" onClick={() => select(pkg.key)}>
                Package details
              </button>
            </p>
          )}
          {isRoot && <p className="note">This service runs as the system administrator, which Brewctl doesn't manage. Use Terminal:</p>}
          {isRoot && <CommandHint command={`sudo brew services restart ${s.name}`} />}

          {!isRoot && !s.running && (
            <div className="svc-alt">
              <button type="button" className="more" disabled={working} onClick={() => serviceAction(s.name, "run")}>
                Run now only
              </button>
              <span className="quiet">Starts it without adding it to login. “Start” does both.</span>
            </div>
          )}

          <dl className="svc-facts">
            {s.command && (
              <div>
                <dt>Runs</dt>
                <dd className="mono">{s.command}</dd>
              </div>
            )}
            {s.user && (
              <div>
                <dt>As</dt>
                <dd>{s.user}</dd>
              </div>
            )}
            {s.label && (
              <div>
                <dt>Label</dt>
                <dd className="mono">{s.label}</dd>
              </div>
            )}
            {hasLog && (
              <div>
                <dt>Log</dt>
                <dd className="mono">{s.logPath || s.errorLog}</dd>
              </div>
            )}
          </dl>

          {hasLog && (
            <>
              <h3 className="sub">Latest log</h3>
              {logError ? <p className="quiet">Couldn't read the log: {logError}</p> : <pre className="console svc-log">{log === null ? "Reading…" : log || "The log is empty."}</pre>}
            </>
          )}

          <h3 className="sub">In Terminal</h3>
          <CommandHint command={`brew services ${s.running ? "restart" : "start"} ${s.name}`} />
        </div>
      )}
    </li>
  );
}

export function Services({ onDiscover }: { onDiscover: (query: string) => void }) {
  const { services } = useBrew();

  if (services === null) {
    return (
      <div className="page" aria-busy="true">
        {Array.from({ length: 3 }, (_, i) => (
          <div key={i} className="card is-skeleton" style={{ height: 58 }} />
        ))}
      </div>
    );
  }

  if (services.length === 0) {
    return (
      <div className="page">
        <Empty icon={<Server size={26} />} title="No background services">
          Databases, web servers and similar tools show up here once installed, ready to start and stop.
          <div className="chips chips-center">
            {IDEAS.map((idea) => (
              <button key={idea} type="button" className="dep" onClick={() => onDiscover(idea)}>
                {idea}
              </button>
            ))}
          </div>
        </Empty>
      </div>
    );
  }

  const running = services.filter((s) => s.running).length;
  const failed = services.filter((s) => !s.running && (s.status === "error" || (s.exitCode != null && s.exitCode !== 0))).length;

  return (
    <div className="page">
      <div className="bar">
        <div className="bar-text">
          <strong>{running ? `${running} of ${plural(services.length, "service")} running` : `${plural(services.length, "service")}, none running`}</strong>
          <span>{failed ? `${plural(failed, "service")} stopped with an error` : "Services run in the background, even when Brewctl is closed."}</span>
        </div>
      </div>
      <ul className="svcs">
        {services.map((s) => (
          <Row key={s.name} s={s} />
        ))}
      </ul>
    </div>
  );
}
