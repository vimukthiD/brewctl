import { useEffect, useMemo, useRef, useState } from "react";
import { Compass, SearchX } from "lucide-react";
import { Button, Empty, Spinner, Tag } from "../components";
import { Brew, errorMessage, plural, refKey, type Kind, type SearchResult } from "../lib/api";
import { useBrew } from "../state";

const IDEAS = ["ripgrep", "node", "python", "docker", "ffmpeg", "visual studio code", "firefox", "font"];

type Filter = "all" | "formula" | "cask";

/** Search everything Homebrew can install, and install it. */
export function Discover({ query, setQuery, focusSearch }: { query: string; setQuery: (q: string) => void; focusSearch: () => void }) {
  const { pkgs, busy, install, select, showRemote, remote, selected } = useBrew();
  // Leaving Discover closes the panel for a package that isn't installed.
  useEffect(() => () => showRemote(null), []); // eslint-disable-line react-hooks/exhaustive-deps
  const [results, setResults] = useState<SearchResult[] | null>(null);
  const [searchedFor, setSearchedFor] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState<Filter>("all");
  const seq = useRef(0);

  const q = query.trim();

  useEffect(() => {
    focusSearch();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    const mine = ++seq.current;
    if (q.length < 2) {
      setResults(null);
      setLoading(false);
      setError(null);
      return;
    }
    setLoading(true);
    let call: ReturnType<typeof Brew.Search> | undefined;
    const timer = window.setTimeout(() => {
      call = Brew.Search(q);
      call
        .then((rs) => {
          if (mine !== seq.current) return;
          setResults(rs ?? []);
          setSearchedFor(q);
          setError(null);
          setLoading(false);
        })
        .catch((e) => {
          if (mine !== seq.current) return;
          setError(errorMessage(e));
          setLoading(false);
        });
    }, 280);
    return () => {
      window.clearTimeout(timer);
      call?.cancel?.();
    };
  }, [q]);

  const installed = useMemo(() => new Set(pkgs.map((p) => p.key)), [pkgs]);
  const shown = useMemo(() => (results ?? []).filter((r) => filter === "all" || r.kind === filter), [results, filter]);
  const count = (k: Filter) => (results ?? []).filter((r) => k === "all" || r.kind === k).length;

  if (q.length < 2) {
    return (
      <div className="page">
        <Empty icon={<Compass size={28} />} title="Find something to install">
          Search the names and descriptions of every tool and app in Homebrew.
          <div className="chips chips-center">
            {IDEAS.map((idea) => (
              <button key={idea} type="button" className="dep" onClick={() => setQuery(idea)}>
                {idea}
              </button>
            ))}
          </div>
        </Empty>
      </div>
    );
  }

  return (
    <div className="page page-table">
      <div className="filters" role="tablist" aria-label="Filter results">
        {(
          [
            ["all", "All"],
            ["formula", "Tools"],
            ["cask", "Apps"],
          ] as [Filter, string][]
        ).map(([id, label]) => (
          <button key={id} type="button" role="tab" aria-selected={filter === id} className={`chip ${filter === id ? "is-on" : ""}`} onClick={() => setFilter(id)}>
            {label}
            {results && <span className="chip-n">{count(id)}</span>}
          </button>
        ))}
        <span className="grow" />
        {loading && (
          <span className="pending">
            <Spinner size={13} /> Searching…
          </span>
        )}
      </div>

      {error ? (
        <Empty icon={<SearchX size={26} />} title="Search didn't work">
          {error}
        </Empty>
      ) : results === null ? (
        <div className="table-skel" aria-busy="true">
          {Array.from({ length: 8 }, (_, i) => (
            <div key={i} className="skel-row">
              <span className="skel" style={{ width: `${16 + ((i * 7) % 12)}%` }} />
              <span className="skel" style={{ width: `${34 + ((i * 11) % 25)}%` }} />
            </div>
          ))}
        </div>
      ) : shown.length === 0 ? (
        <Empty icon={<SearchX size={26} />} title={`Nothing found for “${searchedFor}”`}>
          Try a shorter word, or part of the name.
        </Empty>
      ) : (
        <ul className={`results ${loading ? "is-stale" : ""}`}>
          {shown.map((r) => {
            const key = refKey(r);
            const kind = r.kind as Kind;
            const has = installed.has(key);
            const isOpen = (remote && refKey(remote) === key) || selected?.key === key;
            return (
              <li key={key} className={`result ${isOpen ? "is-selected" : ""} ${busy.has(key) ? "is-working" : ""}`}>
                <button type="button" className="result-main" onClick={() => (has ? select(key) : showRemote({ name: r.name, kind }))}>
                  <span className={`dot ${kind === "cask" ? "seg-app" : "seg-req"}`} title={kind === "cask" ? "App" : "Command-line tool"} />
                  <span className="cell-main">
                    <span className="cell-title">
                      <span className="name">{r.name}</span>
                      {kind === "cask" && <Tag tone="blue">app</Tag>}
                      {kind === "cask" && r.displayName !== r.name && <span className="result-alt">{r.displayName}</span>}
                    </span>
                    <span className="cell-desc">{r.desc || "No description"}</span>
                  </span>
                </button>
                {has ? (
                  <Tag tone="green">installed</Tag>
                ) : (
                  <Button size="sm" busy={busy.has(key)} onClick={() => install({ name: r.name, kind })}>
                    Install
                  </Button>
                )}
              </li>
            );
          })}
          {results.length >= 120 && <li className="result-more">Showing the first {plural(results.length, "result")}. Narrow the search to see others.</li>}
        </ul>
      )}
    </div>
  );
}
