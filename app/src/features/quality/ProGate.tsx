import Link from "next/link";
import { Lock } from "lucide-react";
import { useLicense } from "@/features/hooks";
import { ApiError, type ProFeature } from "@/lib/api";
import { clockNow } from "@/lib/clock";
import { buttonStyles } from "@/ui/atoms";
import { PageHeader } from "@/ui/cells";
import { Canvas } from "@/ui/templates/ShellLayout";
import { gateText } from "./gateReason";

/** True when the server refused a Pro request: the license state changed since the screen loaded. */
export const isForbidden = (error: unknown) => error instanceof ApiError && error.status === 403;

/** A Pro screen without its license: say what it does, why it is closed and where to turn it on. The backend enforces it too. */
export function ProGate({ title, what, feature }: { title: string; what: string; feature: ProFeature }) {
  const { data: license } = useLicense();
  return (
    <Canvas>
      <PageHeader title={title} subline="Part of Atlas Pro." />
      <div className="mt-6 flex max-w-2xl gap-4 rounded-card bg-raised p-6">
        <Lock aria-hidden className="mt-0.5 size-5 flex-none text-muted" />
        <div>
          <p className="text-section">{what}</p>
          <p className="mt-2 text-body text-muted">{license ? gateText(license, feature, clockNow()) : ""}</p>
          <Link href="/settings" className={`${buttonStyles({ variant: "primary" })} mt-5`}>Open Settings</Link>
        </div>
      </div>
    </Canvas>
  );
}
