// In-memory implementation of AtlasApi over the fictional plant. Swap for the OpenAPI client later;
// components only ever see AtlasApi through features/* hooks.
import * as plant from "@/lib/mocks/plant";
import { demoNow as clockNow } from "@/lib/demo/clock";
import { SESSION_COOKIE, SESSION_MAX_AGE_S } from "@/lib/auth";
import { alertChanges } from "./types";
import type {
  Alert, Answer, AskEvent, AtlasApi, BandKind, Citation, Document, DocumentFilter, Overview, Page, Person, SettingsPatch, Source,
} from "./types";

const clone = <T,>(v: T): T => structuredClone(v);
const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));
// shortcut: a small fixed delay keeps loading states honest without making the demo feel slow.
const latency = () => wait(40);

const MOCK_PAGE_SIZE = 50;
/** In-memory paging: the cursor is the index of the first row of the next page. */
const pageOf = <T,>(rows: T[], cursor?: string): Page<T> => {
  const from = Number(cursor ?? 0);
  const to = from + MOCK_PAGE_SIZE;
  return { items: clone(rows.slice(from, to)), nextCursor: to < rows.length ? String(to) : null };
};

/** Idempotency: a request replayed with the same key returns the first result instead of applying twice. */
const seen = new Map<string, Promise<unknown>>();
const once = <T,>(key: string | undefined, run: () => Promise<T>): Promise<T> => {
  if (!key) return run();
  if (!seen.has(key)) seen.set(key, run());
  return seen.get(key) as Promise<T>;
};

// Mutable state for the session (resets on reload).
const db = {
  alerts: clone(plant.alerts),
  answers: clone(plant.answers),
  conversations: clone(plant.conversations),
  people: clone(plant.people),
  settings: clone(plant.settings),
  sources: clone(plant.sources),
  quarantine: clone(plant.quarantine),
  license: clone(plant.license),
  edgeDecisions: new Map<string, "confirmed" | "rejected">(),
};

/** Same rule the server enforces: the organization always keeps an admin. */
function removePerson(id: string) {
  const admins = db.people.filter((p) => p.isActive && p.roles.includes("admin"));
  if (admins.length === 1 && admins[0].id === id) throw new Error("The last admin can't be deleted.");
  db.people = db.people.filter((p) => p.id !== id);
}

/** Dev switch so the Core (unlicensed) experience can be previewed. */
export function setPreviewPlan(plan: "core" | "pro") {
  db.license.plan = plan;
  db.license.features = plan === "pro" ? clone(plant.license.features) : [];
}

const today = plant.NOW.toISOString().slice(0, 10);

function overview(): Overview {
  const log = plant.questionLog;
  const todays = log.filter((q) => q.at.slice(0, 10) === today);
  const answered = todays.filter((q) => q.outcome === "answered").length;
  const days: Overview["week"] = [];
  for (let d = 13; d >= 0; d--) {
    const day = new Date(plant.NOW.getTime() - d * 86_400_000).toISOString().slice(0, 10);
    const rows = log.filter((q) => q.at.slice(0, 10) === day);
    days.push({ day, questions: rows.length, notFound: rows.filter((r) => r.outcome === "not_found").length });
  }
  const last7 = log.filter((q) => plant.NOW.getTime() - new Date(q.at).getTime() < 7 * 86_400_000);
  const counts = new Map<string, number>();
  last7.forEach((q) => counts.set(q.topic, (counts.get(q.topic) ?? 0) + 1));
  // Same definition as the Documents screen: every indexed document, whatever its status.
  const byBand = { procedure: 0, manual: 0, norm: 0, erp: 0 } as Record<BandKind, number>;
  plant.documents.forEach((d) => byBand[d.band]++);
  const open = db.alerts.filter((a) => a.status === "open");
  const sev = (s: Alert["severity"]) => open.filter((a) => a.severity === s).length;
  const top = open.find((a) => a.severity === "high");
  return {
    today: { questions: todays.length, answered, notFound: todays.length - answered, medianLatencyMs: 4100 },
    week: days,
    documents: { indexed: plant.documents.length, byBand, processing: 0, retrying: 0, quarantined: db.quarantine.length },
    openAlerts: { high: sev("high"), medium: sev("medium"), low: sev("low") },
    finding: top
      ? `${top.title}. Technicians asked about it ${counts.get("Coolant concentration") ?? 0} times this week.`
      : "No high-severity alerts are open.",
  };
}

function filterDocs(f: DocumentFilter = {}): Document[] {
  const q = f.q?.trim().toLowerCase();
  return plant.documents.filter((d) =>
    (!q || `${d.code} ${d.title} ${d.entities.map((e) => e.label).join(" ")} ${d.path}`.toLowerCase().includes(q)) &&
    (!f.band || f.band === "all" || d.band === f.band) &&
    (!f.status || f.status === "all" || d.status === f.status) &&
    (!f.sourceId || d.sourceId === f.sourceId),
  );
}

/** Pick a canned answer for the demo; everything else is an honest "not found". */
function matchAnswer(question: string): Answer | null {
  const q = question.toLowerCase();
  if (/torque|par de apriete|husillo|spindle/.test(q)) return plant.answers.find((a) => a.id === "a-torque")!;
  if (/lock ?out|loto|bloqueo|guard|isolat/.test(q)) return plant.answers.find((a) => a.id === "a-loto")!;
  if (/coolant|refrigerante|concentra/.test(q)) return plant.answers.find((a) => a.id === "a-coolant")!;
  return null;
}

async function* ask(question: string, opts: Parameters<AtlasApi["ask"]>[1]): AsyncIterable<AskEvent> {
  await wait(150);
  const base = matchAnswer(question);
  const answerId = `a-${Date.now().toString(36)}`;
  const conversationId = opts.conversationId ?? `c-${Date.now().toString(36)}`;
  const citations: Citation[] = base ? clone(base.citations) : [];
  const notes = base ? clone(base.notes) : [];
  yield { type: "meta", answerId, citations, notes };
  const text = base?.text ??
    "I couldn't find this in your documents. Nothing indexed mentions it closely enough for me to answer with a source.";
  for (const piece of text.match(/\S+\s*/g) ?? []) {
    if (opts.signal?.aborted) return;
    await wait(18 + Math.random() * 30);
    yield { type: "token", t: piece };
  }
  const answer: Answer = {
    id: answerId, conversationId, question, askedById: plant.ME_ID, askedAt: clockNow().toISOString(),
    outcome: base ? "answered" : "not_found", text, citations, notes,
    nearby: base ? [] : filterDocs({ q: question.split(/\s+/).find((w) => w.length > 4) }).slice(0, 3).map((d) => d.id),
    latencyMs: 1900 + text.length * 6, model: db.settings.chatModel.split(" ")[0], scope: null, feedback: null,
  };
  if (!answer.nearby.length && !base) answer.nearby = ["d-pnt014", "d-pnt021", "d-man-cnc07"];
  db.answers.push(answer);
  const conv = db.conversations.find((c) => c.id === conversationId);
  if (conv) { conv.answerIds.push(answerId); conv.updatedAt = answer.askedAt; }
  else db.conversations.unshift({ id: conversationId, title: question.slice(0, 60), updatedAt: answer.askedAt, answerIds: [answerId] });
  yield { type: "done", answer };
}

/** The one account of the demo. Real accounts exist only with the server (NEXT_PUBLIC_ATLAS_API=live). */
export const DEMO_ACCOUNT = { email: "admin@admin.es", password: "admin123", code: "123456" };
const DEMO_CHECK_MS = 900; // shortcut: the demo only simulates the folder check round trip

const demoCookie = (maxAge: number, value: string) => {
  document.cookie = `${SESSION_COOKIE}=${value}; path=/; max-age=${maxAge}; samesite=lax`;
};

export const mockApi: AtlasApi = {
  async setupStatus() { await latency(); return { setupDone: false }; },
  async setup() { await wait(DEMO_CHECK_MS); return { totpUri: "otpauth://totp/Atlas:demo?secret=JBSWY3DPEHPK3PXP&issuer=Atlas" }; },
  async login(email, password) {
    await wait(250);
    if (email.trim().toLowerCase() !== DEMO_ACCOUNT.email || password !== DEMO_ACCOUNT.password) throw new Error("Invalid credentials");
    return { challengeId: "demo", needsCode: true };
  },
  async verifyLogin(_challengeId, code) {
    await wait(250);
    if (code !== DEMO_ACCOUNT.code) throw new Error("Invalid credentials");
    demoCookie(SESSION_MAX_AGE_S, "demo");
  },
  async logout() { demoCookie(0, ""); },
  async checkFolder() { await wait(DEMO_CHECK_MS); return { ok: true, detail: "Atlas can read this folder." }; },
  async createSource(input) {
    await latency();
    const source: Source = { id: `s-${Date.now().toString(36)}`, name: input.name, kind: input.kind, location: input.location, status: "active", lastSyncAt: null, lastError: null, pro: false, access: "everyone", readers: [] };
    db.sources.push(source);
    return clone(source);
  },
  async uploadFiles(files) { await latency(); return { accepted: files.length, rejected: [] }; },
  async overview() { await latency(); return overview(); },
  ask,
  async conversations() {
    await latency();
    return clone(db.conversations)
      .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
      .map((c) => {
        const last = db.answers.find((a) => a.id === c.answerIds[c.answerIds.length - 1])!;
        const labels = last.citations.map((x) => {
          const d = plant.docById(x.documentId)!;
          return `${!d.code ? d.title : d.code} rev.${d.versions.find((v) => v.id === x.versionId)?.revision ?? ""}`;
        });
        return { ...c, askedById: last.askedById, lastOutcome: last.outcome, sources: [...new Set(labels)] };
      });
  },
  async conversation(id) {
    await latency();
    const conversation = db.conversations.find((c) => c.id === id);
    if (!conversation) throw new Error("Conversation not found");
    return { conversation: clone(conversation), answers: conversation.answerIds.map((aid) => clone(db.answers.find((a) => a.id === aid)!)) };
  },
  feedback: (answerId, value, key) => once(key, async () => { await wait(120); const a = db.answers.find((x) => x.id === answerId); if (a) a.feedback = value; }),
  async documents(filter) { await latency(); return clone(filterDocs(filter)); },
  async documentsPage(filter, cursor) { await latency(); return pageOf(filterDocs(filter), cursor); },
  async document(id) {
    await latency();
    const d = plant.docById(id);
    if (!d) throw new Error("Document not found");
    return clone(d);
  },
  async catalog() { await latency(); return { docTypes: plant.docTypeCatalog(), entityTypes: [{ key: "machine", label: "Machine" }, { key: "standard", label: "Standard" }] }; },
  correctMetadata: (id, patch, key) => once(key, async () => {
    await wait(250);
    const d = plant.docById(id);
    if (!d) throw new Error("Document not found");
    Object.assign(d, patch, patch.docType ? { band: plant.docTypeCatalog().find((t) => t.key === patch.docType)?.band ?? d.band } : {});
    d.provenance = [...d.provenance.filter((p) => !(p.field.toLowerCase() in patch)), ...Object.entries(patch).map(([field, value]) => ({ field, value: String(value ?? ""), source: "manual" as const, confidence: 1 }))];
    return clone(d);
  }),
  deleteDocument: (id, key) => once(key, async () => { await wait(250); throw new Error(`Only uploaded documents can be deleted (${id}).`); }),
  async related(id) {
    await latency();
    const g = plant.buildGraph();
    const key = `d:${id}`;
    return g.edges
      .filter((e) => (e.source === key || e.target === key) && (e.kind === "references" || e.kind === "duplicates"))
      .map((e) => ({ document: clone(plant.docById((e.source === key ? e.target : e.source).slice(2))!), via: e.kind }))
      .concat(
        plant.documents.filter((d) => d.id !== id && d.code === plant.docById(id)?.code && d.code)
          .map((d) => ({ document: clone(d), via: "duplicates" as const })),
      );
  },
  async sources() { await latency(); return clone(db.sources); },
  async runs() { await latency(); return clone(plant.runs); },
  async quarantine() { await latency(); return clone(db.quarantine); },
  retryQuarantine: (id, key) => once(key, async () => { await wait(600); db.quarantine = db.quarantine.filter((q) => q.id !== id); }),
  syncSource: (id, key) => once(key, async () => {
    await wait(900);
    const s = db.sources.find((x) => x.id === id);
    if (s) { s.lastSyncAt = clockNow().toISOString(); if (s.status === "error") { s.status = "active"; s.lastError = null; } }
  }),
  removeSource: (id, key) => once(key, async () => {
    await wait(250);
    const s = db.sources.find((x) => x.id === id);
    if (s?.kind === "upload") throw new Error("The uploads source cannot be removed; delete its files instead.");
    db.sources = db.sources.filter((x) => x.id !== id);
  }),
  setSourcePaused: (id, paused, key) => once(key, async () => {
    await wait(250);
    const s = db.sources.find((x) => x.id === id);
    if (!s) throw new Error("Source not found");
    s.status = paused ? "paused" : "active";
    return clone(s);
  }),
  setSourceAccess: (id, input, key) => once(key, async () => {
    await wait(250);
    const s = db.sources.find((x) => x.id === id);
    if (!s) throw new Error("Source not found");
    s.access = input.access;
    s.readers = input.access === "restricted" ? input.readers : [];
    return clone(s);
  }),
  async alerts() { await latency(); return clone(db.alerts); },
  updateAlert: (id, patch, key) => once(key, async () => {
    await wait(220);
    const a = db.alerts.find((x) => x.id === id);
    if (!a) throw new Error("Alert not found");
    Object.assign(a, alertChanges(patch));
    return clone(a);
  }),
  async graph() {
    await latency();
    const g = plant.buildGraph();
    return { ...g, edges: g.edges.filter((e) => db.edgeDecisions.get(e.id) !== "rejected") };
  },
  decideEdge: (id, status, key) => once(key, async () => { await wait(200); db.edgeDecisions.set(id, status); }),
  async audit() { await latency(); return clone(plant.auditLog).reverse(); },
  async auditPage(cursor) { await latency(); return pageOf([...plant.auditLog].reverse(), cursor); },
  async exportAudit({ from, to }) {
    await wait(300);
    const inRange = plant.auditLog.filter((e) => e.at.slice(0, 10) >= from && e.at.slice(0, 10) <= to);
    const rows = inRange.map((e) => [e.id, e.at, e.actorId ?? "", e.action, e.subject, e.detail].map((v) => `"${String(v).replaceAll('"', '""')}"`).join(","));
    return { file: new Blob([["id,at,actor_id,action,subject,detail", ...rows].join("\n")], { type: "text/csv" }), signature: "", keyId: "demo" };
  },
  async verifyAudit() {
    await wait(700);
    let prev = "0".repeat(64);
    for (const e of plant.auditLog) {
      const { hash, prevHash, ...rest } = e;
      if (prevHash !== prev || plant.hash64(prev + JSON.stringify(rest)) !== hash) return { ok: false, checked: e.id - 401, brokenAt: e.id };
      prev = hash;
    }
    return { ok: true, checked: plant.auditLog.length, brokenAt: null };
  },
  async people() { await latency(); return clone(db.people); },
  async peopleNames() { await latency(); return db.people.filter((p) => p.isActive).map(({ id, name }) => ({ id, name })); },
  async peoplePage(cursor) { await latency(); return pageOf(db.people, cursor); },
  updateRoles: (id, roles, key) => once(key, async () => {
    await wait(220);
    const p = db.people.find((x) => x.id === id) as Person;
    p.roles = roles;
    return clone(p);
  }),
  async createUser(input) {
    await wait(300);
    if (db.people.some((p) => p.email.toLowerCase() === input.email.toLowerCase())) throw new Error("An account with this e-mail exists.");
    const person: Person = { id: `u-${Date.now().toString(36)}`, name: input.name, email: input.email, department: "", roles: input.roles,
      isActive: true, lastActiveAt: null, hue: input.name.length * 37 % 360, mustChangePassword: true };
    db.people.push(person);
    return { person: clone(person), password: "demo-one-time-password", totpUri: input.secondFactor ? "otpauth://totp/Atlas:demo?secret=JBSWY3DPEHPK3PXP&issuer=Atlas" : null };
  },
  async resetPassword() { await wait(250); return { password: "demo-one-time-password" }; },
  async resetSecondFactor() { await wait(250); return { totpUri: "otpauth://totp/Atlas:demo?secret=JBSWY3DPEHPK3PXP&issuer=Atlas" }; },
  deleteUser: (id, key) => once(key, async () => { await wait(220); removePerson(id); }),
  changePassword: async (input) => {
    await wait(300);
    if (input.currentPassword === input.newPassword) throw new Error("Choose a password you have not used here.");
  },
  deleteMe: (key) => once(key, async () => { await wait(220); removePerson(plant.ME_ID); }),
  async settings() { await latency(); return clone(db.settings); },
  updateSettings: (patch: SettingsPatch, key?: string) => once(key, async () => { await wait(250); Object.assign(db.settings, patch); return clone(db.settings); }),
  async license() { await wait(60); return clone(db.license); },
  installLicense: (content, key) => once(key, async () => {
    await wait(300);
    if (!/pro/i.test(content)) throw new Error("The file is not a license file.");
    db.license.plan = "pro";
    db.license.features = clone(plant.license.features);
    return clone(db.license);
  }),
  async me() { await wait(30); return { ...clone(db.people.find((p) => p.id === plant.ME_ID)!), orgName: db.settings.orgName }; },
};
