import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { CheckCircle2, CircleSlash, Clock, KeyRound, TerminalSquare, XCircle } from "lucide-react";
import { Button, CommandHint, Empty, Spinner } from "../components";
import { duration, isActive, sudoMessage, timeAgo, type Job } from "../lib/api";
import { useBrew } from "../state";

function StatusIcon({ j }: { j: Job }) {
  switch (j.status) {
    case "running":
      return <Spinner size={15} />;
    case "queued":
      return <Clock size={15} className="st-queued" />;
    case "done":
      return <CheckCircle2 size={15} className="st-done" />;
    case "failed":
      return <XCircle size={15} className="st-failed" />;
    default:
      return <CircleSlash size={15} className="st-queued" />;
  }
}

const STATUS_TEXT: Record<string, string> = {
  queued: "Waiting",
  running: "Running",
  done: "Finished",
  failed: "Failed",
  cancelled: "Cancelled",
};

function Console({ lines, live }: { lines: string[]; live: boolean }) {
  const ref = useRef<HTMLPreElement>(null);
  const stick = useRef(true);
  useLayoutEffect(() => {
    const el = ref.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [lines.length]);
  return (
    <pre
      ref={ref}
      className="console"
      tabIndex={0}
      onScroll={(e) => {
        const el = e.currentTarget;
        stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
      }}
    >
      {lines.length === 0 ? <span className="console-dim">{live ? "Waiting for output…" : "No output."}</span> : lines.map((l, i) => <div key={i} className={lineClass(l)}>{l || " "}</div>)}
    </pre>
  );
}

function lineClass(l: string): string {
  if (l.startsWith("==>")) return "ln-head";
  if (l.startsWith("Error") || l.startsWith("error:")) return "ln-err";
  if (l.startsWith("Warning")) return "ln-warn";
  return "";
}

export function Activity() {
  const { jobs, focusJob, cancelJob } = useBrew();
  const ordered = [...jobs].reverse();
  const [openId, setOpenId] = useState<number | null>(focusJob ?? ordered[0]?.id ?? null);
  const [, setTick] = useState(0);

  useEffect(() => {
    if (focusJob != null) setOpenId(focusJob);
  }, [focusJob]);

  // Follow the job that just started unless the user picked one.
  const runningId = jobs.find((j) => j.status === "running")?.id;
  useEffect(() => {
    if (runningId != null && focusJob == null) setOpenId(runningId);
  }, [runningId, focusJob]);

  const anyActive = jobs.some(isActive);
  useEffect(() => {
    if (!anyActive) return;
    const t = window.setInterval(() => setTick((n) => n + 1), 1000);
    return () => window.clearInterval(t);
  }, [anyActive]);

  if (jobs.length === 0) {
    return (
      <div className="page">
        <Empty icon={<TerminalSquare size={26} />} title="No activity yet">
          Upgrades, removals and cleanups show up here with Homebrew's full output.
        </Empty>
      </div>
    );
  }

  return (
    <div className="page">
      <ul className="jobs">
        {ordered.map((j) => {
          const open = openId === j.id;
          return (
            <li key={j.id} className={`job ${open ? "is-open" : ""} job-${j.status}`}>
              <div className="job-head">
                <button type="button" className="job-toggle" aria-expanded={open} onClick={() => setOpenId(open ? null : j.id)}>
                  <StatusIcon j={j} />
                  <span className="job-title">{j.title}</span>
                  <code className="job-cmd">{j.command}</code>
                  <span className="job-meta">
                    {STATUS_TEXT[j.status] ?? j.status}
                    {j.startedAt ? ` · ${duration(j)}` : ""}
                    {j.endedAt ? ` · ${timeAgo(j.endedAt)}` : ""}
                  </span>
                </button>
                {isActive(j) && (
                  <Button size="sm" variant="ghost" onClick={() => cancelJob(j.id)}>
                    {j.status === "queued" ? "Remove" : "Stop"}
                  </Button>
                )}
              </div>
              {open && (
                <div className="job-body">
                  {j.needsSudo && j.status === "failed" && (
                    <div className="callout">
                      <KeyRound size={16} />
                      <div>
                        <strong>{sudoMessage(j).title}</strong>
                        <p>{sudoMessage(j).body}</p>
                        <CommandHint command={j.command} />
                      </div>
                    </div>
                  )}
                  <Console lines={j.output} live={isActive(j)} />
                </div>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
