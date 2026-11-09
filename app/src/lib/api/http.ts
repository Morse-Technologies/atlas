// AtlasApi over the Go server (server/api/openapi.yaml). Same origin: Caddy sends /api to the server, so the
// HttpOnly session cookie travels by itself and there is no CORS. Payloads are converted between the wire's
// snake_case and the app's camelCase by wire.ts; contract.ts checks the types against the contract.
import { camelizeKeys, snakeizeKeys } from "./wire";
import type {
  AskEvent, AtlasApi, BandKind, Document, DocumentFilter, FolderCheck, LoginChallenge, Overview, UploadResult,
} from "./types";

const BASE = "/api/v1";
const PAGE_SIZE = 200; // the server's maximum
const MAX_PAGES = 50; // a list that long means something is wrong; stop instead of looping
const BANDS: BandKind[] = ["procedure", "manual", "norm", "erp"];

/** A non-2xx answer. `status` lets callers tell "not found" from "forbidden"; `message` is the server's title. */
export class ApiError extends Error {
  constructor(readonly status: number, message: string) {
    super(message);
    this.name = "ApiError";
  }
}

interface Call {
  method?: string;
  body?: unknown;
  form?: FormData;
  key?: string; // Idempotency-Key
  query?: Record<string, string | number | undefined>;
  /** Sign-in calls answer 401 for a wrong password; that is not "your session ended". */
  keepOn401?: boolean;
  accept?: string;
}

function url(path: string, query: Call["query"] = {}): string {
  const params = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) if (v !== undefined && v !== "") params.set(k, String(v));
  const qs = params.toString();
  return `${BASE}${path}${qs ? `?${qs}` : ""}`;
}

function headers(c: Call): HeadersInit {
  const h: Record<string, string> = { Accept: c.accept ?? "application/json" };
  if (c.body !== undefined) h["Content-Type"] = "application/json";
  if (c.key) h["Idempotency-Key"] = c.key;
  return h;
}

/** Sends one request. Throws ApiError for any non-2xx; a 401 outside sign-in sends the browser to /login. */
async function call(path: string, c: Call = {}): Promise<Response> {
  const res = await fetch(url(path, c.query), {
    method: c.method ?? "GET",
    headers: headers(c),
    body: c.form ?? (c.body === undefined ? undefined : JSON.stringify(snakeizeKeys(c.body))),
    credentials: "same-origin",
  });
  if (res.ok) return res;
  if (res.status === 401 && !c.keepOn401) signedOut();
  throw new ApiError(res.status, await problemTitle(res));
}

async function problemTitle(res: Response): Promise<string> {
  try {
    const p = (await res.json()) as { title?: string; detail?: string };
    return p.detail || p.title || res.statusText;
  } catch {
    return res.statusText || `HTTP ${res.status}`;
  }
}

function signedOut() {
  if (typeof window === "undefined" || window.location.pathname === "/login") return;
  const next = encodeURIComponent(window.location.pathname + window.location.search);
  window.location.replace(`/login?next=${next}`);
}

async function json<T>(path: string, c: Call = {}): Promise<T> {
  const res = await call(path, c);
  return res.status === 204 ? (undefined as T) : camelizeKeys<T>(await res.json());
}

/** Reads every page of a list: the server answers one page at a time and names the next in Next-Cursor. */
async function list<T>(path: string, query: Call["query"] = {}): Promise<T[]> {
  const out: T[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < MAX_PAGES; page++) {
    const res = await call(path, { query: { ...query, limit: PAGE_SIZE, cursor } });
    out.push(...camelizeKeys<T[]>(await res.json()));
    cursor = res.headers.get("Next-Cursor") ?? undefined;
    if (!cursor) return out;
  }
  return out;
}

/** The PDF the viewer shows for a document's current version. */
const fileUrl = (d: Pick<Document, "id" | "currentVersionId">) => `${BASE}/documents/${d.id}/versions/${d.currentVersionId}/file`;
const withFile = (d: Document): Document => ({ ...d, sample: fileUrl(d) });

function documentQuery(f: DocumentFilter = {}): Call["query"] {
  return { q: f.q, band: f.band === "all" ? undefined : f.band, status: f.status === "all" ? undefined : f.status, source_id: f.sourceId };
}

function withEveryBand(o: Overview): Overview {
  const byBand = Object.fromEntries(BANDS.map((b) => [b, o.documents.byBand[b] ?? 0])) as Overview["documents"]["byBand"];
  return { ...o, documents: { ...o.documents, byBand } };
}

/** Server-sent events: one AskEvent per `data:` line; lines starting with ":" are keep-alive pings. */
export async function* readEvents(body: ReadableStream<Uint8Array>): AsyncIterable<AskEvent> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let finished = false;
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    for (let end = buffer.indexOf("\n\n"); end >= 0; end = buffer.indexOf("\n\n")) {
      const event = parseFrame(buffer.slice(0, end));
      buffer = buffer.slice(end + 2);
      if (!event) continue;
      yield event;
      finished = event.type === "done" || event.type === "error";
    }
  }
  if (!finished) yield { type: "error", message: "The answer stopped before it was complete." };
}

function parseFrame(frame: string): AskEvent | null {
  const data = frame.split("\n").filter((l) => l.startsWith("data: ")).map((l) => l.slice(6)).join("\n");
  return data ? camelizeKeys<AskEvent>(JSON.parse(data)) : null;
}

async function* ask(question: string, opts: { conversationId?: string; scope?: string | null }): AsyncIterable<AskEvent> {
  const res = await call("/ask", { method: "POST", body: { question, conversationId: opts.conversationId, scope: opts.scope }, accept: "text/event-stream" });
  if (!res.body) throw new ApiError(res.status, "The server sent no answer.");
  yield* readEvents(res.body);
}

const action = (path: string, key?: string) => json<void>(path, { method: "POST", key });

export const httpApi: AtlasApi = {
  setupStatus: () => json("/setup"),
  setup: (input, key) => json("/setup", { method: "POST", body: input, key }),
  login: (email, password) => json<LoginChallenge>("/auth/login", { method: "POST", body: { email, password }, keepOn401: true }),
  verifyLogin: (challengeId, code) => json<void>("/auth/verify", { method: "POST", body: { challengeId, code }, keepOn401: true }),
  logout: () => action("/auth/logout"),
  checkFolder: (path) => json<FolderCheck>("/sources/check", { method: "POST", body: { path } }),
  createSource: (input, key) => json("/sources", { method: "POST", body: input, key }),
  uploadFiles: (files) => {
    const form = new FormData();
    files.forEach((f) => form.append("files", f));
    return json<UploadResult>("/uploads", { method: "POST", form });
  },
  overview: async () => withEveryBand(await json<Overview>("/stats/overview")),
  ask,
  conversations: () => list("/conversations"),
  conversation: (id) => json(`/conversations/${id}`),
  feedback: (answerId, value, key) => json<void>(`/answers/${answerId}/feedback`, { method: "POST", body: { value }, key }),
  documents: async (filter) => (await list<Document>("/documents", documentQuery(filter))).map(withFile),
  document: async (id) => withFile(await json<Document>(`/documents/${id}`)),
  related: async (id) => (await json<{ document: Document; via: never }[]>(`/documents/${id}/related`)).map((r) => ({ ...r, document: withFile(r.document) })),
  sources: () => json("/sources"),
  runs: () => list("/ingestion/runs"),
  quarantine: () => list("/ingestion/quarantine"),
  retryQuarantine: (id, key) => action(`/ingestion/quarantine/${id}/retry`, key),
  syncSource: (id, key) => action(`/sources/${id}/sync`, key),
  setSourcePaused: (id, paused, key) => json(`/sources/${id}`, { method: "PATCH", body: { paused }, key }),
  setSourceAccess: (id, input, key) => json(`/sources/${id}`, { method: "PATCH", body: input, key }),
  alerts: () => list("/alerts"),
  updateAlert: (id, patch, key) => json(`/alerts/${id}`, { method: "PATCH", body: patch, key }),
  graph: () => json("/graph/neighborhood"),
  audit: () => list("/audit/events"),
  verifyAudit: () => json("/audit/verify", { method: "POST" }),
  people: () => list("/users"),
  updateRoles: (id, roles, key) => json(`/users/${id}/roles`, { method: "PATCH", body: { roles }, key }),
  deleteUser: (id, key) => json<void>(`/users/${id}`, { method: "DELETE", key }),
  deleteMe: (key) => json<void>("/me", { method: "DELETE", key }),
  changePassword: (input, key) => json<void>("/me/password", { method: "PUT", body: input, key }),
  settings: () => json("/settings"),
  updateSettings: (patch, key) => json("/settings", { method: "PATCH", body: patch, key }),
  license: () => json("/system/license"),
  me: () => json("/me"),
};
