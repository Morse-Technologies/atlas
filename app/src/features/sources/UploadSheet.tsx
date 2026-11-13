"use client";
import { useState } from "react";
import { newKey, useUpload } from "@/features/hooks";
import { uploadOutcome } from "@/features/access/setupFinish";
import { Button, Spinner } from "@/ui/atoms";
import { Sheet } from "@/ui/cells";
import { UploadFiles } from "./UploadFiles";

function UploadForm({ onDone }: { onDone: () => void }) {
  const upload = useUpload();
  const [files, setFiles] = useState<File[]>([]);
  const outcome = upload.data ? uploadOutcome(upload.data) : null;
  const send = () => upload.mutate({ files, key: newKey() });
  return (
    <div className="px-5 pb-5">
      <UploadFiles files={files} onChange={setFiles} />
      {upload.isError && <p role="alert" className="mt-3 text-small text-negative">{upload.error.message}</p>}
      {outcome && <p role={outcome.ok ? "status" : "alert"} className={`mt-3 text-body ${outcome.ok ? "text-positive" : "text-negative"}`}>{outcome.message}</p>}
      <div className="mt-4 flex justify-end gap-2">
        {outcome?.ok ? <Button variant="primary" onClick={onDone}>Done</Button> : (
          <Button variant="primary" disabled={!files.length || upload.isPending} onClick={send}>
            {upload.isPending && <Spinner className="text-on-primary" />}Upload
          </Button>
        )}
      </div>
    </div>
  );
}

/** "Upload files": loose documents that enter the same pipeline as a folder. Shows what was accepted and what was not, and why. */
export function UploadSheet({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title="Upload files" description="Atlas reads them like any other source.">
      <UploadForm onDone={() => onOpenChange(false)} />
    </Sheet>
  );
}
