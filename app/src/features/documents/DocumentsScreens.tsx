"use client";
import dynamic from "next/dynamic";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { ArrowLeft, FileX } from "lucide-react";
import { useDeferredValue, useMemo, useState } from "react";
import { useConversation, useDocument, useDocuments, usePeople, useRelated, useSources } from "@/features/hooks";
import type { BandKind, DocStatus, Person } from "@/lib/api/types";
import { daysUntil } from "@/lib/format";
import { CiteMark, Input, Select, Skeleton, bandLabel } from "@/ui/atoms";
import { ErrorState, LoadingRows, PageHeader, Segmented, docLabel, revOf } from "@/ui/cells";
import {
  DetailSection, DocumentHeader, DocumentTable, Location, MetadataList, RelatedDocs, Revisions,
} from "@/ui/organisms/Documents";
import { Canvas } from "@/ui/templates/ShellLayout";

const PdfViewer = dynamic(() => import("@/ui/organisms/PdfViewer"), {
  ssr: false,
  loading: () => <div className="flex-1 bg-raised p-6"><Skeleton className="mx-auto aspect-[1/1.414] w-full max-w-[900px]" /></div>,
});

const usePeopleMap = () => {
  const { data } = usePeople();
  return useMemo(() => new Map<string, Person>((data ?? []).map((p) => [p.id, p])), [data]);
};

/* Documents list -------------------------------------------------------------------------------- */
/** Languages the server reports; anything else (or "" before detection) is not shown. */
const LANGUAGE_NAMES: Record<string, string> = { es: "Spanish", en: "English" };

export function DocumentsScreen() {
  const [q, setQ] = useState("");
  const [band, setBand] = useState<BandKind | "all">("all");
  const [status, setStatus] = useState<DocStatus | "all">("all");
  const query = useDeferredValue(q);
  const all = useDocuments({});
  const { data, isPending, isError, refetch } = useDocuments({ q: query, band, status });
  const people = usePeopleMap();
  const total = all.data?.length ?? 0;
  const countBand = (b: BandKind) => all.data?.filter((d) => d.band === b).length;
  const overdue = all.data?.filter((d) => d.reviewDueAt && d.status === "current" && daysUntil(d.reviewDueAt) < 0).length ?? 0;

  return (
    <Canvas>
      <PageHeader
        title="Documents"
        subline={all.data ? `${total} indexed from 6 sources. ${overdue} current ${overdue === 1 ? "document is" : "documents are"} past their review date.` : "Everything Atlas has indexed, with where each value came from."}
      />
      <div className="mt-6 flex flex-wrap items-center gap-3">
        <div className="w-full sm:w-72">
          <label htmlFor="doc-q" className="sr-only">Search documents</label>
          <Input id="doc-q" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Code, title, name or folder" className="h-8" />
        </div>
        <Segmented
          label="Kind"
          value={band}
          onChange={setBand}
          className="max-w-full overflow-x-auto"
          options={[
            { value: "all", label: "All", count: total || undefined },
            ...(["procedure", "manual", "norm", "erp"] as BandKind[]).map((b) => ({ value: b, label: bandLabel[b], count: countBand(b) })),
          ]}
        />
        <div className="w-40">
          <label htmlFor="doc-status" className="sr-only">Status</label>
          <Select id="doc-status" value={status} onChange={(e) => setStatus(e.target.value as DocStatus | "all")}>
            <option value="all">Any status</option>
            <option value="current">Current</option>
            <option value="obsolete">Obsolete</option>
            <option value="unknown">Unknown</option>
          </Select>
        </div>
        <span className="ml-auto text-small text-muted num" aria-live="polite">{data ? `${data.length} of ${total}` : ""}</span>
      </div>
      <div className="mt-5">
        {isPending ? <LoadingRows rows={8} pitch="h-14" /> : isError ? <ErrorState what="documents" onRetry={() => refetch()} />
          : <DocumentTable rows={data} people={people} total={total} />}
      </div>
    </Canvas>
  );
}

/* Viewer (Screen 4) ------------------------------------------------------------------------------- */
export function DocumentScreen({ id }: { id: string }) {
  const params = useSearchParams();
  const page = Number(params.get("page") ?? 1);
  const cite = params.get("cite");
  const from = params.get("from");
  const hl = params.get("hl")?.split(",").map(Number);
  const { data: doc, isPending, isError, refetch } = useDocument(id);
  const related = useRelated(id);
  const { data: sources } = useSources();
  const people = usePeopleMap();
  const owner = doc?.ownerId ? people.get(doc.ownerId) ?? null : null;

  if (isError) return <Canvas><ErrorState what="this document" onRetry={() => refetch()} /></Canvas>;

  return (
    <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
      <section aria-label="Document" className="flex min-h-[70vh] min-w-0 flex-1 flex-col lg:min-h-0">
        <div className="flex h-11 flex-none items-center gap-2 border-b border-rule px-4 text-body">
          {from ? <BackToAnswer conversationId={from} /> : (
            <Link href="/documents" className="inline-flex items-center gap-1.5 text-muted hover:text-text"><ArrowLeft className="size-4" />Documents</Link>
          )}
          {cite && doc && <span className="ml-auto inline-flex flex-none items-center gap-1.5 text-small whitespace-nowrap text-muted"><span className="hidden sm:inline">Showing source</span><CiteMark n={Number(cite)} kind={doc.band} /><span className="hidden sm:inline">{docLabel(doc)} page {page}</span><span className="sm:hidden">p.{page}</span></span>}
        </div>
        {isPending ? <div className="flex-1 bg-raised p-6"><Skeleton className="mx-auto aspect-[1/1.414] w-full max-w-[900px]" /></div>
          : doc.sample ? (
            <PdfViewer
              src={doc.sample}
              initialPage={page}
              highlight={hl && hl.length === 4 ? { page, x: hl[0], y: hl[1], w: hl[2], h: hl[3], label: `Source [${cite}]`, band: doc.band } : undefined}
            />
          ) : (
            <div className="grid flex-1 place-items-center bg-raised p-8">
              <div className="max-w-sm text-center">
                <FileX aria-hidden className="mx-auto size-6 text-muted" />
                <p className="mt-3 text-body font-medium">No preview in this demo snapshot</p>
                <p className="mt-1 text-body text-muted">
                  The demo ships one synthetic PDF (PNT-014). In a real install this page shows {docLabel(doc)} at page {page}.
                </p>
              </div>
            </div>
          )}
      </section>
      <aside aria-label="Document details" className="border-border bg-ground lg:h-[calc((100dvh-66px)/var(--content-zoom))] lg:w-[360px] lg:flex-none lg:overflow-y-auto lg:border-l">
        {isPending ? <div className="p-5"><LoadingRows rows={6} pitch="h-9" /></div> : (
          <>
            <DocumentHeader doc={doc} owner={owner} backHref={from ? `/ask/${from}` : undefined} />
            <DetailSection title="Metadata" aside={<span className="text-small text-muted">and where each value came from</span>}>
              <MetadataList rows={doc.provenance} />
            </DetailSection>
            <DetailSection title={`Revisions (${doc.versions.length})`}><Revisions versions={doc.versions} current={doc.currentVersionId} /></DetailSection>
            <DetailSection title="Related">
              {related.isPending ? <LoadingRows rows={2} pitch="h-8" /> : <RelatedDocs items={related.data ?? []} />}
            </DetailSection>
            <DetailSection title="Location">
              <Location path={doc.path} source={sources?.find((s) => s.id === doc.sourceId)?.name ?? ""} />
            </DetailSection>
            <p className="px-5 pb-6 text-small text-muted num">rev.{revOf(doc)}, {doc.versions.find((v) => v.id === doc.currentVersionId)?.pages} pages{LANGUAGE_NAMES[doc.language] ? `, ${LANGUAGE_NAMES[doc.language]}` : ""}</p>
          </>
        )}
      </aside>
    </div>
  );
}

function BackToAnswer({ conversationId }: { conversationId: string }) {
  const conv = useConversation(conversationId);
  return (
    <Link href={`/ask/${conversationId}`} className="inline-flex min-w-0 items-center gap-1.5 text-muted hover:text-text">
      <ArrowLeft className="size-4 flex-none" />
      <span className="truncate">Back to the answer{conv.data ? `: ${conv.data.conversation.title}` : ""}</span>
    </Link>
  );
}
