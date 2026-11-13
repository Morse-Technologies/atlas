"use client";
import { Pencil } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { newKey, useCatalog, useCorrectMetadata, usePeopleNames } from "@/features/hooks";
import type { DocStatus, Document } from "@/lib/api/types";
import { Button, Input, Select, Spinner } from "@/ui/atoms";
import { Field, Sheet } from "@/ui/cells";
import { changesOf, formOf, type Form } from "./metadataChanges";

const STATUSES: DocStatus[] = ["current", "draft", "obsolete", "unknown"];

function MetadataForm({ doc, onSaved }: { doc: Document; onSaved: () => void }) {
  const catalog = useCatalog();
  const people = usePeopleNames();
  const correct = useCorrectMetadata();
  const [form, setForm] = useState(() => formOf(doc));
  const isFormerOwner = !!form.ownerId && !!people.data && !people.data.some((p) => p.id === form.ownerId);
  const patch = changesOf(doc, form);
  const edit = (change: Partial<Form>) => setForm({ ...form, ...change });
  const save = () => correct.mutate({ id: doc.id, patch, key: newKey() }, {
    onSuccess: () => { toast.success("Details saved", { description: "They stay as you set them when the file is read again." }); onSaved(); },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <form className="flex flex-col gap-4 p-5" onSubmit={(e) => { e.preventDefault(); save(); }}>
      <Field label="Title">{({ id }) => <Input id={id} value={form.title} onChange={(e) => edit({ title: e.target.value })} />}</Field>
      <Field label="Code">{({ id }) => <Input id={id} value={form.code} onChange={(e) => edit({ code: e.target.value })} />}</Field>
      <Field label="Type">{({ id }) => (
        <Select id={id} value={form.docType} onChange={(e) => edit({ docType: e.target.value })}>
          {(catalog.data?.docTypes ?? []).map((t) => <option key={t.key} value={t.key}>{t.label}</option>)}
          {!catalog.data?.docTypes.some((t) => t.key === form.docType) && <option value={form.docType}>{form.docType}</option>}
        </Select>
      )}</Field>
      <Field label="Status">{({ id }) => (
        <Select id={id} value={form.status} onChange={(e) => edit({ status: e.target.value })}>
          {STATUSES.map((s) => <option key={s} value={s}>{s}</option>)}
        </Select>
      )}</Field>
      <Field label="Department">{({ id }) => <Input id={id} value={form.department} onChange={(e) => edit({ department: e.target.value })} />}</Field>
      <Field label="Owner">{({ id }) => (
        <Select id={id} value={form.ownerId} onChange={(e) => edit({ ownerId: e.target.value })}>
          <option value="">No owner</option>
          {(people.data ?? []).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          {isFormerOwner && <option value={form.ownerId}>Former member</option>}
        </Select>
      )}</Field>
      <Field label="Approved on">{({ id }) => <Input id={id} type="date" value={form.approvedAt} onChange={(e) => edit({ approvedAt: e.target.value })} />}</Field>
      <Field label="Review due">{({ id }) => <Input id={id} type="date" value={form.reviewDueAt} onChange={(e) => edit({ reviewDueAt: e.target.value })} />}</Field>
      <Button type="submit" variant="primary" disabled={!Object.keys(patch).length || correct.isPending}>
        {correct.isPending && <Spinner className="text-on-primary" />}Save details
      </Button>
    </form>
  );
}

/** "Edit details": a person with editor_metadata corrects what Atlas read wrongly. Revision is not editable. */
export function EditMetadata({ doc }: { doc: Document }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button size="sm" variant="ghost" onClick={() => setOpen(true)}><Pencil />Edit details</Button>
      <Sheet open={open} onOpenChange={setOpen} title="Edit details" description="Each value you change is marked as corrected by hand.">
        <MetadataForm doc={doc} onSaved={() => setOpen(false)} />
      </Sheet>
    </>
  );
}
