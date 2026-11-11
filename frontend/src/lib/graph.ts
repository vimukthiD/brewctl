// The dependency graph between installed packages, and the layout of the
// dependency map: one package in the middle, what it needs spreading to the
// right step by step, and what needs it spreading to the left.

import type { Pkg } from "./api";

export interface DepGraph {
  /** Package key -> keys of the installed packages it needs directly. */
  needs: Map<string, string[]>;
  /** Package key -> keys of the installed packages that need it directly. */
  neededBy: Map<string, string[]>;
}

/** Builds the graph. Dependencies are formula names, so they resolve through byShort. */
export function buildGraph(pkgs: Pkg[], byShort: Map<string, Pkg>): DepGraph {
  const needs = new Map<string, string[]>();
  const neededBy = new Map<string, string[]>();
  for (const p of pkgs) {
    needs.set(p.key, []);
    if (!neededBy.has(p.key)) neededBy.set(p.key, []);
  }
  for (const p of pkgs) {
    for (const d of p.deps) {
      const dep = byShort.get(d);
      if (!dep || dep.key === p.key) continue;
      needs.get(p.key)!.push(dep.key);
      neededBy.get(dep.key)!.push(p.key);
    }
  }
  return { needs, neededBy };
}

/** Everything reachable from start along edges, not counting start. */
export function closure(edges: Map<string, string[]>, start: string, skip?: string): Set<string> {
  const seen = new Set<string>();
  const stack = [...(edges.get(start) ?? [])];
  while (stack.length) {
    const k = stack.pop()!;
    if (seen.has(k) || k === start || k === skip) continue;
    seen.add(k);
    for (const n of edges.get(k) ?? []) stack.push(n);
  }
  return seen;
}

/**
 * The dependencies of focus that are installed only because of it: nothing
 * you installed yourself needs them except through focus.
 */
export function onlyFor(g: DepGraph, pkgs: Pkg[], focus: string): Set<string> {
  const deps = closure(g.needs, focus);
  const kept = new Set<string>();
  for (const p of pkgs) {
    if (p.key === focus || deps.has(p.key) || !(p.onRequest || p.kind === "cask")) continue;
    kept.add(p.key);
    for (const k of closure(g.needs, p.key, focus)) kept.add(k);
  }
  return new Set([...deps].filter((k) => !kept.has(k)));
}

/** The packages you installed yourself that, directly or not, need focus. */
export function pulledInBy(g: DepGraph, byKey: Map<string, Pkg>, focus: string): Pkg[] {
  return [...closure(g.neededBy, focus)]
    .map((k) => byKey.get(k))
    .filter((p): p is Pkg => !!p && (p.onRequest || p.kind === "cask"))
    .sort((a, b) => a.short.localeCompare(b.short));
}

export interface MapNode {
  key: string;
  col: number; // 0 is the focus; negative columns need it, positive ones it needs
  row: number;
}

export interface MapEdge {
  from: string; // needs...
  to: string; // ...this
}

export interface MapLayout {
  nodes: MapNode[];
  edges: MapEdge[];
  minCol: number;
  maxCol: number;
  tallest: number; // rows in the fullest column
  left: Set<string>;
  right: Set<string>;
}

/**
 * Lays out the map around focus. Each package sits one column further out
 * than the furthest package linking it to the middle, so every line points
 * outward. Within a column, packages are ordered next to what they connect
 * to, which keeps lines from crossing more than they must.
 */
export function layoutMap(g: DepGraph, focus: string, directOnly: boolean): MapLayout {
  const right = directOnly ? new Set(g.needs.get(focus) ?? []) : closure(g.needs, focus);
  const left = directOnly ? new Set(g.neededBy.get(focus) ?? []) : closure(g.neededBy, focus);
  left.delete(focus);
  right.delete(focus);

  // Lines: within the right side (and from the focus into it), and within
  // the left side (and from it into the focus). Lines that would jump from
  // one side across the focus to the other are left out.
  const edges: MapEdge[] = [];
  for (const k of right) if ((g.needs.get(focus) ?? []).includes(k)) edges.push({ from: focus, to: k });
  if (!directOnly) {
    for (const u of right) for (const v of g.needs.get(u) ?? []) if (right.has(v)) edges.push({ from: u, to: v });
    for (const u of left) for (const v of g.needs.get(u) ?? []) if (left.has(v)) edges.push({ from: u, to: v });
  }
  for (const u of left) if ((g.needs.get(u) ?? []).includes(focus)) edges.push({ from: u, to: focus });

  const depth = new Map<string, number>([[focus, 0]]);
  const visiting = new Set<string>();
  // Right: one more than the deepest package in the set that needs it.
  const depthRight = (k: string): number => {
    if (depth.has(k)) return depth.get(k)!;
    if (visiting.has(k)) return 1;
    visiting.add(k);
    let d = 1;
    for (const p of g.neededBy.get(k) ?? []) if (p === focus || right.has(p)) d = Math.max(d, depthRight(p) + 1);
    visiting.delete(k);
    depth.set(k, d);
    return d;
  };
  // Left: one more than the deepest package in the set it needs.
  const depthLeft = (k: string): number => {
    if (depth.has(k)) return depth.get(k)!;
    if (visiting.has(k)) return 1;
    visiting.add(k);
    let d = 1;
    for (const c of g.needs.get(k) ?? []) if (c === focus || left.has(c)) d = Math.max(d, depthLeft(c) + 1);
    visiting.delete(k);
    depth.set(k, d);
    return d;
  };

  const cols = new Map<number, string[]>([[0, [focus]]]);
  const colOf = new Map<string, number>([[focus, 0]]);
  const put = (k: string, c: number) => {
    colOf.set(k, c);
    if (!cols.has(c)) cols.set(c, []);
    cols.get(c)!.push(k);
  };
  for (const k of right) put(k, directOnly ? 1 : depthRight(k));
  for (const k of left) put(k, directOnly ? -1 : -depthLeft(k));

  const neighbours = new Map<string, string[]>();
  for (const e of edges) {
    if (!neighbours.has(e.from)) neighbours.set(e.from, []);
    if (!neighbours.has(e.to)) neighbours.set(e.to, []);
    neighbours.get(e.from)!.push(e.to);
    neighbours.get(e.to)!.push(e.from);
  }

  // Start alphabetically, then sort each column by where its neighbours sit,
  // first working outward from the middle, then back in, then out again.
  const name = (k: string) => k.slice(k.indexOf(":") + 1);
  for (const list of cols.values()) list.sort((a, b) => name(a).localeCompare(name(b)));
  const pos = new Map<string, number>();
  const place = (c: number) => {
    const list = cols.get(c)!;
    list.forEach((k, i) => pos.set(k, i - (list.length - 1) / 2));
  };
  for (const c of cols.keys()) place(c);
  const keys = [...cols.keys()];
  const minCol = Math.min(...keys);
  const maxCol = Math.max(...keys);
  const sweep = (c: number, towardCentre: boolean) => {
    const list = cols.get(c);
    if (!list || list.length < 2) return;
    const ref = (k: string) => {
      const ns = (neighbours.get(k) ?? []).filter((n) => {
        const nc = colOf.get(n)!;
        return towardCentre ? Math.abs(nc) > Math.abs(c) : Math.abs(nc) < Math.abs(c);
      });
      if (!ns.length) return pos.get(k)!;
      return ns.reduce((s, n) => s + pos.get(n)!, 0) / ns.length;
    };
    const bary = new Map(list.map((k) => [k, ref(k)]));
    list.sort((a, b) => bary.get(a)! - bary.get(b)! || name(a).localeCompare(name(b)));
    place(c);
  };
  const outward: number[] = [];
  for (let d = 1; d <= Math.max(maxCol, -minCol); d++) outward.push(d, -d);
  for (const c of outward) sweep(c, false);
  for (const c of [...outward].reverse()) sweep(c, true);
  for (const c of outward) sweep(c, false);

  const nodes: MapNode[] = [];
  let tallest = 1;
  for (const [c, list] of cols) {
    tallest = Math.max(tallest, list.length);
    list.forEach((k, row) => nodes.push({ key: k, col: c, row }));
  }
  return { nodes, edges, minCol, maxCol, tallest, left, right };
}

/**
 * The packages and lines on the way between the focus and k: for a package
 * on the right, everything between the focus and it; on the left, everything
 * between it and the focus.
 */
export function routeTo(g: DepGraph, layout: MapLayout, focus: string, k: string): Set<string> {
  if (k === focus) return new Set();
  const onRight = layout.right.has(k);
  const side = onRight ? layout.right : layout.left;
  const back = onRight ? g.neededBy : g.needs;
  const out = new Set<string>([k]);
  const stack = [k];
  while (stack.length) {
    const cur = stack.pop()!;
    for (const n of back.get(cur) ?? []) {
      if (out.has(n)) continue;
      if (n === focus) {
        out.add(n);
      } else if (side.has(n)) {
        out.add(n);
        stack.push(n);
      }
    }
  }
  // Keep only packages that actually lie between the two ends.
  if (!out.has(focus)) return new Set([k]);
  const forward = onRight ? g.needs : g.neededBy;
  const fromFocus = new Set<string>([focus]);
  const s2 = [focus];
  while (s2.length) {
    const cur = s2.pop()!;
    for (const n of forward.get(cur) ?? []) {
      if (out.has(n) && !fromFocus.has(n)) {
        fromFocus.add(n);
        s2.push(n);
      }
    }
  }
  return fromFocus;
}

/** One shortest chain from the focus to k, in "needs" order (left to right). */
export function chain(g: DepGraph, layout: MapLayout, focus: string, k: string): string[] {
  if (k === focus) return [focus];
  const onRight = layout.right.has(k);
  const side = onRight ? layout.right : layout.left;
  const next = onRight ? g.needs : g.neededBy;
  const prev = new Map<string, string>();
  const queue = [focus];
  const seen = new Set([focus]);
  while (queue.length) {
    const cur = queue.shift()!;
    if (cur === k) break;
    for (const n of next.get(cur) ?? []) {
      if (seen.has(n) || !side.has(n)) continue;
      seen.add(n);
      prev.set(n, cur);
      queue.push(n);
    }
  }
  if (!seen.has(k)) return [k];
  const path = [k];
  while (path[path.length - 1] !== focus) path.push(prev.get(path[path.length - 1])!);
  return onRight ? path.reverse() : path;
}
