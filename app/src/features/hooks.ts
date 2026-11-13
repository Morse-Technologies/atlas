"use client";
// TanStack Query hooks over AtlasApi, one block per bounded context. Screens only call these.
// Reads are stale-while-revalidate (cached data shows at once, refetches quietly); writes are optimistic,
// carry an idempotency key (retries reuse it), and confirm with a toast that can undo.
import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useCallback, useRef, useState } from "react";
import { toast } from "sonner";
import { canOpenScreen, hasAnyRole } from "@/features/shell/screenAccess";
import { alertChanges, api } from "@/lib/api";
import type { Alert, AlertPatch, Answer, Citation, Document, DocumentFilter, IngestionRun, MetadataPatch, NewSource, Page, PasswordChange, Person, ProFeature, Role, Settings, SettingsPatch, Source, SourceAccessInput } from "@/lib/api";

/** A list read page by page: `rows` is every row loaded so far, and `fetchNextPage` loads the next 50. */
function useRows<T>(queryKey: unknown[], read: (cursor?: string) => Promise<Page<T>>, enabled = true) {
  return useInfiniteQuery({
    queryKey, enabled, queryFn: ({ pageParam }) => read(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    select: (data) => data.pages.flatMap((p) => p.items),
  });
}

/** One key per user intent; a retry of the same mutation reuses it. */
export const newKey = () => (typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`);

/** Loader toast that turns into a confirmation with Undo, or an error. */
export function undoable<T>(opts: { pending: string; done: string; run: () => Promise<T>; undo?: () => Promise<unknown> }) {
  const id = toast.loading(opts.pending);
  return opts.run().then(
    (v) => {
      toast.success(opts.done, {
        id,
        action: opts.undo ? { label: "Undo", onClick: () => { const u = toast.loading("Undoing…"); opts.undo!().then(() => toast.success("Undone", { id: u }), () => toast.error("Couldn't undo. Try again.", { id: u })); } } : undefined,
      });
      return v;
    },
    (e) => { toast.error("Couldn't save that. Nothing was changed; try again.", { id }); throw e; },
  );
}

/** Optimistic list patch with rollback. */
async function patchList<T extends { id: string }>(qc: QueryClient, key: unknown[], change: { id: string; patch: Partial<T> }) {
  const { id, patch } = change;
  await qc.cancelQueries({ queryKey: key });
  const prev = qc.getQueryData<T[]>(key);
  if (prev) qc.setQueryData<T[]>(key, prev.map((x) => (x.id === id ? { ...x, ...patch } : x)));
  return () => qc.setQueryData(key, prev);
}

/* identity / license */
export const useMe = () => useQuery({ queryKey: ["me"], queryFn: api.me, staleTime: Infinity });
export const useLicense = () => useQuery({ queryKey: ["license"], queryFn: api.license });
export function useInstallLicense() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ content, key }: { content: string; key: string }) => api.installLicense(content, key),
    onSuccess: (license) => qc.setQueryData(["license"], license), // every screen that asks "is this Pro?" follows
  });
}
export function useFeature(feature: ProFeature) {
  const { data, isPending } = useLicense();
  return { enabled: !!data?.features.includes(feature), isPending, organization: data?.organization };
}
/** Only administrators may read the sources; everyone else gets a 403 if the screen asks. */
export const useIsAdmin = () => !!useMe().data?.roles.includes("admin");
/** Editing metadata and uploading files need editor_metadata; an administrator may do anything but read the audit log. */
export function useCanEditMetadata() {
  const roles = useMe().data?.roles ?? [];
  return roles.includes("editor_metadata") || roles.includes("admin");
}
/** Whether the signed-in person may open a screen; while they load, only the screens open to all. */
export function useCanOpenScreen() {
  const roles = useMe().data?.roles;
  return useCallback((href: string) => canOpenScreen(roles ?? NO_ROLES, href), [roles]);
}
const NO_ROLES: readonly Role[] = [];
/** Judging links needs curator; an administrator may do anything but read the audit log. */
export function useCanDecideLinks() {
  const roles = useMe().data?.roles ?? [];
  return roles.includes("curator") || roles.includes("admin");
}
// shortcut: loads every row, for the screens that look people up by id; replace with per-id fetches if lists pass a few thousand.
export const usePeople = () => useQuery({ queryKey: ["people"], queryFn: api.people });
export const usePeopleNames = () => useQuery({ queryKey: ["people", "names"], queryFn: api.peopleNames });
export const usePeoplePages = () => useRows(["people", "pages"], (cursor) => api.peoplePage(cursor));
export function useUpdateRoles() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, roles, key }: { id: string; roles: Role[]; key: string }) => api.updateRoles(id, roles, key),
    onMutate: ({ id, roles }) => patchList<Person>(qc, ["people"], { id, patch: { roles } }),
    onError: (_e, _v, rollback) => rollback?.(),
    onSettled: () => qc.invalidateQueries({ queryKey: ["people"] }),
  });
}

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.createUser,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["people"] }),
  });
}

export const useResetPassword = () => useMutation({ mutationFn: (id: string) => api.resetPassword(id) });
export const useResetSecondFactor = () => useMutation({ mutationFn: (id: string) => api.resetSecondFactor(id) });

export function useDeleteUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, key }: { id: string; key: string }) => api.deleteUser(id, key),
    onSettled: () => qc.invalidateQueries({ queryKey: ["people"] }),
  });
}
export const useChangePassword = () => useMutation({ mutationFn: (input: PasswordChange) => api.changePassword(input, newKey()) });
export const useDeleteMe = () => useMutation({ mutationFn: (key: string) => api.deleteMe(key) });

/* dashboard */
export const useOverview = () => useQuery({ queryKey: ["overview"], queryFn: api.overview });

/* assistant */
export const useConversations = () => useQuery({ queryKey: ["conversations"], queryFn: api.conversations });
export const useConversation = (id: string) =>
  useQuery({ queryKey: ["conversation", id], queryFn: () => api.conversation(id), enabled: !!id });

type AskState =
  | { status: "idle" }
  | { status: "streaming"; question: string; text: string; citations: Citation[]; notes: string[] }
  | { status: "done"; answer: Answer }
  | { status: "error"; question: string; message: string };

/** Streams an answer from AtlasApi.ask: server-sent events from /api/v1/ask, or the demo stream. */
export function useAskStream() {
  const [state, setState] = useState<AskState>({ status: "idle" });
  const qc = useQueryClient();
  const controller = useRef<AbortController | null>(null);
  const ask = useCallback(
    async (question: string, opts: { conversationId?: string } = {}) => {
      const mine = new AbortController();
      controller.current = mine;
      setState({ status: "streaming", question, text: "", citations: [], notes: [] });
      try {
        for await (const ev of api.ask(question, { ...opts, signal: mine.signal })) {
          if (mine.signal.aborted) return null;
          if (ev.type === "meta") setState((s) => (s.status === "streaming" ? { ...s, citations: ev.citations, notes: ev.notes } : s));
          if (ev.type === "token") setState((s) => (s.status === "streaming" ? { ...s, text: s.text + ev.t } : s));
          if (ev.type === "error") throw new Error(ev.message);
          if (ev.type === "done") {
            setState({ status: "done", answer: ev.answer });
            qc.invalidateQueries({ queryKey: ["conversations"] });
            return ev.answer;
          }
        }
      } catch (e) {
        if (mine.signal.aborted) return null; // the person stopped it: that is not an error to show
        setState({ status: "error", question, message: e instanceof Error ? e.message : "The model didn't respond." });
      }
      return null;
    },
    [qc],
  );
  const reset = useCallback(() => { controller.current?.abort(); setState({ status: "idle" }); }, []);
  return { state, ask, reset };
}

export function useFeedback() {
  return useMutation({
    mutationFn: ({ id, value, key }: { id: string; value: "up" | "down"; key: string }) => api.feedback(id, value, key),
    onSuccess: (_, v) => toast(v.value === "up" ? "Thanks, noted as helpful." : "Thanks. Quality will review this answer."),
    onError: () => toast.error("Couldn't send your feedback. Try again."),
  });
}

/* documents */
// shortcut: loads every row for id lookups; replace with per-id fetches if lists pass a few thousand.
export const useDocuments = (filter: DocumentFilter) =>
  useQuery({ queryKey: ["documents", filter], queryFn: () => api.documents(filter), placeholderData: (prev) => prev });
export const useDocumentsPages = (filter: DocumentFilter) => useRows(["documents", "pages", filter], (cursor) => api.documentsPage(filter, cursor));
export function useDocument(id: string) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: ["document", id],
    queryFn: () => api.document(id),
    // Arriving from a list that already holds this document renders at once (and lets the band morph).
    initialData: () => qc.getQueryData<Document[]>(["documents", {}])?.find((d) => d.id === id),
  });
}
export const useCatalog = () => useQuery({ queryKey: ["catalog"], queryFn: api.catalog, staleTime: Infinity });
export function useCorrectMetadata() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch, key }: { id: string; patch: MetadataPatch; key: string }) => api.correctMetadata(id, patch, key),
    onSuccess: (doc) => {
      qc.setQueryData(["document", doc.id], doc);
      ["documents", "related", "graph"].forEach((k) => qc.invalidateQueries({ queryKey: [k] }));
    },
  });
}

export function useDeleteDocument() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, key }: { id: string; key: string }) => api.deleteDocument(id, key),
    onSuccess: () => ["documents", "sources", "graph"].forEach((k) => qc.invalidateQueries({ queryKey: [k] })),
  });
}
export const useRelated = (id: string) => useQuery({ queryKey: ["related", id], queryFn: () => api.related(id) });

/* sources and ingestion */
/** Uploads files and refreshes what they feed: the sources, their runs and the documents. */
export function useUpload() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ files, key }: { files: File[]; key: string }) => api.uploadFiles(files, key),
    onSuccess: () => ["sources", "runs", "documents"].forEach((k) => qc.invalidateQueries({ queryKey: [k] })),
  });
}

const LIVE_REFRESH_MS = 5_000;
const IDLE_REFRESH_MS = 60_000;
const isScanning = (runs?: IngestionRun[]) => !!runs?.some((r) => r.finishedAt === null);
const refreshEvery = (runs?: IngestionRun[]) => (isScanning(runs) ? LIVE_REFRESH_MS : IDLE_REFRESH_MS);

/** Sources and runs refresh every few seconds while a scan is running, and once a minute otherwise. */
export function useSources(enabled = true) {
  const qc = useQueryClient();
  return useQuery({ queryKey: ["sources"], queryFn: api.sources, enabled,
    refetchInterval: () => refreshEvery(qc.getQueryData<IngestionRun[]>(["runs"])) });
}
export const useRuns = () => useQuery({ queryKey: ["runs"], queryFn: api.runs, refetchInterval: (q) => refreshEvery(q.state.data) });
export const useQuarantine = () => useQuery({ queryKey: ["quarantine"], queryFn: api.quarantine });
export function useSourceActions() {
  const qc = useQueryClient();
  const done = () => qc.invalidateQueries({ queryKey: ["sources"] });
  return {
    sync: useMutation({ mutationFn: ({ id, key }: { id: string; key: string }) => api.syncSource(id, key),
      onSuccess: () => { done(); qc.invalidateQueries({ queryKey: ["runs"] }); toast.success("Scan requested", { description: "New and changed files show up on this screen as they are read." }); },
      onError: () => toast.error("Couldn't start the sync. Try again."),
    }),
    pause: useMutation({
      mutationFn: ({ id, paused, key }: { id: string; paused: boolean; key: string }) => api.setSourcePaused(id, paused, key),
      onMutate: ({ id, paused }) => patchList<Source>(qc, ["sources"], { id, patch: { status: paused ? "paused" : "active" } }),
      onError: (_e, _v, rollback) => rollback?.(),
      onSettled: done,
    }),
    create: useMutation({
      mutationFn: ({ input, key }: { input: NewSource; key: string }) => api.createSource(input, key),
      onSuccess: () => { done(); qc.invalidateQueries({ queryKey: ["runs"] }); },
    }),
    remove: useMutation({
      mutationFn: ({ id, key }: { id: string; key: string }) => api.removeSource(id, key),
      onSuccess: () => { done(); ["documents", "runs", "graph"].forEach((k) => qc.invalidateQueries({ queryKey: [k] })); },
      onError: (e: Error) => toast.error(e.message),
    }),
    access: useMutation({
      mutationFn: ({ id, input, key }: { id: string; input: SourceAccessInput; key: string }) => api.setSourceAccess(id, input, key),
      onSuccess: () => { done(); toast.success("Access saved", { description: "Documents are hidden or shown to people straight away." }); },
      onError: (e: Error) => toast.error(e.message),
    }),
    retry: useMutation({
      mutationFn: ({ id, key }: { id: string; key: string }) => api.retryQuarantine(id, key),
      onSuccess: () => { qc.invalidateQueries({ queryKey: ["quarantine"] }); toast.success("Queued for another try"); },
      onError: () => toast.error("Couldn't queue the file. Try again."),
    }),
  };
}

/* quality */
export function useAlerts() {
  const { enabled } = useFeature("alerts");
  const canOpen = useCanOpenScreen()("/alerts");
  return useQuery({ queryKey: ["alerts"], queryFn: api.alerts, refetchInterval: 60_000, enabled: enabled && canOpen });
}
export function useUpdateAlert() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch, key }: { id: string; patch: AlertPatch; key: string }) => api.updateAlert(id, patch, key),
    onMutate: ({ id, patch }) => patchList<Alert>(qc, ["alerts"], { id, patch: alertChanges(patch) }),
    onError: (_e, _v, rollback) => rollback?.(),
    onSettled: () => { qc.invalidateQueries({ queryKey: ["alerts"] }); qc.invalidateQueries({ queryKey: ["overview"] }); },
  });
}
export function useGraph() {
  const { enabled } = useFeature("map");
  return useQuery({ queryKey: ["graph"], queryFn: api.graph, enabled });
}

export function useDecideEdge() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, status, key }: { id: string; status: "confirmed" | "rejected"; key: string }) => api.decideEdge(id, status, key),
    onSuccess: () => ["graph", "related"].forEach((k) => qc.invalidateQueries({ queryKey: [k] })),
  });
}

/* audit */
// shortcut: loads every event, for the dashboard widget that counts them; replace with a count endpoint if logs pass a few thousand.
export const useAudit = (enabled: boolean) => useQuery({ queryKey: ["audit"], queryFn: api.audit, enabled });
export const useAuditPages = (enabled: boolean) => useRows(["audit", "pages"], (cursor) => api.auditPage(cursor), enabled);
export const useExportAudit = () => useMutation({ mutationFn: api.exportAudit });
export const useVerifyAudit = () => useMutation({ mutationFn: api.verifyAudit });

/* settings */
export const useSettings = () => useQuery({ queryKey: ["settings"], queryFn: api.settings });
export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ patch, key }: { patch: SettingsPatch; key: string }) => api.updateSettings(patch, key),
    onMutate: async ({ patch }) => {
      await qc.cancelQueries({ queryKey: ["settings"] });
      const prev = qc.getQueryData<Settings>(["settings"]);
      if (prev) qc.setQueryData(["settings"], { ...prev, ...patch });
      return () => qc.setQueryData(["settings"], prev);
    },
    onError: (_e, _v, rollback) => rollback?.(),
    onSuccess: (s) => { qc.setQueryData(["settings"], s); void qc.invalidateQueries({ queryKey: ["me"] }); }, // the header takes the organization name from `me`
  });
}

/** Who may read each prefetched query (by its whole key, so `people` is not `people,names`); a query not listed is open to everyone. */
const PREFETCH_ROLES: Readonly<Record<string, readonly Role[]>> = {
  alerts: ["curator", "admin"], sources: ["admin"], runs: ["admin"], quarantine: ["admin"], people: ["admin"], settings: ["admin"],
};

/** Warm the cache for a screen before it is opened (hover or focus on its link); only what the person's role can read. */
export function usePrefetchScreen() {
  const qc = useQueryClient();
  const roles = useMe().data?.roles ?? NO_ROLES;
  return useCallback((href: string) => {
    const q: Record<string, [unknown[], () => Promise<unknown>][]> = {
      "/dashboard": [[["overview"], api.overview], [["alerts"], api.alerts], [["sources"], api.sources]],
      "/ask": [[["conversations"], api.conversations]],
      "/documents": [[["documents", {}], () => api.documents({})], [["people", "names"], api.peopleNames]],
      "/alerts": [[["alerts"], api.alerts]],
      "/map": [[["graph"], api.graph]],
      "/sources": [[["sources"], api.sources], [["runs"], api.runs], [["quarantine"], api.quarantine]],
      "/users": [[["people"], api.people]],
      "/settings": [[["settings"], api.settings], [["license"], api.license]],
    };
    const mayRead = (key: unknown[]) => { const needed = PREFETCH_ROLES[key.join()]; return !needed || hasAnyRole(roles, needed); };
    const readable = (q[href] ?? []).filter(([queryKey]) => mayRead(queryKey));
    readable.forEach(([queryKey, queryFn]) => qc.prefetchQuery({ queryKey, queryFn, staleTime: 30_000 }));
  }, [qc, roles]);
}
