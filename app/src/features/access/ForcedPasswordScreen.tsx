"use client";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { useChangePassword, useMe } from "@/features/hooks";
import { HOME_PATH } from "@/lib/edition";
import { Button, Input, Spinner } from "@/ui/atoms";
import { Field } from "@/ui/cells";
import { AuthLayout } from "@/ui/templates/AuthLayout";

const MIN_PASSWORD = 12;

/** First sign-in of an account whose password someone else set: the only thing the server allows is choosing a new one. */
export function ForcedPasswordScreen() {
  const router = useRouter();
  const { refetch } = useMe();
  const change = useChangePassword();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const tooShort = next.length > 0 && next.length < MIN_PASSWORD;
  const submit = () => change.mutate({ currentPassword: current, newPassword: next }, {
    // The cached "me" still says the change is pending; refresh it before the shell looks at it again.
    onSuccess: async () => { await refetch(); router.replace(HOME_PATH); },
    onError: (e: Error) => toast.error(e.message),
  });
  return (
    <AuthLayout>
      <h1 className="text-title font-medium">Choose your password</h1>
      <p className="mt-1 text-body text-muted">The password you were given works once. Choose one only you know to continue.</p>
      <form className="mt-7 flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); submit(); }}>
        <Field label="Password you were given">
          {({ id }) => <Input id={id} type="password" autoFocus value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" />}
        </Field>
        <Field label="New password" help={`At least ${MIN_PASSWORD} characters.`} error={tooShort ? `Use at least ${MIN_PASSWORD} characters.` : undefined}>
          {({ id, describedBy, invalid }) => <Input id={id} type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" aria-describedby={describedBy} aria-invalid={invalid} />}
        </Field>
        <Button type="submit" variant="primary" size="lg" disabled={!current || next.length < MIN_PASSWORD || change.isPending}>
          {change.isPending && <Spinner className="text-on-primary" />}Continue
        </Button>
      </form>
    </AuthLayout>
  );
}
