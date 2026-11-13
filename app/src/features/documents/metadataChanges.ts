import type { DocStatus, Document, MetadataPatch } from "@/lib/api/types";

/** What the form holds: every field as text, so an untouched field is easy to tell from a changed one. */
export type Form = { title: string; code: string; docType: string; status: string; department: string; ownerId: string; approvedAt: string; reviewDueAt: string };

export const formOf = (d: Document): Form => ({
  title: d.title, code: d.code, docType: d.docType, status: d.status, department: d.department,
  ownerId: d.ownerId ?? "", approvedAt: d.approvedAt ?? "", reviewDueAt: d.reviewDueAt ?? "",
});

/** Only what differs from the document: an empty owner or date means "clear it". */
export function changesOf(doc: Document, form: Form): MetadataPatch {
  const before = formOf(doc);
  const patch: MetadataPatch = {};
  if (form.title !== before.title) patch.title = form.title;
  if (form.code !== before.code) patch.code = form.code;
  if (form.docType !== before.docType) patch.docType = form.docType;
  if (form.status !== before.status) patch.status = form.status as DocStatus;
  if (form.department !== before.department) patch.department = form.department;
  if (form.ownerId !== before.ownerId) patch.ownerId = form.ownerId || null;
  if (form.approvedAt !== before.approvedAt) patch.approvedAt = form.approvedAt || null;
  if (form.reviewDueAt !== before.reviewDueAt) patch.reviewDueAt = form.reviewDueAt || null;
  return patch;
}
