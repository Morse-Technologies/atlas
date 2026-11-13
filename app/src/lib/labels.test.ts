import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import type { FieldProvenance, PersonName } from "@/lib/api/types";
import {
  QUARANTINE_REASONS, auditActionLabel, auditDetailText, labelOfAction, metadataValue, quarantineAdvice, rolesToOffer,
} from "@/lib/labels";
import { auditLog } from "@/lib/mocks/plant";

const serverActions: string[] = JSON.parse(readFileSync(new URL("../../../server/api/audit-actions.json", import.meta.url), "utf8"));

const ownerRow = (value: string): FieldProvenance => ({ field: "owner_id", value, source: "manual", confidence: 1 });
const people = new Map<string, PersonName>([["u-1", { id: "u-1", name: "Javier García" }]]);

describe("audit event labels", () => {
  it("every action the server writes has a label", () => {
    const unlabelled = serverActions.filter((a) => !(a in auditActionLabel));
    expect(unlabelled).toEqual([]);
  });

  it("no label is kept for an action the server does not write", () => {
    const stale = Object.keys(auditActionLabel).filter((a) => !serverActions.includes(a));
    expect(stale).toEqual([]);
  });

  it("every audit action in the demo data is one the server writes", () => {
    const invented = auditLog.map((e) => e.action).filter((a) => !serverActions.includes(a));
    expect(invented).toEqual([]);
  });

  it("an unknown action shows its key", () => {
    expect(labelOfAction("something.new")).toBe("something.new");
  });
});

describe("audit detail text", () => {
  it("shows the changed settings and metadata fields with their labels", () => {
    expect(auditDetailText("fields=code,owner_id")).toBe("Code, Owner");
  });

  it("shows the roles with their labels", () => {
    expect(auditDetailText("roles=viewer,admin")).toBe("Ask, Administrator");
  });

  it("leaves any other detail as it is", () => {
    expect(auditDetailText("status=dismissed")).toBe("status=dismissed");
  });
});

describe("metadata values", () => {
  it("shows the owner by name", () => {
    expect(metadataValue(ownerRow("u-1"), people)).toBe("Javier García");
  });

  it("shows a former member when the owner is not in the list", () => {
    expect(metadataValue(ownerRow("u-gone"), people)).toBe("Former member");
  });

  it("shows any other field as it is", () => {
    expect(metadataValue({ ...ownerRow("PNT-014"), field: "code" }, people)).toBe("PNT-014");
  });
});

describe("roles offered", () => {
  it("does not offer the unused role to someone who does not have it", () => {
    expect(rolesToOffer([])).not.toContain("organizer_approver");
  });

  it("offers the unused role last to someone who has it, so it can be removed", () => {
    const offered = rolesToOffer(["organizer_approver"]);
    expect(offered.at(-1)).toBe("organizer_approver");
    expect(offered).toHaveLength(6);
  });
});

describe("quarantine advice", () => {
  it.each(QUARANTINE_REASONS)("%s has a full sentence", (reason) => {
    expect(quarantineAdvice[reason]).toMatch(/\.$/);
  });
});
