import { describe, expect, it } from "vitest";
import type { Role } from "@/lib/api/types";
import { canOpenScreen, mayUseWidget } from "./screenAccess";

const OPEN_TO_ALL = ["/dashboard", "/ask", "/documents", "/map"];
const RESTRICTED = ["/alerts", "/sources", "/audit", "/users", "/settings"];

const opens = (roles: Role[], screens: string[]) => screens.filter((s) => canOpenScreen(roles, s));

describe("which screens a role can open", () => {
  it("a viewer opens the screens open to all and none of the restricted ones", () => {
    expect(opens(["viewer"], [...OPEN_TO_ALL, ...RESTRICTED])).toEqual(OPEN_TO_ALL);
  });

  it("a curator opens Alerts", () => {
    expect(canOpenScreen(["curator"], "/alerts")).toBe(true);
  });

  it("an editor opens Sources and not Users", () => {
    expect(canOpenScreen(["editor_metadata"], "/sources")).toBe(true);
    expect(canOpenScreen(["editor_metadata"], "/users")).toBe(false);
  });

  it("an auditor opens Audit and not Settings", () => {
    expect(canOpenScreen(["auditor"], "/audit")).toBe(true);
    expect(canOpenScreen(["auditor"], "/settings")).toBe(false);
  });

  it("an administrator opens every screen", () => {
    expect(opens(["admin"], [...OPEN_TO_ALL, ...RESTRICTED])).toEqual([...OPEN_TO_ALL, ...RESTRICTED]);
  });

  it("while the person is still loading only the screens open to all are offered", () => {
    expect(opens([], [...OPEN_TO_ALL, ...RESTRICTED])).toEqual(OPEN_TO_ALL);
  });
});

describe("which widgets a role can use", () => {
  const SOURCES_NEEDS: Role[] = ["admin"];
  const ALERTS_NEEDS: Role[] = ["curator", "admin"];

  it("a viewer uses none of the widgets that read sources, quarantine or alerts", () => {
    expect(mayUseWidget(["viewer"], SOURCES_NEEDS)).toBe(false);
    expect(mayUseWidget(["viewer"], ALERTS_NEEDS)).toBe(false);
  });

  it("a curator uses the alerts widget", () => {
    expect(mayUseWidget(["curator"], ALERTS_NEEDS)).toBe(true);
  });

  it("an administrator uses all of them", () => {
    expect(mayUseWidget(["admin"], SOURCES_NEEDS) && mayUseWidget(["admin"], ALERTS_NEEDS)).toBe(true);
  });

  it("a widget that needs nothing is used by anyone", () => {
    expect(mayUseWidget([], undefined)).toBe(true);
  });
});
