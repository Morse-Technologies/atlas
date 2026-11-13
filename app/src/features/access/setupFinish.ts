import type { UploadResult } from "@/lib/api";

/** What the setup wizard tells the person after an upload: whether to go on, and the words. */
export function uploadOutcome(result: UploadResult): { ok: boolean; message: string } {
  const rejected = result.rejected.map((r) => `${r.name} (${r.reason})`).join(", ");
  const notAccepted = rejected ? ` Not accepted: ${rejected}.` : "";
  if (result.accepted === 0) return { ok: false, message: `No file was accepted.${notAccepted}` };
  const noun = result.accepted === 1 ? "file" : "files";
  return { ok: true, message: `Atlas is indexing ${result.accepted} ${noun}.${notAccepted}` };
}
