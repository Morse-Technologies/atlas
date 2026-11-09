"use client";
import { useState } from "react";
import { newKey, usePeople, useSourceActions } from "@/features/hooks";
import type { Person, Source } from "@/lib/api/types";
import { Button, Checkbox } from "@/ui/atoms";
import { Card, Sheet } from "@/ui/cells";

const summary = (s: Source, people: Person[]) => {
  if (s.access === "everyone") return "Everyone who signs in";
  const names = s.readers.map((id) => people.find((p) => p.id === id)?.name ?? "Someone").join(", ");
  return `Only ${names}`;
};

/** Who may read each source: everyone, or the people listed. Hidden documents do not exist for the others. */
export function SourceAccess({ sources }: { sources: Source[] }) {
  const people = usePeople().data ?? [];
  const [editing, setEditing] = useState<Source | null>(null);
  const folders = sources.filter((s) => s.kind === "folder" || s.kind === "upload");
  return (
    <Card id="access-h" title="Who can read" aside="Per source" className="mt-5">
      <ul className="divide-y divide-rule">
        {folders.map((s) => (
          <li key={s.id} className="flex items-center justify-between gap-4 px-5 py-3">
            <div className="min-w-0">
              <p className="truncate text-body font-medium">{s.name}</p>
              <p className="truncate text-small text-muted">{summary(s, people)}</p>
            </div>
            <Button variant="outline" size="sm" onClick={() => setEditing(s)} aria-label={`Change who can read ${s.name}`}>Change</Button>
          </li>
        ))}
      </ul>
      {editing && <AccessSheet key={editing.id} source={editing} people={people} onClose={() => setEditing(null)} />}
    </Card>
  );
}

function AccessSheet({ source, people, onClose }: { source: Source; people: Person[]; onClose: () => void }) {
  const { access } = useSourceActions();
  const [restricted, setRestricted] = useState(source.access === "restricted");
  const [readers, setReaders] = useState(new Set(source.readers));
  const toggle = (id: string, on: boolean) => setReaders((r) => { const next = new Set(r); if (on) next.add(id); else next.delete(id); return next; });
  const save = () => access.mutate(
    { id: source.id, key: newKey(), input: restricted ? { access: "restricted", readers: [...readers] } : { access: "everyone" } },
    { onSuccess: onClose },
  );
  return (
    <Sheet open onOpenChange={(o) => !o && onClose()} title={source.name} description="Who can read the documents of this source">
      <div className="flex flex-col gap-4 p-5">
        <label className="flex items-center gap-3 text-body">
          <Checkbox checked={!restricted} onCheckedChange={(v) => setRestricted(!v)} aria-label="Everyone who signs in" />Everyone who signs in
        </label>
        <fieldset disabled={!restricted} className="flex flex-col gap-3 disabled:opacity-50">
          <legend className="mb-1 text-body font-medium">Only these people</legend>
          {people.map((p) => (
            <label key={p.id} className="flex items-center gap-3 text-body">
              <Checkbox checked={readers.has(p.id)} onCheckedChange={(v) => toggle(p.id, v)} aria-label={p.name} />{p.name}
              <span className="text-small text-muted">{p.email}</span>
            </label>
          ))}
        </fieldset>
        <p className="text-small text-muted">Others will not see these documents in search, answers, lists or counts. They cannot tell that they exist.</p>
        <Button variant="primary" onClick={save} disabled={access.isPending || (restricted && readers.size === 0)}>Save</Button>
      </div>
    </Sheet>
  );
}
