import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

describe("the api entry point", () => {
  it("does not import the demo adapter, so the real build can drop it", () => {
    const source = readFileSync(new URL("./index.ts", import.meta.url), "utf8");
    expect(source).not.toContain('from "./mock"');
  });
});
