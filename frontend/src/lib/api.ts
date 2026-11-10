import { Browser } from "@wailsio/runtime";
import * as Brew from "../../bindings/dev.vimukthid/brewctl/brewservice";
import type {
  Package,
  Job as RawJob,
  CleanupPreview as RawCleanup,
  Leftover,
  SearchResult,
  Remote as RawRemote,
} from "../../bindings/dev.vimukthid/brewctl/internal/brew/models";
import type { Status } from "../../bindings/dev.vimukthid/brewctl/models";

export { Brew };
export type { Status, Leftover, SearchResult };

export interface Remote extends Omit<RawRemote, "deps" | "kind"> {
  kind: Kind;
  deps: string[];
}

export interface Ref {
  name: string;
  kind: Kind;
}

export const refKey = (r: { name: string; kind: string }) => `${r.kind}:${r.name}`;

export type Kind = "formula" | "cask";

export interface Pkg extends Omit<Package, "deps" | "allDeps" | "apps" | "zap" | "kind"> {
  kind: Kind;
  key: string; // "kind:name", unique
  short: string; // name without tap prefix, as dependencies refer to it
  deps: string[];
  allDeps: string[];
  apps: string[];
}

export interface Job extends Omit<RawJob, "output"> {
  output: string[];
}

export interface CleanupPreview {
  orphans: string[];
  items: { path: string; size: string }[];
  summary: string;
}

export function toPkg(p: Package): Pkg {
  const kind: Kind = p.kind === "cask" ? "cask" : "formula";
  return {
    ...p,
    kind,
    key: `${kind}:${p.name}`,
    short: p.name.slice(p.name.lastIndexOf("/") + 1),
    deps: p.deps ?? [],
    allDeps: p.allDeps ?? [],
    apps: p.apps ?? [],
  };
}

/** Where a leftover came from, most certain first. */
export const LEFTOVER_GROUPS: { source: string; title: string; hint: string }[] = [
  { source: "cache", title: "Homebrew's download", hint: "The installer Homebrew kept. Deleted for good; Homebrew can fetch it again." },
  { source: "homebrew", title: "Settings and data in Homebrew", hint: "Kept by Homebrew on purpose, in case you reinstall." },
  { source: "zap", title: "The app's own files", hint: "Listed by the app as belonging to it." },
  { source: "home", title: "Folders with the same name", hint: "Probably created by it, but matched by name only. Check before removing." },
];

export function plural(n: number, one: string, many = one + "s"): string {
  return `${n} ${n === 1 ? one : many}`;
}

export function toJob(j: RawJob): Job {
  return { ...j, output: j.output ?? [] };
}

export function toCleanup(c: RawCleanup | null): CleanupPreview {
  return { orphans: c?.orphans ?? [], items: c?.items ?? [], summary: c?.summary ?? "" };
}

export const isActive = (j: Job) => j.status === "queued" || j.status === "running";

export function openURL(url: string) {
  if (!/^https?:\/\//i.test(url)) return;
  Browser.OpenURL(url).catch(() => window.open(url, "_blank", "noopener"));
}

export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    return ok;
  }
}

/** Bytes in decimal units, the way Finder reports them. */
export function formatBytes(n: number | null | undefined): string {
  if (n == null) return "—";
  if (n < 1000) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = n;
  let i = -1;
  do {
    v /= 1000;
    i++;
  } while (v >= 999.5 && i < units.length - 1);
  return `${v >= 100 ? v.toFixed(0) : v >= 10 ? v.toFixed(1).replace(/\.0$/, "") : v.toFixed(1)} ${units[i]}`;
}

export function timeAgo(unix: number, now = Date.now()): string {
  if (!unix) return "—";
  const s = Math.max(0, Math.round(now / 1000 - unix));
  if (s < 45) return "just now";
  const steps: [number, string][] = [
    [60, "minute"],
    [3600, "hour"],
    [86400, "day"],
    [604800, "week"],
    [2629800, "month"],
    [31557600, "year"],
  ];
  let label = "minute";
  let size = 60;
  for (const [secs, name] of steps) {
    if (s >= secs) {
      size = secs;
      label = name;
    }
  }
  const n = Math.max(1, Math.round(s / size));
  return `${n} ${label}${n === 1 ? "" : "s"} ago`;
}

export function formatDate(unix: number): string {
  if (!unix) return "—";
  return new Date(unix * 1000).toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

export function duration(j: Job, now = Date.now()): string {
  if (!j.startedAt) return "";
  const end = j.endedAt || Math.round(now / 1000);
  const s = Math.max(0, end - j.startedAt);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
}

export function errorMessage(e: unknown): string {
  if (e instanceof Error) return e.message;
  if (typeof e === "string") return e;
  try {
    return JSON.stringify(e);
  } catch {
    return "Something went wrong";
  }
}
