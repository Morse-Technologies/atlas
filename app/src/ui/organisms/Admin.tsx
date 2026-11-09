"use client";
import { Copy, Pencil } from "lucide-react";
import type { ReactNode } from "react";
import { toast } from "sonner";
import type { AuditEvent, Document, Person, Role } from "@/lib/api/types";
import { cn } from "@/lib/cn";
import { fmtAgo, fmtDate, fmtTime } from "@/lib/format";
import { Avatar, Button, Checkbox, Mark, Pill } from "@/ui/atoms";
import { EmptyState, WithDocChips } from "@/ui/cells";

/* Audit log ------------------------------------------------------------------------------------- */
const actionLabel: Record<string, string> = {
  "answer.generated": "Answer generated", "answer.feedback": "Feedback", "metadata.corrected": "Metadata corrected",
  "alert.raised": "Alert raised", "alert.dismissed": "Alert dismissed", "source.updated": "Source changed",
  "source.synced": "Source synced", "source.sync_failed": "Sync failed", "document.quarantined": "Quarantined",
  "user.login": "Signed in", "audit.exported": "Log exported",
};

export function AuditTable({ events, people, docs }: { events: AuditEvent[]; people: Map<string, Person>; docs: Document[] }) {
  if (!events.length) return <EmptyState>No events match this filter.</EmptyState>;
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[860px] table-fixed text-body">
        <caption className="sr-only">Audit events, newest first</caption>
        <colgroup><col className="w-[60px]" /><col className="w-[150px]" /><col className="w-[170px]" /><col className="w-[170px]" /><col /><col className="w-[120px]" /></colgroup>
        <thead>
          <tr className="border-b border-border text-left text-small text-muted">
            <th scope="col" className="h-10 text-right font-normal">#</th>
            <th scope="col" className="h-10 pl-5 font-normal">When</th>
            <th scope="col" className="h-10 font-normal">Who</th>
            <th scope="col" className="h-10 font-normal">What</th>
            <th scope="col" className="h-10 font-normal">Detail</th>
            <th scope="col" className="h-10 font-normal">Hash</th>
          </tr>
        </thead>
        <tbody>
          {events.map((e) => {
            const p = e.actorId ? people.get(e.actorId) : null;
            return (
              <tr key={e.id} className="border-b border-rule align-top">
                <td className="py-3 text-right text-muted num">{e.id}</td>
                <td className="py-3 pl-5"><span className="block num" suppressHydrationWarning>{fmtDate(e.at)}</span><span className="block text-small text-muted num" suppressHydrationWarning>{fmtTime(e.at)}</span></td>
                <td className="py-3">
                  {p ? <span className="inline-flex items-center gap-2"><Avatar name={p.name} hue={p.hue} size={20} /><span className="truncate">{p.name}</span></span>
                    : <span className="inline-flex items-center gap-2 text-muted"><Mark className="size-5" />Atlas</span>}
                </td>
                <td className="py-3"><span className="block">{actionLabel[e.action] ?? e.action}</span><span className="block truncate text-small text-muted">{e.subject}</span></td>
                <td className="py-3 pr-4"><span className="line-clamp-2"><WithDocChips text={e.detail} docs={docs} /></span></td>
                <td className="py-3">
                  <button type="button" onClick={() => { navigator.clipboard?.writeText(e.hash); toast("Hash copied"); }}
                    className="inline-flex items-center gap-1.5 rounded-control num text-small text-muted hover:text-text" aria-label={`Copy hash of event ${e.id}`}>
                    {e.hash.slice(0, 8)}…<Copy className="size-3" />
                  </button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

/* Users ------------------------------------------------------------------------------------------ */
export const roleInfo: Record<Role, { label: string; help: string }> = {
  viewer: { label: "Ask", help: "Asks questions and opens the documents they already have access to." },
  editor_metadata: { label: "Edit metadata", help: "Corrects codes, owners and dates. Corrections survive re-indexing." },
  curator: { label: "Curator", help: "Looks after the documentation: works through alerts, confirms or rejects suggested links and entities." },
  organizer_approver: { label: "Approve reorganisation", help: "Signs folder reorganisation plans before anything moves." },
  admin: { label: "Administrator", help: "Manages sources, people, models and the license." },
  auditor: { label: "Auditor", help: "Reads and exports the audit log. Read-only everywhere else." },
};

export function UserTable({ people, onEdit }: { people: Person[]; onEdit: (p: Person) => void }) {
  return (
    <>
    <div aria-hidden className="hidden grid-cols-[minmax(0,2fr)_140px_minmax(0,2fr)_120px_auto] gap-x-4 border-b border-border pb-2 text-small text-muted md:grid">
      <span>Person</span><span>Department</span><span>Roles</span><span>Last active</span><span className="w-[72px]" />
    </div>
    <ul className="divide-y divide-rule">
      {people.map((p) => (
        <li key={p.id} className={cn("grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 py-3 md:grid-cols-[minmax(0,2fr)_140px_minmax(0,2fr)_120px_auto]", !p.isActive && "text-muted")}>
          <span className="flex min-w-0 items-center gap-3">
            <Avatar name={p.name} hue={p.hue} size={32} className={cn(!p.isActive && "grayscale")} />
            <span className="min-w-0">
              <span className="block truncate font-medium">{p.name}</span>
              <span className="block truncate text-small text-muted">{p.email}</span>
            </span>
          </span>
          <span className="hidden text-body md:block">{p.department}</span>
          <span className="col-span-2 flex flex-wrap gap-1.5 md:col-span-1">
            {p.roles.map((r) => <Pill key={r} tone="neutral">{roleInfo[r].label}</Pill>)}
            {!p.isActive && <Pill tone="warning">Deactivated</Pill>}
          </span>
          <span className="hidden text-small text-muted md:block" suppressHydrationWarning>{p.isActive ? fmtAgo(p.lastActiveAt) : "n/a"}</span>
          <Button size="sm" variant="ghost" onClick={() => onEdit(p)} aria-label={`Edit roles for ${p.name}`} className="row-start-1 justify-self-end md:row-auto"><Pencil />Roles</Button>
        </li>
      ))}
    </ul>
    </>
  );
}

export function RoleEditor({ roles, onChange }: { roles: Role[]; onChange: (r: Role[]) => void }) {
  return (
    <fieldset className="flex flex-col">
      <legend className="sr-only">Roles</legend>
      {(Object.keys(roleInfo) as Role[]).map((r) => (
        <div key={r} className="flex items-start gap-3 border-b border-rule px-5 py-3.5 last:border-0">
          <Checkbox id={`role-${r}`} checked={roles.includes(r)} aria-describedby={`role-${r}-help`}
            onCheckedChange={(v) => onChange(v ? [...roles, r] : roles.filter((x) => x !== r))} className="mt-0.5" />
          <label htmlFor={`role-${r}`} className="text-body">
            <span className="font-medium">{roleInfo[r].label}</span>
            <span id={`role-${r}-help`} className="block text-small text-muted">{roleInfo[r].help}</span>
          </label>
        </div>
      ))}
    </fieldset>
  );
}

/* Settings: a label column beside a panel ---------------------------------------------------------- */
export function SettingsSection({ title, description, children, aside }: { title: string; description: ReactNode; children: ReactNode; aside?: ReactNode }) {
  return (
    <section className="grid gap-4 border-t border-rule py-8 first:border-t-0 first:pt-2 md:grid-cols-[minmax(0,360px)_minmax(0,1fr)] md:gap-10" aria-label={title}>
      <div className="md:pt-1.5">
        <h2 className="text-section font-medium">{title}</h2>
        <p className="mt-1 text-body text-muted">{description}</p>
        {aside}
      </div>
      <div className="flex flex-col gap-5 rounded-card bg-raised p-5">{children}</div>
    </section>
  );
}

export function SwitchRow({ title, description, control, badge }: { title: string; description: ReactNode; control: ReactNode; badge?: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-6">
      <div>
        <p className="flex items-center gap-2 text-body font-medium">{title}{badge}</p>
        <p className="mt-0.5 text-body text-muted">{description}</p>
      </div>
      <div className="pt-0.5">{control}</div>
    </div>
  );
}

export const fmtWhen = (iso: string) => `${fmtDate(iso)}, ${fmtTime(iso)}`;
