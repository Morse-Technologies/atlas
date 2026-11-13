import { describe, expect, it, vi } from "vitest";
import { NOW } from "@/lib/mocks/plant";

describe("clockNow", () => {
  it("is the real time when there is no demo kit", async () => {
    vi.resetModules();
    vi.doMock("@/lib/demo", () => ({ demo: null }));
    const { clockNow } = await import("./clock");
    expect(Math.abs(clockNow().getTime() - Date.now())).toBeLessThan(1000);
  });

  it("starts at the fixture snapshot in the demo", async () => {
    vi.resetModules();
    vi.doUnmock("@/lib/demo");
    const { clockNow } = await import("./clock");
    expect(Math.abs(clockNow().getTime() - NOW.getTime())).toBeLessThan(1000);
  });
});
