"use client";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { newKey, useCanEditMetadata, useDeleteDocument } from "@/features/hooks";
import type { Document } from "@/lib/api/types";
import { HoldButton } from "@/ui/atoms";

/** Delete for documents that came from an upload; a document of a folder belongs to the folder's owner. */
export function DeleteDocument({ doc }: { doc: Document }) {
  const router = useRouter();
  const remove = useDeleteDocument();
  const allowed = useCanEditMetadata();
  if (doc.sourceKind !== "upload" || !allowed) return null;
  const confirm = () => remove.mutateAsync({ id: doc.id, key: newKey() }).then(
    () => { toast.success(`${doc.title} was deleted`); router.replace("/documents"); },
    (e: Error) => { toast.error(e.message); throw e; },
  );
  return (
    <div className="flex flex-col gap-2 border-t border-rule px-5 py-4">
      <span className="text-body font-medium">Delete document</span>
      <p className="text-small text-muted">Removes the uploaded file from Atlas for good. It stops appearing in search, lists and the map.</p>
      <HoldButton variant="outline" className="self-start" doneLabel="Deleted" onConfirm={confirm}>Hold to delete</HoldButton>
    </div>
  );
}
