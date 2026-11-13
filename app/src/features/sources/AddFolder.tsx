"use client";
import { useState } from "react";
import { toast } from "sonner";
import { newKey, useSourceActions } from "@/features/hooks";
import { Button, Input, Spinner } from "@/ui/atoms";
import { Field, Sheet } from "@/ui/cells";
import { FolderPicker, folderName, type FolderState } from "./FolderPicker";

const EMPTY: FolderState = { folder: "", check: "idle", checkDetail: "" };

function AddFolderForm({ onAdded }: { onAdded: () => void }) {
  const { create } = useSourceActions();
  const [state, setState] = useState(EMPTY);
  const [name, setName] = useState("");
  const shownName = name || (state.folder ? folderName(state.folder) : "");
  const add = () => create.mutate({ input: { name: shownName, kind: "folder", location: state.folder.trim() }, key: newKey() }, {
    onSuccess: () => { toast.success(`${shownName} added`, { description: "Atlas starts reading it now." }); onAdded(); },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <form className="flex flex-col gap-4 p-5" onSubmit={(e) => { e.preventDefault(); add(); }}>
      <FolderPicker {...state} onChange={(patch) => setState({ ...state, ...patch })} />
      <Field label="Name">{({ id }) => <Input id={id} value={shownName} onChange={(e) => setName(e.target.value)} />}</Field>
      <Button type="submit" variant="primary" disabled={state.check !== "ok" || !shownName.trim() || create.isPending}>
        {create.isPending && <Spinner className="text-on-primary" />}Add
      </Button>
    </form>
  );
}

/** "Add folder": the same read-only check as the setup wizard, then the source and its first scan. */
export function AddFolder({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title="Add folder" description="Atlas reads it where it is and never changes it.">
      <AddFolderForm onAdded={() => onOpenChange(false)} />
    </Sheet>
  );
}
