"use client";
import { Pause, Play, RefreshCw, TriangleAlert } from "lucide-react";
import type { BandKind, IngestionRun, QuarantineItem, Source } from "@/lib/api/types";
import { cn } from "@/lib/cn";
import { fmtAgo, fmtNum } from "@/lib/format";
import { HoldButton, Pill, type Tone } from "@/ui/atoms";
import { EmptyState, SourceStatus } from "@/ui/cells";

const kindLabel: Record<Source["kind"], string> = { folder: "Network folder", sharepoint: "SharePoint", erp_sql: "ERP database", upload: "Uploads" };

/** One row per source: what it is, its state, how much it holds, when it last synced, and what you can do. A failure gets its own line. */
export function SourceFlow({
  sources, docCounts, bands, onSync, onPause, busyId, proEnabled,
}: {
  sources: Source[]; docCounts: Record<string, number>; bands: Record<string, BandKind>;
  onSync: (id: string) => void; onPause: (id: string, paused: boolean) => void; busyId?: string | null; proEnabled: boolean;
}) {
  return (
    <div className="relative overflow-x-auto" tabIndex={0} role="region" aria-label="Connected sources">
      <table className="w-full min-w-[760px] text-body">
        <caption className="sr-only">Sources Atlas reads from</caption>
        <thead>
          <tr className="border-b border-rule text-left text-small text-muted">
            <th scope="col" className="h-10 pl-5 font-normal">Source</th>
            <th scope="col" className="h-10 font-normal">Status</th>
            <th scope="col" className="h-10 text-right font-normal">Documents</th>
            <th scope="col" className="h-10 pl-6 font-normal">Last sync</th>
            <th scope="col" className="h-10 pr-5 text-right font-normal"><span className="sr-only">Actions</span></th>
          </tr>
        </thead>
        {sources.map((s) => {
          const locked = s.pro && !proEnabled;
          const failing = s.status === "error" && !locked;
          return (
            <tbody key={s.id} className="border-b border-rule last:border-b-0">
              <tr>
                <td className="py-3 pl-5">
                  <div className="flex items-stretch gap-3">
                    <span aria-hidden className="w-1 flex-none rounded-full" style={{ background: `var(--band-${locked || s.status === "paused" ? "muted" : bands[s.id] ?? "procedure"}, var(--control))` }} />
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="truncate font-medium">{s.name}</span>
                        {s.pro && <Pill tone={locked ? "neutral" : "accent"}>Pro</Pill>}
                      </div>
                      <div className="truncate text-small text-muted">{kindLabel[s.kind]}, {s.location}</div>
                    </div>
                  </div>
                </td>
                <td><SourceStatus status={locked ? "paused" : s.status} /></td>
                <td className="text-right num">{fmtNum(docCounts[s.id] ?? 0)}</td>
                <td className="pl-6 text-muted" suppressHydrationWarning>{s.lastSyncAt ? fmtAgo(s.lastSyncAt) : "Never"}</td>
                <td className="py-3 pr-5">
                  <div className="flex items-center justify-end gap-1.5">
                    <HoldButton size="sm" variant="outline" holdMs={500} doneLabel="Synced" onConfirm={() => onSync(s.id)} disabled={busyId === s.id || s.status === "paused" || locked} aria-label={`Sync ${s.name} now`}>
                      <RefreshCw />Sync
                    </HoldButton>
                    <HoldButton size="icon" variant="ghost" holdMs={600} after doneLabel={s.status === "paused" ? "Resumed" : "Paused"} onConfirm={() => onPause(s.id, s.status !== "paused")} disabled={locked}
                      aria-label={s.status === "paused" ? `Resume ${s.name}` : `Pause ${s.name}`}>
                      {s.status === "paused" ? <Play /> : <Pause />}
                    </HoldButton>
                  </div>
                </td>
              </tr>
              {failing && (
                <tr>
                  <td colSpan={5} className="px-5 pb-3">
                    <p role="alert" className="flex items-start gap-2 rounded-[6px] bg-[var(--pill-negative-bg)] px-3 py-2 text-small text-[var(--pill-negative-ink)]">
                      <TriangleAlert aria-hidden className="mt-0.5 size-4 flex-none" />{s.lastError ?? "Sync failed."}
                    </p>
                  </td>
                </tr>
              )}
              {locked && (
                <tr><td colSpan={5} className="px-5 pb-3 text-small text-muted">Included in Atlas Pro.</td></tr>
              )}
            </tbody>
          );
        })}
      </table>
    </div>
  );
}

/** Recent runs; the duration bar is drawn to exact scale on a shared 0–5 minute axis. */
export function RunsTable({ runs, names }: { runs: IngestionRun[]; names: Record<string, string> }) {
  const axis = 300; // seconds
  return (
    <div className="relative overflow-x-auto" tabIndex={0} role="region" aria-label="Recent runs">
      <table className="w-full min-w-[760px] text-body">
        <caption className="sr-only">Recent ingestion runs</caption>
        <thead>
          <tr className="border-b border-border text-left text-small text-muted">
            <th scope="col" className="h-10 font-normal">Source</th>
            <th scope="col" className="h-10 font-normal">Finished</th>
            <th scope="col" className="h-10 w-[88px] pl-3 text-right font-normal">Found</th>
            <th scope="col" className="h-10 w-[88px] pl-3 text-right font-normal">Indexed</th>
            <th scope="col" className="h-10 w-[88px] pl-3 text-right font-normal">Unchanged</th>
            <th scope="col" className="h-10 w-[88px] pl-3 text-right font-normal">Failed</th>
            <th scope="col" className="h-10 w-[28%] pl-6 font-normal">Duration (0–5 min)</th>
          </tr>
        </thead>
        <tbody>
          {runs.map((r) => {
            const secs = r.finishedAt ? (new Date(r.finishedAt).getTime() - new Date(r.startedAt).getTime()) / 1000 : 0;
            return (
              <tr key={r.id} className="border-b border-rule">
                <td className="h-11 pr-3">{names[r.sourceId]}</td>
                <td className="h-11 text-muted" suppressHydrationWarning>{fmtAgo(r.finishedAt)}</td>
                <td className="h-11 pl-3 text-right num">{r.discovered}</td>
                <td className="h-11 pl-3 text-right num">{r.indexed}</td>
                <td className="h-11 pl-3 text-right text-muted num">{r.skipped}</td>
                <td className={cn("h-11 pl-3 text-right num", r.failed ? "font-medium text-[var(--pill-negative-ink)] dark:text-negative" : "text-muted")}>{r.failed}</td>
                <td className="h-11 pl-6">
                  <div className="flex items-center gap-3">
                    <div className="h-1.5 flex-1 rounded-full bg-rule">
                      <div className="h-full origin-left rounded-full bg-text animate-[bar-grow-x_360ms_var(--ease-out)_both]" style={{ width: `${Math.min(secs / axis, 1) * 100}%` }} />
                    </div>
                    <span className="w-12 text-right text-small text-muted num">{secs >= 60 ? `${Math.floor(secs / 60)}m ${Math.round(secs % 60)}s` : `${Math.round(secs)} s`}</span>
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

const reason: Record<QuarantineItem["reason"], [Tone, string]> = {
  encrypted: ["warning", "Encrypted"], corrupt: ["negative", "Corrupt"], unsupported: ["neutral", "Unsupported format"], too_large: ["neutral", "Too large"], hostile: ["negative", "Unsafe file"],
};

export function QuarantineList({ items, names, onRetry, retrying }: {
  items: QuarantineItem[]; names: Record<string, string>; onRetry: (id: string) => void; retrying?: string | null;
}) {
  if (!items.length) return <EmptyState>Nothing in quarantine. Every file Atlas found could be read.</EmptyState>;
  return (
    <ul className="divide-y divide-rule">
      {items.map((q) => (
        <li key={q.id} className="flex flex-wrap items-start gap-x-4 gap-y-2 py-3">
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="truncate text-body font-medium">{q.path.split("/").pop()}</span>
              <Pill tone={reason[q.reason][0]}>{reason[q.reason][1]}</Pill>
            </div>
            <p className="mt-0.5 text-body">{q.detail}</p>
            <p className="mt-0.5 truncate text-small text-muted" suppressHydrationWarning>{names[q.sourceId]}, {q.path}, {fmtAgo(q.at)}</p>
          </div>
          <HoldButton size="sm" variant="outline" holdMs={500} doneLabel="Queued" onConfirm={() => onRetry(q.id)} disabled={retrying === q.id}>
            <RefreshCw />Try again
          </HoldButton>
        </li>
      ))}
    </ul>
  );
}
