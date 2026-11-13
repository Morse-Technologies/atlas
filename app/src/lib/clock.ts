import { demo } from "@/lib/demo";

/** The current time: the real one, or the demo's clock that starts at the fixture snapshot. */
export const clockNow = () => demo?.clockNow() ?? new Date();
