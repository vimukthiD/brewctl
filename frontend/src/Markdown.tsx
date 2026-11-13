import { createContext, Fragment, useContext, useState, type ReactNode } from "react";
import { Check, Copy } from "lucide-react";
import { openURL, type Ref } from "./lib/api";
import { useBrew } from "./state";

/**
 * Renders the small subset of Markdown the assistant writes: paragraphs,
 * lists, fenced code, `code`, **bold**, *italic* and [links](https://…).
 * Everything becomes React elements, so no HTML from the model is ever
 * injected. Inline code that names an installed package opens its details;
 * one that names a known catalogue package opens it in the catalogue panel.
 */
export function Markdown({ text, refs }: { text: string; refs?: Map<string, Ref> }) {
  return (
    <RefsContext.Provider value={refs ?? NO_REFS}>
      <div className="md">{blocks(text)}</div>
    </RefsContext.Provider>
  );
}

const NO_REFS = new Map<string, Ref>();
const RefsContext = createContext<Map<string, Ref>>(NO_REFS);

type Block = { kind: "table"; head: string[]; rows: string[][] } | { kind: "p"; lines: string[] } | { kind: "ul" | "ol"; items: string[] } | { kind: "code"; lang: string; body: string[] } | { kind: "h"; text: string };

function parse(text: string): Block[] {
  const out: Block[] = [];
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const fence = line.match(/^\s*```\s*([\w+-]*)\s*$/);
    if (fence) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !/^\s*```\s*$/.test(lines[i])) body.push(lines[i++]);
      i++; // closing fence (or end of a still-streaming answer)
      out.push({ kind: "code", lang: fence[1], body });
      continue;
    }
    if (!line.trim()) {
      i++;
      continue;
    }
    // A table: a header row, a |---|---| rule, then rows.
    if (/^\s*\|.*\|\s*$/.test(line) && i + 1 < lines.length && /^\s*\|?\s*:?-{2,}/.test(lines[i + 1])) {
      const cells = (l: string) => l.trim().replace(/^\||\|$/g, "").split("|").map((c) => c.trim());
      const head = cells(line);
      const rows: string[][] = [];
      i += 2;
      while (i < lines.length && /^\s*\|.*\|?\s*$/.test(lines[i]) && lines[i].trim()) rows.push(cells(lines[i++]));
      out.push({ kind: "table", head, rows });
      continue;
    }
    const h = line.match(/^#{1,4}\s+(.*)$/);
    if (h) {
      out.push({ kind: "h", text: h[1] });
      i++;
      continue;
    }
    const bullet = /^\s*[-*•]\s+(.*)$/;
    const num = /^\s*\d+[.)]\s+(.*)$/;
    for (const [re, kind] of [
      [bullet, "ul"],
      [num, "ol"],
    ] as const) {
      if (re.test(line)) {
        const items: string[] = [];
        while (i < lines.length && (re.test(lines[i]) || (/^\s{2,}\S/.test(lines[i]) && items.length))) {
          const m = lines[i].match(re);
          if (m) items.push(m[1]);
          else items[items.length - 1] += " " + lines[i].trim();
          i++;
        }
        out.push({ kind, items });
        break;
      }
    }
    if (bullet.test(line) || num.test(line)) continue;
    const para: string[] = [];
    while (i < lines.length && lines[i].trim() && !/^\s*```/.test(lines[i]) && !/^\s*\|.*\|\s*$/.test(lines[i]) && !bullet.test(lines[i]) && !num.test(lines[i]) && !/^#{1,4}\s/.test(lines[i])) {
      para.push(lines[i++]);
    }
    out.push({ kind: "p", lines: para });
  }
  return out;
}

function blocks(text: string): ReactNode {
  return parse(text).map((b, i) => {
    switch (b.kind) {
      case "code":
        return <CodeBlock key={i} body={b.body.join("\n")} />;
      case "table":
        return (
          <div key={i} className="md-table">
            <table>
              <thead>
                <tr>
                  {b.head.map((c, j) => (
                    <th key={j}>
                      <Inline text={c} />
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {b.rows.map((r, j) => (
                  <tr key={j}>
                    {b.head.map((_, k) => (
                      <td key={k}>
                        <Inline text={r[k] ?? ""} />
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        );
      case "h":
        return (
          <p key={i} className="md-h">
            <Inline text={b.text} />
          </p>
        );
      case "ul":
      case "ol": {
        const List = b.kind;
        return (
          <List key={i}>
            {b.items.map((it, j) => (
              <li key={j}>
                <Inline text={it} />
              </li>
            ))}
          </List>
        );
      }
      default:
        return (
          <p key={i}>
            {b.lines.map((l, j) => (
              <Fragment key={j}>
                {j > 0 && <br />}
                <Inline text={l} />
              </Fragment>
            ))}
          </p>
        );
    }
  });
}

const INLINE = /(``.+?``|`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\(https?:\/\/[^)\s]+\)|(?<![\w*])\*[^*\s][^*]*\*(?!\w))/g;

function Inline({ text }: { text: string }) {
  const { byShort, select, pkgs, showRemote } = useBrew();
  const refs = useContext(RefsContext);
  const parts = text.split(INLINE);
  return (
    <>
      {parts.map((part, i) => {
        if (i % 2 === 0) return part;
        if (part.startsWith("`")) {
          const code = (part.startsWith("``") ? part.slice(2, -2) : part.slice(1, -1)).trim();
          const name = code.slice(code.lastIndexOf("/") + 1);
          const pkg = byShort.get(name) ?? pkgs.find((p) => p.kind === "cask" && p.name === code);
          if (pkg && /^[\w@.+/-]+$/.test(code)) {
            return (
              <button key={i} type="button" className="md-pkg" title={`Show ${pkg.short}`} onClick={() => select(pkg.key)}>
                {code}
              </button>
            );
          }
          const ref = !pkg && refs.get(code);
          if (ref) {
            return (
              <button key={i} type="button" className="md-pkg is-remote" title={`Not installed. Show ${code} in the catalogue`} onClick={() => showRemote(ref)}>
                {code}
              </button>
            );
          }
          return <code key={i}>{code}</code>;
        }
        if (part.startsWith("**"))
          return (
            <strong key={i}>
              <Inline text={part.slice(2, -2)} />
            </strong>
          );
        if (part.startsWith("[")) {
          const m = part.match(/^\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)$/);
          if (m)
            return (
              <button key={i} type="button" className="md-link" title={m[2]} onClick={() => openURL(m[2])}>
                {m[1]}
              </button>
            );
          return part;
        }
        return <em key={i}>{part.slice(1, -1)}</em>;
      })}
    </>
  );
}

function CodeBlock({ body }: { body: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(body);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1400);
    } catch {
      /* clipboard unavailable */
    }
  };
  return (
    <div className="md-code">
      <pre>
        <code>{body}</code>
      </pre>
      <button type="button" className="cmd-copy" aria-label="Copy" title="Copy" onClick={copy}>
        {copied ? <Check size={13} /> : <Copy size={13} />}
      </button>
    </div>
  );
}
