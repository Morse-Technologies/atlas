"use client";
import { toast } from "sonner";
import { useResetPassword, useResetSecondFactor } from "@/features/hooks";
import type { Person } from "@/lib/api/types";
import { HoldButton } from "@/ui/atoms";
import type { Secrets } from "./SecretsOnce";

/** Gives another person new access when they forgot the password or lost their phone. The secrets are handed to `onSecrets`. */
export function RecoverAccess({ person, onSecrets }: { person: Person; onSecrets: (secrets: Secrets) => void }) {
  const password = useResetPassword();
  const secondFactor = useResetSecondFactor();
  const failed = (e: Error) => { toast.error(e.message); throw e; };
  return (
    <div className="flex flex-col gap-2 border-t border-rule px-5 py-4">
      <span className="text-body font-medium">Recover access</span>
      <p className="text-small text-muted">Signs {person.name} out everywhere. The new value is shown once.</p>
      <div className="flex flex-wrap gap-2">
        <HoldButton variant="outline" doneLabel="Done" onConfirm={() => password.mutateAsync(person.id).then((r) => onSecrets({ password: r.password }), failed)}>
          Hold to reset password
        </HoldButton>
        <HoldButton variant="outline" doneLabel="Done" onConfirm={() => secondFactor.mutateAsync(person.id).then((r) => onSecrets({ totpUri: r.totpUri }), failed)}>
          Hold to reset second factor
        </HoldButton>
      </div>
    </div>
  );
}
