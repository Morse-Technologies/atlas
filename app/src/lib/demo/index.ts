import { mockApi, DEMO_ACCOUNT, setPreviewPlan } from "@/lib/api/mock";
import { settings, suggestions } from "@/lib/mocks/plant";
import { demoNow } from "./clock";
import type { DemoKit } from "./types";

export const demo: DemoKit | null = {
  clockNow: demoNow,
  api: mockApi,
  account: DEMO_ACCOUNT,
  setPreviewPlan,
  suggestions,
  brand: settings.orgName,
  previewNote: "The demo ships one synthetic PDF. In a real install, documents Atlas can render show here.",
};
