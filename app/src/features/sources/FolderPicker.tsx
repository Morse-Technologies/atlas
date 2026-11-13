"use client";
import { Check, FolderOpen } from "lucide-react";
import { api } from "@/lib/api";
import { Button, Input, Spinner } from "@/ui/atoms";
import { Field } from "@/ui/cells";

const FOLDER_PATH = /^(\\\\[^\\]+\\[^\\]+.*|\/.+)$/; // \\server\share\… or an absolute path

/** The last segment of a path, as the default name of the source. */
export const folderName = (path: string) => path.trim().split(/[\\/]/).filter(Boolean).pop() ?? "Documents";

export const isFolderPath = (path: string) => FOLDER_PATH.test(path.trim());

export type FolderState = { folder: string; check: "idle" | "checking" | "ok" | "failed"; checkDetail: string };
type Props = FolderState & { onChange: (patch: Partial<FolderState>) => void };

/** A folder path with the read-only access check the setup wizard and the Sources screen share. */
export function FolderPicker({ folder, check, checkDetail, onChange }: Props) {
  const folderOk = isFolderPath(folder);
  const runCheck = async () => {
    onChange({ check: "checking" });
    try {
      const result = await api.checkFolder(folder.trim());
      onChange({ check: result.ok ? "ok" : "failed", checkDetail: result.detail });
    } catch {
      onChange({ check: "failed", checkDetail: "Atlas couldn't check this folder. Try again." });
    }
  };
  return (
    <div className="mt-5 flex flex-col gap-3">
      <Field label="Folder path" help="A folder Atlas may read, for example /sources/docs." error={folder && !folderOk ? "Use \\\\server\\share or an absolute path." : undefined}>
        {({ id, describedBy, invalid }) => (
          <div className="flex gap-2">
            <Input id={id} value={folder} onChange={(e) => onChange({ folder: e.target.value, check: "idle" })} placeholder="/sources/docs" aria-describedby={describedBy} aria-invalid={invalid} />
            <Button variant="outline" size="lg" disabled={!folderOk || check === "checking"} onClick={runCheck}>
              {check === "checking" ? <Spinner /> : <FolderOpen />}Check
            </Button>
          </div>
        )}
      </Field>
      {check === "ok" && <p role="status" className="flex items-center gap-2 text-body text-positive animate-enter"><Check className="size-4" />Atlas can read this folder with a read-only account.</p>}
      {check === "failed" && <p role="alert" className="text-body text-negative animate-enter">{checkDetail}</p>}
    </div>
  );
}
