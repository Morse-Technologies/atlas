"use client";
import { useMemo } from "react";
import { newKey, undoable, useDocuments, useFeature, useQuarantine, useRuns, useSourceActions, useSources } from "@/features/hooks";
import { fmtAgo, fmtNum, plural } from "@/lib/format";
import type { BandKind } from "@/lib/api/types";
import { Card, ErrorState, KpiStrip, LoadingRows, PageHeader } from "@/ui/cells";
import { SourceAccess } from "./SourceAccess";
import { QuarantineList, RunsTable, SourceFlow } from "@/ui/organisms/Sources";
import { Canvas } from "@/ui/templates/ShellLayout";

/** Screen 5: is everything being read, and what failed? */
export function SourcesScreen() {
  const sources = useSources();
  const runs = useRuns();
  const quarantine = useQuarantine();
  const docs = useDocuments({});
  const pro = useFeature("sharepoint");
  const { sync, pause, retry } = useSourceActions();

  const names = useMemo(() => Object.fromEntries((sources.data ?? []).map((s) => [s.id, s.name])), [sources.data]);
  const docCounts = useMemo(() => (docs.data ?? []).reduce<Record<string, number>>((m, d) => ((m[d.sourceId] = (m[d.sourceId] ?? 0) + 1), m), {}), [docs.data]);
  const bands = useMemo(() => {
    const tally: Record<string, Record<string, number>> = {};
    (docs.data ?? []).forEach((d) => { (tally[d.sourceId] ??= {})[d.band] = (tally[d.sourceId]?.[d.band] ?? 0) + 1; });
    return Object.fromEntries(Object.entries(tally).map(([id, t]) => [id, Object.entries(t).sort((a, b) => b[1] - a[1])[0][0]])) as Record<string, BandKind>;
  }, [docs.data]);
  const failing = (sources.data ?? []).filter((s) => s.status === "error");
  const q = quarantine.data?.length ?? 0;

  const subline = sources.data
    ? failing.length
      ? `${failing[0].name} needs attention: ${failing[0].lastError} ${q ? `${plural(q, "file")} ${q === 1 ? "is" : "are"} in quarantine.` : ""}`
      : `All sources are syncing. ${q ? `${plural(q, "file")} ${q === 1 ? "is" : "are"} in quarantine.` : "Nothing in quarantine."}`
    : "Where Atlas reads from. It never writes to these folders.";

  return (
    <Canvas>
      <PageHeader title="Sources" subline={subline} />
      <KpiStrip className="mt-6" items={[
        { label: "Sources syncing", value: sources.data ? `${sources.data.filter((s) => s.status === "active").length} of ${sources.data.length}` : "…", note: failing.length ? `${failing.length} needs attention` : "No errors" },
        { label: "Documents searchable", value: docs.data ? fmtNum(docs.data.length) : "…" },
        { label: "In quarantine", value: quarantine.data ? q : "…", note: "Files Atlas couldn't read" },
        { label: "Last sync", value: sources.data ? fmtAgo(sources.data.map((s) => s.lastSyncAt).filter(Boolean).sort().at(-1) ?? null) : "…" },
      ]} />
      <Card id="flow-h" flush title="Connected sources" aside="Read-only. Atlas never writes to your sources." className="mt-5">
        {sources.isPending || docs.isPending ? <div className="px-5"><LoadingRows rows={6} pitch="h-14" /></div> : sources.isError ? <ErrorState what="sources" onRetry={() => sources.refetch()} /> : (
          <SourceFlow
            sources={sources.data} docCounts={docCounts} bands={bands} proEnabled={pro.enabled}
            onSync={(id) => sync.mutate({ id, key: newKey() })}
            onPause={(id, paused) => {
              const name = sources.data.find((s) => s.id === id)?.name ?? "Source";
              void undoable({
                pending: paused ? `Pausing ${name}…` : `Resuming ${name}…`,
                done: paused ? `${name} paused. Its documents stay searchable.` : `${name} resumed`,
                run: () => pause.mutateAsync({ id, paused, key: newKey() }),
                undo: () => pause.mutateAsync({ id, paused: !paused, key: newKey() }),
              }).catch(() => {});
            }}
            busyId={sync.isPending ? sync.variables?.id : null}
          />
        )}
      </Card>

      {sources.data && <SourceAccess sources={sources.data} />}

      <div className="mt-5 grid items-start gap-5 xl:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <Card id="runs-h" title="Recent runs" aside={runs.data ? `Last ${runs.data.length}, newest first` : undefined}>
          {runs.isPending ? <LoadingRows rows={6} pitch="h-11" /> : runs.isError ? <ErrorState what="runs" onRetry={() => runs.refetch()} />
            : <RunsTable runs={[...runs.data].sort((a, b) => (b.finishedAt ?? "").localeCompare(a.finishedAt ?? ""))} names={names} />}
        </Card>
        <Card id="q-h" title="Quarantine" aside={`${q} ${q === 1 ? "file" : "files"}`}>
          {quarantine.isPending ? <LoadingRows rows={3} pitch="h-20" /> : quarantine.isError ? <ErrorState what="quarantine" onRetry={() => quarantine.refetch()} /> : (
            <QuarantineList items={quarantine.data ?? []} names={names} onRetry={(id) => retry.mutate({ id, key: newKey() })}
              retrying={retry.isPending ? retry.variables?.id : null} />
          )}
        </Card>
      </div>
    </Canvas>
  );
}
