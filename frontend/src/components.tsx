import { useEffect, useRef, useState, type ButtonHTMLAttributes, type ReactNode } from "react";
import { AlertTriangle, Check, Copy, Loader2, X } from "lucide-react";
import { copyText, duration, vulnLabel, type Job, type Pkg } from "./lib/api";
import { useBrew } from "./state";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "default" | "ghost" | "danger";
  size?: "sm" | "md";
  busy?: boolean;
  icon?: ReactNode;
};

export function Button({ variant = "default", size = "md", busy, icon, children, className, disabled, ...rest }: ButtonProps) {
  return (
    <button
      type="button"
      className={`btn btn-${variant} btn-${size} ${className ?? ""}`}
      disabled={disabled || busy}
      {...rest}
    >
      {busy ? <Loader2 className="spin" size={14} aria-hidden /> : icon}
      {children && <span>{children}</span>}
    </button>
  );
}

export function Tag({ tone = "neutral", children, title }: { tone?: "neutral" | "accent" | "green" | "red" | "blue"; children: ReactNode; title?: string }) {
  return (
    <span className={`tag tag-${tone}`} title={title}>
      {children}
    </span>
  );
}

export function Spinner({ size = 14 }: { size?: number }) {
  return <Loader2 className="spin" size={size} aria-label="Working" />;
}

/** A terminal command the user can copy: the app doubles as a brew cheat sheet. */
export function CommandHint({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="cmd">
      <span className="cmd-prompt" aria-hidden>
        $
      </span>
      <code>{command}</code>
      <button
        type="button"
        className="cmd-copy"
        aria-label="Copy command"
        title="Copy command"
        onClick={async () => {
          if (await copyText(command)) {
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1400);
          }
        }}
      >
        {copied ? <Check size={13} /> : <Copy size={13} />}
      </button>
    </div>
  );
}

export function Empty({ icon, title, children }: { icon: ReactNode; title: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <div className="empty-icon">{icon}</div>
      <div className="empty-title">{title}</div>
      {children && <div className="empty-body">{children}</div>}
    </div>
  );
}

export function PkgTags({ p }: { p: Pkg }) {
  const { vulnBy } = useBrew();
  const finding = p.kind === "formula" ? vulnBy.get(p.short) : undefined;
  return (
    <>
      {finding && (
        <Tag tone="red" title={`${vulnLabel(finding)} in this version`}>
          vulnerable
        </Tag>
      )}
      {p.kind === "cask" && <Tag tone="blue">app</Tag>}
      {p.outdated && <Tag tone="accent">update</Tag>}
      {p.pinned && <Tag title="Pinned: upgrades skip this package">pinned</Tag>}
      {(p.deprecated || p.disabled) && (
        <Tag tone="red" title="No longer maintained in Homebrew">
          {p.disabled ? "disabled" : "deprecated"}
        </Tag>
      )}
    </>
  );
}

/** Counts up to a number on first render and when it changes. */
export function useCountUp(target: number, ms = 600): number {
  const [value, setValue] = useState(target);
  const from = useRef(0);
  useEffect(() => {
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      setValue(target);
      from.current = target;
      return;
    }
    const start = performance.now();
    const a = from.current;
    let raf = 0;
    const tick = (t: number) => {
      const k = Math.min(1, (t - start) / ms);
      const eased = 1 - Math.pow(1 - k, 3);
      setValue(a + (target - a) * eased);
      if (k < 1) raf = requestAnimationFrame(tick);
      else from.current = target;
    };
    raf = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(raf);
      from.current = target;
    };
  }, [target, ms]);
  return value;
}

export function ConfirmDialog() {
  const { confirming } = useBrew();
  const okRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!confirming) return;
    okRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        confirming.resolve(false);
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [confirming]);
  if (!confirming) return null;
  return (
    <div className="scrim" onMouseDown={() => confirming.resolve(false)}>
      <div
        className="dialog"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="dlg-title"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className={`dialog-icon ${confirming.danger ? "is-danger" : ""}`}>
          <AlertTriangle size={20} />
        </div>
        <h2 id="dlg-title">{confirming.title}</h2>
        <div className="dialog-body">{confirming.body}</div>
        {confirming.command && <CommandHint command={confirming.command} />}
        <div className="dialog-actions">
          <Button onClick={() => confirming.resolve(false)}>Cancel</Button>
          <button
            ref={okRef}
            type="button"
            className={`btn btn-md ${confirming.danger ? "btn-danger-solid" : "btn-primary"}`}
            onClick={() => confirming.resolve(true)}
          >
            <span>{confirming.confirmLabel}</span>
          </button>
        </div>
      </div>
    </div>
  );
}

export function Toasts() {
  const { toasts, dismissToast, showJob } = useBrew();
  return (
    <div className="toasts" role="status" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`toast toast-${t.tone}`}>
          <span className="toast-dot" aria-hidden />
          <span className="toast-text">{t.text}</span>
          {t.jobId != null && t.tone === "error" && (
            <button
              type="button"
              className="toast-link"
              onClick={() => {
                showJob(t.jobId!);
                dismissToast(t.id);
              }}
            >
              View log
            </button>
          )}
          <button type="button" className="toast-x" aria-label="Dismiss" onClick={() => dismissToast(t.id)}>
            <X size={13} />
          </button>
        </div>
      ))}
    </div>
  );
}

const STEPS = ["Downloading", "Installing", "Finishing up"];

/** How far an install or upgrade has got, read from brew's own output. */
function stepOf(lines: string[]): number {
  let step = 0;
  for (const l of lines) {
    if (/^==> (Caveats|Summary|Cleanup|Running `brew cleanup)|^\u{1F37A}|successfully (installed|upgraded)/u.test(l)) step = Math.max(step, 2);
    else if (/^==> (Pouring|Installing|Upgrading|Moving|Linking|Running)/.test(l)) step = Math.max(step, 1);
  }
  return step;
}

/**
 * Live progress for the job acting on a package. Homebrew doesn't report a
 * percentage, so installs and upgrades show which stage they're in and other
 * jobs show a plain activity bar, always with the latest line of output.
 */
export function JobProgress({ job }: { job: Job }) {
  const { showJob, cancelJob } = useBrew();
  const [, setTick] = useState(0);
  useEffect(() => {
    const t = window.setInterval(() => setTick((n) => n + 1), 1000);
    return () => window.clearInterval(t);
  }, []);

  const queued = job.status === "queued";
  const staged = /^brew (install|upgrade) /.test(job.command);
  const step = staged ? stepOf(job.output) : 0;
  const last = job.output[job.output.length - 1];
  const label = queued ? "Waiting for another job to finish" : staged ? STEPS[step] : job.title;

  return (
    <div className="progress" role="status" aria-live="polite">
      <div className="progress-head">
        <strong>
          {label}
          {!queued && "…"}
        </strong>
        {!queued && <span className="progress-time">{duration(job)}</span>}
      </div>
      {staged ? (
        <div className="progress-steps" aria-hidden>
          {STEPS.map((s, i) => (
            <span key={s} className={queued ? "" : i < step ? "is-done" : i === step ? "is-now" : ""} />
          ))}
        </div>
      ) : (
        <div className="progress-steps" aria-hidden>
          <span className={queued ? "" : "is-now"} />
        </div>
      )}
      <div className="progress-line">{queued ? "Homebrew runs one change at a time." : last ?? "Starting…"}</div>
      <div className="progress-actions">
        <button type="button" className="more" onClick={() => showJob(job.id)}>
          Show full output
        </button>
        <button type="button" className="more more-quiet" onClick={() => cancelJob(job.id)}>
          {queued ? "Remove from queue" : "Stop"}
        </button>
      </div>
    </div>
  );
}
