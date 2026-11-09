"use client";
import { CornerDownLeft, MessageSquareText, Search } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { cn } from "@/lib/cn";
import { Kbd } from "@/ui/atoms";
import { Modal } from "@/ui/cells";

export type PaletteItem = { id: string; group: string; label: ReactNode; hint?: ReactNode; icon?: ReactNode; onSelect: () => void };

/** ⌘K: ask Atlas, jump to a screen, or open a document. Arrow keys move, Enter runs. */
export function CommandPalette({
  open, onOpenChange, query, onQueryChange, items, onAsk,
}: {
  open: boolean; onOpenChange: (o: boolean) => void; query: string; onQueryChange: (q: string) => void;
  items: PaletteItem[]; onAsk: (q: string) => void;
}) {
  const all = useMemo<PaletteItem[]>(() => {
    const ask: PaletteItem[] = query.trim()
      ? [{ id: "ask", group: "Ask", label: <>Ask Atlas: <span className="font-medium">“{query.trim()}”</span></>, icon: <MessageSquareText />, onSelect: () => onAsk(query.trim()) }]
      : [];
    return [...ask, ...items];
  }, [items, query, onAsk]);
  // The cursor resets whenever the query or the open state changes.
  const key = `${open}|${query}`;
  const [pos, setPos] = useState({ key, cursor: 0 });
  const cursor = pos.key === key ? pos.cursor : 0;
  const setCursor = (fn: (c: number) => number) => setPos({ key, cursor: fn(cursor) });

  const groups = all.reduce<Record<string, PaletteItem[]>>((acc, it) => ((acc[it.group] ??= []).push(it), acc), {});
  const run = (it?: PaletteItem) => { if (it) { it.onSelect(); onOpenChange(false); } };

  return (
    <Modal open={open} onOpenChange={onOpenChange} title="Search or ask" labelHidden instant className="overflow-hidden">
      <div className="flex items-center gap-3 border-b border-rule px-4">
        <Search aria-hidden className="size-4 text-muted" />
        <input
          autoFocus
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") { e.preventDefault(); setCursor((c) => Math.min(c + 1, all.length - 1)); }
            if (e.key === "ArrowUp") { e.preventDefault(); setCursor((c) => Math.max(c - 1, 0)); }
            if (e.key === "Enter") { e.preventDefault(); run(all[cursor]); }
          }}
          role="combobox"
          aria-expanded
          aria-controls="palette-list"
          aria-activedescendant={all[cursor] ? `pal-${all[cursor].id}` : undefined}
          placeholder="Ask a question, find a document or jump to a screen"
          className="h-14 flex-1 bg-transparent text-section outline-none placeholder:text-faint"
        />
        <Kbd>Esc</Kbd>
      </div>
      <div id="palette-list" role="listbox" aria-label="Results" className="max-h-[min(420px,60vh)] overflow-y-auto p-2">
        {all.length === 0 && <p className="px-3 py-6 text-center text-body text-muted">Nothing matches. Press Enter to ask Atlas instead.</p>}
        {Object.entries(groups).map(([group, list]) => (
          <div key={group} role="group" aria-label={group} className="mb-1">
            <div className="px-3 pt-2 pb-1 text-small text-muted">{group}</div>
            {list.map((it) => {
              const i = all.indexOf(it);
              return (
                <div
                  key={it.id}
                  id={`pal-${it.id}`}
                  role="option"
                  aria-selected={i === cursor}
                  onMouseMove={() => setCursor(() => i)}
                  onClick={() => run(it)}
                  className={cn("flex h-10 cursor-default items-center gap-3 rounded-control px-3 text-body [&_svg]:size-4 [&_svg]:text-muted", i === cursor && "bg-raised")}
                >
                  {it.icon}
                  <span className="min-w-0 flex-1 truncate">{it.label}</span>
                  {it.hint && <span className="text-small text-muted">{it.hint}</span>}
                  {i === cursor && <CornerDownLeft aria-hidden />}
                </div>
              );
            })}
          </div>
        ))}
      </div>
    </Modal>
  );
}
