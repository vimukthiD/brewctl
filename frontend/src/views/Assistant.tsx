import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import {
  AlertCircle,
  ArrowUp,
  Ban,
  Brush,
  ChartColumn,
  Check,
  ChevronDown,
  CircleArrowUp,
  CircleCheck,
  CircleX,
  KeyRound,
  Loader2,
  MessageSquarePlus,
  PackageMinus,
  PackagePlus,
  Play,
  RotateCw,
  Sparkles,
  Square,
  X,
} from "lucide-react";
import { useAssistant, type ActionCard, type ChatItem, type ToolStep } from "../assistant";
import { Button, JobProgress } from "../components";
import { openURL } from "../lib/api";
import { Markdown } from "../Markdown";
import { useBrew } from "../state";

const STARTERS = ["What's taking up the most space?", "Is anything here vulnerable?", "What can I safely remove?", "Find me a good app for browsing SQLite databases"];

const money = (n: number) => (n === 0 ? "$0.00" : n < 0.01 ? "under 1¢" : `$${n.toFixed(2)}`);

export function AssistantView() {
  const a = useAssistant();
  const [settings, setSettings] = useState(false);

  if (!a.status) {
    return (
      <div className="chat-center">
        <Loader2 className="spin" size={18} />
      </div>
    );
  }
  if (!a.status.hasKey) return <KeySetup />;

  return (
    <div className="chat">
      <div className="chat-bar">
        <ModelPicker />
        {a.cost > 0 && <span className="chat-cost" title="What this conversation has cost on your Anthropic account so far (estimated from list prices)">This chat: {money(a.cost)}</span>}
        <span className="grow" />
        <Button size="sm" variant="ghost" icon={<MessageSquarePlus size={14} />} disabled={a.items.length === 0} onClick={a.newChat}>
          New chat
        </Button>
        <button type="button" className="icon-btn" aria-label="Usage and settings" title="Usage and settings" aria-expanded={settings} data-popover-toggle="usage" onClick={() => setSettings(!settings)}>
          <ChartColumn size={15} />
        </button>
        {settings && <UsagePopover onClose={() => setSettings(false)} />}
      </div>
      <Transcript />
      <Composer />
    </div>
  );
}

function Transcript() {
  const { items, ask } = useAssistant();
  const end = useRef<HTMLDivElement>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);

  // Follow the answer as it streams, unless the user scrolled up to read.
  useLayoutEffect(() => {
    if (pinned.current) end.current?.scrollIntoView({ block: "end" });
  }, [items]);

  if (items.length === 0) {
    return (
      <div className="chat-scroll">
        <div className="chat-hello">
          <div className="chat-hello-mark">
            <Sparkles size={22} />
          </div>
          <h2>Ask about your Homebrew</h2>
          <p>Claude can see what's installed and look up details, dependencies, security findings, services and recent activity. It can suggest installs, upgrades and other changes, but nothing runs until you approve it.</p>
          <div className="chat-starters">
            {STARTERS.map((s) => (
              <button key={s} type="button" className="chat-starter" onClick={() => void ask(s)}>
                {s}
              </button>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div
      className="chat-scroll"
      ref={scroller}
      onScroll={(e) => {
        const el = e.currentTarget;
        pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
      }}
    >
      <div className="chat-log">
        {items.map((it) => (it.role === "user" ? <UserMsg key={it.id} text={it.text} /> : <AssistantMsg key={it.id} it={it} />))}
        <div ref={end} />
      </div>
    </div>
  );
}

function UserMsg({ text }: { text: string }) {
  return (
    <div className="msg msg-user">
      <div className="bubble">{text}</div>
    </div>
  );
}

function Steps({ steps }: { steps: ToolStep[] }) {
  if (!steps.length) return null;
  return (
    <ul className="steps">
      {steps.map((s) => (
        <li key={s.id} className={s.failed ? "is-failed" : s.done ? "is-done" : ""}>
          {s.failed ? <AlertCircle size={12} /> : s.done ? <Check size={12} /> : <Loader2 size={12} className="spin" />}
          <span>{s.label}</span>
        </li>
      ))}
    </ul>
  );
}

function AssistantMsg({ it }: { it: Extract<ChatItem, { role: "assistant" }> }) {
  const { refs } = useAssistant();
  return (
    <div className="msg msg-ai">
      <div className="msg-avatar" aria-hidden>
        <Sparkles size={13} />
      </div>
      <div className="msg-body">
        <Steps steps={it.steps} />
        {it.text && <Markdown text={it.text} refs={refs} />}
        {it.cards.map((c) => (
          <ActionCardView key={c.id} c={c} />
        ))}
        {it.state === "working" && !it.text && (
          <div className="msg-thinking">
            <span className="dots" aria-hidden>
              <i />
              <i />
              <i />
            </span>
            {it.steps.some((s) => !s.done) ? "Looking things up…" : "Thinking…"}
          </div>
        )}
        {it.state === "writing" && <span className="caret" aria-hidden />}
        {it.state === "stopped" && <p className="msg-note">Stopped. This answer isn't kept in the conversation.</p>}
        {it.state === "refused" && <p className="msg-note">Claude declined to answer this one. Try rephrasing the question.</p>}
        {it.state === "truncated" && <p className="msg-note">The answer was too long and got cut off. Try asking for less at once.</p>}
        {it.state === "error" && (
          <p className="msg-error">
            <AlertCircle size={14} /> {it.error}
          </p>
        )}
      </div>
    </div>
  );
}

function Composer() {
  const { ask, busy, stop } = useAssistant();
  const [text, setText] = useState("");
  const ref = useRef<HTMLTextAreaElement>(null);

  useEffect(() => ref.current?.focus(), []);
  // Grow with the text, up to a limit.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
  }, [text]);

  const send = () => {
    if (!text.trim() || busy) return;
    void ask(text);
    setText("");
  };

  return (
    <div className="composer-wrap">
      <div className="composer">
        <textarea
          ref={ref}
          rows={1}
          value={text}
          placeholder="Ask about your packages…"
          aria-label="Message"
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault();
              send();
            }
          }}
        />
        {busy ? (
          <button type="button" className="composer-btn is-stop" aria-label="Stop" title="Stop" onClick={stop}>
            <Square size={12} fill="currentColor" />
          </button>
        ) : (
          <button type="button" className="composer-btn" aria-label="Send" title="Send  ↩" disabled={!text.trim()} onClick={send}>
            <ArrowUp size={16} />
          </button>
        )}
      </div>
      <p className="composer-hint">Claude can make mistakes. It only changes something when you approve a suggestion.</p>
    </div>
  );
}

function KeySetup() {
  const { saveKey } = useAssistant();
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const save = async () => {
    setBusy(true);
    setError("");
    try {
      await saveKey(key);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="chat-center">
      <div className="key-card card">
        <div className="chat-hello-mark">
          <Sparkles size={22} />
        </div>
        <h2>Ask Claude about your Homebrew</h2>
        <p className="key-lead">Ask why something is installed, what's safe to remove, what's taking space, or whether anything is vulnerable. Claude looks up the answer from your own Mac.</p>
        <form
          className="key-form"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <label className="key-field">
            <KeyRound size={14} aria-hidden />
            <input type="password" value={key} onChange={(e) => setKey(e.target.value)} placeholder="Anthropic API key (sk-ant-…)" aria-label="Anthropic API key" autoComplete="off" spellCheck={false} />
          </label>
          <Button variant="primary" busy={busy} disabled={!key.trim()} type="submit">
            {busy ? "Checking…" : "Save key"}
          </Button>
        </form>
        {error && (
          <p className="msg-error">
            <AlertCircle size={14} /> {error}
          </p>
        )}
        <p className="key-small">
          Don't have one?{" "}
          <button type="button" className="md-link" onClick={() => openURL("https://console.anthropic.com/settings/keys")}>
            Create a key in the Anthropic Console
          </button>
          . Usage is billed to your Anthropic account; a typical question costs a few cents.
        </p>
        <ul className="key-facts">
          <li>Your key is kept in the macOS Keychain, not in a file.</li>
          <li>Nothing is sent until you ask something. Then your question and the names and versions of your packages go to Anthropic.</li>
          <li>Conversations aren't saved; they're gone when you quit Brewctl.</li>
        </ul>
      </div>
    </div>
  );
}

/** Closes a popover on Escape or a click outside it (and outside its toggle). */
function useDismiss(ref: React.RefObject<HTMLElement | null>, toggle: string, onClose: () => void) {
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      const t = e.target as Element;
      if (ref.current && !ref.current.contains(t) && !t.closest?.(`[data-popover-toggle='${toggle}']`)) onClose();
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("mousedown", onDown);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [ref, toggle, onClose]);
}

const shortName = (name: string) => name.replace(/^Claude /, "");

function ModelPicker() {
  const { status, setModel, busy } = useAssistant();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const close = () => setOpen(false);
  useDismiss(ref, "model", close);
  const models = status?.models ?? [];
  const current = models.find((m) => m.id === status?.model);

  return (
    <div className="model-pick">
      <button type="button" className="model-btn" data-popover-toggle="model" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)}>
        <Sparkles size={13} aria-hidden />
        {current ? current.name : "Choose a model"}
        <ChevronDown size={13} aria-hidden />
      </button>
      {open && (
        <div className="popover model-menu" ref={ref} role="menu" aria-label="Model">
          {models.map((m) => {
            const on = m.id === status?.model;
            return (
              <button
                key={m.id}
                type="button"
                role="menuitemradio"
                aria-checked={on}
                className={`model-item${on ? " is-on" : ""}`}
                onClick={() => {
                  close();
                  if (!on) void setModel(m.id);
                }}
              >
                <span className="model-check">{on && <Check size={14} />}</span>
                <span className="model-text">
                  <strong>{shortName(m.name)}</strong>
                  <span>{m.blurb}</span>
                  <em>Typically {m.typical}</em>
                </span>
              </button>
            );
          })}
          <p className="model-note">{busy ? "The change applies from your next question." : "You can switch at any point; the conversation carries on."}</p>
        </div>
      )}
    </div>
  );
}

const ACTION_ICON: Record<ActionCard["action"], ReactNode> = {
  install: <PackagePlus size={15} />,
  upgrade: <CircleArrowUp size={15} />,
  uninstall: <PackageMinus size={15} />,
  start_service: <Play size={15} />,
  stop_service: <Square size={13} />,
  restart_service: <RotateCw size={15} />,
  autoremove: <Brush size={15} />,
  cleanup: <Brush size={15} />,
};

const APPROVE_LABEL: Record<ActionCard["action"], string> = {
  install: "Install",
  upgrade: "Upgrade",
  uninstall: "Uninstall…",
  start_service: "Start",
  stop_service: "Stop",
  restart_service: "Restart",
  autoremove: "Remove",
  cleanup: "Clean up",
};

function ActionCardView({ c }: { c: ActionCard }) {
  const { cardState, cardJobs, approve, decline } = useAssistant();
  const { byShort, pkgs, select, showRemote, showJob } = useBrew();
  const [starting, setStarting] = useState(false);
  const state = cardState(c);
  const jobs = cardJobs(c);
  const live = jobs.find((j) => j.status === "running") ?? jobs.find((j) => j.status === "queued");
  const failed = jobs.find((j) => j.status === "failed");
  const danger = c.action === "uninstall" || c.action === "autoremove";
  const many = c.packages.length > 1;

  const open = (p: ActionCard["packages"][number]) => {
    const installed = p.kind === "formula" ? byShort.get(p.name.slice(p.name.lastIndexOf("/") + 1)) : pkgs.find((x) => x.kind === "cask" && x.name === p.name);
    if (installed) select(installed.key);
    else showRemote({ name: p.name, kind: p.kind });
  };

  return (
    <div className={`action-card is-${state}`} role="group" aria-label={`Suggested: ${c.title}`}>
      <div className="ac-head">
        <span className="ac-icon" aria-hidden>
          {ACTION_ICON[c.action]}
        </span>
        <div className="ac-title">
          <span className="ac-kicker">Suggested change</span>
          <strong>{c.title}</strong>
        </div>
        <CardBadge state={state} />
      </div>
      <p className="ac-reason">{c.reason}</p>
      {c.packages.length > 0 && c.action !== "start_service" && c.action !== "stop_service" && c.action !== "restart_service" && (
        <ul className={`ac-pkgs${many ? " is-many" : ""}`}>
          {c.packages.map((p) => (
            <li key={`${p.kind}:${p.name}`}>
              <button type="button" className="ac-pkg" onClick={() => open(p)} title={`Show ${p.name}`}>
                {p.name}
              </button>
              {c.action === "upgrade" && p.latest ? (
                <span className="ac-ver">
                  {p.version} → {p.latest}
                </span>
              ) : p.version ? (
                <span className="ac-ver">{p.version}</span>
              ) : null}
              {p.kind === "cask" && <span className="ac-kind">app</span>}
              {p.desc && !many && <span className="ac-desc">{p.desc}</span>}
            </li>
          ))}
        </ul>
      )}
      <code className="ac-cmd">{c.command}</code>

      {state === "pending" && (
        <div className="ac-foot">
          <Button size="sm" variant="ghost" onClick={() => decline(c)}>
            Decline
          </Button>
          <Button
            size="sm"
            variant={danger ? "danger" : "primary"}
            busy={starting}
            onClick={async () => {
              setStarting(true);
              try {
                await approve(c);
              } finally {
                setStarting(false);
              }
            }}
          >
            {APPROVE_LABEL[c.action]}
          </Button>
        </div>
      )}
      {state === "confirming" && <p className="ac-status">Confirm in the uninstall window…</p>}
      {state === "running" && live && <JobProgress job={live} />}
      {state === "running" && !live && (
        <p className="ac-status">
          <Loader2 size={13} className="spin" /> Starting…
        </p>
      )}
      {(state === "done" || state === "failed" || state === "cancelled") && jobs.length > 0 && (
        <div className="ac-foot is-after">
          <span className="ac-status">{state === "done" ? "Done. Claude will know when you next write." : state === "failed" ? "It didn't work. Ask Claude what went wrong." : "Stopped before it finished."}</span>
          <button type="button" className="more" onClick={() => showJob((failed ?? jobs[jobs.length - 1]).id)}>
            Show output
          </button>
        </div>
      )}
    </div>
  );
}

function CardBadge({ state }: { state: ReturnType<ReturnType<typeof useAssistant>["cardState"]> }) {
  switch (state) {
    case "running":
      return (
        <span className="ac-badge is-running">
          <Loader2 size={12} className="spin" /> Running
        </span>
      );
    case "done":
      return (
        <span className="ac-badge is-done">
          <CircleCheck size={12} /> Done
        </span>
      );
    case "failed":
      return (
        <span className="ac-badge is-failed">
          <CircleX size={12} /> Failed
        </span>
      );
    case "cancelled":
      return (
        <span className="ac-badge">
          <Ban size={12} /> Stopped
        </span>
      );
    case "declined":
      return (
        <span className="ac-badge">
          <X size={12} /> Declined
        </span>
      );
    default:
      return null;
  }
}

const tokens = (n: number) => (n >= 1e6 ? `${(n / 1e6).toFixed(1)}M` : n >= 1e3 ? `${Math.round(n / 1e3)}k` : String(n));
const cents = (n: number) => (n === 0 ? "$0" : n < 0.01 ? "<1¢" : `$${n.toFixed(2)}`);
const dayLabel = (iso: string) => new Date(`${iso}T12:00:00`).toLocaleDateString(undefined, { month: "short", day: "numeric" });

function UsagePopover({ onClose }: { onClose: () => void }) {
  const { status, removeKey, usage, loadUsage, cost } = useAssistant();
  const ref = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<number | null>(null);
  useDismiss(ref, "usage", onClose);
  useEffect(() => {
    void loadUsage();
  }, [loadUsage, cost]);

  const days = usage?.days ?? [];
  const max = Math.max(...days.map((d) => d.cost), 0);
  const shown = hover !== null ? days[hover] : null;
  const byModel = usage?.byModel ?? [];

  return (
    <div className="popover usage-pop" ref={ref} role="dialog" aria-label="Assistant usage and settings">
      <div className="popover-head">
        <strong>Usage</strong>
        <button type="button" className="icon-btn" aria-label="Close" onClick={onClose}>
          <X size={14} />
        </button>
      </div>

      <div className="usage-tiles">
        <Tile label="Today" cost={usage?.today.cost} q={usage?.today.questions} />
        <Tile label="This month" cost={usage?.month.cost} q={usage?.month.questions} />
        <Tile label="All time" cost={usage?.all.cost} q={usage?.all.questions} />
      </div>

      <div className="usage-chart">
        <div className="usage-chart-head">
          <span>Last 30 days</span>
          <span className="usage-readout" aria-live="polite">
            {shown ? `${dayLabel(shown.date)}: ${cents(shown.cost)} · ${shown.questions} ${shown.questions === 1 ? "question" : "questions"}` : max > 0 ? `Busiest day ${cents(max)}` : "Nothing yet"}
          </span>
        </div>
        <div className="usage-bars" onMouseLeave={() => setHover(null)}>
          {days.map((d, i) => (
            <div
              key={d.date}
              className={`usage-bar${hover === i ? " is-hover" : ""}`}
              onMouseEnter={() => setHover(i)}
              title={`${dayLabel(d.date)}: ${cents(d.cost)}`}
              aria-label={`${dayLabel(d.date)}: ${cents(d.cost)}, ${d.questions} questions`}
            >
              <span style={{ height: d.cost > 0 && max > 0 ? `${Math.max(6, (d.cost / max) * 100)}%` : 0 }} />
            </div>
          ))}
        </div>
        <div className="usage-axis" aria-hidden>
          <span>{days[0] ? dayLabel(days[0].date) : ""}</span>
          <span>Today</span>
        </div>
      </div>

      {byModel.length > 0 && (
        <div className="usage-models">
          <span className="usage-sub">This month by model</span>
          <table>
            <tbody>
              {byModel.map((m) => (
                <tr key={m.model}>
                  <th>{shortName(m.name)}</th>
                  <td>{m.questions} q</td>
                  <td title="Tokens sent to the model (including cached) / written by it">
                    {tokens(m.inputTokens)} in · {tokens(m.outputTokens)} out
                  </td>
                  <td className="usage-cost">{cents(m.cost)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <p className="usage-note">
        Estimated from list prices; your bill is in the{" "}
        <button type="button" className="md-link" onClick={() => openURL("https://console.anthropic.com/settings/usage")}>
          Anthropic Console
        </button>
        .{usage?.since ? ` Counting since ${dayLabel(usage.since)}.` : ""}
      </p>

      <div className="usage-key">
        <span>
          <KeyRound size={13} aria-hidden /> {status?.keySource === "environment" ? "Key from ANTHROPIC_API_KEY" : "Key in the macOS Keychain"}
        </span>
        {status?.keySource === "keychain" && (
          <Button variant="danger" size="sm" onClick={() => void removeKey().then(onClose)}>
            Remove key
          </Button>
        )}
      </div>
    </div>
  );
}

function Tile({ label, cost, q }: { label: string; cost?: number; q?: number }) {
  return (
    <div className="usage-tile">
      <span>{label}</span>
      <strong>{cost === undefined ? "–" : cents(cost)}</strong>
      <em>{q === undefined ? "" : `${q} ${q === 1 ? "question" : "questions"}`}</em>
    </div>
  );
}
