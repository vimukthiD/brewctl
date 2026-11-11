import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { ArrowLeft, Info, Maximize2, Minus, Plus, X } from "lucide-react";
import { formatBytes, plural, type Pkg } from "./lib/api";
import { chain, layoutMap, onlyFor, pulledInBy, routeTo } from "./lib/graph";
import { useBrew } from "./state";

const NODE_W = 152;
const NODE_H = 30;
const GAP_Y = 8;
const COL_GAP = 52;
const PAD_X = 28;
const PAD_Y = 44; // room for the column headings

/** A full-size map of what one package needs and what needs it. */
export function DepMap() {
  const { mapKey, showMap, graph, byKey, pkgs, sizes, vulnBy, select } = useBrew();
  const [history, setHistory] = useState<string[]>([]);
  const [directOnly, setDirectOnly] = useState(false);
  const [hover, setHover] = useState<string | null>(null);
  // null means "fit to the window"; otherwise a fixed scale.
  const [zoom, setZoom] = useState<number | null>(null);
  const [view, setView] = useState({ w: 0, h: 0 });
  const scroller = useRef<HTMLDivElement>(null);

  const focus = mapKey && byKey.has(mapKey) ? mapKey : null;
  const p = focus ? byKey.get(focus)! : null;

  const layout = useMemo(() => (focus ? layoutMap(graph, focus, directOnly) : null), [graph, focus, directOnly]);
  const only = useMemo(() => (focus ? onlyFor(graph, pkgs, focus) : new Set<string>()), [graph, pkgs, focus]);
  const roots = useMemo(() => (focus ? pulledInBy(graph, byKey, focus) : []), [graph, byKey, focus]);
  // Only packages on this map count: the pointer can leave a package
  // without a mouseleave when the map closes or changes under it.
  const hot = hover && layout && (hover === focus || layout.left.has(hover) || layout.right.has(hover)) ? hover : null;
  const route = useMemo(() => (focus && layout && hot ? routeTo(graph, layout, focus, hot) : null), [graph, layout, focus, hot]);

  useEffect(() => setHover(null), [focus]);

  // Closing: Escape, unless something on top of the map wants it.
  useEffect(() => {
    if (!focus) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        showMap(null);
      } else if ((e.key === "[" && e.metaKey) || (e.key === "ArrowLeft" && e.altKey)) {
        e.preventDefault();
        back();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  });

  // A new map starts with no history.
  useEffect(() => {
    if (!mapKey) setHistory([]);
  }, [mapKey]);

  // Track the visible area, to fit the map into it.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const measure = () => setView({ w: el.clientWidth, h: el.clientHeight });
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [focus]);

  const width = layout ? PAD_X * 2 + (layout.maxCol - layout.minCol) * (NODE_W + COL_GAP) + NODE_W : 0;
  const height = layout ? PAD_Y + layout.tallest * (NODE_H + GAP_Y) + 24 : 0;
  // Fit shrinks a big map to the window, but not below where names stay readable.
  const fit = view.w && width ? Math.max(0.75, Math.min(1, view.w / width, view.h / height)) : 1;
  const scale = zoom ?? fit;

  // Bring the focus into view whenever the map or its scale changes.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el || !layout) return;
    const fx = (PAD_X + (0 - layout.minCol) * (NODE_W + COL_GAP) + NODE_W / 2) * scale;
    const fy = (PAD_Y + ((layout.tallest - 1) / 2) * (NODE_H + GAP_Y) + NODE_H / 2) * scale;
    el.scrollTo({ left: Math.max(0, fx - el.clientWidth / 2), top: Math.max(0, fy - el.clientHeight / 2) });
  }, [layout, scale]);

  if (!focus || !p || !layout) return null;

  const refocus = (k: string) => {
    if (k === focus) return;
    setHistory((h) => [...h, focus]);
    setHover(null);
    showMap(k);
  };
  function back() {
    setHistory((h) => {
      if (!h.length) return h;
      showMap(h[h.length - 1]);
      setHover(null);
      return h.slice(0, -1);
    });
  }

  const colX = (c: number) => PAD_X + (c - layout.minCol) * (NODE_W + COL_GAP);
  const colLen = new Map<number, number>();
  for (const n of layout.nodes) colLen.set(n.col, (colLen.get(n.col) ?? 0) + 1);
  const rowY = (c: number, row: number) => PAD_Y + ((layout.tallest - colLen.get(c)!) / 2 + row) * (NODE_H + GAP_Y);
  const at = new Map(layout.nodes.map((n) => [n.key, { x: colX(n.col), y: rowY(n.col, n.row), col: n.col }]));

  const nNeeds = layout.right.size;
  const nNeededBy = layout.left.size;
  const onlyBytes = sizes ? [...only].reduce((n, k) => n + (sizes[k] ?? 0), 0) : null;
  const short = (k: string) => byKey.get(k)?.short ?? k;
  const lit = (k: string) => !route || route.has(k);

  const heading = (c: number) => {
    if (c === 0) return null;
    if (c === 1) return "Needs";
    if (c === -1) return "Needed by";
    return c > 1 ? `…which need` : `…needed by`;
  };

  const readout = hot && hot !== focus ? chain(graph, layout, focus, hot) : null;

  return (
    <div className="depmap" role="dialog" aria-modal="true" aria-label={`Dependency map of ${p.short}`}>
      <header className="depmap-head">
        <button type="button" className="icon-btn" aria-label="Back" title="Back  ⌘[" disabled={!history.length} onClick={back}>
          <ArrowLeft size={16} />
        </button>
        <div className="depmap-title">
          <h2>{p.short}</h2>
          <span>
            {[
              nNeeds ? `Needs ${plural(nNeeds, "package")}` : "Needs nothing",
              nNeededBy ? `needed by ${plural(nNeededBy, "package")}` : "nothing needs it",
            ].join(" · ") + (directOnly && (nNeeds || nNeededBy) ? " directly" : "")}
          </span>
        </div>
        <span className="grow" />
        <div className="seg" role="radiogroup" aria-label="Levels">
          <button type="button" role="radio" aria-checked={!directOnly} className={!directOnly ? "is-on" : ""} onClick={() => setDirectOnly(false)}>
            All levels
          </button>
          <button type="button" role="radio" aria-checked={directOnly} className={directOnly ? "is-on" : ""} onClick={() => setDirectOnly(true)}>
            Direct only
          </button>
        </div>
        <button
          type="button"
          className="btn btn-sm btn-default"
          onClick={() => {
            showMap(null);
            select(focus);
          }}
        >
          <span>Details</span>
        </button>
        <button type="button" className="icon-btn" aria-label="Close map" title="Close  Esc" onClick={() => showMap(null)}>
          <X size={16} />
        </button>
      </header>

      <div className="depmap-facts">
        <Fact p={p} roots={roots} />
        {only.size > 0 && (
          <span className="depmap-fact">
            <i className="depmap-swatch is-only" aria-hidden />
            {plural(only.size, "dependency", "dependencies")} {only.size === 1 ? "is" : "are"} only here for {p.short}
            {onlyBytes ? ` (${formatBytes(onlyBytes)})` : ""}
            {nNeededBy === 0 && !directOnly ? ". Uninstalling it, then clearing unused ones in Cleanup, removes them." : "."}
          </span>
        )}
        <span className="grow" />
        <span className="depmap-legend" aria-hidden>
          <span>
            <i className="depmap-swatch is-you" /> You installed
          </span>
          <span>
            <i className="depmap-swatch" /> Installed as a dependency
          </span>
          <span>
            <i className="depmap-swatch is-vuln" /> Known vulnerability
          </span>
        </span>
      </div>

      <div className="depmap-scroll" ref={scroller}>
        <div className="depmap-sizer" style={{ width: width * scale, height: height * scale }}>
        <div className="depmap-canvas" style={{ width, height, transform: `scale(${scale})` }}>
          {Array.from({ length: layout.maxCol - layout.minCol + 1 }, (_, i) => layout.minCol + i)
            .filter((c) => c !== 0 && colLen.has(c))
            .map((c) => (
              <div key={c} className="depmap-col" style={{ left: colX(c), width: NODE_W }}>
                {heading(c)}
              </div>
            ))}

          <svg className="depmap-lines" width={width} height={height} aria-hidden>
            {layout.edges.map((e) => {
              const a = at.get(e.from);
              const b = at.get(e.to);
              if (!a || !b) return null;
              const x1 = a.x + NODE_W;
              const y1 = a.y + NODE_H / 2;
              const x2 = b.x;
              const y2 = b.y + NODE_H / 2;
              const mid = Math.max(24, (x2 - x1) / 2);
              const on = route ? route.has(e.from) && route.has(e.to) : false;
              return (
                <path
                  key={`${e.from}>${e.to}`}
                  d={`M${x1},${y1} C${x1 + mid},${y1} ${x2 - mid},${y2} ${x2},${y2}`}
                  className={on ? "is-on" : route ? "is-dim" : ""}
                />
              );
            })}
          </svg>

          {layout.nodes.map((n) => {
            const q = byKey.get(n.key)!;
            const pos = at.get(n.key)!;
            const isFocus = n.key === focus;
            const vuln = q.kind === "formula" && vulnBy.has(q.short);
            const cls = [
              "depmap-node",
              isFocus && "is-focus",
              (q.onRequest || q.kind === "cask") && "is-you",
              only.has(n.key) && "is-only",
              vuln && "is-vuln",
              !lit(n.key) && "is-dim",
              hot === n.key && "is-hover",
            ]
              .filter(Boolean)
              .join(" ");
            return (
              <button
                key={n.key}
                type="button"
                className={cls}
                style={{ left: pos.x, top: pos.y, width: NODE_W, height: NODE_H }}
                title={`${q.short}${sizes?.[n.key] ? ` · ${formatBytes(sizes[n.key])}` : ""}${q.desc ? `\n${q.desc}` : ""}${isFocus ? "\nClick for details" : "\nClick to centre the map on it"}`}
                onMouseEnter={() => setHover(n.key)}
                onMouseLeave={() => setHover((h) => (h === n.key ? null : h))}
                onFocus={() => setHover(n.key)}
                onBlur={() => setHover((h) => (h === n.key ? null : h))}
                onClick={() => (isFocus ? (showMap(null), select(focus)) : refocus(n.key))}
              >
                <span className="depmap-name">{q.short}</span>
                {/* Long names need the room more than the size does. */}
                {sizes && sizes[n.key] > 0 && q.short.length <= 10 && <span className="depmap-size">{formatBytes(sizes[n.key])}</span>}
              </button>
            );
          })}
        </div>
        </div>
      </div>

      <footer className="depmap-foot" aria-live="polite">
        {readout ? (
          <span className="depmap-chain">
            {readout.map((k, i) => (
              <span key={k}>
                {i > 0 && <span className="depmap-arrow">needs</span>}
                <b className={k === hot ? "is-end" : ""}>{short(k)}</b>
              </span>
            ))}
          </span>
        ) : (
          <span className="quiet">
            <Info size={12} /> Point at a package to see how it connects to {p.short}. Click one to centre the map on it.
          </span>
        )}
        <span className="grow" />
        <div className="depmap-zoom" aria-label="Zoom">
          <button type="button" className="icon-btn" aria-label="Zoom out" disabled={scale <= 0.4} onClick={() => setZoom(Math.max(0.4, Math.round((scale - 0.1) * 10) / 10))}>
            <Minus size={14} />
          </button>
          <span className="depmap-pct">{Math.round(scale * 100)}%</span>
          <button type="button" className="icon-btn" aria-label="Zoom in" disabled={scale >= 1.5} onClick={() => setZoom(Math.min(1.5, Math.round((scale + 0.1) * 10) / 10))}>
            <Plus size={14} />
          </button>
          <button type="button" className={`icon-btn ${zoom === null ? "is-on" : ""}`} aria-label="Fit to window" title="Fit to window" onClick={() => setZoom(null)}>
            <Maximize2 size={13} />
          </button>
        </div>
      </footer>
    </div>
  );
}

/** Why the focus package is installed, in one line. */
function Fact({ p, roots }: { p: Pkg; roots: Pkg[] }) {
  if (p.onRequest || p.kind === "cask") return <span className="depmap-fact">You installed {p.short} yourself.</span>;
  if (!roots.length) return <span className="depmap-fact">Nothing you installed needs {p.short} any more; Cleanup can remove it.</span>;
  const names = roots.slice(0, 4).map((r) => r.short);
  const more = roots.length - names.length;
  return (
    <span className="depmap-fact">
      Here because you installed {names.join(", ")}
      {more > 0 ? ` and ${more} more` : ""}.
    </span>
  );
}
