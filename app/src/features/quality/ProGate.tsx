import Link from "next/link";
import { Lock } from "lucide-react";
import { buttonStyles } from "@/ui/atoms";
import { PageHeader } from "@/ui/cells";
import { Canvas } from "@/ui/templates/ShellLayout";

/** A Pro screen without a license: say what it does and where to turn it on. The backend enforces it too. */
export function ProGate({ title, what }: { title: string; what: string }) {
  return (
    <Canvas>
      <PageHeader title={title} subline="Part of Atlas Pro." />
      <div className="mt-6 flex max-w-2xl gap-4 rounded-card bg-raised p-6">
        <Lock aria-hidden className="mt-0.5 size-5 flex-none text-muted" />
        <div>
          <p className="text-section">{what}</p>
          <p className="mt-2 text-body text-muted">This installation runs Atlas Core. An administrator can add a Pro license file in Settings; nothing is sent outside the plant to check it.</p>
          <Link href="/settings" className={`${buttonStyles({ variant: "primary" })} mt-5`}>Open Settings</Link>
        </div>
      </div>
    </Canvas>
  );
}
