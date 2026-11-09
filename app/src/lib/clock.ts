import { NOW } from "@/lib/mocks/plant";

// Demo clock: starts at the fixture snapshot (NOW) and advances in real time from page load,
// so "4 min ago" in the fixtures reads as designed whatever the real date is.
const startedAt = Date.now();
export const clockNow = () => new Date(NOW.getTime() + (Date.now() - startedAt));
