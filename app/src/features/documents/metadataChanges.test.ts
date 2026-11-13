import { describe, expect, it } from "vitest";
import type { Document } from "@/lib/api/types";
import { changesOf, formOf } from "./metadataChanges";

const doc = { title: "Spindle", code: "PNT-014", docType: "procedure", status: "current", department: "Quality", ownerId: "u1", approvedAt: "2025-03-12", reviewDueAt: null } as Document;

describe("changesOf", () => {
  it("sends nothing when nothing changed", () => {
    expect(changesOf(doc, formOf(doc))).toEqual({});
  });

  it("sends only the fields that changed", () => {
    expect(changesOf(doc, { ...formOf(doc), code: "PNT-015" })).toEqual({ code: "PNT-015" });
  });

  it("clears the owner and the dates when they are emptied", () => {
    const cleared = { ...formOf(doc), ownerId: "", approvedAt: "" };
    expect(changesOf(doc, cleared)).toEqual({ ownerId: null, approvedAt: null });
  });

  it("turns a date typed into an empty field into a value", () => {
    expect(changesOf(doc, { ...formOf(doc), reviewDueAt: "2027-01-01" })).toEqual({ reviewDueAt: "2027-01-01" });
  });
});
