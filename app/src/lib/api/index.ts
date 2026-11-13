import { demo } from "@/lib/demo";
import { httpApi } from "./http";
import type { AtlasApi } from "./types";

/**
 * `pnpm dev` runs on demo data and needs no server. The Docker image is built with NEXT_PUBLIC_ATLAS_API=live
 * and talks to the Go server through Caddy (deploy/compose); that build has no demo kit at all.
 */
export const isDemoApi = demo !== null;
export const api: AtlasApi = demo?.api ?? httpApi;
export type * from "./types";
export { alertChanges } from "./types";
export { ApiError } from "./http";
