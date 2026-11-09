"use client";
import { CountUp } from "@/ui/atoms/CountUp";
import Link from "next/link";
import { useRef } from "react";
import { useGrow } from "@/lib/gsapIn";
import type { ReactNode } from "react";
import { cn } from "@/lib/cn";
import { fmtDayMonth, fmtNum, fmtWeekday } from "@/lib/format";

/** KPIs as type in one container split by hairlines; no tiles, no icons, no sparklines. */
export function KpiStrip({ items, className }: {
  items: { label: string; value: ReactNode; note?: ReactNode; href?: string }[];
  className?: string;
}) {
  return (
    <dl className={cn("grid divide-rule rounded-card border border-border bg-surface sm:divide-x", className)}
      style={{ gridTemplateColumns: `repeat(auto-fit, minmax(160px, 1fr))` }}>
      {items.map((k) => {
        const body = (
          <>
            <dt className="text-small text-muted">{k.label}</dt>
            <dd className="mt-1 text-kpi font-medium tracking-[-0.01em] num"><CountUp value={k.value} /></dd>
            {k.note && <dd className="mt-0.5 text-small text-muted">{k.note}</dd>}
          </>
        );
        return (
          <div key={k.label} className="border-rule px-5 py-4 max-sm:border-b">
            {k.href ? (
              <Link href={k.href} className="block rounded-control outline-offset-4 transition-opacity hover:opacity-80">{body}</Link>
            ) : body}
          </div>
        );
      })}
    </dl>
  );
}

/** Questions per day: one series in ink, today in the accent, not-found as a grey cap. */
export function DayBars({ data, height = 160 }: { data: { day: string; questions: number; notFound: number }[]; height?: number }) {
  const max = Math.max(...data.map((d) => d.questions), 1);
  const top = Math.ceil(max / 10) * 10;
  const ticks = [0, top / 2, top];
  const fig = useRef<HTMLElement>(null);
  useGrow(fig, { selector: "[data-bar]", axis: "y" });
  return (
    <figure ref={fig} className="w-full">
      <div className="relative flex gap-3" style={{ height }}>
        <div className="flex w-6 flex-col justify-between text-right text-small text-muted num" aria-hidden>
          {[...ticks].reverse().map((t) => <span key={t} className="-translate-y-2 leading-4">{t}</span>)}
        </div>
        <div className="relative flex flex-1 items-end gap-1.5">
          {ticks.map((t) => (
            <span key={t} aria-hidden className="absolute inset-x-0 border-t border-dashed border-grid" style={{ bottom: `${(t / top) * 100}%` }} />
          ))}
          {data.map((d, i) => {
            const isToday = i === data.length - 1;
            return (
              <div key={d.day} className="group relative flex h-full flex-1 flex-col justify-end" title={`${fmtDayMonth(d.day)}: ${d.questions} questions, ${d.notFound} not found`}>
                <div
                  data-bar className={cn("relative w-full origin-bottom rounded-t-[3px]", isToday ? "bg-accent" : "bg-[var(--bar)] group-hover:bg-text")}
                  style={{ height: `${(d.questions / top) * 100}%`}}
                >
                  {d.notFound > 0 && (
                    <span className="absolute inset-x-0 top-0 rounded-t-[3px] bg-[var(--bar-miss)]" style={{ height: `${(d.notFound / d.questions) * 100}%` }} />
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </div>
      <div className="mt-2 flex gap-1.5 pl-9 text-small text-muted" aria-hidden>
        {data.map((d, i) => (
          <span key={d.day} className={cn("flex-1 text-center leading-4 num", i === data.length - 1 && "font-medium text-text")}>
            {fmtWeekday(d.day).slice(0, 2)}<span className="block text-[11px]">{fmtDayMonth(d.day).split(" ")[0]}</span>
          </span>
        ))}
      </div>
      <figcaption className="sr-only">
        Questions per day over the last {data.length} days. Today: {data[data.length - 1]?.questions} questions.
      </figcaption>
    </figure>
  );
}

/** A one-figure ranking: thin grey bars, the top item in ink. */
export function BarList({ rows, unit }: { rows: { label: ReactNode; value: number; href?: string }[]; unit?: string }) {
  const max = Math.max(...rows.map((r) => r.value), 1);
  const list = useRef<HTMLUListElement>(null);
  useGrow(list, { selector: "[data-barx]", axis: "x" });
  return (
    <ul ref={list} className="flex flex-col gap-3">
      {rows.map((r, i) => (
        <li key={i}>
          <div className="flex items-baseline justify-between gap-4 text-body">
            <span className="truncate">{r.label}</span>
            <span className="text-muted num">{fmtNum(r.value)}{unit && <span className="sr-only"> {unit}</span>}</span>
          </div>
          <div className="mt-1.5 h-[3px] rounded-full bg-rule">
            <div
              data-barx className={cn("h-full origin-left rounded-full", i === 0 ? "bg-[var(--bar)]" : "bg-faint/60")}
              style={{ width: `${(r.value / max) * 100}%` }}
            />
          </div>
        </li>
      ))}
    </ul>
  );
}

/** Key-value rows for object details. */
export function KeyValues({ rows }: { rows: { k: ReactNode; v: ReactNode }[] }) {
  return (
    <dl className="divide-y divide-rule">
      {rows.map((r, i) => (
        <div key={i} className="flex min-h-8 items-baseline justify-between gap-4 py-1.5 text-body">
          <dt className="text-muted">{r.k}</dt>
          <dd className="min-w-0 text-right">{r.v}</dd>
        </div>
      ))}
    </dl>
  );
}
