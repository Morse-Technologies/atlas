import { httpApi } from "./http";
import { mockApi, setPreviewPlan, DEMO_ACCOUNT } from "./mock";
import type { AtlasApi } from "./types";

/**
 * `pnpm dev` runs on demo data and needs no server. The Docker image is built with NEXT_PUBLIC_ATLAS_API=live
 * and talks to the Go server through Caddy (deploy/compose).
 */
export const isDemoApi = process.env.NEXT_PUBLIC_ATLAS_API !== "live";
export const api: AtlasApi = isDemoApi ? mockApi : httpApi;
export type * from "./types";
export { alertChanges } from "./types";
export { ApiError } from "./http";
// The demo account and the Core/Pro switch exist only in the mock.
export { DEMO_ACCOUNT, setPreviewPlan };
