// A fictional plant ("Planta Norte") used as demo data. Nothing here is a real customer, person or metric.
// Totals shown in the UI are always computed from these rows; reconciliations are asserted at the bottom.
import type {
  Alert, Answer, AuditEvent, BandKind, Conversation, DocType, Document, DocumentVersion,
  EntityRef, FieldProvenance, GraphEdge, GraphNode, IngestionRun, License, Person, QuarantineItem,
  QuestionLogEntry, Settings, Source,
} from "@/lib/api/types";
import { entityNodeId, STANDARD_ENTITY_TYPE as STANDARD } from "@/lib/api/types";

/** The demo snapshot's "now". Fixed so server and client render the same. */
export const NOW = new Date("2026-10-30T08:42:00Z"); // 09:42 in Madrid
const DAY = 86_400_000;
const iso = (d: Date) => d.toISOString();
const ago = (ms: number) => iso(new Date(NOW.getTime() - ms));
const dayAt = (daysAgo: number, h: number, m = 0) => {
  const d = new Date(NOW.getTime() - daysAgo * DAY);
  d.setUTCHours(h - 1, m, 0, 0); // Madrid is UTC+1 at the end of October
  return iso(d);
};

/* People ------------------------------------------------------------------ */
export const people: Person[] = [
  { id: "u-admin", name: "Admin", email: "admin@admin.es", department: "IT", roles: ["admin", "curator"], isActive: true, lastActiveAt: ago(0), hue: 190 },
  { id: "u-lucia", name: "Lucía Marín", email: "lucia.marin@plantanorte.example", department: "Quality", roles: ["curator", "editor_metadata", "organizer_approver"], isActive: true, lastActiveAt: ago(2 * 60_000), hue: 168 },
  { id: "u-javier", name: "Javier García", email: "javier.garcia@plantanorte.example", department: "Quality", roles: ["editor_metadata", "viewer"], isActive: true, lastActiveAt: ago(3 * 3_600_000), hue: 32 },
  { id: "u-ana", name: "Ana Torres", email: "ana.torres@plantanorte.example", department: "IT", roles: ["admin"], isActive: true, lastActiveAt: ago(26 * 60_000), hue: 222 },
  { id: "u-pablo", name: "Pablo Ruiz", email: "pablo.ruiz@plantanorte.example", department: "Maintenance", roles: ["viewer"], isActive: true, lastActiveAt: ago(9 * 60_000), hue: 98 },
  { id: "u-marta", name: "Marta Vidal", email: "marta.vidal@plantanorte.example", department: "Compliance", roles: ["auditor"], isActive: true, lastActiveAt: ago(6 * DAY), hue: 300 },
  { id: "u-diego", name: "Diego Ortega", email: "diego.ortega@plantanorte.example", department: "Production", roles: ["viewer"], isActive: false, lastActiveAt: ago(41 * DAY), hue: 8 },
];
export const ME_ID = "u-admin";

/* Sources ----------------------------------------------------------------- */
export const sources: Source[] = [
  { id: "s-quality", name: "Quality share", kind: "folder", location: "\\\\srv-calidad\\Calidad", status: "active", lastSyncAt: ago(4 * 60_000), lastError: null, pro: false, access: "everyone", readers: [] },
  { id: "s-maint", name: "Maintenance manuals", kind: "folder", location: "\\\\srv-mant\\Manuales", status: "active", lastSyncAt: ago(11 * 60_000), lastError: null, pro: false, access: "everyone", readers: [] },
  { id: "s-norms", name: "Norms library", kind: "folder", location: "\\\\srv-calidad\\Normas", status: "active", lastSyncAt: ago(52 * 60_000), lastError: null, pro: false, access: "everyone", readers: [] },
  { id: "s-sharepoint", name: "Production SharePoint", kind: "sharepoint", location: "plantanorte.sharepoint.com/sites/produccion", status: "error", lastSyncAt: ago(3 * 3_600_000), lastError: "Microsoft Graph throttled the sync (429). Atlas retries at 09:46.", pro: true, access: "everyone", readers: [] },
  { id: "s-erp", name: "ERP machine master", kind: "erp_sql", location: "erp-db01, view v_maquinas", status: "active", lastSyncAt: ago(38 * 60_000), lastError: null, pro: true, access: "everyone", readers: [] },
  { id: "s-upload", name: "Manual uploads", kind: "upload", location: "Files added in Atlas", status: "paused", lastSyncAt: ago(9 * DAY), lastError: null, pro: false, access: "everyone", readers: [] },
];

/* Documents --------------------------------------------------------------- */
const bandOf = (t: DocType): BandKind =>
  t === "norm" ? "norm" : t === "erp_record" ? "erp"
  : t === "procedure" || t === "work_instruction" || t === "form" ? "procedure" : "manual";

type DocSeed = {
  id: string; code: string; title: string; docType: DocType; status?: Document["status"];
  department: string; line?: string | null; machines?: string[]; norms?: string[];
  ownerId?: string | null; approvedAt?: string | null; reviewDueAt?: string | null;
  sourceId: string; path: string; revisions: string[]; pages: number; summary: string;
  language?: "es" | "en"; sample?: string;
};

const seeds: DocSeed[] = [
  { id: "d-pnt014", code: "PNT-014", title: "Spindle assembly", docType: "procedure", department: "Quality", line: "Line 2", machines: ["CNC-07", "CNC-08"], norms: ["ISO 9001:2015"], ownerId: "u-javier", approvedAt: "2025-03-12", reviewDueAt: "2026-03-12", sourceId: "s-quality", path: "Calidad/PNT/Línea 2/PNT-014_v3.pdf", revisions: ["1", "2", "3"], pages: 9, summary: "Steps to fit and torque the spindle on CNC machining centres.", sample: "/samples/PNT-014-rev3.pdf" },
  { id: "d-pnt014-old", code: "PNT-014", title: "Spindle assembly (old copy)", docType: "procedure", status: "obsolete", department: "Quality", line: "Line 2", machines: ["CNC-07"], norms: ["ISO 9001:2015"], ownerId: "u-javier", approvedAt: "2023-06-02", reviewDueAt: null, sourceId: "s-quality", path: "Calidad/Línea 2/Antiguos/PNT-014_v2 copia final.pdf", revisions: ["2"], pages: 8, summary: "Superseded revision still stored in another folder." },
  { id: "d-pnt009", code: "PNT-009", title: "Spindle torque check", docType: "procedure", department: "Quality", line: "Line 2", machines: ["CNC-07"], norms: [], ownerId: "u-javier", approvedAt: "2025-03-10", reviewDueAt: "2027-03-10", sourceId: "s-quality", path: "Calidad/PNT/Línea 2/PNT-009_v2.pdf", revisions: ["1", "2"], pages: 4, summary: "Periodic verification of spindle nut torque." },
  { id: "d-pnt021", code: "PNT-021", title: "Lockout–tagout before maintenance", docType: "procedure", department: "Maintenance", line: null, machines: ["CNC-07", "CNC-08", "CNC-12", "PRS-03", "ROB-02"], norms: ["ISO 45001:2018", "UNE-EN ISO 13849-1:2016"], ownerId: "u-lucia", approvedAt: "2024-11-20", reviewDueAt: "2026-11-20", sourceId: "s-quality", path: "Calidad/PNT/PNT-021_v6.pdf", revisions: ["4", "5", "6"], pages: 12, summary: "Energy isolation, padlocks and tags before any intervention." },
  { id: "d-loto-copy", code: "", title: "Procedimiento LOTO (copia).docx", docType: "procedure", status: "unknown", department: "Maintenance", line: null, machines: ["CNC-07"], norms: [], ownerId: null, approvedAt: null, reviewDueAt: null, sourceId: "s-maint", path: "Manuales/Varios/Procedimiento LOTO (copia).docx", revisions: ["n/a"], pages: 11, summary: "Near-identical copy of PNT-021 rev.5.", language: "es" },
  { id: "d-pnt031", code: "PNT-031", title: "Coolant change on machining centres", docType: "procedure", department: "Maintenance", line: "Line 2", machines: ["CNC-07", "CNC-08", "CNC-12"], norms: [], ownerId: "u-lucia", approvedAt: "2026-01-15", reviewDueAt: "2027-01-15", sourceId: "s-quality", path: "Calidad/PNT/Línea 2/PNT-031_v4.pdf", revisions: ["3", "4"], pages: 6, summary: "Draining, cleaning and refilling coolant; target concentration." },
  { id: "d-pnt005", code: "PNT-005", title: "Calibration of measuring equipment", docType: "procedure", department: "Quality", line: null, machines: [], norms: ["ISO 9001:2015"], ownerId: "u-javier", approvedAt: "2024-02-01", reviewDueAt: "2026-02-01", sourceId: "s-quality", path: "Calidad/PNT/PNT-005_v7.pdf", revisions: ["7"], pages: 14, summary: "Calibration intervals and records, including torque wrench TW-03." },
  { id: "d-pnt003", code: "PNT-003", title: "Document control", docType: "procedure", department: "Quality", line: null, machines: [], norms: ["ISO 9001:2015"], ownerId: "u-lucia", approvedAt: "2025-09-01", reviewDueAt: "2027-09-01", sourceId: "s-quality", path: "Calidad/PNT/PNT-003_v5.pdf", revisions: ["5"], pages: 10, summary: "How procedures are coded, approved, revised and withdrawn." },
  { id: "d-pnt040", code: "PNT-040", title: "Injection mould change", docType: "procedure", department: "Production", line: "Line 3", machines: ["INY-04"], norms: [], ownerId: "u-diego", approvedAt: "2025-05-20", reviewDueAt: "2027-05-20", sourceId: "s-quality", path: "Calidad/PNT/Línea 3/PNT-040_v2.pdf", revisions: ["2"], pages: 7, summary: "Changeover sequence and safety checks for INY-04." },
  { id: "d-pnt050", code: "PNT-050", title: "Waste segregation", docType: "procedure", department: "Environment", line: null, machines: [], norms: ["ISO 14001:2015"], ownerId: "u-lucia", approvedAt: "2025-10-02", reviewDueAt: "2027-10-02", sourceId: "s-quality", path: "Calidad/PNT/PNT-050_v3.pdf", revisions: ["3"], pages: 5, summary: "Containers, labelling and collection of plant waste." },
  { id: "d-it044", code: "IT-044", title: "Daily press check", docType: "work_instruction", department: "Production", line: "Line 1", machines: ["PRS-03"], norms: [], ownerId: null, approvedAt: "2024-07-08", reviewDueAt: "2026-07-08", sourceId: "s-quality", path: "Calidad/IT/Línea 1/IT-044.pdf", revisions: ["2"], pages: 2, summary: "Start-of-shift checks on press PRS-03." },
  { id: "d-it052", code: "IT-052", title: "Oven start-up sequence", docType: "work_instruction", department: "Production", line: "Line 3", machines: ["HRN-01"], norms: [], ownerId: "u-diego", approvedAt: "2025-12-01", reviewDueAt: "2026-12-01", sourceId: "s-quality", path: "Calidad/IT/Línea 3/IT-052.pdf", revisions: ["1"], pages: 3, summary: "Pre-heat ramp and interlock checks for HRN-01." },
  { id: "d-it060", code: "IT-060", title: "Welding cell daily check", docType: "work_instruction", department: "Production", line: "Line 1", machines: ["SOL-02"], norms: [], ownerId: "u-pablo", approvedAt: "2026-02-14", reviewDueAt: "2027-02-14", sourceId: "s-quality", path: "Calidad/IT/Línea 1/IT-060.pdf", revisions: ["1"], pages: 2, summary: "Torch, wire feed and extraction checks." },
  { id: "d-f007", code: "F-007", title: "Maintenance intervention record", docType: "form", department: "Maintenance", line: null, machines: [], norms: [], ownerId: "u-pablo", approvedAt: "2025-01-10", reviewDueAt: "2027-01-10", sourceId: "s-quality", path: "Calidad/Formularios/F-007.xlsx", revisions: ["4"], pages: 1, summary: "Form filled after every corrective intervention." },
  { id: "d-man-cnc07", code: "MAN-CNC07", title: "CNC-07 maintenance manual", docType: "manual", department: "Maintenance", line: "Line 2", machines: ["CNC-07"], norms: ["UNE-EN 60204-1:2018"], ownerId: "u-pablo", approvedAt: null, reviewDueAt: null, sourceId: "s-maint", path: "Manuales/CNC-07/Maintenance_manual_revB.pdf", revisions: ["A", "B"], pages: 240, summary: "Supplier maintenance manual for the CNC-07 machining centre.", language: "en" },
  { id: "d-man-prs03", code: "MAN-PRS03", title: "PRS-03 press operating manual", docType: "manual", department: "Maintenance", line: "Line 1", machines: ["PRS-03"], norms: [], ownerId: "u-pablo", approvedAt: null, reviewDueAt: null, sourceId: "s-maint", path: "Manuales/PRS-03/Manual_operacion.pdf", revisions: ["3"], pages: 186, summary: "Operation and daily maintenance of hydraulic press PRS-03." },
  { id: "d-man-rob02", code: "MAN-ROB02", title: "ROB-02 robot cell manual", docType: "manual", department: "Maintenance", line: "Line 1", machines: ["ROB-02"], norms: ["UNE-EN ISO 13849-1:2016"], ownerId: "u-pablo", approvedAt: null, reviewDueAt: null, sourceId: "s-maint", path: "Manuales/ROB-02/Robot_cell_manual.pdf", revisions: ["2"], pages: 132, summary: "Safety, recovery and reset procedures for the robot cell.", language: "en" },
  { id: "d-man-cmp01", code: "MAN-CMP01", title: "CMP-01 compressor manual", docType: "manual", department: "Maintenance", line: null, machines: ["CMP-01"], norms: [], ownerId: null, approvedAt: null, reviewDueAt: null, sourceId: "s-maint", path: "Manuales/CMP-01/Compresor.pdf", revisions: ["1"], pages: 64, summary: "Compressor service intervals." },
  { id: "d-fds112", code: "FDS-112", title: "Coolant safety data sheet", docType: "safety_sheet", department: "Environment", line: null, machines: [], norms: [], ownerId: "u-lucia", approvedAt: "2025-04-03", reviewDueAt: "2027-04-03", sourceId: "s-quality", path: "Calidad/FDS/FDS-112.pdf", revisions: ["3"], pages: 11, summary: "Hazards, PPE and first aid for the water-miscible coolant." },
  { id: "d-drw-sp", code: "DRW-CNC07-SP", title: "Spindle assembly drawing", docType: "drawing", department: "Engineering", line: "Line 2", machines: ["CNC-07"], norms: [], ownerId: "u-javier", approvedAt: "2022-11-30", reviewDueAt: null, sourceId: "s-maint", path: "Manuales/CNC-07/Planos/Husillo.pdf", revisions: ["C"], pages: 2, summary: "Exploded drawing of the spindle with part numbers." },
  { id: "d-rpt-09", code: "RPT-2026-09", title: "Monthly maintenance report, September", docType: "report", department: "Maintenance", line: null, machines: ["CNC-07", "PRS-03", "HRN-01"], norms: [], ownerId: "u-pablo", approvedAt: "2026-10-05", reviewDueAt: null, sourceId: "s-sharepoint", path: "sites/produccion/Informes/RPT-2026-09.docx", revisions: ["1"], pages: 6, summary: "Interventions, downtime and open actions for September." },
  { id: "d-mail-torque", code: "", title: "Quality notice: new torque values", docType: "email", department: "Quality", line: "Line 2", machines: ["CNC-07", "CNC-08"], norms: [], ownerId: "u-javier", approvedAt: null, reviewDueAt: null, sourceId: "s-quality", path: "Calidad/Avisos/2025-03-12 Nuevos pares.msg", revisions: ["n/a"], pages: 1, summary: "Announcement of the March 2025 torque update." },
  { id: "d-iso9001", code: "ISO 9001:2015", title: "Quality management systems: Requirements", docType: "norm", department: "Quality", line: null, machines: [], norms: [], ownerId: "u-lucia", approvedAt: "2015-09-15", reviewDueAt: null, sourceId: "s-norms", path: "Normas/ISO 9001-2015.pdf", revisions: ["2015"], pages: 29, summary: "Requirements for a quality management system.", language: "en" },
  { id: "d-iso45001", code: "ISO 45001:2018", title: "Occupational health and safety management systems", docType: "norm", department: "Quality", line: null, machines: [], norms: [], ownerId: "u-lucia", approvedAt: "2018-03-12", reviewDueAt: null, sourceId: "s-norms", path: "Normas/ISO 45001-2018.pdf", revisions: ["2018"], pages: 41, summary: "Requirements for OH&S management systems.", language: "en" },
  { id: "d-iso14001", code: "ISO 14001:2015", title: "Environmental management systems", docType: "norm", department: "Environment", line: null, machines: [], norms: [], ownerId: "u-lucia", approvedAt: "2015-09-15", reviewDueAt: null, sourceId: "s-norms", path: "Normas/ISO 14001-2015.pdf", revisions: ["2015"], pages: 35, summary: "Requirements for environmental management systems.", language: "en" },
  { id: "d-iso13849", code: "UNE-EN ISO 13849-1:2016", title: "Safety of machinery: Safety-related parts of control systems", docType: "norm", status: "obsolete", department: "Engineering", line: null, machines: [], norms: [], ownerId: "u-lucia", approvedAt: "2016-01-01", reviewDueAt: null, sourceId: "s-norms", path: "Normas/UNE-EN ISO 13849-1-2016.pdf", revisions: ["2016"], pages: 98, summary: "Withdrawn; replaced by the 2023 edition." },
  { id: "d-en60204", code: "UNE-EN 60204-1:2018", title: "Electrical equipment of machines", docType: "norm", department: "Engineering", line: null, machines: [], norms: [], ownerId: "u-lucia", approvedAt: "2018-06-01", reviewDueAt: null, sourceId: "s-norms", path: "Normas/UNE-EN 60204-1-2018.pdf", revisions: ["2018"], pages: 112, summary: "General requirements for electrical equipment of machines." },
  { id: "d-erp-cnc07", code: "ERP CNC-07", title: "Machine record CNC-07", docType: "erp_record", department: "Maintenance", line: "Line 2", machines: ["CNC-07"], norms: [], ownerId: "u-pablo", approvedAt: null, reviewDueAt: null, sourceId: "s-erp", path: "v_maquinas, CNC-07", revisions: ["live"], pages: 1, summary: "Asset data, location and responsible person from the ERP." },
  { id: "d-erp-7731", code: "ERP 7731-H", title: "Spare part 7731-H spindle nut", docType: "erp_record", department: "Maintenance", line: null, machines: ["CNC-07", "CNC-08"], norms: [], ownerId: null, approvedAt: null, reviewDueAt: null, sourceId: "s-erp", path: "v_recambios, 7731-H", revisions: ["live"], pages: 1, summary: "Stock 6 units, supplier lead time 12 days." },
];

const provenanceFor = (s: DocSeed): FieldProvenance[] => {
  const rows: FieldProvenance[] = [];
  if (s.code) rows.push({ field: "Code", value: s.code, source: s.docType === "norm" ? "path" : "titleblock", confidence: 0.99 });
  rows.push({ field: "Revision", value: s.revisions[s.revisions.length - 1], source: s.docType === "manual" ? "path" : "titleblock", confidence: 0.97 });
  rows.push({ field: "Department", value: s.department, source: "path", confidence: 0.95 });
  if (s.line) rows.push({ field: "Line", value: s.line, source: s.machines?.length ? "erp" : "path", confidence: 0.9 });
  if (s.approvedAt) rows.push({ field: "Approved", value: s.approvedAt, source: "titleblock", confidence: 0.93 });
  rows.push({ field: "Summary", value: s.summary, source: "llm", confidence: 0.78 });
  if (s.id === "d-pnt014") rows.push({ field: "Owner", value: "Javier García", source: "manual", confidence: 1 });
  return rows;
};

/* The demo tenant runs a Viper-style pack: machines are a pack entity type; standards are a Core one. */
const MACHINE = "machine";
const machineRef = (label: string): EntityRef => ({ type: MACHINE, label });

const entitiesOf = (s: DocSeed): EntityRef[] => [
  ...(s.machines ?? []).map(machineRef),
  ...(s.norms ?? []).map((label) => ({ type: STANDARD, label })),
];

const attributesOf = (s: DocSeed): Record<string, string> => (s.line ? { area_line: s.line } : {});

export const documents: Document[] = seeds.map((s, i) => {
  const versions: DocumentVersion[] = s.revisions.map((rev, j) => ({
    id: `${s.id}-v${j + 1}`,
    revision: rev,
    state: j === s.revisions.length - 1 ? "indexed" : "superseded",
    indexedAt: ago((s.revisions.length - j) * 40 * DAY + i * 3_600_000),
    pages: s.pages,
  }));
  return {
    id: s.id, code: s.code, title: s.title, docType: s.docType, band: bandOf(s.docType),
    status: s.status ?? "current", department: s.department,
    entities: entitiesOf(s), attributes: attributesOf(s), ownerId: s.ownerId ?? null,
    approvedAt: s.approvedAt ?? null, reviewDueAt: s.reviewDueAt ?? null, sourceId: s.sourceId,
    path: s.path, language: s.language ?? "es", currentVersionId: versions[versions.length - 1].id,
    versions, provenance: provenanceFor(s), summary: s.summary, sample: s.sample,
  };
});

export const docById = (id: string) => documents.find((d) => d.id === id);
const versionOf = (id: string) => docById(id)!.currentVersionId;

/** Machines known to the ERP. LAS-05 has no document mentioning it. */
const machines = ["CNC-07", "CNC-08", "CNC-12", "PRS-03", "ROB-02", "HRN-01", "INY-04", "SOL-02", "CMP-01", "LAS-05"];

/* Ingestion ----------------------------------------------------------------- */
export const runs: IngestionRun[] = [
  { id: "r-118", sourceId: "s-quality", startedAt: ago(4 * 60_000 + 38_000), finishedAt: ago(4 * 60_000), discovered: 3, indexed: 2, skipped: 1, failed: 0 },
  { id: "r-117", sourceId: "s-maint", startedAt: ago(11 * 60_000 + 95_000), finishedAt: ago(11 * 60_000), discovered: 5, indexed: 3, skipped: 1, failed: 1 },
  { id: "r-116", sourceId: "s-erp", startedAt: ago(38 * 60_000 + 21_000), finishedAt: ago(38 * 60_000), discovered: 2, indexed: 2, skipped: 0, failed: 0 },
  { id: "r-115", sourceId: "s-norms", startedAt: ago(52 * 60_000 + 12_000), finishedAt: ago(52 * 60_000), discovered: 0, indexed: 0, skipped: 0, failed: 0 },
  { id: "r-114", sourceId: "s-sharepoint", startedAt: ago(3 * 3_600_000 + 240_000), finishedAt: ago(3 * 3_600_000), discovered: 14, indexed: 9, skipped: 0, failed: 5 },
  { id: "r-113", sourceId: "s-quality", startedAt: ago(64 * 60_000 + 40_000), finishedAt: ago(64 * 60_000), discovered: 1, indexed: 1, skipped: 0, failed: 0 },
];

export const quarantine: QuarantineItem[] = [
  { id: "q-1", sourceId: "s-maint", path: "Manuales/Proveedores/Hidraulica_2019.pdf", reason: "encrypted", detail: "The PDF is password-protected. Ask the supplier for an open copy or upload it decrypted.", at: ago(11 * 60_000) },
  { id: "q-2", sourceId: "s-quality", path: "Calidad/Registros/F-007_2021.xls", reason: "unsupported", detail: "Excel 97 workbooks with macros are not parsed. Save as .xlsx.", at: ago(4 * DAY) },
  { id: "q-3", sourceId: "s-maint", path: "Manuales/ROB-02/Esquemas.dwg", reason: "unsupported", detail: "CAD drawings (.dwg) are not indexed. Export the sheets to PDF.", at: ago(12 * DAY) },
];

/* Conversations and answers ---------------------------------------------------- */
export const answers: Answer[] = [
  {
    id: "a-torque", conversationId: "c-torque", askedById: "u-lucia", askedAt: ago(0), scope: "Line 2",
    question: "What's the tightening torque for the spindle on CNC-07?",
    outcome: "answered", model: "qwen-14b-instruct", latencyMs: 4120, feedback: null,
    text: "Tighten the spindle nut to 40 Nm [1], in two passes in a cross pattern: first to 20 Nm, then to 40 Nm [1]. Use the calibrated torque wrench TW-03.\n\nThe CNC-07 maintenance manual gives the same value and adds that the torque must be checked again after 8 hours of running [2].",
    notes: ["PNT-009 rev.1 gave 35 Nm, but rev.2 replaced it in March 2025, so I left it out."],
    nearby: [],
    citations: [
      { n: 1, documentId: "d-pnt014", versionId: versionOf("d-pnt014"), page: 4, section: "4.2 Spindle nut", quote: "Apretar la tuerca del husillo a 40 Nm en dos pasadas en cruz (20 Nm / 40 Nm) con la llave dinamométrica TW-03.", score: 0.91, bbox: { x: 66, y: 184, w: 470, h: 40 } },
      { n: 2, documentId: "d-man-cnc07", versionId: versionOf("d-man-cnc07"), page: 112, section: "7.3 Spindle service", quote: "Re-check spindle nut torque (40 Nm) after 8 operating hours.", score: 0.84 },
    ],
  },
  {
    id: "a-loto", conversationId: "c-loto", askedById: "u-pablo", askedAt: ago(DAY + 3 * 3_600_000), scope: null,
    question: "What do I need to lock out before opening the CNC-08 guard?",
    outcome: "answered", model: "qwen-14b-instruct", latencyMs: 3870, feedback: "up",
    text: "Isolate three energy sources before opening the guard: the main electrical switch, the pneumatic supply and the coolant pump [1]. Fit your personal padlock and tag on each, then try to start the machine to confirm it is isolated [1].\n\nThe procedure cites UNE-EN ISO 13849-1:2016, which has been withdrawn, so check with Quality before relying on the safety category it mentions [2].",
    notes: [],
    nearby: [],
    citations: [
      { n: 1, documentId: "d-pnt021", versionId: versionOf("d-pnt021"), page: 5, section: "5.1 Energy isolation", quote: "Aislar alimentación eléctrica principal, neumática y bomba de refrigerante; colocar candado y tarjeta personal en cada punto; verificar ausencia de energía intentando el arranque.", score: 0.88 },
      { n: 2, documentId: "d-iso13849", versionId: versionOf("d-iso13849"), page: 1, section: "Scope", quote: "This edition has been withdrawn and replaced by UNE-EN ISO 13849-1:2023.", score: 0.52 },
    ],
  },
  {
    id: "a-coolant", conversationId: "c-coolant", askedById: "u-pablo", askedAt: ago(2 * DAY + 5 * 3_600_000), scope: "Line 2",
    question: "What coolant concentration should CNC-07 run at?",
    outcome: "answered", model: "qwen-14b-instruct", latencyMs: 4510, feedback: "down",
    text: "Your documents disagree. The plant procedure sets the concentration at 5 % [1], while the CNC-07 manual recommends 8 % for steel machining [2].\n\nQuality has an open alert about this contradiction; until it is resolved, follow the plant procedure [1] and record the reading on F-007 [3].",
    notes: ["Contradiction flagged: PNT-031 rev.4 and MAN-CNC07 rev.B."],
    nearby: [],
    citations: [
      { n: 1, documentId: "d-pnt031", versionId: versionOf("d-pnt031"), page: 3, section: "3.4 Concentration", quote: "Concentración objetivo del refrigerante: 5 % (refractómetro, lectura diaria).", score: 0.86 },
      { n: 2, documentId: "d-man-cnc07", versionId: versionOf("d-man-cnc07"), page: 88, section: "5.2 Coolant", quote: "Recommended emulsion concentration for steel: 8 %.", score: 0.81 },
      { n: 3, documentId: "d-f007", versionId: versionOf("d-f007"), page: 1, section: "Readings", quote: "Lectura refractómetro (%): ____", score: 0.44 },
    ],
  },
  {
    id: "a-crane", conversationId: "c-crane", askedById: "u-diego", askedAt: ago(3 * DAY + 2 * 3_600_000), scope: "Line 3",
    question: "What's the maximum load for the overhead crane in Line 3?",
    outcome: "not_found", model: "qwen-14b-instruct", latencyMs: 1980, feedback: null,
    text: "I couldn't find the overhead crane's rated load in your documents. None of the indexed manuals or procedures mention a crane in Line 3.",
    notes: [], citations: [],
    nearby: ["d-pnt040", "d-it052", "d-erp-cnc07"],
  },
];

export const conversations: Conversation[] = [
  { id: "c-torque", title: "Spindle torque on CNC-07", updatedAt: ago(0), answerIds: ["a-torque"] },
  { id: "c-loto", title: "Lockout before opening CNC-08", updatedAt: ago(DAY + 3 * 3_600_000), answerIds: ["a-loto"] },
  { id: "c-coolant", title: "Coolant concentration CNC-07", updatedAt: ago(2 * DAY + 5 * 3_600_000), answerIds: ["a-coolant"] },
  { id: "c-crane", title: "Overhead crane load, Line 3", updatedAt: ago(3 * DAY + 2 * 3_600_000), answerIds: ["a-crane"] },
];

/* Alerts --------------------------------------------------------------------- */
export const alerts: Alert[] = [
  { id: "al-1", rule: "CONTRADICTION", severity: "high", status: "open", title: "Coolant concentration differs: 5 % vs 8 %", detail: "PNT-031 rev.4 and the CNC-07 manual give different target concentrations for the same machine.", documentIds: ["d-pnt031", "d-man-cnc07"], entity: machineRef("CNC-07"), assigneeId: "u-lucia", firstSeenAt: ago(9 * DAY), lastSeenAt: ago(4 * 60_000),
    evidence: [{ documentId: "d-pnt031", page: 3, quote: "Concentración objetivo del refrigerante: 5 %." }, { documentId: "d-man-cnc07", page: 88, quote: "Recommended emulsion concentration for steel: 8 %." }] },
  { id: "al-2", rule: "OBSOLETE_STANDARD", severity: "high", status: "open", title: "PNT-021 cites a withdrawn norm", detail: "UNE-EN ISO 13849-1:2016 was replaced by the 2023 edition. PNT-021 rev.6 still refers to it in section 2.", documentIds: ["d-pnt021", "d-iso13849"], entity: null, assigneeId: null, firstSeenAt: ago(21 * DAY), lastSeenAt: ago(4 * 60_000),
    evidence: [{ documentId: "d-pnt021", page: 2, quote: "Categoría de seguridad según UNE-EN ISO 13849-1:2016." }] },
  { id: "al-3", rule: "REVIEW_OVERDUE", severity: "medium", status: "open", title: "PNT-014 review is 232 days overdue", detail: "Review was due on 12 Mar 2026. The procedure is still the current revision and is cited in answers every day.", documentIds: ["d-pnt014"], entity: machineRef("CNC-07"), assigneeId: "u-javier", firstSeenAt: ago(232 * DAY), lastSeenAt: ago(4 * 60_000), evidence: [] },
  { id: "al-4", rule: "NO_OWNER", severity: "medium", status: "open", title: "IT-044 has no owner", detail: "Neither the title block nor the ERP names a responsible person for the daily press check.", documentIds: ["d-it044"], entity: machineRef("PRS-03"), assigneeId: null, firstSeenAt: ago(17 * DAY), lastSeenAt: ago(4 * 60_000), evidence: [] },
  { id: "al-5", rule: "ENTITY_NO_DOCS", severity: "medium", status: "ack", title: "LAS-05 has no current documents", detail: "The laser cutter is active in the ERP (Line 3) but no indexed manual, procedure or instruction mentions it.", documentIds: [], entity: machineRef("LAS-05"), assigneeId: "u-pablo", firstSeenAt: ago(30 * DAY), lastSeenAt: ago(38 * 60_000), evidence: [] },
  { id: "al-6", rule: "OLD_VERSION_CIRCULATING", severity: "medium", status: "open", title: "PNT-014 rev.2 still in another folder", detail: "A superseded copy sits in Calidad/Línea 2/Antiguos and is readable by Line 2 technicians.", documentIds: ["d-pnt014-old", "d-pnt014"], entity: machineRef("CNC-07"), assigneeId: null, firstSeenAt: ago(5 * DAY), lastSeenAt: ago(4 * 60_000), evidence: [] },
  { id: "al-7", rule: "DUPLICATE", severity: "low", status: "open", title: "Near-duplicate of PNT-021", detail: "“Procedimiento LOTO (copia).docx” matches PNT-021 rev.5 at 96 % similarity.", documentIds: ["d-loto-copy", "d-pnt021"], entity: null, assigneeId: null, firstSeenAt: ago(12 * DAY), lastSeenAt: ago(11 * 60_000), evidence: [] },
  { id: "al-8", rule: "NO_OWNER", severity: "medium", status: "resolved", title: "IT-060 had no owner", detail: "Pablo Ruiz was set as owner on the title block.", documentIds: ["d-it060"], entity: machineRef("SOL-02"), assigneeId: "u-pablo", firstSeenAt: ago(40 * DAY), lastSeenAt: ago(19 * DAY), evidence: [] },
  { id: "al-9", rule: "REVIEW_OVERDUE", severity: "medium", status: "dismissed", title: "PNT-005 review overdue", detail: "Dismissed: the calibration procedure is being rewritten for rev.8 (due December).", documentIds: ["d-pnt005"], entity: null, assigneeId: "u-javier", firstSeenAt: ago(271 * DAY), lastSeenAt: ago(14 * DAY), evidence: [] },
];

/* Question log (deterministic) ---------------------------------------------------- */
const TOPICS = ["Spindle torque", "Lockout procedure", "Coolant concentration", "Press daily check", "Oven start-up", "Calibration", "Waste segregation", "Robot cell reset", "Spare parts", "ISO 9001 audit"];
const WEIGHTS = [9, 7, 6, 5, 4, 3, 2, 2, 2, 1];
function rng(seed: number) {
  return () => ((seed = (seed * 1_664_525 + 1_013_904_223) >>> 0) / 2 ** 32);
}
const pickTopic = (r: () => number) => {
  const total = WEIGHTS.reduce((a, b) => a + b, 0);
  let x = r() * total;
  for (let i = 0; i < TOPICS.length; i++) if ((x -= WEIGHTS[i]) < 0) return TOPICS[i];
  return TOPICS[0];
};
export const questionLog: QuestionLogEntry[] = (() => {
  const r = rng(7);
  const askers = ["u-pablo", "u-pablo", "u-diego", "u-lucia", "u-javier"];
  const out: QuestionLogEntry[] = [];
  for (let d = 29; d >= 0; d--) {
    const day = new Date(NOW.getTime() - d * DAY);
    const weekday = day.getUTCDay();
    const weekend = weekday === 0 || weekday === 6;
    let n = weekend ? Math.floor(r() * 3) : 18 + Math.floor(r() * 17);
    if (d === 0) n = 14; // the morning so far
    for (let i = 0; i < n; i++) {
      // UTC hours 5..16 (06:00–17:00 in Madrid); this morning stops before 08:42 UTC.
      const hour = d === 0 ? 5 + Math.floor((i / n) * 3) : 5 + Math.floor(r() * 12);
      out.push({
        id: `ql-${d}-${i}`,
        at: dayAt(d, hour + 1, Math.floor(r() * 60)),
        userId: askers[Math.floor(r() * askers.length)],
        topic: pickTopic(r),
        outcome: r() < 0.08 ? "not_found" : "answered",
      });
    }
  }
  return out;
})();

/* Settings and license -------------------------------------------------------------- */
export const settings: Settings = {
  orgName: "Planta Norte",
  chatModel: "qwen-14b-instruct (vLLM, local)",
  embedModel: "bge-m3",
  rerankModel: "bge-reranker-v2-m3",
  networkIsolated: true,
  requireCitation: true,
  warnContradictions: true,
  notFoundThreshold: 0.35,
  answerLanguage: "user",
  retentionDays: 730,
};

export const license: License = {
  plan: "pro",
  organization: "Planta Norte",
  validUntil: "2027-06-30",
  features: ["alerts", "map", "sso", "inherited_acl", "audit_reports", "sharepoint", "erp"],
};

/* Graph ------------------------------------------------------------------------------- */
/** Graph layers from docs/ATLAS_DESIGN.md §9.1. */
const LAYER = { explicit: 1, entities: 2, similarity: 3, llmJudge: 4 } as const;

const entityNode = (e: EntityRef, ref: string | null): GraphNode =>
  ({ id: entityNodeId(e), kind: "entity", entityType: e.type, label: e.label, ref, band: e.type === STANDARD ? "norm" : undefined });

const entityEdge = (d: Document, e: EntityRef): GraphEdge => {
  const cites = e.type === STANDARD;
  return { id: `e:${d.id}:${e.label}`, source: `d:${d.id}`, target: entityNodeId(e), kind: cites ? "cites_norm" : "mentions", layer: cites ? LAYER.explicit : LAYER.entities, status: "confirmed" };
};

const edgesOfDocument = (d: Document): GraphEdge[] => [
  { id: `e:${d.id}:dep`, source: `d:${d.id}`, target: `dep:${d.department}`, kind: "belongs_to", layer: LAYER.explicit, status: "confirmed" },
  ...d.entities.map((e) => entityEdge(d, e)),
];

const curatedEdges: GraphEdge[] = [
  { id: "e:ref:014-005", source: "d:d-pnt014", target: "d:d-pnt005", kind: "references", layer: LAYER.explicit, status: "confirmed" },
  { id: "e:ref:014-009", source: "d:d-pnt014", target: "d:d-pnt009", kind: "references", layer: LAYER.explicit, status: "confirmed" },
  { id: "e:ref:031-man", source: "d:d-pnt031", target: "d:d-man-cnc07", kind: "references", layer: LAYER.explicit, status: "confirmed" },
  { id: "e:contra", source: "d:d-pnt031", target: "d:d-man-cnc07", kind: "contradicts", layer: LAYER.llmJudge, status: "suggested" },
  { id: "e:dup", source: "d:d-loto-copy", target: "d:d-pnt021", kind: "duplicates", layer: LAYER.similarity, status: "suggested" },
];

const nodeOfDocument = (d: Document): GraphNode =>
  d.docType === "norm"
    ? entityNode({ type: STANDARD, label: d.code }, d.id)
    : { id: `d:${d.id}`, kind: "document", label: d.code || d.title, ref: d.id, band: d.band };

export function buildGraph(): { nodes: GraphNode[]; edges: GraphEdge[] } {
  const live = documents.filter((d) => d.status !== "obsolete" || d.docType === "norm");
  const departments = [...new Set(live.map((d) => d.department))];
  const candidates: GraphNode[] = [
    ...departments.map((dep): GraphNode => ({ id: `dep:${dep}`, kind: "department", label: dep, ref: null })),
    ...machines.map((m) => entityNode(machineRef(m), m)),
    ...live.map(nodeOfDocument),
  ];
  const nodes = [...new Map(candidates.map((n) => [n.id, n])).values()];
  const documentEdges = live.filter((d) => d.docType !== "norm").flatMap(edgesOfDocument);
  return { nodes, edges: [...documentEdges, ...curatedEdges] };
}

/* Audit log with a verifiable hash chain (mock hash: FNV-1a widened to 64 hex chars) ---------- */
export function hash64(input: string) {
  let out = "";
  for (let k = 0; k < 8; k++) {
    let h = 0x811c9dc5 ^ k;
    for (let i = 0; i < input.length; i++) { h ^= input.charCodeAt(i); h = Math.imul(h, 0x01000193) >>> 0; }
    out += h.toString(16).padStart(8, "0");
  }
  return out;
}
const rawAudit: Omit<AuditEvent, "id" | "hash" | "prevHash">[] = [
  { at: ago(9 * DAY), actorId: null, action: "alert.raised", subject: "Alert al-1", detail: "CONTRADICTION between PNT-031 rev.4 and MAN-CNC07 rev.B" },
  { at: ago(6 * DAY), actorId: "u-marta", action: "audit.exported", subject: "Audit log", detail: "Exported 1–412 as CSV for the ISO 9001 surveillance audit" },
  { at: ago(5 * DAY), actorId: null, action: "alert.raised", subject: "Alert al-6", detail: "OLD_VERSION_CIRCULATING for PNT-014 rev.2" },
  { at: ago(3 * DAY + 2 * 3_600_000), actorId: "u-diego", action: "answer.generated", subject: "Answer a-crane", detail: "Not found, “What's the maximum load for the overhead crane in Line 3?”" },
  { at: ago(2 * DAY + 5 * 3_600_000), actorId: "u-pablo", action: "answer.generated", subject: "Answer a-coolant", detail: "Answered with 3 citations, PNT-031 rev.4, MAN-CNC07 rev.B, F-007 rev.4" },
  { at: ago(2 * DAY + 5 * 3_600_000 - 40_000), actorId: "u-pablo", action: "answer.feedback", subject: "Answer a-coolant", detail: "Not right, “the manual says 8 %”" },
  { at: ago(DAY + 6 * 3_600_000), actorId: "u-lucia", action: "metadata.corrected", subject: "PNT-014", detail: "Owner set to Javier García (was empty)" },
  { at: ago(DAY + 3 * 3_600_000), actorId: "u-pablo", action: "answer.generated", subject: "Answer a-loto", detail: "Answered with 2 citations, PNT-021 rev.6, UNE-EN ISO 13849-1:2016" },
  { at: ago(DAY + 3 * 3_600_000 - 25_000), actorId: "u-pablo", action: "answer.feedback", subject: "Answer a-loto", detail: "Helpful" },
  { at: ago(DAY + 2 * 3_600_000), actorId: "u-ana", action: "source.updated", subject: "Production SharePoint", detail: "Added site /sites/produccion/Informes" },
  { at: ago(14 * 3_600_000), actorId: "u-javier", action: "alert.dismissed", subject: "Alert al-9", detail: "PNT-005 is being rewritten for rev.8 (due December)" },
  { at: ago(3 * 3_600_000), actorId: null, action: "source.sync_failed", subject: "Production SharePoint", detail: "429 from Microsoft Graph; 5 items retried later" },
  { at: ago(52 * 60_000), actorId: null, action: "source.synced", subject: "Norms library", detail: "0 changes" },
  { at: ago(38 * 60_000), actorId: null, action: "source.synced", subject: "ERP machine master", detail: "2 records updated" },
  { at: ago(26 * 60_000), actorId: "u-ana", action: "user.login", subject: "Ana Torres", detail: "Signed in with company account" },
  { at: ago(11 * 60_000), actorId: null, action: "document.quarantined", subject: "Hidraulica_2019.pdf", detail: "Encrypted PDF" },
  { at: ago(4 * 60_000), actorId: null, action: "source.synced", subject: "Quality share", detail: "2 documents indexed, 1 unchanged" },
  { at: ago(2 * 60_000), actorId: "u-lucia", action: "user.login", subject: "Lucía Marín", detail: "Signed in with company account" },
  { at: ago(0), actorId: "u-lucia", action: "answer.generated", subject: "Answer a-torque", detail: "Answered with 2 citations, PNT-014 rev.3, MAN-CNC07 rev.B" },
];
export const auditLog: AuditEvent[] = (() => {
  let prev = "0".repeat(64);
  return rawAudit.map((e, i) => {
    const id = 401 + i;
    const hash = hash64(prev + JSON.stringify({ id, ...e }));
    const ev = { id, ...e, prevHash: prev, hash };
    prev = hash;
    return ev;
  });
})();

/* Reconciliations: figures that must agree --------------------------------------------- */
const todays = questionLog.filter((q) => q.at.slice(0, 10) === iso(NOW).slice(0, 10));
console.assert(todays.length === 14, "today's question count should be 14", todays.length);
console.assert(documents.every((d) => d.versions.some((v) => v.id === d.currentVersionId)), "every document has its current version");
console.assert(answers.every((a) => a.outcome !== "answered" || a.citations.length > 0), "no answer without a citation");
console.assert(answers.every((a) => a.citations.every((c) => docById(c.documentId))), "every citation points at a document");
console.assert(alerts.every((a) => a.documentIds.every((id) => docById(id))), "every alert points at real documents");
console.assert(!documents.some((d) => d.entities.some((e) => e.label === "LAS-05")), "LAS-05 must have no documents (ENTITY_NO_DOCS)");
console.assert(new Set(buildGraph().nodes.map((n) => n.id)).size === buildGraph().nodes.length, "graph node ids are unique");
