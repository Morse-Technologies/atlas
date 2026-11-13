import { describe, expect, it } from "vitest";
import { uploadOutcome } from "./setupFinish";

describe("uploadOutcome", () => {
  it("does not go on when the server accepted nothing, and says why", () => {
    const out = uploadOutcome({ accepted: 0, rejected: [{ name: "a.exe", reason: "unsupported type" }] });
    expect(out).toEqual({ ok: false, message: "No file was accepted. Not accepted: a.exe (unsupported type)." });
  });

  it("goes on and still lists what was rejected", () => {
    const out = uploadOutcome({ accepted: 2, rejected: [{ name: "b.zip", reason: "too large" }] });
    expect(out).toEqual({ ok: true, message: "Atlas is indexing 2 files. Not accepted: b.zip (too large)." });
  });

  it("goes on when everything was accepted", () => {
    expect(uploadOutcome({ accepted: 1, rejected: [] })).toEqual({ ok: true, message: "Atlas is indexing 1 file." });
  });
});
