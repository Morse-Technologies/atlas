// Compile-time check that the app's types still match server/api/openapi.yaml. There is no runtime code here:
// if the contract changes (`pnpm gen:api` refreshes schema.d.ts) and a type in types.ts no longer fits, `tsc` fails
// on the line below. The wire value, camelized, must be usable as the app type.
import type { components } from "./schema";
import type * as App from "./types";
import type { Camelize } from "./wire";

type Wire<N extends keyof components["schemas"]> = Camelize<components["schemas"][N]>;
// The server lists only the bands that have documents; the adapter fills the rest with zero.
type WireOverview = Omit<App.Overview, "documents"> & {
  documents: Omit<App.Overview["documents"], "byBand"> & { byBand: Partial<Record<App.BandKind, number>> };
};
type Fits<W, A> = [W] extends [A] ? true : never;

export const contract: {
  person: Fits<Wire<"Person">, App.Person>;
  document: Fits<Wire<"Document">, App.Document>;
  related: Fits<Wire<"Related">, { document: App.Document; via: App.GraphEdge["kind"] }>;
  citation: Fits<Wire<"Citation">, App.Citation>;
  answer: Fits<Wire<"Answer">, App.Answer>;
  conversation: Fits<Wire<"ConversationSummary">, App.ConversationSummary>;
  conversationDetail: Fits<Wire<"ConversationDetail">, { conversation: App.Conversation; answers: App.Answer[] }>;
  source: Fits<Wire<"Source">, App.Source>;
  run: Fits<Wire<"IngestionRun">, App.IngestionRun>;
  quarantine: Fits<Wire<"QuarantineItem">, App.QuarantineItem>;
  alert: Fits<Wire<"Alert">, App.Alert>;
  graph: Fits<Wire<"Graph">, { nodes: App.GraphNode[]; edges: App.GraphEdge[] }>;
  audit: Fits<Wire<"AuditEvent">, App.AuditEvent>;
  verification: Fits<Wire<"AuditVerification">, { ok: boolean; checked: number; brokenAt: number | null }>;
  settings: Fits<Wire<"Settings">, App.Settings>;
  license: Fits<Wire<"License">, App.License>;
  overview: Fits<Wire<"Overview">, WireOverview>;
  askMeta: Fits<Wire<"AskMetaEvent">, Extract<App.AskEvent, { type: "meta" }>>;
  askToken: Fits<Wire<"AskTokenEvent">, Extract<App.AskEvent, { type: "token" }>>;
  askDone: Fits<Wire<"AskDoneEvent">, Extract<App.AskEvent, { type: "done" }>>;
  askError: Fits<Wire<"AskErrorEvent">, Extract<App.AskEvent, { type: "error" }>>;
} = {
  person: true, document: true, related: true, citation: true, answer: true, conversation: true, conversationDetail: true,
  source: true, run: true, quarantine: true, alert: true, graph: true, audit: true, verification: true, settings: true,
  license: true, overview: true, askMeta: true, askToken: true, askDone: true, askError: true,
};

// The other direction: what the app sends. After snake_case conversion the body must fit the request the
// contract describes, so a field renamed or made required on the server fails here, not in the browser.
type Request<N extends keyof components["schemas"]> = Camelize<components["schemas"][N]>;
type Sends<A, W> = [A] extends [W] ? true : never;

export const requestContract: {
  setup: Sends<App.SetupInput, Request<"SetupRequest">>;
  createSource: Sends<App.NewSource, Request<"SourceCreate">>;
  alertPatch: Sends<App.AlertPatch, Request<"AlertPatch">>;
  settingsPatch: Sends<App.SettingsPatch, Request<"SettingsPatch">>;
  roles: Sends<{ roles: App.Role[] }, Request<"RolesUpdate">>;
  feedback: Sends<{ value: "up" | "down" }, Request<"FeedbackRequest">>;
  folderCheck: Sends<{ path: string }, Request<"FolderCheckRequest">>;
  sourceAccess: Sends<App.SourceAccessInput, Request<"SourcePatch">>;
  password: Sends<App.PasswordChange, Request<"PasswordChange">>;
  login: Sends<{ email: string; password: string }, Request<"LoginRequest">>;
  verify: Sends<{ challengeId: string; code?: string }, Request<"VerifyRequest">>;
  sourcePatch: Sends<{ paused: boolean }, Request<"SourcePatch">>;
  ask: Sends<{ question: string; conversationId?: string; scope?: string | null }, Request<"AskRequest">>;
} = {
  setup: true, createSource: true, alertPatch: true, settingsPatch: true, roles: true, feedback: true,
  folderCheck: true, password: true, sourceAccess: true, login: true, verify: true, sourcePatch: true, ask: true,
};
