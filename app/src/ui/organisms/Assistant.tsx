"use client";
import Link from "next/link";
import { ArrowUp, BookOpen, Check, ChevronDown, Copy, RotateCcw, Square, ThumbsDown, ThumbsUp } from "lucide-react";
import { Fragment, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { gsap } from "gsap";
import { reduced } from "@/lib/motion";
import type { Answer, BandKind, Citation, ConversationSummary, Document, Person } from "@/lib/api/types";
import { cn } from "@/lib/cn";
import { clockNow } from "@/lib/clock";
import { fmtSeconds } from "@/lib/format";
import { Band, BandSwatch, CiteMark, Kbd, Mark, Spinner, Tooltip, bandLabel, buttonStyles } from "@/ui/atoms";
import { CitationCard, DocBandLink, docLabel, revOf } from "@/ui/cells";

const COPY_FEEDBACK_MS = 1400;

const viewerHref = (c: Citation, conversationId?: string) =>
  `/documents/${c.documentId}?page=${c.page}&cite=${c.n}${c.bbox ? `&hl=${c.bbox.x},${c.bbox.y},${c.bbox.w},${c.bbox.h}` : ""}${conversationId ? `&from=${conversationId}` : ""}`;

/* Composer: a floating rounded field, like a messaging app ----------------------------------------- */
export function AskComposer({
  value, onChange, onSubmit, onStop, busy, scope, onScopeChange, scopes, autoFocus, large, placeholder = "Ask about a procedure, a policy or a norm",
}: {
  value: string; onChange: (v: string) => void; onSubmit: () => void; onStop?: () => void; busy?: boolean;
  scope: string; onScopeChange: (s: string) => void; scopes: string[]; autoFocus?: boolean; large?: boolean; placeholder?: string;
}) {
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 220)}px`;
  }, [value]);
  const send = () => { if (value.trim() && !busy) onSubmit(); };
  return (
    <form
      onSubmit={(e) => { e.preventDefault(); send(); }}
      className="rounded-[26px] border border-control bg-surface shadow-[0_2px_12px_rgb(14_18_16/0.06)] transition-[border-color] duration-[var(--dur-2)] ease-out focus-within:border-accent"
    >
      <label htmlFor="ask-input" className="sr-only">Your question</label>
      <textarea
        id="ask-input" ref={ref} autoFocus={autoFocus} rows={1} value={value} placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); } }}
        className={cn("block w-full resize-none bg-transparent px-5 text-text caret-accent outline-none placeholder:text-faint", large ? "pt-4 text-section leading-7" : "pt-3.5 text-section")}
      />
      <div className="flex items-center justify-between gap-3 px-2.5 pb-2.5">
        <label className="relative inline-flex">
          <span className="sr-only">Limit to</span>
          <select value={scope} onChange={(e) => onScopeChange(e.target.value)}
            className="hit-slop h-8 appearance-none rounded-full border border-border bg-surface pr-7 pl-3 text-small text-muted transition-colors hover:border-control hover:text-text">
            {scopes.map((s) => <option key={s} value={s}>{s === "all" ? "All lines" : s}</option>)}
          </select>
          <ChevronDown aria-hidden className="pointer-events-none absolute top-1/2 right-2.5 size-3.5 -translate-y-1/2 text-muted" />
        </label>
        <div className="flex items-center gap-2 text-small text-muted">
          <span className="hidden items-center gap-1 sm:inline-flex"><Kbd>↵</Kbd> send <span aria-hidden className="mx-1 h-3 w-px bg-border" /><Kbd>⇧</Kbd><Kbd>↵</Kbd> new line</span>
          {busy && onStop ? (
            <button type="button" onClick={onStop} aria-label="Stop answering"
              className="hit-slop grid size-9 place-items-center rounded-full bg-active text-text transition-transform duration-[var(--dur-1)] active:scale-95">
              <Square className="size-3.5 fill-current" />
            </button>
          ) : (
            <button type="submit" aria-label="Ask" disabled={!value.trim() || busy}
              className="hit-slop grid size-9 place-items-center rounded-full bg-primary text-on-primary transition-[transform,opacity] duration-[var(--dur-1)] active:scale-95 disabled:opacity-30">
              {busy ? <Spinner className="text-on-primary" /> : <ArrowUp className="size-[18px]" strokeWidth={2.2} />}
            </button>
          )}
        </div>
      </div>
    </form>
  );
}

/* Answer text with band-coloured [n] marks ------------------------------------------------------------- */
function AnswerText({
  text, citations, docs, active, onHover, streaming, conversationId,
}: {
  text: string; citations: Citation[]; docs: Map<string, Document>; active: number | null;
  onHover: (n: number | null) => void; streaming?: boolean; conversationId?: string;
}) {
  const kindOf = (n: number): BandKind => {
    const c = citations.find((x) => x.n === n);
    return (c && docs.get(c.documentId)?.band) || "procedure";
  };
  const paragraphs = text.split(/\n\n+/);
  // While streaming, each new word fades up as it lands (GSAP); words already shown are left alone.
  const box = useRef<HTMLDivElement>(null);
  const shown = useRef(0);
  useLayoutEffect(() => {
    const words = box.current?.querySelectorAll<HTMLElement>("[data-w]");
    if (!words) return;
    const fresh = Array.from(words).slice(shown.current);
    shown.current = words.length;
    if (!streaming || !fresh.length || reduced()) return;
    gsap.fromTo(fresh, { opacity: 0, y: 4 }, { opacity: 1, y: 0, duration: 0.35, ease: "power2.out", stagger: 0.015, clearProps: "transform" });
  }, [text, streaming]);
  return (
    <div ref={box} className="space-y-3 text-[16px] leading-[1.7]" aria-live={streaming ? "polite" : undefined} aria-busy={streaming}>
      {paragraphs.map((p, pi) => (
        <p key={pi}>
          {p.split(/(\[\d+\])/g).map((part, i) => {
            const m = part.match(/^\[(\d+)\]$/);
            if (!m) return <Fragment key={i}>{part.split(/(\s+)/).map((w, j) => (/^\s*$/.test(w) ? w : <span key={j} data-w className="inline-block">{w}</span>))}</Fragment>;
            const n = Number(m[1]);
            const c = citations.find((x) => x.n === n);
            if (!c) return <CiteMark key={i} n={n} kind="procedure" />;
            const d = docs.get(c.documentId);
            return (
              <Link
                key={i} href={viewerHref(c, conversationId)}
                onMouseEnter={() => onHover(n)} onMouseLeave={() => onHover(null)} onFocus={() => onHover(n)} onBlur={() => onHover(null)}
                aria-label={`Source ${n}: ${d ? docLabel(d) : ""}, page ${c.page}`}
                className="hit-slop relative rounded-[3px] outline-offset-1"
              >
                <CiteMark n={n} kind={kindOf(n)} active={active === n} />
              </Link>
            );
          })}
          {streaming && pi === paragraphs.length - 1 && (
            <span aria-hidden className="ml-0.5 inline-block h-[1.1em] w-[2px] translate-y-[3px] animate-pulse bg-accent" />
          )}
        </p>
      ))}
    </div>
  );
}

/* The user's message: a right-aligned bubble --------------------------------------------------------- */
export function UserMessage({ text }: { text: string }) {
  return (
    <div className="flex justify-end">
      <p className="max-w-[min(80%,560px)] origin-bottom-right animate-[bubble-in_240ms_var(--ease-out)_both] rounded-[22px] rounded-br-[8px] bg-active px-4 py-2.5 text-section">
        {text}
      </p>
    </div>
  );
}

function IconAction({ label, onClick, children, pressed }: { label: string; onClick: () => void; children: ReactNode; pressed?: boolean }) {
  return (
    <Tooltip label={label}>
      <button type="button" onClick={onClick} aria-label={label} aria-pressed={pressed}
        className={cn("hit-slop grid size-8 place-items-center rounded-full text-muted transition-[background-color,color,transform] duration-[var(--dur-1)] ease-out hover:bg-raised hover:text-text active:scale-90 [&_svg]:size-4",
          pressed && "bg-raised text-text")}>
        {children}
      </button>
    </Tooltip>
  );
}

/* Atlas's reply: no bubble, sources as bands, an action row ------------------------------------------- */
export function AssistantMessage({
  answer, streamText, citations, notes, docs, people, active, onHover, onFeedback, onRetry, conversationId,
}: {
  answer?: Answer; streamText?: string; citations: Citation[]; notes: string[]; docs: Map<string, Document>; people: Map<string, Person>;
  active: number | null; onHover: (n: number | null) => void; onFeedback?: (v: "up" | "down") => void; onRetry?: () => void; conversationId?: string;
}) {
  const streaming = streamText !== undefined;
  const [quotes, setQuotes] = useState(false);
  const [copied, setCopied] = useState(false);
  const first = citations[0];
  const firstDoc = first && docs.get(first.documentId);
  // The source bands slide in from the left, one after another, when the answer is complete.
  const bands = useRef<HTMLUListElement>(null);
  useBandsIn(bands, !!answer);

  return (
    <article className="flex gap-3.5">
      <span className="mt-0.5 grid size-7 flex-none place-items-center rounded-full bg-raised text-text"><Mark className="size-4" /></span>
      <div className="min-w-0 flex-1">
        <p className="mb-2 flex flex-wrap items-center gap-x-2 text-small text-muted">
          <span className="font-medium text-text">Atlas</span>
          {answer && <span className="num">answered in {fmtSeconds(answer.latencyMs)}, {answer.model}, local</span>}
          {streaming && <span className="inline-flex items-center gap-1.5"><Spinner className="size-3" />{citations.length ? `Reading ${citations.length} sources` : "Searching your documents"}</span>}
        </p>

        {streaming && !streamText ? (
          <div className="space-y-2.5 pt-1" aria-label="Looking for the answer">
            <span className="block h-3.5 w-[56ch] max-w-full animate-pulse rounded-full bg-active" />
            <span className="block h-3.5 w-[40ch] max-w-full animate-pulse rounded-full bg-active [animation-delay:150ms]" />
          </div>
        ) : answer?.outcome === "not_found" ? (
          <NotFound answer={answer} docs={docs} />
        ) : (
          <AnswerText text={answer?.text ?? streamText ?? ""} citations={citations} docs={docs} active={active} onHover={onHover} streaming={streaming} conversationId={conversationId} />
        )}

        {notes.length > 0 && (
          <ul className="mt-4 space-y-2">
            {notes.map((n, i) => <li key={i} className="rounded-card border border-border px-3.5 py-2.5 text-body">{n}</li>)}
          </ul>
        )}

        {citations.length > 0 && answer && (
          <section aria-label="Sources" className="mt-5">
            <ul ref={bands} className="flex flex-col gap-1.5">
              {citations.map((c) => {
                const d = docs.get(c.documentId);
                if (!d) return null;
                return (
                  <li key={c.n} data-band
                    onMouseEnter={() => onHover(c.n)} onMouseLeave={() => onHover(null)}>
                    <Link href={viewerHref(c, conversationId)} className="block max-w-[520px] rounded-[4px] outline-offset-2"
                      aria-label={`Source ${c.n}: ${docLabel(d)} revision ${revOf(d)}, page ${c.page}. Open in viewer`}>
                      <Band kind={d.band} active={active === c.n}>{c.n}, {docLabel(d)} rev.{revOf(d)}, §{c.section.split(" ")[0]}, p.{c.page}</Band>
                    </Link>
                  </li>
                );
              })}
            </ul>
            <button type="button" onClick={() => setQuotes((q) => !q)} aria-expanded={quotes}
              className="hit-slop relative mt-2.5 inline-flex min-h-6 items-center gap-1 text-small text-muted transition-colors hover:text-text">
              <ChevronDown className={cn("size-3.5 transition-transform duration-[var(--dur-2)] ease-out", quotes && "rotate-180")} />
              {quotes ? "Hide the quoted passages" : "Show the quoted passages"}
            </button>
            {/* Height animates through grid rows, so opening can be interrupted half-way and reverses smoothly. */}
            <div className={cn("grid transition-[grid-template-rows,opacity] duration-[var(--dur-3)] ease-in-out", quotes ? "grid-rows-[1fr] opacity-100" : "grid-rows-[0fr] opacity-0")}>
              <div className="min-h-0 overflow-hidden" inert={!quotes}>
                <div className="max-w-[560px] pt-4">
                  {citations.map((c) => {
                    const d = docs.get(c.documentId);
                    return d ? <CitationCard key={c.n} citation={c} doc={d} owner={d.ownerId ? people.get(d.ownerId) : null} active={active === c.n} onHover={onHover} href={viewerHref(c, conversationId)} /> : null;
                  })}
                  <ul className="flex flex-wrap gap-x-4 gap-y-1 text-small text-muted" aria-label="Band colours">
                    {(["procedure", "manual", "norm", "erp"] as BandKind[]).map((k) => <li key={k} className="inline-flex items-center gap-1.5"><BandSwatch kind={k} />{bandLabel[k]}</li>)}
                  </ul>
                </div>
              </div>
            </div>
          </section>
        )}

        {answer && (
          <div className="mt-4 flex flex-wrap items-center gap-1">
            {answer.outcome === "answered" && first && firstDoc && (
              <Link href={viewerHref(first, conversationId)} className={cn(buttonStyles({ variant: "primary", size: "sm" }), "mr-2 rounded-full px-3")}>
                <BookOpen />Open {docLabel(firstDoc)} p.{first.page}
              </Link>
            )}
            <IconAction label={copied ? "Copied" : "Copy answer"} onClick={() => {
              navigator.clipboard?.writeText(answer.text.replace(/\s?\[\d+\]/g, ""));
              setCopied(true); setTimeout(() => setCopied(false), COPY_FEEDBACK_MS);
            }}>{copied ? <Check /> : <Copy />}</IconAction>
            {answer.outcome === "answered" && <>
              <IconAction label="Helpful" pressed={answer.feedback === "up"} onClick={() => onFeedback?.("up")}><ThumbsUp /></IconAction>
              <IconAction label="Not right" pressed={answer.feedback === "down"} onClick={() => onFeedback?.("down")}><ThumbsDown /></IconAction>
            </>}
            {onRetry && <IconAction label="Ask again" onClick={onRetry}><RotateCcw /></IconAction>}
          </div>
        )}
      </div>
    </article>
  );
}

function NotFound({ answer, docs }: { answer: Answer; docs: Map<string, Document> }) {
  const nearby = answer.nearby.map((id) => docs.get(id)).filter(Boolean) as Document[];
  return (
    <div>
      <p className="text-[16px] leading-[1.7]">{answer.text}</p>
      <p className="mt-2 text-body text-muted">
        I only answer when I can point to a page, so I won&apos;t guess. If this should be documented, tell Quality and it will show up here once it&apos;s indexed.
      </p>
      {nearby.length > 0 && (
        <div className="mt-4">
          <h3 className="mb-2 text-body font-medium">These might be related</h3>
          <ul className="flex max-w-md flex-col gap-1.5">
            {nearby.map((d) => <li key={d.id}><DocBandLink doc={d} href={`/documents/${d.id}`} suffix={d.title} /></li>)}
          </ul>
        </div>
      )}
    </div>
  );
}

/* Conversation history, grouped by day ------------------------------------------------------------- */
const dayGroup = (iso: string) => {
  const days = Math.floor((clockNow().getTime() - new Date(iso).getTime()) / 86_400_000);
  return days < 1 ? "Today" : days < 2 ? "Yesterday" : days < 7 ? "This week" : "Earlier";
};

export function HistoryList({ items, activeId, onPick }: { items: ConversationSummary[]; activeId?: string; onPick?: () => void }) {
  const groups = items.reduce<Record<string, ConversationSummary[]>>((m, c) => ((m[dayGroup(c.updatedAt)] ??= []).push(c), m), {});
  return (
    <nav aria-label="Conversations" className="flex flex-col gap-4">
      {Object.entries(groups).map(([g, list]) => (
        <section key={g}>
          <h3 className="px-2.5 pb-1 text-small text-muted" suppressHydrationWarning>{g}</h3>
          <ul className="flex flex-col gap-0.5">
            {list.map((c) => (
              <li key={c.id}>
                <Link href={`/ask/${c.id}`} onClick={onPick} aria-current={c.id === activeId ? "page" : undefined}
                  className={cn("block truncate rounded-control px-2.5 py-2 text-body transition-colors duration-[var(--dur-1)] hover:bg-raised",
                    c.id === activeId ? "bg-active text-text" : "text-text")}>
                  {c.title}
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </nav>
  );
}

function useBandsIn(ref: React.RefObject<HTMLElement | null>, ready: boolean) {
  useLayoutEffect(() => {
    const el = ref.current;
    if (!ready || !el || reduced()) return;
    const ctx = gsap.context(() => gsap.from(el.querySelectorAll("[data-band]"), { x: -16, opacity: 0, duration: 0.5, ease: "power3.out", stagger: 0.06, clearProps: "transform,opacity" }), el);
    return () => ctx.revert();
  }, [ref, ready]);
}
