import type { AtlasApi } from "@/lib/api/types";

/** Everything that exists only in the demo build. The real build swaps the whole kit for `null` (see next.config.ts). */
export interface DemoKit {
  clockNow(): Date;
  api: AtlasApi;
  /** The one account of the demo; real accounts exist only with the server. */
  account: { email: string; password: string; code: string };
  /** Switches the demo license between Atlas Core and Atlas Pro. */
  setPreviewPlan(plan: "core" | "pro"): void;
  /** Example questions of the empty assistant. */
  suggestions: string[];
  /** Name of the fictional organization shown next to the mark. */
  brand: string;
  /** Why most documents of the demo have no preview. */
  previewNote: string;
}
