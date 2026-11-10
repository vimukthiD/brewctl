import { useMemo, useState } from "react";
import { ArrowDown, ArrowUp, PackageSearch } from "lucide-react";
import { Empty, PkgTags, Spinner } from "../components";
import { formatBytes, timeAgo, type Pkg } from "../lib/api";
import { useBrew } from "../state";

type Filter = "all" | "requested" | "deps" | "casks" | "outdated";
type SortKey = "name" | "size" | "installed";

const FILTERS: { id: Filter; label: string; test: (p: Pkg) => boolean }[] = [
  { id: "all", label: "All", test: () => true },
  { id: "requested", label: "Installed by you", test: (p) => p.onRequest },
  { id: "deps", label: "Dependencies", test: (p) => !p.onRequest },
  { id: "casks", label: "Apps", test: (p) => p.kind === "cask" },
  { id: "outdated", label: "Outdated", test: (p) => p.outdated },
];

export function Installed({ query }: { query: string }) {
  const { pkgs, sizes, select, selected, busy, loading } = useBrew();
  const [filter, setFilter] = useState<Filter>("all");
  const [sort, setSort] = useState<SortKey>("name");
  const [desc, setDesc] = useState(false);

  const q = query.trim().toLowerCase();
  const matchesQuery = (p: Pkg) =>
    !q || p.name.toLowerCase().includes(q) || p.desc.toLowerCase().includes(q) || p.displayName.toLowerCase().includes(q);

  const searched = useMemo(() => pkgs.filter(matchesQuery), [pkgs, q]); // eslint-disable-line react-hooks/exhaustive-deps

  const rows = useMemo(() => {
    const test = FILTERS.find((f) => f.id === filter)!.test;
    const list = searched.filter(test);
    const dir = desc ? -1 : 1;
    list.sort((a, b) => {
      if (sort === "size") return dir * ((sizes?.[a.key] ?? 0) - (sizes?.[b.key] ?? 0)) || a.short.localeCompare(b.short);
      if (sort === "installed") return dir * (a.installedAt - b.installedAt) || a.short.localeCompare(b.short);
      return dir * a.short.localeCompare(b.short);
    });
    return list;
  }, [searched, filter, sort, desc, sizes]);

  const maxSize = useMemo(() => (sizes ? Math.max(1, ...pkgs.map((p) => sizes[p.key] ?? 0)) : 1), [pkgs, sizes]);

  const setSortKey = (k: SortKey) => {
    if (k === sort) setDesc(!desc);
    else {
      setSort(k);
      setDesc(k !== "name"); // biggest / newest first
    }
  };

  const Th = ({ k, children, className }: { k: SortKey; children: string; className?: string }) => (
    <th className={className} aria-sort={sort === k ? (desc ? "descending" : "ascending") : "none"}>
      <button type="button" className="th-btn" onClick={() => setSortKey(k)}>
        {children}
        {sort === k && (desc ? <ArrowDown size={12} /> : <ArrowUp size={12} />)}
      </button>
    </th>
  );

  return (
    <div className="page page-table">
      <div className="filters" role="tablist" aria-label="Filter packages">
        {FILTERS.map((f) => {
          const n = searched.filter(f.test).length;
          return (
            <button key={f.id} type="button" role="tab" aria-selected={filter === f.id} className={`chip ${filter === f.id ? "is-on" : ""}`} onClick={() => setFilter(f.id)}>
              {f.label}
              <span className="chip-n">{n}</span>
            </button>
          );
        })}
      </div>

      {loading ? (
        <div className="table-skel" aria-busy="true">
          {Array.from({ length: 10 }, (_, i) => (
            <div key={i} className="skel-row">
              <span className="skel" style={{ width: `${18 + ((i * 7) % 12)}%` }} />
              <span className="skel" style={{ width: `${30 + ((i * 11) % 25)}%` }} />
            </div>
          ))}
        </div>
      ) : rows.length === 0 ? (
        <Empty icon={<PackageSearch size={26} />} title={q ? `Nothing matches “${query.trim()}”` : "Nothing here"}>
          {q ? "Search looks at names and descriptions of installed packages." : "No installed packages in this group."}
        </Empty>
      ) : (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <Th k="name">Name</Th>
                <th className="col-ver">Version</th>
                <Th k="size" className="col-size">
                  Size
                </Th>
                <Th k="installed" className="col-date">
                  Installed
                </Th>
              </tr>
            </thead>
            <tbody>
              {rows.map((p) => {
                const n = sizes?.[p.key];
                return (
                  <tr
                    key={p.key}
                    className={`${selected?.key === p.key ? "is-selected" : ""} ${busy.has(p.key) ? "is-busy" : ""}`}
                    onClick={() => select(p.key)}
                    tabIndex={0}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        select(p.key);
                      }
                    }}
                  >
                    <td>
                      <div className="cell-name">
                        <span className={`dot ${p.kind === "cask" ? "seg-app" : p.onRequest ? "seg-req" : "seg-dep"}`} title={p.kind === "cask" ? "App" : p.onRequest ? "Installed by you" : "Dependency"} />
                        <div className="cell-main">
                          <div className="cell-title">
                            <span className="name">{p.short}</span>
                            <PkgTags p={p} />
                            {busy.has(p.key) && <Spinner size={12} />}
                          </div>
                          <div className="cell-desc">{p.desc || (p.onRequest ? "" : "Dependency")}</div>
                        </div>
                      </div>
                    </td>
                    <td className="col-ver">
                      <span className="ver">{p.version}</span>
                    </td>
                    <td className="col-size">
                      <span className="size-cell">
                        <span className="size-num">{sizes ? formatBytes(n ?? 0) : "…"}</span>
                        <span className="size-bar">
                          <span style={{ width: `${sizes ? Math.max(1.5, ((n ?? 0) / maxSize) * 100) : 0}%` }} />
                        </span>
                      </span>
                    </td>
                    <td className="col-date">{timeAgo(p.installedAt)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
