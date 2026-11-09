"use client";
// TanStack Query hooks over AtlasApi, one block per bounded context. Screens only call these.
// Reads are stale-while-revalidate (cached data shows at once, refetches quietly); writes are optimistic,
// carry an idempotency key (retries reuse it), and confirm with a toast that can undo.
import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useCallback, useRef, useState } from "react";
import { toast } from "sonner";
import { alertChanges, api } from "@/lib/api";
import type { Alert, AlertPatch, Answer, Citation, Document, DocumentFilter, PasswordChange, Person, ProFeature, Role, Settings, SettingsPatch, Source, SourceAccessInput } from "@/lib/api";

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
export function useFeature(feature: ProFeature) {
  const { data, isPending } = useLicense();
  return { enabled: !!data?.features.includes(feature), isPending, organization: data?.organization };
}
export const usePeople = () => useQuery({ queryKey: ["people"], queryFn: api.people });
export function useUpdateRoles() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, roles, key }: { id: string; roles: Role[]; key: string }) => api.updateRoles(id, roles, key),
    onMutate: ({ id, roles }) => patchList<Person>(qc, ["people"], { id, patch: { roles } }),
    onError: (_e, _v, rollback) => rollback?.(),
    onSettled: () => qc.invalidateQueries({ queryKey: ["people"] }),
  });
}

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
  const abort = useRef(false);
  const ask = useCallback(
    async (question: string, opts: { conversationId?: string; scope?: string | null } = {}) => {
      abort.current = false;
      setState({ status: "streaming", question, text: "", citations: [], notes: [] });
      try {
        for await (const ev of api.ask(question, opts)) {
          if (abort.current) return null;
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
        setState({ status: "error", question, message: e instanceof Error ? e.message : "The model didn't respond." });
      }
      return null;
    },
    [qc],
  );
  const reset = useCallback(() => { abort.current = true; setState({ status: "idle" }); }, []);
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
export const useDocuments = (filter: DocumentFilter) =>
  useQuery({ queryKey: ["documents", filter], queryFn: () => api.documents(filter), placeholderData: (prev) => prev });
export function useDocument(id: string) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: ["document", id],
    queryFn: () => api.document(id),
    // Arriving from a list that already holds this document renders at once (and lets the band morph).
    initialData: () => qc.getQueryData<Document[]>(["documents", {}])?.find((d) => d.id === id),
  });
}
export const useRelated = (id: string) => useQuery({ queryKey: ["related", id], queryFn: () => api.related(id) });

/* sources and ingestion */
export const useSources = () => useQuery({ queryKey: ["sources"], queryFn: api.sources, refetchInterval: 60_000 });
export const useRuns = () => useQuery({ queryKey: ["runs"], queryFn: api.runs });
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
export const useAlerts = () => useQuery({ queryKey: ["alerts"], queryFn: api.alerts, refetchInterval: 60_000 });
export function useUpdateAlert() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch, key }: { id: string; patch: AlertPatch; key: string }) => api.updateAlert(id, patch, key),
    onMutate: ({ id, patch }) => patchList<Alert>(qc, ["alerts"], { id, patch: alertChanges(patch) }),
    onError: (_e, _v, rollback) => rollback?.(),
    onSettled: () => { qc.invalidateQueries({ queryKey: ["alerts"] }); qc.invalidateQueries({ queryKey: ["overview"] }); },
  });
}
export const useGraph = () => useQuery({ queryKey: ["graph"], queryFn: api.graph });

/* audit */
export const useAudit = () => useQuery({ queryKey: ["audit"], queryFn: api.audit });
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
    onSuccess: (s) => qc.setQueryData(["settings"], s),
  });
}

/** Warm the cache for a screen before it is opened (hover or focus on its link). */
export function usePrefetchScreen() {
  const qc = useQueryClient();
  return useCallback((href: string) => {
    const q: Record<string, [unknown[], () => Promise<unknown>][]> = {
      "/dashboard": [[["overview"], api.overview], [["alerts"], api.alerts], [["sources"], api.sources]],
      "/ask": [[["conversations"], api.conversations]],
      "/documents": [[["documents", {}], () => api.documents({})], [["people"], api.people]],
      "/alerts": [[["alerts"], api.alerts]],
      "/map": [[["graph"], api.graph]],
      "/sources": [[["sources"], api.sources], [["runs"], api.runs], [["quarantine"], api.quarantine]],
      "/audit": [[["audit"], api.audit]],
      "/users": [[["people"], api.people]],
      "/settings": [[["settings"], api.settings], [["license"], api.license]],
    };
    (q[href] ?? []).forEach(([queryKey, queryFn]) => qc.prefetchQuery({ queryKey, queryFn, staleTime: 30_000 }));
  }, [qc]);
}
