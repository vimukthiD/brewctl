import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Events } from "@wailsio/runtime";
import * as Assistant from "../bindings/dev.vimukthid/brewctl/assistantservice";
import type { AssistantStatus } from "../bindings/dev.vimukthid/brewctl/models";
import type { UsageSummary } from "../bindings/dev.vimukthid/brewctl/internal/assist/models";
import { isActive, type Job, type Ref } from "./lib/api";
import { useBrew } from "./state";

/** A change Claude suggests; it runs only when the user approves it. */
export interface ActionCard {
  id: string;
  action: "install" | "upgrade" | "uninstall" | "start_service" | "stop_service" | "restart_service" | "autoremove" | "cleanup";
  title: string;
  packages: { name: string; kind: "formula" | "cask"; version?: string; latest?: string; desc?: string }[];
  reason: string;
  command: string;
}

/** One streamed piece of an answer, as AssistantService sends it. */
interface AssistantEvent {
  turn: number;
  kind: "text" | "tool" | "toolDone" | "done" | "error" | "action" | "refs";
  text?: string;
  tool?: string;
  label?: string;
  failed?: boolean;
  stop?: "end_turn" | "stopped" | "refusal" | "max_tokens";
  cost?: number;
  action?: ActionCard;
  refs?: Ref[];
}

export interface ToolStep {
  id: string;
  label: string;
  done: boolean;
  failed: boolean;
}

export type ChatItem =
  | { role: "user"; id: string; text: string }
  | {
      role: "assistant";
      id: string;
      turn: number;
      text: string;
      steps: ToolStep[];
      cards: ActionCard[];
      /** working: thinking or using tools; writing: text is arriving. */
      state: "working" | "writing" | "done" | "stopped" | "refused" | "truncated" | "error";
      error?: string;
    };

/** Where a card stands. Job-backed states come from the jobs themselves. */
export type CardState = "pending" | "confirming" | "running" | "done" | "failed" | "cancelled" | "declined";

interface Decision {
  /** Set once approved; jobs fill in as they start. */
  jobs: number[];
  declined?: boolean;
  /** The uninstall dialog is open for this card. */
  confirming?: boolean;
  /** The final outcome has been told to Claude. */
  reported?: boolean;
}

interface AssistantCtx {
  status: AssistantStatus | null;
  items: ChatItem[];
  busy: boolean;
  cost: number;
  /** Catalogue packages the conversation has come across, by name. */
  refs: Map<string, Ref>;
  ask: (text: string) => Promise<void>;
  stop: () => void;
  newChat: () => void;
  saveKey: (key: string) => Promise<void>;
  removeKey: () => Promise<void>;
  setModel: (id: string) => Promise<void>;
  usage: UsageSummary | null;
  loadUsage: () => Promise<void>;
  cardState: (c: ActionCard) => CardState;
  cardJobs: (c: ActionCard) => Job[];
  approve: (c: ActionCard) => Promise<void>;
  decline: (c: ActionCard) => void;
}

const Ctx = createContext<AssistantCtx | null>(null);

const errText = (e: unknown) => (e instanceof Error ? e.message : String(e));

type AssistantItem = Extract<ChatItem, { role: "assistant" }>;

function apply(it: AssistantItem, e: AssistantEvent): AssistantItem {
  switch (e.kind) {
    case "text":
      return { ...it, text: it.text + (e.text ?? ""), state: "writing" };
    case "tool":
      return { ...it, state: "working", steps: [...it.steps, { id: e.tool ?? "", label: e.label ?? "", done: false, failed: false }] };
    case "toolDone":
      return { ...it, steps: it.steps.map((s) => (s.id === e.tool ? { ...s, done: true, failed: !!e.failed } : s)) };
    case "action":
      return e.action && !it.cards.some((c) => c.id === e.action!.id) ? { ...it, cards: [...it.cards, e.action] } : it;
    case "done":
      return { ...it, state: e.stop === "end_turn" ? "done" : e.stop === "refusal" ? "refused" : e.stop === "max_tokens" ? "truncated" : "stopped" };
    case "error":
      return { ...it, state: "error", error: e.text };
  }
  return it;
}

const SERVICE_VERB = { start_service: "start", stop_service: "stop", restart_service: "restart" } as const;

export function AssistantProvider({ children }: { children: ReactNode }) {
  const brew = useBrew();
  const [status, setStatus] = useState<AssistantStatus | null>(null);
  const [items, setItems] = useState<ChatItem[]>([]);
  const [cost, setCost] = useState(0);
  const [refs, setRefs] = useState<Map<string, Ref>>(new Map());
  const [usage, setUsage] = useState<UsageSummary | null>(null);
  const [decisions, setDecisions] = useState<Record<string, Decision>>({});
  const seq = useRef(0);
  // Events can beat Ask's reply; hold them until the turn is on screen.
  const known = useRef(new Set<number>());
  const early = useRef(new Map<number, AssistantEvent[]>());

  const refreshStatus = useCallback(async () => {
    try {
      const s = await Assistant.Status();
      setStatus(s);
      setCost(s.cost);
    } catch {
      setStatus(null);
    }
  }, []);

  const loadUsage = useCallback(async () => {
    try {
      setUsage(await Assistant.Usage());
    } catch {
      /* keep what we had */
    }
  }, []);

  useEffect(() => {
    void refreshStatus();
  }, [refreshStatus]);

  useEffect(() => {
    const off = Events.On("assistant", (ev: { data: unknown }) => {
      const e = (Array.isArray(ev.data) ? ev.data[0] : ev.data) as AssistantEvent;
      if (!e) return;
      if (typeof e.cost === "number") setCost(e.cost);
      if (e.kind === "refs") {
        setRefs((m) => {
          const next = new Map(m);
          for (const r of e.refs ?? []) next.set(r.name, r);
          return next;
        });
        return;
      }
      if (!known.current.has(e.turn)) {
        early.current.set(e.turn, [...(early.current.get(e.turn) ?? []), e]);
        return;
      }
      setItems((list) => list.map((it) => (it.role === "assistant" && it.turn === e.turn ? apply(it, e) : it)));
    });
    return () => off();
  }, []);

  const busy = items.some((it) => it.role === "assistant" && (it.state === "working" || it.state === "writing"));

  // ---- Cards ---------------------------------------------------------------

  const jobsById = useMemo(() => new Map(brew.jobs.map((j) => [j.id, j])), [brew.jobs]);

  const cardJobs = useCallback((c: ActionCard) => (decisions[c.id]?.jobs ?? []).map((id) => jobsById.get(id)).filter((j): j is Job => !!j), [decisions, jobsById]);

  const cardState = useCallback(
    (c: ActionCard): CardState => {
      const d = decisions[c.id];
      if (!d) return "pending";
      if (d.declined) return "declined";
      if (d.confirming) return "confirming";
      const js = cardJobs(c);
      if (!js.length) return d.jobs.length ? "running" : "pending";
      if (js.some(isActive)) return "running";
      if (js.some((j) => j.status === "failed")) return "failed";
      if (js.some((j) => j.status === "cancelled")) return "cancelled";
      return "done";
    },
    [decisions, cardJobs],
  );

  const decide = useCallback((id: string, d: Partial<Decision>) => setDecisions((all) => ({ ...all, [id]: { ...all[id], ...d, jobs: d.jobs ?? all[id]?.jobs ?? [] } })), []);

  const approve = useCallback(
    async (c: ActionCard) => {
      const add = (js: (Job | undefined)[]) => {
        const ids = js.filter((j): j is Job => !!j).map((j) => j.id);
        setDecisions((all) => ({ ...all, [c.id]: { ...all[c.id], confirming: false, jobs: [...(all[c.id]?.jobs ?? []), ...ids] } }));
        return ids.length;
      };
      decide(c.id, { declined: false });
      switch (c.action) {
        case "install": {
          const js: (Job | undefined)[] = [];
          for (const p of c.packages) js.push(await brew.install({ name: p.name, kind: p.kind }));
          if (!add(js)) decide(c.id, { jobs: [] });
          return;
        }
        case "upgrade": {
          const pkgs = c.packages.map((p) => brew.pkgs.find((x) => x.name === p.name && x.kind === p.kind)).filter((p) => !!p);
          add((await brew.upgrade(pkgs)) ?? []);
          return;
        }
        case "uninstall": {
          const p = brew.pkgs.find((x) => x.name === c.packages[0].name && x.kind === c.packages[0].kind);
          if (!p) return;
          // The existing dialog takes it from here (leftovers, confirmation).
          decide(c.id, { confirming: true });
          brew.watchUninstall(p.key, (job) => add([job]));
          brew.requestUninstall(p);
          return;
        }
        case "start_service":
        case "stop_service":
        case "restart_service":
          add([await brew.serviceAction(c.packages[0].name, SERVICE_VERB[c.action])]);
          return;
        case "autoremove":
          add([await brew.autoremove()]);
          return;
        case "cleanup":
          add([await brew.cleanupNow(false)]);
          return;
      }
    },
    [brew, decide],
  );

  // Closing the uninstall dialog without uninstalling puts the card back.
  const confirmingIds = Object.entries(decisions)
    .filter(([, d]) => d.confirming)
    .map(([id]) => id);
  useEffect(() => {
    if (brew.uninstallTarget || !confirmingIds.length) return;
    setDecisions((all) => {
      const next = { ...all };
      for (const id of confirmingIds) if (next[id]?.confirming && !next[id].jobs.length) next[id] = { jobs: [] };
      return next;
    });
  }, [brew.uninstallTarget, confirmingIds.join()]);

  const decline = useCallback((c: ActionCard) => decide(c.id, { declined: true }), [decide]);

  /** What happened to the cards since Claude last heard, as notes. */
  const notes = useCallback(() => {
    const out: string[] = [];
    const reported: string[] = [];
    for (const it of items) {
      if (it.role !== "assistant") continue;
      for (const c of it.cards) {
        const d = decisions[c.id];
        if (d?.reported) continue;
        const st = cardState(c);
        const what = `“${c.title}” (${c.command.replace(/\n/g, "; ")})`;
        switch (st) {
          case "pending":
          case "confirming":
            out.push(`The user hasn't approved or declined ${what} yet.`);
            break;
          case "running":
            out.push(`The user approved ${what}; it is still running.`);
            break;
          case "declined":
            out.push(`The user declined ${what}.`);
            reported.push(c.id);
            break;
          case "cancelled":
            out.push(`The user approved ${what}, but it was stopped before it finished.`);
            reported.push(c.id);
            break;
          case "done":
            out.push(`The user approved ${what} and it finished successfully.`);
            reported.push(c.id);
            break;
          case "failed": {
            const tail = cardJobs(c)
              .filter((j) => j.status === "failed")
              .flatMap((j) => j.output.slice(-12))
              .join("\n");
            out.push(`The user approved ${what}, but it failed.${tail ? ` Last output:\n${tail}` : ""}`);
            reported.push(c.id);
            break;
          }
        }
      }
    }
    return { out, reported };
  }, [items, decisions, cardState, cardJobs]);

  // ---- Conversation ----------------------------------------------------------

  const ask = useCallback(
    async (text: string) => {
      const q = text.trim();
      if (!q || busy) return;
      const n = ++seq.current;
      const { out, reported } = notes();
      setItems((list) => [...list, { role: "user", id: `u${n}`, text: q }]);
      try {
        const turn = await Assistant.Ask(q, out);
        if (reported.length) setDecisions((all) => Object.fromEntries(Object.entries(all).map(([id, d]) => [id, reported.includes(id) ? { ...d, reported: true } : d])));
        known.current.add(turn);
        const held = early.current.get(turn) ?? [];
        early.current.delete(turn);
        const item = held.reduce<AssistantItem>(apply, { role: "assistant", id: `a${n}`, turn, text: "", steps: [], cards: [], state: "working" });
        setItems((list) => [...list, item]);
      } catch (e) {
        setItems((list) => [...list, { role: "assistant", id: `a${n}`, turn: -n, text: "", steps: [], cards: [], state: "error", error: errText(e) }]);
      }
    },
    [busy, notes],
  );

  const stop = useCallback(() => void Assistant.Stop(), []);

  const newChat = useCallback(() => {
    void Assistant.NewChat();
    setItems([]);
    setCost(0);
    setRefs(new Map());
    setDecisions({});
  }, []);

  const saveKey = useCallback(
    async (key: string) => {
      await Assistant.SetAPIKey(key);
      await refreshStatus();
    },
    [refreshStatus],
  );

  const removeKey = useCallback(async () => {
    await Assistant.RemoveAPIKey();
    await refreshStatus();
  }, [refreshStatus]);

  const setModel = useCallback(
    async (id: string) => {
      setStatus((s) => (s ? ({ ...s, model: id } as AssistantStatus) : s));
      try {
        await Assistant.SetModel(id);
      } finally {
        await refreshStatus();
      }
    },
    [refreshStatus],
  );

  const value = useMemo(
    () => ({ status, items, busy, cost, refs, ask, stop, newChat, saveKey, removeKey, setModel, usage, loadUsage, cardState, cardJobs, approve, decline }),
    [status, items, busy, cost, refs, ask, stop, newChat, saveKey, removeKey, setModel, usage, loadUsage, cardState, cardJobs, approve, decline],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAssistant(): AssistantCtx {
  const c = useContext(Ctx);
  if (!c) throw new Error("useAssistant outside AssistantProvider");
  return c;
}
