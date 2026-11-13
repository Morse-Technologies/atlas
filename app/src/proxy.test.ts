import { NextRequest } from "next/server";
import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.unstubAllEnvs();
  vi.doUnmock("@/lib/demo");
  vi.resetModules();
});

/** Loads the gate as a build with (or without) the demo kit would. */
async function proxyOf(demoKit: object | null) {
  vi.resetModules();
  vi.doMock("@/lib/demo", () => ({ demo: demoKit }));
  return (await import("./proxy")).proxy;
}

const withoutCookie = () => new NextRequest("http://localhost/ask");

describe("the auth-off switch", () => {
  it("is ignored in a real build: a request with no cookie still goes to sign in", async () => {
    vi.stubEnv("ATLAS_AUTH_OFF", "1");
    const response = (await proxyOf(null))(withoutCookie());
    expect(response.status).toBe(307);
    expect(response.headers.get("location")).toBe("http://localhost/login?next=%2Fask");
  });

  it("works on demo data, where there is no server to sign in to", async () => {
    vi.stubEnv("ATLAS_AUTH_OFF", "1");
    const response = (await proxyOf({}))(withoutCookie());
    expect(response.headers.get("location")).toBeNull();
  });

  it("is off unless it is set to 1, also on demo data", async () => {
    const response = (await proxyOf({}))(withoutCookie());
    expect(response.headers.get("location")).toBe("http://localhost/login?next=%2Fask");
  });
});
