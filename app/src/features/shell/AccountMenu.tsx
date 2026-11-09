"use client";
import { useQueryClient } from "@tanstack/react-query";
import { KeyRound, LogOut, ShieldCheck, Trash2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { newKey, useChangePassword, useDeleteMe } from "@/features/hooks";
import { api } from "@/lib/api";
import { Button, HoldButton, Input } from "@/ui/atoms";
import { Field, Sheet } from "@/ui/cells";

/** Ends the session and forgets cached data, so the next account starts clean. */
function useSignOut() {
  const router = useRouter();
  const qc = useQueryClient();
  // The server ends the session and clears the cookie; if it can't be reached the person still leaves this screen.
  return () => { api.logout().catch(() => undefined).finally(() => { qc.clear(); router.replace("/login"); }); };
}

/** The account menu items plus the sheet that confirms deleting your own account. */
export function useAccountMenu() {
  const router = useRouter();
  const leave = useSignOut();
  const [deleting, setDeleting] = useState(false);
  const [changing, setChanging] = useState(false);
  const items = [
    { label: "Change password", icon: <KeyRound />, onClick: () => setChanging(true) },
    { label: "Privacy", icon: <ShieldCheck />, onClick: () => router.push("/privacy") },
    { label: "Delete my account", icon: <Trash2 />, tone: "danger" as const, onClick: () => setDeleting(true) },
    "separator" as const,
    { label: "Sign out", icon: <LogOut />, onClick: leave },
  ];
  const sheet = (
    <>
      <ChangePasswordSheet open={changing} onOpenChange={setChanging} />
      <DeleteMeSheet open={deleting} onOpenChange={setDeleting} onDeleted={leave} />
    </>
  );
  return { items, sheet };
}

function DeleteMeSheet({ open, onOpenChange, onDeleted }: { open: boolean; onOpenChange: (o: boolean) => void; onDeleted: () => void }) {
  const remove = useDeleteMe();
  const confirm = () => remove.mutateAsync(newKey()).then(onDeleted, (e: Error) => { toast.error(e.message); throw e; });
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title="Delete my account" description="This can't be undone.">
      <div className="flex flex-col gap-4 p-5 text-body">
        <p>Your sign-in, sessions and preferences are erased. Audit records stay, under an anonymous id, for the period your organization keeps them.</p>
        <HoldButton variant="outline" doneLabel="Deleted" onConfirm={confirm}>Hold to delete my account</HoldButton>
      </div>
    </Sheet>
  );
}

const MIN_PASSWORD = 12;

function ChangePasswordSheet({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const change = useChangePassword();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const tooShort = next.length > 0 && next.length < MIN_PASSWORD;
  const submit = () => {
    change.mutate({ currentPassword: current, newPassword: next }, {
      onSuccess: () => { toast.success("Password changed. Your other sessions were signed out."); setCurrent(""); setNext(""); onOpenChange(false); },
      onError: (e: Error) => toast.error(e.message),
    });
  };
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title="Change password" description="You stay signed in here; other devices are signed out.">
      <form className="flex flex-col gap-4 p-5" onSubmit={(e) => { e.preventDefault(); submit(); }}>
        <Field label="Current password">
          {({ id }) => <Input id={id} type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" />}
        </Field>
        <Field label="New password" help={`At least ${MIN_PASSWORD} characters.`} error={tooShort ? `Use at least ${MIN_PASSWORD} characters.` : undefined}>
          {({ id, describedBy, invalid }) => <Input id={id} type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" aria-describedby={describedBy} aria-invalid={invalid} />}
        </Field>
        <Button type="submit" variant="primary" disabled={!current || next.length < MIN_PASSWORD || change.isPending}>Change password</Button>
      </form>
    </Sheet>
  );
}
