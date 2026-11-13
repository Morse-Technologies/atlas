"use client";
import { useState } from "react";
import { toast } from "sonner";
import { newKey, useDeleteUser, usePeoplePages, useUpdateRoles } from "@/features/hooks";
import type { Person, Role } from "@/lib/api/types";
import { Plus } from "lucide-react";
import { Button, HoldButton, Spinner } from "@/ui/atoms";
import { ErrorState, LoadMore, LoadingRows, PageHeader, Sheet } from "@/ui/cells";
import { RoleEditor, UserTable } from "@/ui/organisms/Admin";
import { Canvas } from "@/ui/templates/ShellLayout";
import { AddPerson } from "./AddPerson";
import { RecoverAccess } from "./RecoverAccess";
import { SecretsOnce, type Secrets } from "./SecretsOnce";

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
  const { data, isPending, isError, refetch, hasNextPage, isFetchingNextPage, fetchNextPage } = usePeoplePages();
  const update = useUpdateRoles();
  const [adding, setAdding] = useState(false);
  const [secrets, setSecrets] = useState<Secrets | null>(null);
  const [editing, setEditing] = useState<Person | null>(null);
  const [roles, setRoles] = useState<Role[]>([]);
  const active = data?.filter((p) => p.isActive).length ?? 0;

  return (
    <Canvas>
      <PageHeader
        title="Users"
        actions={<Button variant="primary" onClick={() => setAdding(true)}><Plus />Add person</Button>}
        subline={data ? `${data.length} people, ${active} active. Answers only ever come from documents a person can already open in the source.` : "Who can ask, edit and approve."}
      />
      <div className="mt-6 flex flex-wrap items-center gap-3 rounded-card bg-raised px-4 py-3 text-body">
        <span className="font-medium">Sign-in</span>
        <span className="text-muted">Local accounts: a password, plus a code from an authenticator app for administrators, auditors and curators.</span>
      </div>
      <div className="mt-6">
        {isPending ? <LoadingRows rows={6} pitch="h-16" /> : isError ? <ErrorState what="people" onRetry={() => refetch()} />
          : <UserTable people={data} onEdit={(p) => { setEditing(p); setRoles(p.roles); }} />}
        {data && <LoadMore loaded={data.length} hasMore={!!hasNextPage} loading={isFetchingNextPage} onLoadMore={() => void fetchNextPage()} />}
      </div>

      <AddPerson open={adding} onOpenChange={setAdding} />
      <Sheet open={!!secrets} onOpenChange={(o) => !o && setSecrets(null)} title="New access" description="They must choose their own password the next time they sign in.">
        {secrets && <SecretsOnce secrets={secrets} onDone={() => setSecrets(null)} />}
      </Sheet>
      <Sheet open={!!editing} onOpenChange={(o) => !o && setEditing(null)} title={editing ? `Roles for ${editing.name}` : ""} description={editing?.email}>
        <RoleEditor roles={roles} onChange={setRoles} />
        {editing && <RecoverAccess person={editing} onSecrets={(s) => { setEditing(null); setSecrets(s); }} />}
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
