import { describe, expect, it } from "vitest";
import type { License } from "@/lib/api";
import { gateReason, gateText } from "./gateReason";

const NOW = new Date("2026-11-12T10:00:00Z");
const core: License = { plan: "core", organization: "", validUntil: "", features: [] };
const pro: License = { plan: "pro", organization: "Acme", validUntil: "2027-06-30", features: ["alerts"] };

describe("gateReason", () => {
  it("is open when the license includes the feature", () => {
    expect(gateReason(pro, "alerts", NOW)).toBe("enabled");
  });

  it("says Core when there is no Pro license", () => {
    expect(gateReason(core, "alerts", NOW)).toBe("core");
  });

  it("says expired when the license ended and no feature is left", () => {
    const expired: License = { ...pro, validUntil: "2026-10-01", features: [] };
    expect(gateReason(expired, "alerts", NOW)).toBe("expired");
  });

  it("still counts the last day of the license as valid", () => {
    const lastDay: License = { ...pro, validUntil: "2026-11-12", features: [] };
    expect(gateReason(lastDay, "map", NOW)).toBe("not_included");
  });

  it("says not included when a valid license leaves the feature out", () => {
    expect(gateReason(pro, "map", NOW)).toBe("not_included");
  });
});

describe("gateText", () => {
  it("names the expiry date and the missing feature", () => {
    const expired: License = { ...pro, validUntil: "2026-10-01", features: [] };
    expect(gateText(expired, "alerts", NOW)).toContain("expired on");
    expect(gateText(pro, "map", NOW)).toBe("Your license does not include the map. Ask Morse to add it to your license.");
  });
});
