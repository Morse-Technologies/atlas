import { describe, expect, it } from "vitest";
import { buildGraph } from "@/lib/mocks/plant";

const SAME_CODE_LAYER = 2;
const KINDS_THE_SERVER_DOES_NOT_WRITE = ["contradicts", "owned_by"];

describe("the demo graph", () => {
  const { edges } = buildGraph();

  it("has no suggested links, because the server never suggests any", () => {
    expect(edges.filter((e) => e.status === "suggested")).toEqual([]);
  });

  it("has no link kinds the server does not write", () => {
    expect(edges.filter((e) => KINDS_THE_SERVER_DOES_NOT_WRITE.includes(e.kind))).toEqual([]);
  });

  it("uses only layers 1 and 2, with duplicates only in layer 2", () => {
    expect(new Set(edges.map((e) => e.layer))).toEqual(new Set([1, SAME_CODE_LAYER]));
    expect(edges.filter((e) => e.kind === "duplicates").every((e) => e.layer === SAME_CODE_LAYER)).toBe(true);
    expect(edges.filter((e) => e.layer === SAME_CODE_LAYER).every((e) => e.kind === "duplicates")).toBe(true);
  });
});
