"use client";
import { Copy } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/ui/atoms";

/** What the server hands over once: a one-time password and/or an authenticator link. `null` means "not part of this answer". */
export interface Secrets { password?: string; totpUri?: string | null }

function SecretRow({ label, value }: { label: string; value: string }) {
  const copy = () => { void navigator.clipboard?.writeText(value); toast("Copied"); };
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-body font-medium">{label}</span>
      <div className="flex items-center justify-between gap-3 rounded-card bg-raised px-4 py-3">
        <code className="text-body break-all">{value}</code>
        <button type="button" onClick={copy} aria-label={`Copy ${label.toLowerCase()}`}
          className="hit-slop grid size-8 flex-none place-items-center rounded-full text-muted hover:bg-surface hover:text-text">
          <Copy className="size-4" />
        </button>
      </div>
    </div>
  );
}

/** Shows secrets once. "Done" hands control back to the caller, which drops them from memory. */
export function SecretsOnce({ secrets, onDone }: { secrets: Secrets; onDone: () => void }) {
  return (
    <div className="flex flex-col gap-4 p-5">
      <p role="alert" className="rounded-card bg-raised px-4 py-3 text-body">Shown once. Give them to the person in person. Atlas cannot show them again.</p>
      {secrets.password && <SecretRow label="One-time password" value={secrets.password} />}
      {secrets.totpUri && <SecretRow label="Authenticator link" value={secrets.totpUri} />}
      <Button variant="primary" className="self-end" onClick={onDone}>Done</Button>
    </div>
  );
}
