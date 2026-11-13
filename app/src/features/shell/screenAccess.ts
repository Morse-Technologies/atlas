import type { Role } from "@/lib/api/types";

export const hasAnyRole = (roles: readonly Role[], needed: readonly Role[]): boolean => needed.some((r) => roles.includes(r));

/** A widget without `needs` is for everyone. */
export const mayUseWidget = (roles: readonly Role[], needs?: readonly Role[]): boolean => !needs || hasAnyRole(roles, needs);

/** The roles that may open each restricted screen, from the `x-atlas-role` of the operation that loads it. A screen not listed is open to everyone. */
export const SCREEN_ROLES: Readonly<Record<string, readonly Role[]>> = {
  "/alerts": ["curator", "admin"],
  "/sources": ["editor_metadata", "admin"],
  "/audit": ["auditor", "admin"],
  "/users": ["admin"],
  "/settings": ["admin"],
};

export function canOpenScreen(roles: readonly Role[], href: string): boolean {
  const needed = SCREEN_ROLES[href];
  return !needed || hasAnyRole(roles, needed);
}
