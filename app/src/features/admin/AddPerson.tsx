"use client";
import { useState } from "react";
import { toast } from "sonner";
import { useCreateUser } from "@/features/hooks";
import { isEmail } from "@/lib/format";
import type { Role } from "@/lib/api/types";
import { Button, Checkbox, Input, Spinner } from "@/ui/atoms";
import { Field, Sheet } from "@/ui/cells";
import { RoleEditor } from "@/ui/organisms/Admin";
import { SecretsOnce, type Secrets } from "./SecretsOnce";

/** Roles that cannot sign in without a second factor (the server enforces the same rule). */
const SECOND_FACTOR_ROLES: Role[] = ["admin", "auditor", "curator"];
const requiresSecondFactor = (roles: Role[]) => roles.some((r) => SECOND_FACTOR_ROLES.includes(r));

interface Form { name: string; email: string; roles: Role[]; secondFactor: boolean }
const EMPTY: Form = { name: "", email: "", roles: ["viewer"], secondFactor: false };

/** The form of "Add person"; it reports what the server returned so the sheet can show the secrets. */
function PersonForm({ onCreated }: { onCreated: (secrets: Secrets) => void }) {
  const create = useCreateUser();
  const [form, setForm] = useState<Form>(EMPTY);
  const locked = requiresSecondFactor(form.roles);
  const ready = form.name.trim() !== "" && isEmail(form.email) && form.roles.length > 0;
  const submit = () => create.mutate({ ...form, secondFactor: form.secondFactor || locked }, {
    onSuccess: (r) => onCreated({ password: r.password, totpUri: r.totpUri }),
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <form className="flex flex-col" onSubmit={(e) => { e.preventDefault(); submit(); }}>
      <div className="flex flex-col gap-4 p-5">
        <Field label="Full name">{({ id }) => <Input id={id} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} autoComplete="off" />}</Field>
        <Field label="Work email">{({ id }) => <Input id={id} type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} autoComplete="off" />}</Field>
      </div>
      <RoleEditor roles={form.roles} onChange={(roles) => setForm({ ...form, roles })} />
      <div className="flex items-start gap-3 border-t border-rule px-5 py-4">
        <Checkbox id="second-factor" checked={form.secondFactor || locked} disabled={locked} onCheckedChange={(v) => setForm({ ...form, secondFactor: v })}
          aria-describedby="second-factor-help" className="mt-0.5" />
        <label htmlFor="second-factor" className="text-body">
          <span className="font-medium">Second factor</span>
          <span id="second-factor-help" className="block text-small text-muted">{locked ? "Required for administrators, auditors and curators." : "An authenticator code at every sign-in."}</span>
        </label>
      </div>
      <div className="sticky bottom-0 flex justify-end border-t border-rule bg-surface px-5 py-4">
        <Button type="submit" variant="primary" disabled={!ready || create.isPending}>
          {create.isPending && <Spinner className="text-on-primary" />}Create account
        </Button>
      </div>
    </form>
  );
}

/** "Add person": creates the account, then shows its one-time secrets until "Done". */
export function AddPerson({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const [secrets, setSecrets] = useState<Secrets | null>(null);
  const close = () => { setSecrets(null); onOpenChange(false); };
  return (
    <Sheet open={open} onOpenChange={(o) => (o ? onOpenChange(true) : close())} title={secrets ? "Account created" : "Add person"}
      description={secrets ? "They must choose their own password the first time they sign in." : "Atlas makes a one-time password for them."}>
      {secrets ? <SecretsOnce secrets={secrets} onDone={close} /> : <PersonForm onCreated={setSecrets} />}
    </Sheet>
  );
}
