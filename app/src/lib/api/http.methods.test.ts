import { afterEach, describe, expect, it, vi } from "vitest";
import { httpApi } from "./http";
import type { AtlasApi } from "./types";

interface Case {
  http: string;
  path: string;
  /** JSON body of the canned answer; `null` answers with no body. */
  reply: unknown;
  status?: number;
  run: () => unknown;
}

const doc = { id: "d1", current_version_id: "v1", versions: [] };
const overview = { today: {}, week: [], documents: { by_band: {} }, open_alerts: {}, finding: "" };
const sseDone = 'data: {"type":"done","answer":{"id":"a1","conversation_id":"c1"}}\n\n';

// A method added to AtlasApi without a case here does not compile.
const CASES = {
  setupStatus: { http: "GET", path: "/api/v1/setup", reply: { setup_done: true }, run: () => httpApi.setupStatus() },
  setup: { http: "POST", path: "/api/v1/setup", reply: { totp_uri: "u" }, run: () => httpApi.setup({ orgName: "o", name: "n", email: "e", password: "p" }) },
  login: { http: "POST", path: "/api/v1/auth/login", reply: { challenge_id: "c", needs_code: false }, run: () => httpApi.login("e", "p") },
  verifyLogin: { http: "POST", path: "/api/v1/auth/verify", reply: null, status: 204, run: () => httpApi.verifyLogin("c", "123456") },
  logout: { http: "POST", path: "/api/v1/auth/logout", reply: null, status: 204, run: () => httpApi.logout() },
  checkFolder: { http: "POST", path: "/api/v1/sources/check", reply: { ok: true, detail: "" }, run: () => httpApi.checkFolder("/sources/docs") },
  createSource: { http: "POST", path: "/api/v1/sources", reply: { id: "s1" }, status: 201, run: () => httpApi.createSource({ name: "n", kind: "folder", location: "/x" }) },
  uploadFiles: { http: "POST", path: "/api/v1/uploads", reply: { accepted: 1, rejected: [] }, status: 202, run: () => httpApi.uploadFiles([new File(["x"], "a.pdf")]) },
  overview: { http: "GET", path: "/api/v1/stats/overview", reply: overview, run: () => httpApi.overview() },
  ask: { http: "POST", path: "/api/v1/ask", reply: sseDone, run: async () => { for await (const e of httpApi.ask("q", {})) void e; } },
  conversations: { http: "GET", path: "/api/v1/conversations", reply: [], run: () => httpApi.conversations() },
  conversation: { http: "GET", path: "/api/v1/conversations/c1", reply: {}, run: () => httpApi.conversation("c1") },
  feedback: { http: "POST", path: "/api/v1/answers/a1/feedback", reply: null, status: 204, run: () => httpApi.feedback("a1", "up") },
  documents: { http: "GET", path: "/api/v1/documents", reply: [doc], run: () => httpApi.documents() },
  documentsPage: { http: "GET", path: "/api/v1/documents", reply: [doc], run: () => httpApi.documentsPage({}) },
  document: { http: "GET", path: "/api/v1/documents/d1", reply: doc, run: () => httpApi.document("d1") },
  catalog: { http: "GET", path: "/api/v1/catalog", reply: { doc_types: [], entity_types: [] }, run: () => httpApi.catalog() },
  correctMetadata: { http: "PATCH", path: "/api/v1/documents/d1/metadata", reply: doc, run: () => httpApi.correctMetadata("d1", { code: "PNT-1" }) },
  deleteDocument: { http: "DELETE", path: "/api/v1/documents/d1", reply: null, status: 204, run: () => httpApi.deleteDocument("d1") },
  related: { http: "GET", path: "/api/v1/documents/d1/related", reply: [], run: () => httpApi.related("d1") },
  sources: { http: "GET", path: "/api/v1/sources", reply: [], run: () => httpApi.sources() },
  runs: { http: "GET", path: "/api/v1/ingestion/runs", reply: [], run: () => httpApi.runs() },
  quarantine: { http: "GET", path: "/api/v1/ingestion/quarantine", reply: [], run: () => httpApi.quarantine() },
  retryQuarantine: { http: "POST", path: "/api/v1/ingestion/quarantine/q1/retry", reply: null, status: 202, run: () => httpApi.retryQuarantine("q1") },
  syncSource: { http: "POST", path: "/api/v1/sources/s1/sync", reply: null, status: 202, run: () => httpApi.syncSource("s1") },
  removeSource: { http: "DELETE", path: "/api/v1/sources/s1", reply: null, status: 204, run: () => httpApi.removeSource("s1") },
  setSourcePaused: { http: "PATCH", path: "/api/v1/sources/s1", reply: { id: "s1" }, run: () => httpApi.setSourcePaused("s1", true) },
  setSourceAccess: { http: "PATCH", path: "/api/v1/sources/s1", reply: { id: "s1" }, run: () => httpApi.setSourceAccess("s1", { access: "everyone" }) },
  alerts: { http: "GET", path: "/api/v1/alerts", reply: [], run: () => httpApi.alerts() },
  updateAlert: { http: "PATCH", path: "/api/v1/alerts/a1", reply: { id: "a1" }, run: () => httpApi.updateAlert("a1", { status: "ack" }) },
  decideEdge: { http: "PATCH", path: "/api/v1/graph/edges/e1", reply: null, status: 204, run: () => httpApi.decideEdge("e1", "rejected") },
  graph: { http: "GET", path: "/api/v1/graph/neighborhood", reply: { nodes: [], edges: [] }, run: () => httpApi.graph() },
  audit: { http: "GET", path: "/api/v1/audit/events", reply: [], run: () => httpApi.audit() },
  exportAudit: { http: "GET", path: "/api/v1/audit/export", reply: [], run: () => httpApi.exportAudit({ from: "2026-11-01", to: "2026-11-02", format: "csv" }) },
  auditPage: { http: "GET", path: "/api/v1/audit/events", reply: [], run: () => httpApi.auditPage() },
  verifyAudit: { http: "POST", path: "/api/v1/audit/verify", reply: { ok: true, checked: 0 }, run: () => httpApi.verifyAudit() },
  people: { http: "GET", path: "/api/v1/users", reply: [], run: () => httpApi.people() },
  peopleNames: { http: "GET", path: "/api/v1/people", reply: [], run: () => httpApi.peopleNames() },
  peoplePage: { http: "GET", path: "/api/v1/users", reply: [], run: () => httpApi.peoplePage() },
  updateRoles: { http: "PATCH", path: "/api/v1/users/u1/roles", reply: { id: "u1" }, run: () => httpApi.updateRoles("u1", ["viewer"]) },
  createUser: { http: "POST", path: "/api/v1/users", reply: { person: {}, password: "p", totp_uri: null }, status: 201, run: () => httpApi.createUser({ name: "n", email: "e@x.test", roles: ["viewer"], secondFactor: false }) },
  resetPassword: { http: "POST", path: "/api/v1/users/u1/password-reset", reply: { password: "p" }, run: () => httpApi.resetPassword("u1") },
  resetSecondFactor: { http: "POST", path: "/api/v1/users/u1/totp-reset", reply: { totp_uri: "u" }, run: () => httpApi.resetSecondFactor("u1") },
  deleteUser: { http: "DELETE", path: "/api/v1/users/u1", reply: null, status: 204, run: () => httpApi.deleteUser("u1") },
  deleteMe: { http: "DELETE", path: "/api/v1/me", reply: null, status: 204, run: () => httpApi.deleteMe() },
  changePassword: { http: "PUT", path: "/api/v1/me/password", reply: null, status: 204, run: () => httpApi.changePassword({ currentPassword: "a", newPassword: "b" }) },
  settings: { http: "GET", path: "/api/v1/settings", reply: {}, run: () => httpApi.settings() },
  updateSettings: { http: "PATCH", path: "/api/v1/settings", reply: {}, run: () => httpApi.updateSettings({ orgName: "o" }) },
  license: { http: "GET", path: "/api/v1/system/license", reply: {}, run: () => httpApi.license() },
  installLicense: { http: "PUT", path: "/api/v1/system/license", reply: {}, run: () => httpApi.installLicense("{}") },
  me: { http: "GET", path: "/api/v1/me", reply: { org_name: "Acme" }, run: () => httpApi.me() },
} satisfies Record<keyof AtlasApi, Case>;

/** Answers every request with the case's canned reply and records the last one. */
function answerWith(c: Case) {
  const seen: { url: string; method: string }[] = [];
  vi.stubGlobal("fetch", vi.fn(async (url: string, init: RequestInit) => {
    seen.push({ url, method: init.method ?? "GET" });
    const isStream = typeof c.reply === "string";
    const body = c.reply === null || isStream ? (c.reply as string | null) : JSON.stringify(c.reply);
    const type = isStream ? "text/event-stream" : "application/json";
    return new Response(body, { status: c.status ?? 200, headers: { "Content-Type": type } });
  }));
  return seen;
}

afterEach(() => vi.unstubAllGlobals());

describe("every AtlasApi method of the http adapter", () => {
  it.each(Object.entries(CASES))("%s sends its request and accepts the answer", async (_name, c: Case) => {
    const seen = answerWith(c);
    await c.run();
    expect(seen[0].method).toBe(c.http);
    expect(seen[0].url.split("?")[0]).toBe(c.path);
  });
});

describe("ask", () => {
  it("stops the request when the person aborts it", async () => {
    vi.stubGlobal("fetch", vi.fn(async (_url: string, init: RequestInit) => {
      init.signal?.throwIfAborted();
      return new Response(null);
    }));
    const stop = new AbortController();
    stop.abort();
    const drain = async () => { for await (const e of httpApi.ask("q", { signal: stop.signal })) void e; };
    await expect(drain()).rejects.toMatchObject({ name: "AbortError" });
  });
});

describe("the lists that page", () => {
  /** Answers with two rows and a Next-Cursor, and records the URL asked. */
  function pagedAnswer() {
    const seen: string[] = [];
    vi.stubGlobal("fetch", vi.fn(async (url: string) => {
      seen.push(url);
      return new Response(JSON.stringify([doc, doc]), { status: 200, headers: { "Content-Type": "application/json", "Next-Cursor": "after-d1" } });
    }));
    return seen;
  }

  it("asks for 50 rows after the cursor and returns the next cursor from the header", async () => {
    const seen = pagedAnswer();
    const page = await httpApi.documentsPage({ q: "pump" }, "c1");
    expect(seen[0]).toContain("limit=50");
    expect(seen[0]).toContain("cursor=c1");
    expect(seen[0]).toContain("q=pump");
    expect(page.nextCursor).toBe("after-d1");
    expect(page.items).toHaveLength(2);
  });

  it("starts without a cursor and has no next page when the header is absent", async () => {
    const seen: string[] = [];
    vi.stubGlobal("fetch", vi.fn(async (url: string) => { seen.push(url); return new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } }); }));
    const page = await httpApi.auditPage();
    expect(seen[0]).not.toContain("cursor=");
    expect(page).toEqual({ items: [], nextCursor: null });
  });
});
