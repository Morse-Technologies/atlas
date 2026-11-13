import type { License, ProFeature } from "@/lib/api";
import { fmtDate } from "@/lib/format";

export type GateReason = "enabled" | "core" | "expired" | "not_included";

const DAY_MS = 86_400_000;

/** Why a Pro feature is closed for this license: no Pro license, an expired one, or one that leaves the feature out. */
export function gateReason(license: License, feature: ProFeature, now: Date): GateReason {
  if (license.features.includes(feature)) return "enabled";
  if (license.plan === "core") return "core";
  // The server keeps a license valid through the whole day it names.
  const expired = now.getTime() > Date.parse(license.validUntil) + DAY_MS;
  return expired ? "expired" : "not_included";
}

const FEATURE_NAMES: Record<ProFeature, string> = {
  alerts: "alerts", map: "the map", sso: "single sign-on", inherited_acl: "inherited permissions",
  audit_reports: "signed audit reports", sharepoint: "the SharePoint connector", erp: "the ERP connector",
};

/** The sentence that tells a person why the feature is closed and what to do about it. */
export function gateText(license: License, feature: ProFeature, now: Date): string {
  switch (gateReason(license, feature, now)) {
    case "core":
      return "This installation runs Atlas Core. An administrator can add a Pro license file in Settings; nothing is sent outside the plant to check it.";
    case "expired":
      return `The Atlas Pro license expired on ${fmtDate(license.validUntil)}. An administrator can install a renewed license in Settings.`;
    default:
      return `Your license does not include ${FEATURE_NAMES[feature]}. Ask Morse to add it to your license.`;
  }
}
