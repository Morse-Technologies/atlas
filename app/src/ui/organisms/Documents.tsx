"use client";
import { cn } from "@/lib/cn";
import {
  createColumnHelper, createSortedRowModel, rowSortingFeature, sortFn_alphanumeric, tableFeatures, useTable, type SortingState,
} from "@tanstack/react-table";
import { ArrowDown, ArrowUp, Copy, TriangleAlert } from "lucide-react";
import Link from "next/link";
import { ViewTransition, useMemo, useState, type ReactNode } from "react";
import { toast } from "sonner";
import type { Document, DocumentVersion, FieldProvenance, GraphEdge, Person } from "@/lib/api/types";
import { daysUntil, fmtAgo, fmtDate } from "@/lib/format";
import { Avatar, Band, Pill } from "@/ui/atoms";
import { DocBandLink, DocChip, DocFurniture, DocStatusPill, EmptyState, ProvenanceTag, docLabel, revOf } from "@/ui/cells";

/* Table ------------------------------------------------------------------------------------- */
const features = tableFeatures({
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
  sortFns: { alphanumeric: sortFn_alphanumeric },
});
const col = createColumnHelper<typeof features, Document>();

function ReviewDue({ iso }: { iso: string | null }) {
  if (!iso) return <span className="text-faint">n/a</span>;
  const d = daysUntil(iso);
  return d < 0
    ? <span className="text-[var(--pill-negative-ink)] dark:text-negative" title={`Overdue by ${-d} days`}>{fmtDate(iso)}<span className="ml-1.5 text-small">overdue</span></span>
    : <span>{fmtDate(iso)}</span>;
}

export function DocumentTable({ rows, people, total }: { rows: Document[]; people: Map<string, Person>; total: number }) {
  const [sorting, setSorting] = useState<SortingState>([]);
  const columns = useMemo(() => col.columns([
    col.accessor((d) => `${d.code} ${d.title}`, {
      id: "document", header: "Document",
      cell: ({ row: { original: d } }) => (
        <Link href={`/documents/${d.id}`} className="group flex min-w-0 flex-col items-start gap-1 outline-offset-2">
          <ViewTransition name={`doc-${d.id}`} share="morph" default="none"><DocChip doc={d} className="transition-transform duration-[var(--dur-2)] ease-out group-hover:-translate-x-0.5" /></ViewTransition>
          <span className="block max-w-full truncate text-body group-hover:underline">{!d.code ? d.path : d.title}</span>
        </Link>
      ),
    }),
    col.accessor((d) => revOf(d), { id: "rev", header: "Rev.", cell: (c) => <span className="num">{c.getValue()}</span> }),
    col.accessor((d) => (d.ownerId ? people.get(d.ownerId)?.name ?? "" : ""), {
      id: "owner", header: "Owner",
      cell: ({ row: { original: d } }) => {
        const p = d.ownerId ? people.get(d.ownerId) : null;
        return p ? <span className="inline-flex items-center gap-2"><Avatar name={p.name} hue={p.hue} size={20} /><span className="truncate">{p.name}</span></span>
          : <span className="text-[var(--pill-warning-ink)]">No owner</span>;
      },
    }),
    col.accessor((d) => d.department, { id: "where", header: "Department", cell: (c) => <span className="truncate text-muted">{c.getValue()}</span> }),
    col.accessor((d) => d.reviewDueAt ?? "9999", { id: "review", header: "Review due", cell: ({ row: { original: d } }) => <ReviewDue iso={d.reviewDueAt} /> }),
    col.accessor("status", { header: "Status", cell: (c) => <DocStatusPill status={c.getValue()} /> }),
  ]), [people]);
  const table = useTable({ features, columns, data: rows, state: { sorting }, onSortingChange: setSorting });

  if (rows.length === 0) return <EmptyState>No documents match these filters. Clear the search or pick another kind.</EmptyState>;

  return (
    <>
      {/* Desktop and tablet: a real table */}
      <div className="relative hidden overflow-x-auto md:block" tabIndex={0} role="region" aria-label="Documents">
        <table className="w-full table-fixed text-body">
          <caption className="sr-only">Documents, {rows.length} of {total}</caption>
          <colgroup>
            <col className="w-[40%]" /><col className="w-[6%]" /><col className="w-[17%]" /><col className="w-[15%]" /><col className="w-[12%]" /><col className="w-[10%]" />
          </colgroup>
          <thead>
            {table.getHeaderGroups().map((hg) => (
              <tr key={hg.id} className="border-b border-border">
                {hg.headers.map((h) => {
                  const dir = h.column.getIsSorted();
                  return (
                    <th key={h.id} scope="col" aria-sort={dir === "asc" ? "ascending" : dir === "desc" ? "descending" : "none"} className={cn("h-10 px-3 text-left font-normal text-small text-muted first:pl-0", h.column.id === "rev" && "text-right")}>
                      <button type="button" onClick={h.column.getToggleSortingHandler()} className="inline-flex items-center gap-1 hover:text-text">
                        <table.FlexRender header={h} />
                        {dir === "asc" ? <ArrowUp className="size-3" /> : dir === "desc" ? <ArrowDown className="size-3" /> : null}
                      </button>
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {table.getRowModel().rows.map((r) => (
              <tr key={r.id} className="border-b border-rule transition-colors duration-[var(--dur-1)] hover:bg-raised/60">
                {r.getAllCells().map((c) => (
                  <td key={c.id} className={cn("h-14 truncate px-3 first:pl-0", c.column.id === "rev" && "text-right")}><table.FlexRender cell={c} /></td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {/* Phone: two-line rows */}
      <ul className="divide-y divide-rule md:hidden">
        {table.getRowModel().rows.map(({ original: d }) => (
          <li key={d.id}>
            <Link href={`/documents/${d.id}`} className="flex items-center gap-3 py-3">
              <span className="min-w-0 flex-1">
                <span className="flex items-center justify-between gap-2">
                  <span className="flex min-w-0 items-center gap-2"><DocChip doc={d} /><span className="text-small text-muted">rev.{revOf(d)}</span></span>
                  <DocStatusPill status={d.status} />
                </span>
                <span className="block truncate text-small text-muted">{!d.code ? d.path : d.title}, {d.department}</span>
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </>
  );
}

/* Detail panel ----------------------------------------------------------------------------------- */
function Section({ title, children, aside }: { title: string; children: ReactNode; aside?: ReactNode }) {
  return (
    <section className="border-t border-rule px-5 py-5 first:border-t-0">
      <div className="mb-3 flex items-baseline justify-between"><h2 className="text-body font-medium">{title}</h2>{aside}</div>
      {children}
    </section>
  );
}

export function DocumentHeader({ doc, owner, backHref }: { doc: Document; owner: Person | null; backHref?: string }) {
  const band = (
    <ViewTransition name={`doc-${doc.id}`} share="morph" default="none">
      <Band kind={doc.band} arrow={!!backHref} size="lg">{docLabel(doc)} rev.{revOf(doc)}</Band>
    </ViewTransition>
  );
  return (
    <div className="px-5 pt-5 pb-4">
      {backHref ? <Link href={backHref} aria-label="Back to the answer that cites this document" className="block rounded-[4px] outline-offset-2">{band}</Link> : band}
      <h1 className="mt-4 text-[20px] leading-7 font-medium">{doc.title}</h1>
      <p className="mt-1 text-body text-muted">{doc.summary}</p>
      <DocFurniture doc={doc} owner={owner} className="mt-3" />
      {doc.status === "obsolete" && (
        <p role="alert" className="mt-4 flex gap-2 rounded-card bg-[var(--pill-negative-bg)] px-3 py-2.5 text-body text-[var(--pill-negative-ink)]">
          <TriangleAlert aria-hidden className="mt-0.5 size-4 flex-none" />
          This is not the current revision. Don&apos;t work from it.
        </p>
      )}
    </div>
  );
}

export function MetadataList({ rows }: { rows: FieldProvenance[] }) {
  return (
    <dl className="flex flex-col">
      {rows.map((r) => (
        <div key={r.field} className="grid grid-cols-[96px_1fr] gap-3 py-1.5 text-body">
          <dt className="text-muted">{r.field}</dt>
          <dd className="min-w-0">
            <span className="block">{r.value}</span>
            <ProvenanceTag source={r.source} confidence={r.confidence} />
          </dd>
        </div>
      ))}
    </dl>
  );
}

export function Revisions({ versions, current }: { versions: DocumentVersion[]; current: string }) {
  return (
    <ol className="flex flex-col">
      {[...versions].reverse().map((v) => (
        <li key={v.id} className="flex items-center justify-between gap-3 border-b border-rule py-2 text-body last:border-0">
          <span className="font-medium num">rev. {v.revision}</span>
          <span className="flex-1 text-small text-muted" suppressHydrationWarning>indexed {fmtAgo(v.indexedAt)}</span>
          {v.id === current ? <Pill tone="positive">Current</Pill> : <Pill>Superseded</Pill>}
        </li>
      ))}
    </ol>
  );
}

const via: Record<GraphEdge["kind"], string> = {
  references: "Referenced", contradicts: "Contradicts", duplicates: "Same code or copy", cites_norm: "Cites", mentions: "Mentions", owned_by: "Owner", belongs_to: "Department",
};
export function RelatedDocs({ items }: { items: { document: Document; via: GraphEdge["kind"] }[] }) {
  if (!items.length) return <p className="text-body text-muted">No links to other documents yet.</p>;
  return (
    <ul className="flex flex-col gap-2">
      {items.map(({ document: d, via: v }) => (
        <li key={`${d.id}-${v}`}>
          <DocBandLink doc={d} href={`/documents/${d.id}`} suffix={via[v]} />
        </li>
      ))}
    </ul>
  );
}

export function Location({ path, source }: { path: string; source: string }) {
  return (
    <div className="flex items-start gap-2">
      <div className="min-w-0 flex-1">
        <p className="text-small text-muted">{source}</p>
        <p className="mt-0.5 text-body break-all">{path}</p>
      </div>
      <button type="button" aria-label="Copy path" className="grid size-8 flex-none place-items-center rounded-control text-muted hover:bg-raised hover:text-text"
        onClick={() => { navigator.clipboard?.writeText(path); toast("Path copied"); }}>
        <Copy className="size-4" />
      </button>
    </div>
  );
}

export { Section as DetailSection };
