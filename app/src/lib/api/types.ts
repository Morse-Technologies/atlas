// Resource shapes mirroring docs/ATLAS_DESIGN.md §13 and docs/ATLAS.md §5.4 (snake_case on the wire, camelCase here).
// Core is domain-agnostic: sector vocabulary (machines, lines, clients…) arrives as catalog data from a domain pack.

export type BandKind = "procedure" | "manual" | "norm" | "erp";
/** Key in the tenant's document-type taxonomy (catalog.doc_types); domain packs add their own. */
export type DocType = string;
export type DocStatus = "current" | "draft" | "obsolete" | "unknown";
export type ProvenanceSource = "manual" | "titleblock" | "path" | "erp" | "llm";
export type Role = "viewer" | "editor_metadata" | "curator" | "organizer_approver" | "admin" | "auditor";

/** An entity from the catalog. `type` is a key in catalog.entity_types (Core: person, department, organization, standard). */
export interface EntityRef { type: string; label: string }
/** The one entity type Core ships with a meaning of its own: norms and standards a document cites. */
export const STANDARD_ENTITY_TYPE = "standard";
/** Graph node id of an entity; alerts use it to point at the node. */
export const entityNodeId = (e: EntityRef) => `${e.type}:${e.label}`;

export interface Person {
  id: string;
  name: string;
  email: string;
  department: string;
  roles: Role[];
  isActive: boolean;
  lastActiveAt: string | null; // ISO
  hue: number; // stable identity tint
}

export interface DocumentVersion {
  id: string;
  revision: string;
  state: "indexed" | "superseded" | "processing" | "quarantined";
  indexedAt: string;
  pages: number;
}

export interface FieldProvenance {
  field: string;
  value: string;
  source: ProvenanceSource;
  confidence: number; // 0..1
}

export interface Document {
  id: string;
  code: string; // PNT-014
  title: string;
  docType: DocType;
  band: BandKind;
  status: DocStatus;
  department: string;
  entities: EntityRef[]; // standards cited are entities of type "standard"
  attributes: Record<string, string>; // domain-pack fields, validated by the pack's JSON Schema
  ownerId: string | null;
  approvedAt: string | null;
  reviewDueAt: string | null;
  sourceId: string;
  path: string;
  language: string; // "es", "en"… or "" until the server detects it
  currentVersionId: string;
  versions: DocumentVersion[];
  provenance: FieldProvenance[];
  summary: string;
  sample?: string; // public URL of a synthetic PDF, when one exists
}

export interface Citation {
  n: number;
  documentId: string;
  versionId: string;
  page: number;
  section: string;
  quote: string;
  score: number; // rerank 0..1
  bbox?: { x: number; y: number; w: number; h: number }; // PDF points, origin top-left
}

export type Outcome = "answered" | "not_found" | "refused" | "error";

export interface Answer {
  id: string;
  conversationId: string;
  question: string;
  askedById: string;
  askedAt: string;
  outcome: Outcome;
  text: string; // contains [n] markers
  citations: Citation[];
  notes: string[]; // e.g. superseded revisions left out
  nearby: string[]; // document ids, for not_found
  latencyMs: number;
  model: string;
  scope: string | null;
  feedback: "up" | "down" | null;
}

export interface Conversation {
  id: string;
  title: string;
  updatedAt: string;
  answerIds: string[];
}

export interface ConversationSummary extends Conversation {
  askedById: string;
  lastOutcome: Outcome;
  sources: string[]; // labels of cited documents, e.g. "PNT-014 rev.3"
}

export type AskEvent =
  | { type: "meta"; answerId: string; citations: Citation[]; notes: string[] }
  | { type: "token"; t: string }
  | { type: "done"; answer: Answer }
  | { type: "error"; message: string };

export type SourceKind = "folder" | "sharepoint" | "erp_sql" | "upload";

export interface Source {
  id: string;
  name: string;
  kind: SourceKind;
  location: string;
  status: "active" | "paused" | "error";
  lastSyncAt: string | null;
  lastError: string | null;
  pro: boolean;
  /** Who may read the documents of this source; `readers` are person ids and empty when everyone may. */
  access: "everyone" | "restricted";
  readers: string[];
}

/** Open a source to everyone, or restrict it to the people listed. */
export type SourceAccessInput = { access: "everyone" } | { access: "restricted"; readers: string[] };

export interface IngestionRun {
  id: string;
  sourceId: string;
  startedAt: string;
  finishedAt: string | null;
  discovered: number;
  indexed: number;
  skipped: number;
  failed: number;
}

export interface QuarantineItem {
  id: string;
  sourceId: string;
  path: string;
  reason: "encrypted" | "corrupt" | "unsupported" | "too_large" | "hostile";
  detail: string;
  at: string;
}

export type AlertRule =
  | "OBSOLETE_STANDARD" | "CONTRADICTION" | "NO_OWNER" | "ENTITY_NO_DOCS" | "SUSPECT_CONTENT"
  | "REVIEW_OVERDUE" | "OLD_VERSION_CIRCULATING" | "DUPLICATE";
export type Severity = "high" | "medium" | "low";
export type AlertStatus = "open" | "ack" | "resolved" | "dismissed";

export interface Alert {
  id: string;
  rule: AlertRule;
  severity: Severity;
  status: AlertStatus;
  title: string;
  detail: string;
  documentIds: string[];
  entity: EntityRef | null; // subject of ENTITY_NO_DOCS, or the entity two documents disagree about
  assigneeId: string | null;
  firstSeenAt: string;
  lastSeenAt: string;
  evidence: { documentId: string; page: number; quote: string }[];
}

export type NodeKind = "document" | "entity" | "person" | "department";
export interface PasswordChange { currentPassword: string; newPassword: string }

export interface GraphNode { id: string; kind: NodeKind; label: string; ref: string | null; band?: BandKind; entityType?: string }
export interface GraphEdge {
  id: string;
  source: string;
  target: string;
  kind: "references" | "cites_norm" | "mentions" | "owned_by" | "contradicts" | "duplicates" | "belongs_to";
  layer: 1 | 2 | 3 | 4;
  status: "confirmed" | "suggested" | "rejected";
}

export interface AuditEvent {
  id: number;
  at: string;
  actorId: string | null; // null = system
  action: string; // answer.generated, metadata.corrected, alert.dismissed…
  subject: string;
  detail: string;
  hash: string;
  prevHash: string;
}

export interface QuestionLogEntry {
  id: string;
  at: string;
  userId: string;
  topic: string;
  outcome: Outcome;
}

export interface Settings {
  orgName: string;
  chatModel: string;
  embedModel: string;
  rerankModel: string;
  networkIsolated: boolean;
  requireCitation: boolean;
  warnContradictions: boolean;
  notFoundThreshold: number;
  answerLanguage: "user" | "es" | "en";
  retentionDays: number;
}

export interface License {
  plan: "core" | "pro";
  organization: string;
  validUntil: string;
  features: ProFeature[];
}
export type ProFeature = "alerts" | "map" | "sso" | "inherited_acl" | "audit_reports" | "sharepoint" | "erp";

export interface Overview {
  today: { questions: number; answered: number; notFound: number; medianLatencyMs: number };
  week: { day: string; questions: number; notFound: number }[];
  topTopics: { topic: string; count: number }[];
  documents: { indexed: number; byBand: Record<BandKind, number>; processing: number; quarantined: number };
  openAlerts: { high: number; medium: number; low: number };
  finding: string;
}

export interface DocumentFilter {
  q?: string;
  band?: BandKind | "all";
  status?: DocStatus | "all";
  sourceId?: string;
}

/** Alert change; `note` is the reason kept in the audit log (required by the UI when dismissing). */
export type AlertPatch = Partial<Pick<Alert, "status" | "assigneeId">> & { note?: string };
/** Settings an admin can change at run time; models and network isolation are set in the deployment. */
/** The alert fields a patch changes, without the audit-only note. */
export function alertChanges(patch: AlertPatch): Omit<AlertPatch, "note"> {
  const changes = { ...patch };
  delete changes.note; // the note goes to the audit log, not the alert
  return changes;
}
export type SettingsPatch = Partial<Pick<Settings, "orgName" | "notFoundThreshold" | "answerLanguage" | "retentionDays" | "requireCitation" | "warnContradictions">>;

/** Step one of signing in. `needsCode` is false for accounts without a second factor. */
export interface LoginChallenge { challengeId: string; needsCode: boolean }

/** The first administrator and the organization, created once by /setup. */
export interface SetupInput { orgName: string; name: string; email: string; password: string }

export interface FolderCheck { ok: boolean; detail: string }
export interface UploadResult { accepted: number; rejected: { name: string; reason: string }[] }
export interface NewSource { name: string; kind: "folder"; location: string }

export interface AtlasApi {
  setupStatus(): Promise<{ setupDone: boolean }>;
  /** Creates the organization and its administrator. Returns the otpauth URI for the authenticator app. */
  setup(input: SetupInput, key?: string): Promise<{ totpUri: string }>;
  login(email: string, password: string): Promise<LoginChallenge>;
  /** Step two: the second-factor code (omit it when the account has none). Starts the session. */
  verifyLogin(challengeId: string, code?: string): Promise<void>;
  logout(): Promise<void>;
  checkFolder(path: string): Promise<FolderCheck>;
  createSource(input: NewSource, key?: string): Promise<Source>;
  uploadFiles(files: File[], key?: string): Promise<UploadResult>;
  overview(): Promise<Overview>;
  ask(question: string, opts: { conversationId?: string; scope?: string | null }): AsyncIterable<AskEvent>;
  conversations(): Promise<ConversationSummary[]>;
  conversation(id: string): Promise<{ conversation: Conversation; answers: Answer[] }>;
  feedback(answerId: string, value: "up" | "down", key?: string): Promise<void>;
  documents(filter?: DocumentFilter): Promise<Document[]>;
  document(id: string): Promise<Document>;
  related(id: string): Promise<{ document: Document; via: GraphEdge["kind"] }[]>;
  sources(): Promise<Source[]>;
  runs(): Promise<IngestionRun[]>;
  quarantine(): Promise<QuarantineItem[]>;
  retryQuarantine(id: string, key?: string): Promise<void>;
  syncSource(id: string, key?: string): Promise<void>;
  setSourcePaused(id: string, paused: boolean, key?: string): Promise<Source>;
  setSourceAccess(id: string, input: SourceAccessInput, key?: string): Promise<Source>;
  alerts(): Promise<Alert[]>;
  updateAlert(id: string, patch: AlertPatch, key?: string): Promise<Alert>;
  graph(): Promise<{ nodes: GraphNode[]; edges: GraphEdge[] }>;
  audit(): Promise<AuditEvent[]>;
  verifyAudit(): Promise<{ ok: boolean; checked: number; brokenAt: number | null }>;
  people(): Promise<Person[]>;
  updateRoles(id: string, roles: Role[], key?: string): Promise<Person>;
  deleteUser(id: string, key?: string): Promise<void>;
  deleteMe(key?: string): Promise<void>;
  changePassword(input: PasswordChange, key?: string): Promise<void>;
  settings(): Promise<Settings>;
  updateSettings(patch: SettingsPatch, key?: string): Promise<Settings>;
  license(): Promise<License>;
  me(): Promise<Person>;
}

/** Entity nodes other than standards: standards are drawn like documents, the rest as outlined nodes. */
export const isNonStandardEntity = (n: Pick<GraphNode, "kind" | "entityType">) =>
  n.kind === "entity" && n.entityType !== STANDARD_ENTITY_TYPE;
