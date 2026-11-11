import { useEffect, useMemo, useState } from "react";
import { Lock, Trash2 } from "lucide-react";
import { Button, CommandHint, Spinner } from "./components";
import { Brew, LEFTOVER_GROUPS, errorMessage, formatBytes, plural, type Leftover, type Pkg, type UninstallPlan } from "./lib/api";
import { useBrew } from "./state";

/** Confirms an uninstall and offers to clear what `brew uninstall` leaves behind. */
export function UninstallDialog({ p }: { p: Pkg }) {
  const { requestUninstall, uninstall } = useBrew();
  const [plan, setPlan] = useState<UninstallPlan | null>(null);
  const [found, setFound] = useState<Leftover[] | null>(null);
  const [scanError, setScanError] = useState<string | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());

  const close = () => requestUninstall(null);

  useEffect(() => {
    let live = true;
    Brew.UninstallPlan({ name: p.name, kind: p.kind })
      .then((pl) => live && setPlan(pl))
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [p.name, p.kind]);

  useEffect(() => {
    let live = true;
    Brew.Leftovers({ name: p.name, kind: p.kind })
      .then((list) => {
        if (!live) return;
        const items = list ?? [];
        setFound(items);
        // Preselect only what certainly belongs to this package. Folders
        // matched by name alone are the user's call.
        setPicked(new Set(items.filter((l) => l.source === "cache").map((l) => l.path)));
      })
      .catch((e) => {
        if (!live) return;
        setFound([]);
        setScanError(errorMessage(e));
      });
    return () => {
      live = false;
    };
  }, [p.name, p.kind]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        close();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  });

  const groups = useMemo(
    () =>
      LEFTOVER_GROUPS.map((g) => ({ ...g, items: (found ?? []).filter((l) => l.source === g.source) })).filter((g) => g.items.length),
    [found],
  );
  const pickedBytes = (found ?? []).filter((l) => picked.has(l.path)).reduce((n, l) => n + l.size, 0);
  const trashCount = (found ?? []).filter((l) => picked.has(l.path) && l.source !== "cache").length;
  const adminCount = (found ?? []).filter((l) => picked.has(l.path) && l.admin).length;

  const toggle = (path: string) =>
    setPicked((cur) => {
      const next = new Set(cur);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });

  const flag = p.kind === "cask" ? "--cask " : "";

  return (
    <div className="scrim" onMouseDown={close}>
      <div className="dialog dialog-wide" role="alertdialog" aria-modal="true" aria-labelledby="un-title" onMouseDown={(e) => e.stopPropagation()}>
        <div className="dialog-icon is-danger">
          <Trash2 size={20} />
        </div>
        <h2 id="un-title">Uninstall {p.short}?</h2>
        <div className="dialog-body">
          {p.kind === "cask"
            ? "Removes the app and the files Homebrew installed for it."
            : p.deps.length
              ? `Removes ${p.short} from your Mac. Its dependencies stay until you clear unused ones from Cleanup.`
              : `Removes ${p.short} from your Mac.`}
        </div>
        {plan?.stopService && (
          <div className="dialog-body">
            {plan.rootService
              ? `Its background service runs as the system administrator and is ${plan.running ? "running" : "set to start with the Mac"}. Brewctl stops it first and gives its files back to you, which asks for your Mac password. If that doesn't work, nothing is uninstalled.`
              : `Its background service is ${plan.running ? "running" : "set to start at login"}, so Brewctl stops it first. Homebrew would otherwise leave it running.`}
          </div>
        )}
        {plan?.reclaim && !plan.rootService && (
          <div className="dialog-body">
            Its service once ran as the system administrator, which left some of its files owned by the system. Brewctl gives them back to you first, which asks for your Mac password.
          </div>
        )}
        <CommandHint command={plan?.command ?? `brew uninstall ${flag}${p.name}`} />

        <div className="leftovers">
          {found === null ? (
            <div className="pending">
              <Spinner /> Looking for files it would leave behind…
            </div>
          ) : scanError ? (
            <p className="quiet">Couldn't check for leftover files: {scanError}</p>
          ) : groups.length === 0 ? (
            <p className="quiet">No leftover files found. Uninstalling removes everything.</p>
          ) : (
            <>
              <div className="leftovers-head">
                <strong>Uninstalling leaves {plural(found.length, "item")} behind</strong>
                <span>Tick what to clear as well.</span>
              </div>
              <div className="leftovers-list">
                {groups.map((g) => (
                  <fieldset key={g.source}>
                    <legend>{g.title}</legend>
                    <p className="leftovers-hint">{g.hint}</p>
                    {g.items.map((l) => (
                      <label key={l.path} className="leftover" title={l.path}>
                        <input type="checkbox" checked={picked.has(l.path)} onChange={() => toggle(l.path)} />
                        <span className="leftover-path">{l.label}</span>
                        {l.admin && (
                          <span className="leftover-lock" title="A system file: removing it needs your Mac password">
                            <Lock size={11} aria-label="Needs your password" />
                          </span>
                        )}
                        <span className="leftover-size">{formatBytes(l.size)}</span>
                      </label>
                    ))}
                  </fieldset>
                ))}
              </div>
              {trashCount > 0 && (
                <p className="leftovers-foot">
                  {plural(trashCount, "item")} will go to the Trash, so you can put {trashCount === 1 ? "it" : "them"} back.
                  {adminCount > 0 && ` ${adminCount === 1 ? "One is a system file" : `${adminCount} are system files`}, so Brewctl asks for your Mac password once the uninstall is done.`}
                </p>
              )}
            </>
          )}
        </div>

        <div className="dialog-actions">
          <Button onClick={close}>Cancel</Button>
          <button
            type="button"
            className="btn btn-md btn-danger-solid"
            autoFocus
            onClick={() => {
              uninstall(p, [...picked]);
              close();
            }}
          >
            <span>{picked.size ? `Uninstall and clear ${formatBytes(pickedBytes)}` : "Uninstall"}</span>
          </button>
        </div>
      </div>
    </div>
  );
}
