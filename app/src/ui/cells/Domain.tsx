import Link from "next/link";
import type { ReactNode } from "react";
import type { Alert, AlertStatus, Citation, DocStatus, Document, Person, ProvenanceSource, Severity, Source } from "@/lib/api/types";
import { cn } from "@/lib/cn";
import { Avatar, Band, Dot, Pill, type Tone } from "@/ui/atoms";

/* Status pills: one mark per row, pale tint with dark ink ------------------------------------------ */
const docStatus: Record<DocStatus, [Tone, string]> = {
  current: ["positive", "Current"], draft: ["warning", "Draft"], obsolete: ["negative", "Obsolete"], unknown: ["neutral", "Unknown"],
};
export const DocStatusPill = ({ status }: { status: DocStatus }) => <Pill tone={docStatus[status][0]}>{docStatus[status][1]}</Pill>;

const sev: Record<Severity, { label: string; bars: number; ink: string }> = {
  high: { label: "High", bars: 3, ink: "text-[var(--pill-negative-ink)] dark:text-negative" },
  medium: { label: "Medium", bars: 2, ink: "text-[var(--pill-warning-ink)]" },
  low: { label: "Low", bars: 1, ink: "text-muted" },
};
/** Severity as a signal glyph: three rising bars, filled by how serious, then the word in the same ink. */
export const SeverityPill = ({ severity }: { severity: Severity }) => {
  const x = sev[severity];
  return (
    <span className={cn("inline-flex flex-none items-center gap-1.5 text-small font-medium whitespace-nowrap", x.ink)}>
      <svg viewBox="0 0 12 12" aria-hidden className="size-3.5">
        {[4, 7, 10].map((h, i) => <rect key={i} x={i * 4.4} y={12 - h} width="2.6" height={h} rx="0.8" fill="currentColor" opacity={i < x.bars ? 1 : 0.22} />)}
      </svg>
      {x.label}
    </span>
  );
};

const alertStatus: Record<AlertStatus, [Tone, string]> = {
  open: ["neutral", "Open"], ack: ["accent", "Acknowledged"], resolved: ["positive", "Resolved"], dismissed: ["neutral", "Dismissed"],
};
export const AlertStatusPill = ({ status }: { status: AlertStatus }) => <Pill tone={alertStatus[status][0]}>{alertStatus[status][1]}</Pill>;

const sourceStatus: Record<Source["status"], [Tone, string]> = { active: ["positive", "Syncing"], paused: ["neutral", "Paused"], error: ["negative", "Needs attention"] };
export const SourceStatus = ({ status }: { status: Source["status"] }) => (
  <span className="inline-flex items-center gap-1.5 text-body whitespace-nowrap"><Dot tone={sourceStatus[status][0]} />{sourceStatus[status][1]}</span>
);

export const ruleLabel: Record<Alert["rule"], string> = {
  OBSOLETE_STANDARD: "Cites a withdrawn standard",
  CONTRADICTION: "Documents contradict",
  NO_OWNER: "No owner",
  ENTITY_NO_DOCS: "Entity without documents",
  SUSPECT_CONTENT: "Text aimed at the AI",
  REVIEW_OVERDUE: "Review overdue",
  OLD_VERSION_CIRCULATING: "Old revision circulating",
  DUPLICATE: "Duplicate",
};

/* Provenance: where a metadata value came from ------------------------------------------------------- */
const prov: Record<ProvenanceSource, string> = {
  manual: "Corrected by hand", titleblock: "Title block", path: "Folder path", erp: "ERP", llm: "Inferred by the model",
};
export function ProvenanceTag({ source, confidence }: { source: ProvenanceSource; confidence: number }) {
  return (
    <span className={cn("inline-flex items-center text-small whitespace-nowrap", source === "llm" ? "text-[var(--pill-warning-ink)]" : "text-muted")}>
      {prov[source]}
      {source !== "manual" && <span className="num">, {Math.round(confidence * 100)} %</span>}
    </span>
  );
}

/* Document furniture: rev, owner, department, status. Same order on every screen, one line, department truncates first. */
export function DocFurniture({ doc, owner, className }: { doc: Document; owner?: Person | null; className?: string }) {
  const rev = doc.versions.find((v) => v.id === doc.currentVersionId)?.revision ?? "n/a";
  return (
    <div className={cn("flex min-w-0 items-center gap-x-2 text-small whitespace-nowrap text-muted", className)}>
      <span className="flex-none num">rev. {rev}</span>
      <span aria-hidden className="h-3 w-px flex-none bg-border" />
      <span className="inline-flex flex-none items-center gap-1.5">
        {owner ? <><Avatar name={owner.name} hue={owner.hue} size={20} />{owner.name}</> : <span className="text-[var(--pill-warning-ink)]">No owner</span>}
      </span>
      <span aria-hidden className="h-3 w-px flex-none bg-border" />
      <span className="min-w-0 truncate">{doc.department}</span>
      <DocStatusPill status={doc.status} />
    </div>
  );
}

/** A document code as a compact band: how a document looks wherever it is mentioned. */
export function DocChip({ doc, className }: { doc: Pick<Document, "code" | "title" | "band">; className?: string }) {
  return (
    <span className={cn("inline-flex max-w-full flex-none align-middle", className)}>
      <Band kind={doc.code ? doc.band : "muted"} size="xs" arrow={false}>{!doc.code ? doc.title : doc.code}</Band>
    </span>
  );
}

/** Text with known document codes rendered as chips (audit details, notes). */
export function WithDocChips({ text, docs }: { text: string; docs: Document[] }) {
  const codes = [...new Set(docs.filter((d) => d.code).map((d) => d.code))].sort((a, b) => b.length - a.length);
  if (!codes.length) return <>{text}</>;
  const re = new RegExp(`(${codes.map((c) => c.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|")})(?: rev\\.\\w+)?`, "g");
  const parts: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(re)) {
    parts.push(text.slice(last, m.index));
    const d = docs.find((x) => x.code === m[1])!;
    parts.push(<DocChip key={m.index} doc={{ ...d, code: m[0] }} />);
    last = m.index! + m[0].length;
  }
  parts.push(text.slice(last));
  return <>{parts}</>;
}

export const docLabel = (d: Pick<Document, "code" | "title">) => (!d.code ? d.title : d.code);
export const revOf = (d: Document) => d.versions.find((v) => v.id === d.currentVersionId)?.revision ?? "n/a";

/* Citation band with its quote: the signature unit of evidence ------------------------------------------- */
export function CitationCard({
  citation, doc, owner, active, onHover, href,
}: {
  citation: Citation; doc: Document; owner?: Person | null; active?: boolean;
  onHover?: (n: number | null) => void; href: string;
}) {
  return (
    <article
      onMouseEnter={() => onHover?.(citation.n)}
      onMouseLeave={() => onHover?.(null)}
      className="animate-band-in"
      style={{ animationDelay: `${citation.n * 70}ms` }}
    >
      <Link href={href} onFocus={() => onHover?.(citation.n)} onBlur={() => onHover?.(null)} className="block rounded-[4px] outline-offset-2"
        aria-label={`Source ${citation.n}: ${docLabel(doc)} revision ${revOf(doc)}, section ${citation.section}, page ${citation.page}. Open in viewer`}>
        <Band kind={doc.band} size="lg" active={active}>
          {citation.n}, {docLabel(doc)} rev.{revOf(doc)}, §{citation.section.split(" ")[0]}, p.{citation.page}
        </Band>
      </Link>
      <div className="px-0.5 pt-2.5 pb-5">
        <p className="text-body font-medium">{doc.title}</p>
        <DocFurniture doc={doc} owner={owner} className="mt-1" />
        <blockquote className={cn("mt-3 border-l-2 pl-3 text-body", { procedure: "border-band-procedure", manual: "border-band-manual", norm: "border-band-norm", erp: "border-band-erp" }[doc.band])}>
          “{citation.quote}”
        </blockquote>
      </div>
    </article>
  );
}

/* A document as a compact band link (alerts, nearby docs, related) ---------------------------------------- */
export function DocBandLink({ doc, href, suffix }: { doc: Document; href: string; suffix?: ReactNode }) {
  return (
    <Link href={href} className="group block rounded-[4px] outline-offset-2">
      <Band kind={doc.band} className="transition-transform duration-[var(--dur-2)] ease-out group-hover:-translate-x-0.5">
        {docLabel(doc)} rev.{revOf(doc)}{suffix ? <span className="font-normal opacity-90">, {suffix}</span> : null}
      </Band>
    </Link>
  );
}

/* Step indicator for the setup wizard -------------------------------------------------------------------- */
export function Steps({ steps, current }: { steps: string[]; current: number }) {
  return (
    <ol className="flex flex-wrap gap-x-6 gap-y-2" aria-label="Setup progress">
      {steps.map((s, i) => (
        <li key={s} aria-current={i === current ? "step" : undefined} className={cn("flex items-center gap-2 text-body", i <= current ? "text-text" : "text-muted")}>
          <span className={cn("grid size-6 place-items-center rounded-full text-small font-medium num transition-colors duration-[var(--dur-2)]",
            i < current ? "bg-accent text-on-accent" : i === current ? "bg-primary text-on-primary" : "bg-active text-muted")}>
            {i + 1}
          </span>
          {s}
        </li>
      ))}
    </ol>
  );
}
