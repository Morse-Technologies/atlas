import { describe, expect, it } from "vitest";
import { camelizeKeys, snakeizeKeys } from "./wire";

describe("camelizeKeys", () => {
  it("converts nested objects and arrays", () => {
    const wire = { current_version_id: "v1", versions: [{ indexed_at: "x" }], open_alerts: { high: 1 } };
    expect(camelizeKeys(wire)).toEqual({ currentVersionId: "v1", versions: [{ indexedAt: "x" }], openAlerts: { high: 1 } });
  });

  it("leaves the keys of domain attributes and of per-band counts alone", () => {
    const wire = { attributes: { doc_code: "PNT-014", line_no: "2" }, documents: { by_band: { procedure: 3 } } };
    expect(camelizeKeys(wire)).toEqual({ attributes: { doc_code: "PNT-014", line_no: "2" }, documents: { byBand: { procedure: 3 } } });
  });

  it("keeps null, numbers and strings as they are", () => {
    expect(camelizeKeys({ last_sync_at: null, layer: 1 })).toEqual({ lastSyncAt: null, layer: 1 });
  });
});

describe("snakeizeKeys", () => {
  it("converts a request body to the wire form", () => {
    expect(snakeizeKeys({ assigneeId: "u1", note: "n", attributes: { docCode: "x" } })).toEqual({ assignee_id: "u1", note: "n", attributes: { docCode: "x" } });
  });

  it("undoes camelizeKeys for ordinary payloads", () => {
    const wire = { not_found_threshold: 0.35, answer_language: "es" };
    expect(snakeizeKeys(camelizeKeys(wire))).toEqual(wire);
  });
});
