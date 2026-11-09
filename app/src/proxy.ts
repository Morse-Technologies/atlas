import { NextResponse, type NextRequest } from "next/server";
import { SESSION_COOKIE } from "@/lib/auth";
import { HOME_PATH } from "@/lib/edition";

const PUBLIC_PATHS = ["/login", "/setup", "/privacy"];

// Optimistic gate: no session cookie, no app screens. It only checks that the cookie exists; the API validates it
// on every request and answers 401 if it is wrong, which sends the browser back to /login (lib/api/http.ts).
export function proxy(request: NextRequest) {
  // ATLAS_AUTH_OFF=1 skips the gate (screenshots and automated checks only).
  if (process.env.ATLAS_AUTH_OFF === "1") return NextResponse.next();
  const signedIn = request.cookies.has(SESSION_COOKIE);
  const { pathname, search } = request.nextUrl;
  const isPublic = PUBLIC_PATHS.includes(pathname);
  if (!signedIn && !isPublic) {
    const url = new URL("/login", request.url);
    if (pathname !== "/") url.searchParams.set("next", pathname + search);
    return NextResponse.redirect(url);
  }
  if (signedIn && pathname === "/login") return NextResponse.redirect(new URL(HOME_PATH, request.url));
  return NextResponse.next();
}

export const config = {
  matcher: ["/((?!_next|api|samples|.*\\.(?:svg|png|ico|pdf|txt)$).*)"],
};
