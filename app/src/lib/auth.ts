import { HOME_PATH } from "@/lib/edition";

/** The server sets this cookie (HttpOnly) when sign-in completes; the demo adapter sets a plain one. */
export const SESSION_COOKIE = "atlas_session";
export const SESSION_MAX_AGE_S = 12 * 60 * 60; // one work shift

/** Same-site path to land on after sign-in; "//host" and absolute URLs fall back to home. */
export const safeNext = (next: string | null) =>
  next?.startsWith("/") && !next.startsWith("//") && !next.startsWith("/\\") ? next : HOME_PATH;
