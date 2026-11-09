"use client";
import { useState } from "react";
import { toast } from "sonner";
import { newKey, useDeleteUser, useFeature, usePeople, useUpdateRoles } from "@/features/hooks";
import type { Person, Role } from "@/lib/api/types";
import { Button, HoldButton, Pill, Spinner } from "@/ui/atoms";
import { ErrorState, LoadingRows, PageHeader, Sheet } from "@/ui/cells";
import { RoleEditor, UserTable } from "@/ui/organisms/Admin";
import { Canvas } from "@/ui/templates/ShellLayout";

/** Erases an account (GDPR art. 17). The server refuses to delete the last admin. */
function DeleteUser({ person, onDeleted }: { person: Person; onDeleted: () => void }) {
  const remove = useDeleteUser();
  const confirm = () => remove.mutateAsync({ id: person.id, key: newKey() }).then(
    () => { toast.success(`${person.name}'s account was deleted`); onDeleted(); },
    (e: Error) => { toast.error(e.message); throw e; },
  );
  return (
    <div className="flex flex-col gap-2 border-t border-rule px-5 py-4">
      <span className="text-body font-medium">Delete account</span>
      <p className="text-small text-muted">Erases their sign-in, sessions and preferences. Audit records stay under an anonymous id.</p>
      <HoldButton variant="outline" className="self-start" doneLabel="Deleted" onConfirm={confirm}>Hold to delete</HoldButton>
    </div>
  );
}

/* Screen 9: Users ---------------------------------------------------------------------------------- */
export function UsersScreen() {
  const { data, isPending, isError, refetch } = usePeople();
  const sso = useFeature("sso");
  const update = useUpdateRoles();
  const [editing, setEditing] = useState<Person | null>(null);
  const [roles, setRoles] = useState<Role[]>([]);
  const active = data?.filter((p) => p.isActive).length ?? 0;

  return (
    <Canvas>
      <PageHeader
        title="Users"
        subline={data ? `${data.length} people, ${active} active. Answers only ever come from documents a person can already open in the source.` : "Who can ask, edit and approve."}
      />
      <div className="mt-6 flex flex-wrap items-center gap-3 rounded-card bg-raised px-4 py-3 text-body">
        <span className="font-medium">Sign-in</span>
        <span className="text-muted">{sso.enabled ? "Company accounts through Entra ID. Groups and folder permissions are synced from Active Directory every hour." : "Local accounts. Company sign-in (LDAP / Entra ID) and inherited folder permissions come with Atlas Pro."}</span>
        {sso.enabled && <Pill tone="positive">Connected</Pill>}
      </div>
      <div className="mt-6">
        {isPending ? <LoadingRows rows={6} pitch="h-16" /> : isError ? <ErrorState what="people" onRetry={() => refetch()} />
          : <UserTable people={data} onEdit={(p) => { setEditing(p); setRoles(p.roles); }} />}
      </div>

      <Sheet open={!!editing} onOpenChange={(o) => !o && setEditing(null)} title={editing ? `Roles for ${editing.name}` : ""} description={editing?.email}>
        <RoleEditor roles={roles} onChange={setRoles} />
        {editing && <DeleteUser person={editing} onDeleted={() => setEditing(null)} />}
        <div className="sticky bottom-0 flex justify-end gap-2 border-t border-rule bg-surface px-5 py-4">
          <Button variant="ghost" onClick={() => setEditing(null)}>Cancel</Button>
          <Button variant="primary" disabled={!roles.length || update.isPending}
            onClick={() => editing && update.mutate({ id: editing.id, roles, key: newKey() }, {
              onSuccess: () => { toast.success(`Roles updated for ${editing.name}`); setEditing(null); },
              onError: () => toast.error("Couldn't save the roles. Nothing was changed; try again."),
            })}>
            {update.isPending && <Spinner className="text-on-primary" />}Save roles
          </Button>
        </div>
        {!roles.length && <p className="px-5 pb-4 text-small text-[var(--pill-warning-ink)]">Pick at least one role. To remove access, delete the account instead.</p>}
      </Sheet>
    </Canvas>
  );
}
