"use client";
import { Upload, X } from "lucide-react";
import { useId, useState, type DragEvent } from "react";

/** The server accepts at most this many files per upload. */
export const MAX_UPLOAD_FILES = 50;

type Props = { files: File[]; onChange: (files: File[]) => void };

/** Drop zone plus a file picker, and the list of chosen files. The picker is a real input, so keyboard and screen readers work. */
export function UploadFiles({ files, onChange }: Props) {
  const inputId = useId();
  const [dragging, setDragging] = useState(false);
  const add = (chosen: File[]) => onChange([...files, ...chosen].slice(0, MAX_UPLOAD_FILES));
  const drop = (e: DragEvent<HTMLLabelElement>) => { e.preventDefault(); setDragging(false); add([...e.dataTransfer.files]); };
  return (
    <div className="mt-5 flex flex-col gap-3">
      <label htmlFor={inputId} onDragOver={(e) => { e.preventDefault(); setDragging(true); }} onDragLeave={() => setDragging(false)} onDrop={drop}
        className={`flex min-h-32 cursor-pointer flex-col items-center justify-center gap-2 rounded-card border border-dashed p-6 text-center focus-within:ring-2 focus-within:ring-accent/40 ${dragging ? "border-accent bg-raised" : "border-control"}`}>
        <Upload aria-hidden className="size-5 text-muted" />
        <span className="text-body">Drop PDFs, Word or Excel files here, or choose them.</span>
        <span className="text-small text-muted">Up to {MAX_UPLOAD_FILES} files at a time.</span>
        <input id={inputId} type="file" multiple className="sr-only" onChange={(e) => { add([...(e.target.files ?? [])]); e.target.value = ""; }} />
      </label>
      <ChosenFiles files={files} onChange={onChange} />
    </div>
  );
}

function ChosenFiles({ files, onChange }: Props) {
  if (files.length === 0) return null;
  return (
    <ul aria-label="Chosen files" className="flex flex-col gap-1">
      {files.map((f, i) => (
        <li key={`${f.name}-${i}`} className="flex items-center justify-between gap-2 rounded-control bg-raised px-3 py-1.5 text-small">
          <span className="truncate">{f.name}</span>
          <button type="button" aria-label={`Remove ${f.name}`} onClick={() => onChange(files.filter((_, j) => j !== i))}
            className="hit-slop grid size-6 flex-none place-items-center rounded-full text-muted hover:bg-surface hover:text-text">
            <X className="size-3.5" />
          </button>
        </li>
      ))}
    </ul>
  );
}
