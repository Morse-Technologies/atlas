"use client";
import Link from "next/link";
import { useLicense, useMe } from "@/features/hooks";
import { cn } from "@/lib/cn";
import { daysUntil, fmtDate } from "@/lib/format";

const RENEWAL_WARN_DAYS = 30;

/** Tells admins, inside the app, that the Pro license runs out soon: Atlas sends no email. */
export function RenewalBanner() {
  const { data: me } = useMe();
  const { data: license } = useLicense();
  if (!license || license.plan !== "pro" || !me?.roles.includes("admin")) return null;
  const days = daysUntil(license.validUntil);
  if (days > RENEWAL_WARN_DAYS) return null;
  const expired = days < 0;
  return (
    <div role="status" className={cn("px-5 py-2 text-body", expired ? "bg-[var(--pill-negative-bg)] text-[var(--pill-negative-ink)]" : "bg-[var(--pill-warning-bg)] text-[var(--pill-warning-ink)]")}>
      {expired ? `The Atlas Pro license expired on ${fmtDate(license.validUntil)}. Pro features stop until it is renewed.` : `The Atlas Pro license ends in ${days} days, on ${fmtDate(license.validUntil)}. Ask Morse for a renewal file.`}
      {" "}<Link href="/settings" className="underline">License</Link>
    </div>
  );
}
