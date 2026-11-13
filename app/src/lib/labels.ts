import type { FieldProvenance, PersonName, QuarantineItem, Role } from "@/lib/api/types";

/** Textos para personas: ninguna pantalla enseña una clave interna donde hay un texto aquí. Sin JSX. */

/* Roles ------------------------------------------------------------------------------------------ */
export const roleInfo: Record<Role, { label: string; help: string }> = {
  viewer: { label: "Ask", help: "Asks questions and opens the documents they already have access to." },
  editor_metadata: { label: "Edit metadata", help: "Corrects codes, owners and dates. Corrections survive re-indexing." },
  curator: { label: "Curator", help: "Looks after the documentation: works through alerts and rejects wrong links on the map." },
  organizer_approver: { label: "Approve reorganisation (not used)", help: "Atlas does not use this role. Untick it to remove it." },
  admin: { label: "Administrator", help: "Manages sources, people, models and the license." },
  auditor: { label: "Auditor", help: "Reads and exports the audit log. Read-only everywhere else." },
};

export const OFFERED_ROLES: Role[] = ["viewer", "editor_metadata", "curator", "admin", "auditor"];

/** Roles that can be ticked: the offered ones, plus any unused role the person already has (so it can be removed). */
export function rolesToOffer(current: readonly Role[]): Role[] {
  return [...OFFERED_ROLES, ...current.filter((r) => !OFFERED_ROLES.includes(r))];
}

/* Metadata and settings fields ------------------------------------------------------------------ */
export const fieldLabel: Record<string, string> = {
  title: "Title", code: "Code", doc_type: "Type", status: "Status", department: "Department", language: "Language",
  owner_id: "Owner", approved_at: "Approved on", review_due_at: "Review due",
  org_name: "Organization name", not_found_threshold: "Relevance needed to answer", answer_language: "Answer in",
  retention_days: "Keep records for", require_citation: "Always cite a source", warn_contradictions: "Warn about contradictions",
  search_language: "Word search language",
};

export const labelOfField = (key: string): string => fieldLabel[key] ?? key;

export const FORMER_MEMBER = "Former member";

/** The value of a metadata row; the owner is shown by name. */
export function metadataValue(row: FieldProvenance, people: Map<string, PersonName>): string {
  if (row.field !== "owner_id") return row.value;
  return people.get(row.value)?.name ?? FORMER_MEMBER;
}

/* Audit events ----------------------------------------------------------------------------------- */
export const auditActionLabel: Record<string, string> = {
  "answer.generated": "Answer generated", "answer.feedback": "Feedback", "metadata.corrected": "Metadata corrected",
  "user.login": "Signed in", "audit.exported": "Log exported",
  "user.created": "Person added", "user.deleted": "Person removed", "user.login_failed": "Sign-in failed", "user.logout": "Signed out",
  "user.password_changed": "Password changed", "user.password_reset": "Password reset", "user.roles_changed": "Roles changed",
  "user.totp_reset": "Second factor reset", "setup.completed": "Atlas set up", "settings.updated": "Settings changed",
  "alert.updated": "Alert updated", "edge.decided": "Link decided", "license.installed": "License installed",
  "license.rejected": "License refused", "audit.export_verified": "Report verified", "document.deleted": "Document deleted",
  "upload.received": "Files uploaded", "quarantine.retried": "Retry requested", "source.created": "Source added",
  "source.paused": "Source paused", "source.resumed": "Source resumed", "source.removed": "Source removed",
  "source.sync_requested": "Scan requested", "source.access_changed": "Access changed",
};

export const labelOfAction = (action: string): string => auditActionLabel[action] ?? action;

const FIELDS_PREFIX = "fields=";
const ROLES_PREFIX = "roles=";

/** The detail of an audit event; `fields=` and `roles=` lists are shown with their labels, any other detail as it is. */
export function auditDetailText(detail: string): string {
  if (detail.startsWith(FIELDS_PREFIX)) return labelList(detail.slice(FIELDS_PREFIX.length), labelOfField);
  if (detail.startsWith(ROLES_PREFIX)) return labelList(detail.slice(ROLES_PREFIX.length), labelOfRole);
  return detail;
}

const labelOfRole = (key: string): string => roleInfo[key as Role]?.label ?? key;

const labelList = (keys: string, label: (key: string) => string): string => keys.split(",").map(label).join(", ");

/* Quarantine ------------------------------------------------------------------------------------- */
export const QUARANTINE_REASONS = ["encrypted", "corrupt", "unsupported", "too_large", "hostile", "retries_exhausted"] as const satisfies readonly QuarantineItem["reason"][];

export const quarantineAdvice: Record<QuarantineItem["reason"], string> = {
  encrypted: "The file is password-protected. Remove the password and save it again.",
  corrupt: "The file is damaged and cannot be opened. Save or export it again.",
  unsupported: "Atlas does not read this kind of file. Save it as PDF or as an Office document if it should be searchable.",
  too_large: "The file is larger than Atlas accepts. Split it or make it smaller.",
  hostile: "The file looks unsafe and was not opened. Check it with IT before trying again.",
  retries_exhausted: "Failed after several tries. Press Try again once the models are running.",
};
