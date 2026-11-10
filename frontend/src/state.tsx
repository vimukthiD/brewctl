import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Events } from "@wailsio/runtime";
import {
  Brew,
  errorMessage,
  formatBytes,
  isActive,
  plural,
  toCleanup,
  toJob,
  toPkg,
  type CleanupPreview,
  type Job,
  type Pkg,
  type Status,
} from "./lib/api";

export type View = "overview" | "installed" | "updates" | "cleanup" | "activity";

export interface ConfirmOptions {
  title: string;
  body: ReactNode;
  confirmLabel: string;
  danger?: boolean;
  command?: string;
}

export interface Toast {
  id: number;
  tone: "ok" | "error" | "info";
  text: string;
  jobId?: number;
}

interface Ctx {
  status: Status | null;
  pkgs: Pkg[];
  byShort: Map<string, Pkg>;
  usedBy: Map<string, string[]>;
  sizes: Record<string, number> | null;
  cacheSize: number | null;
  cleanup: CleanupPreview | null;
  jobs: Job[];
  loading: boolean;
  refreshing: boolean;
  error: string | null;
  fetchedAt: number | null;
  busy: Set<string>;
  refresh: () => Promise<void>;

  view: View;
  setView: (v: View) => void;
  selected: Pkg | null;
  select: (key: string | null) => void;
  focusJob: number | null;
  showJob: (id: number) => void;

  upgrade: (pkgs: Pkg[]) => Promise<void>;
  /** Uninstalls p, then clears the given leftover paths once that succeeds. */
  uninstall: (p: Pkg, leftovers?: string[]) => Promise<void>;
  uninstallTarget: Pkg | null;
  requestUninstall: (p: Pkg | null) => void;
  setPinned: (p: Pkg, pinned: boolean) => Promise<void>;
  updateIndex: () => Promise<void>;
  autoremove: () => Promise<void>;
  cleanupNow: (deep: boolean) => Promise<void>;
  doctor: () => Promise<void>;
  cancelJob: (id: number) => void;

  confirm: (o: ConfirmOptions) => Promise<boolean>;
  confirming: (ConfirmOptions & { resolve: (ok: boolean) => void }) | null;
  toasts: Toast[];
  dismissToast: (id: number) => void;
  toast: (t: Omit<Toast, "id">) => void;
}

const BrewContext = createContext<Ctx | null>(null);

export function useBrew(): Ctx {
  const ctx = useContext(BrewContext);
  if (!ctx) throw new Error("useBrew outside provider");
  return ctx;
}

let toastSeq = 1;

export function BrewProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<Status | null>(null);
  const [pkgs, setPkgs] = useState<Pkg[]>([]);
  const [sizes, setSizes] = useState<Record<string, number> | null>(null);
  const [cacheSize, setCacheSize] = useState<number | null>(null);
  const [cleanup, setCleanup] = useState<CleanupPreview | null>(null);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fetchedAt, setFetchedAt] = useState<number | null>(null);
  const [busyByJob, setBusyByJob] = useState<Record<number, string[]>>({});
  const [view, setView] = useState<View>("overview");
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [focusJob, setFocusJob] = useState<number | null>(null);
  const [confirming, setConfirming] = useState<Ctx["confirming"]>(null);
  const [toasts, setToasts] = useState<Toast[]>([]);

  const [uninstallTarget, setUninstallTarget] = useState<Pkg | null>(null);
  const pendingLeftovers = useRef<Record<number, { name: string; kind: string; paths: string[] }>>({});

  const refreshSeq = useRef(0);
  const jobsRef = useRef<Job[]>([]);
  jobsRef.current = jobs;

  const dismissToast = useCallback((id: number) => {
    setToasts((ts) => ts.filter((t) => t.id !== id));
  }, []);

  const toast = useCallback(
    (t: Omit<Toast, "id">) => {
      const id = toastSeq++;
      setToasts((ts) => [...ts.slice(-3), { ...t, id }]);
      window.setTimeout(() => dismissToast(id), t.tone === "error" ? 9000 : 4500);
    },
    [dismissToast],
  );

  const refresh = useCallback(async () => {
    const seq = ++refreshSeq.current;
    setRefreshing(true);
    try {
      const st = await Brew.Status();
      if (seq !== refreshSeq.current) return;
      setStatus(st);
      if (!st.found) {
        setError(null);
        return;
      }
      const snap = await Brew.Snapshot();
      if (seq !== refreshSeq.current) return;
      setPkgs((snap?.packages ?? []).map(toPkg));
      setFetchedAt(snap?.fetchedAt ?? null);
      setError(null);
      setLoading(false);
      setRefreshing(false);
      // Slower, secondary data: fill in when ready.
      const [sz, cl] = await Promise.allSettled([Brew.Sizes(), Brew.CleanupPreview()]);
      if (seq !== refreshSeq.current) return;
      if (sz.status === "fulfilled" && sz.value) {
        setSizes((sz.value.packages ?? {}) as Record<string, number>);
        setCacheSize(sz.value.cache);
      }
      if (cl.status === "fulfilled") setCleanup(toCleanup(cl.value));
    } catch (e) {
      if (seq === refreshSeq.current) setError(errorMessage(e));
    } finally {
      if (seq === refreshSeq.current) {
        setLoading(false);
        setRefreshing(false);
      }
    }
  }, []);

  // Initial load + live job events.
  useEffect(() => {
    refresh();
    Brew.Jobs()
      .then((js) => setJobs((cur) => (cur.length ? cur : (js ?? []).map(toJob))))
      .catch(() => {});

    let refreshTimer: number | undefined;
    const offUpdate = Events.On("job:update", (ev: { data: unknown }) => {
      const raw = (Array.isArray(ev.data) ? ev.data[0] : ev.data) as Job;
      if (!raw || typeof raw.id !== "number") return;
      const prev = jobsRef.current.find((j) => j.id === raw.id);
      const wasActive = prev ? isActive(prev) : true;
      setJobs((cur) => {
        const i = cur.findIndex((j) => j.id === raw.id);
        if (i < 0) return [...cur, { ...raw, output: [] }];
        const next = cur.slice();
        next[i] = { ...raw, output: cur[i].output };
        return next;
      });
      const finished = !isActive({ ...raw, output: [] });
      if (!finished || !wasActive) return;

      setBusyByJob((b) => {
        if (!(raw.id in b)) return b;
        const { [raw.id]: _, ...rest } = b;
        return rest;
      });
      const pending = pendingLeftovers.current[raw.id];
      delete pendingLeftovers.current[raw.id];
      if (pending && raw.status === "done") {
        Brew.RemoveLeftovers({ name: pending.name, kind: pending.kind }, pending.paths)
          .then((res) => {
            const failed = res?.failed ?? [];
            if (res?.removed) toast({ tone: "ok", text: `Cleared ${plural(res.removed, "leftover")} (${formatBytes(res.freed)})` });
            if (failed.length) toast({ tone: "error", text: `Couldn't remove ${failed.join(", ")}` });
          })
          .catch((e) => toast({ tone: "error", text: `Leftovers not removed: ${errorMessage(e)}` }));
      }
      if (raw.status === "done") {
        if (raw.mutates) toast({ tone: "ok", text: `${raw.title} — done`, jobId: raw.id });
      } else if (raw.status === "failed") {
        toast({
          tone: "error",
          text: raw.needsSudo
            ? `${raw.title} needs your admin password`
            : `${raw.title} failed`,
          jobId: raw.id,
        });
      }
      if (raw.mutates) {
        window.clearTimeout(refreshTimer);
        refreshTimer = window.setTimeout(() => {
          const others = jobsRef.current.some((j) => j.id !== raw.id && j.mutates && isActive(j));
          if (!others) refresh();
        }, 250);
      }
    });
    const offOutput = Events.On("job:output", (ev: { data: unknown }) => {
      const d = (Array.isArray(ev.data) ? ev.data[0] : ev.data) as { id: number; lines: string[] };
      if (!d || !d.lines) return;
      setJobs((cur) =>
        cur.map((j) => (j.id === d.id ? { ...j, output: [...j.output, ...d.lines].slice(-3000) } : j)),
      );
    });
    return () => {
      window.clearTimeout(refreshTimer);
      offUpdate();
      offOutput();
    };
  }, [refresh, toast]);

  const byShort = useMemo(() => {
    const m = new Map<string, Pkg>();
    for (const p of pkgs) if (p.kind === "formula") m.set(p.short, p);
    return m;
  }, [pkgs]);

  const usedBy = useMemo(() => {
    const m = new Map<string, string[]>();
    for (const p of pkgs) {
      for (const d of p.deps) {
        const list = m.get(d);
        if (list) list.push(p.short);
        else m.set(d, [p.short]);
      }
    }
    return m;
  }, [pkgs]);

  const busy = useMemo(() => new Set(Object.values(busyByJob).flat()), [busyByJob]);

  const selected = useMemo(
    () => (selectedKey ? pkgs.find((p) => p.key === selectedKey) ?? null : null),
    [pkgs, selectedKey],
  );

  const track = useCallback((job: { id: number } | null | undefined, keys: string[]) => {
    if (!job || !keys.length) return;
    setBusyByJob((b) => ({ ...b, [job.id]: keys }));
  }, []);

  const guard = useCallback(
    async (fn: () => Promise<void>) => {
      try {
        await fn();
      } catch (e) {
        toast({ tone: "error", text: errorMessage(e) });
      }
    },
    [toast],
  );

  const confirm = useCallback(
    (o: ConfirmOptions) =>
      new Promise<boolean>((resolve) => {
        setConfirming({
          ...o,
          resolve: (ok) => {
            setConfirming(null);
            resolve(ok);
          },
        });
      }),
    [],
  );

  const upgrade = useCallback(
    (list: Pkg[]) =>
      guard(async () => {
        if (!list.length) return;
        const js = await Brew.Upgrade(list.map((p) => ({ name: p.name, kind: p.kind })));
        for (const kind of ["formula", "cask"] as const) {
          const keys = list.filter((p) => p.kind === kind).map((p) => p.key);
          const job = (js ?? []).find((j) => j.command.includes(kind === "cask" ? "--cask" : "--formula"));
          track(job, keys);
        }
      }),
    [guard, track],
  );

  const uninstall = useCallback(
    (p: Pkg, leftovers: string[] = []) =>
      guard(async () => {
        const job = await Brew.Uninstall({ name: p.name, kind: p.kind });
        if (job && leftovers.length) pendingLeftovers.current[job.id] = { name: p.name, kind: p.kind, paths: leftovers };
        track(job, [p.key]);
      }),
    [guard, track],
  );

  const setPinned = useCallback(
    (p: Pkg, pinned: boolean) =>
      guard(async () => {
        const job = await Brew.SetPinned(p.name, pinned);
        track(job, [p.key]);
      }),
    [guard, track],
  );

  const updateIndex = useCallback(() => guard(async () => void (await Brew.UpdateIndex())), [guard]);
  const autoremove = useCallback(() => guard(async () => void (await Brew.Autoremove())), [guard]);
  const cleanupNow = useCallback(
    (deep: boolean) => guard(async () => void (await Brew.Cleanup(deep))),
    [guard],
  );
  const showJob = useCallback((id: number) => {
    setFocusJob(id);
    setSelectedKey(null);
    setView("activity");
  }, []);
  const doctor = useCallback(
    () =>
      guard(async () => {
        const job = await Brew.Doctor();
        if (job) showJob(job.id);
      }),
    [guard, showJob],
  );
  const cancelJob = useCallback((id: number) => void Brew.CancelJob(id), []);

  const value: Ctx = {
    status,
    pkgs,
    byShort,
    usedBy,
    sizes,
    cacheSize,
    cleanup,
    jobs,
    loading,
    refreshing,
    error,
    fetchedAt,
    busy,
    refresh,
    view,
    setView: (v) => {
      setView(v);
      if (v !== "activity") setFocusJob(null);
    },
    selected,
    select: setSelectedKey,
    focusJob,
    showJob,
    upgrade,
    uninstall,
    uninstallTarget,
    requestUninstall: setUninstallTarget,
    setPinned,
    updateIndex,
    autoremove,
    cleanupNow,
    doctor,
    cancelJob,
    confirm,
    confirming,
    toasts,
    dismissToast,
    toast,
  };

  return <BrewContext.Provider value={value}>{children}</BrewContext.Provider>;
}
