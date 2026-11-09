import Link from "next/link";
import type { ReactNode } from "react";
import { isDemoApi } from "@/lib/api";
import { Halftone, Mark } from "@/ui/atoms";

/** Sign-in and setup: the form on the left, a piece of the real product on the right (hidden on phones). */
export function AuthLayout({ children, aside }: { children: ReactNode; aside?: ReactNode }) {
  return (
    <div className="grid min-h-dvh bg-ground lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
      <main id="main" className="flex flex-col px-5 py-8 sm:px-10 lg:px-16">
        <div className="flex items-center gap-2 text-text">
          <Mark className="size-7" />
          <span className="text-section font-medium">Atlas</span>
          {isDemoApi && <span className="text-body text-muted">Planta Norte</span>}
        </div>
        <div className="flex flex-1 flex-col items-center justify-center py-10">
          <div className="w-full max-w-[440px]">{children}</div>
        </div>
        <p className="text-small text-muted">Runs on your own server. Documents never leave it. <Link href="/privacy" className="underline hover:text-text">Privacy</Link></p>
      </main>
      {aside && (
        <aside className="relative hidden border-l border-border lg:block">
          <Halftone />
          <div className="absolute right-10 bottom-10 left-10 max-w-[420px] animate-enter rounded-card bg-surface px-5 py-4 text-body [animation-delay:200ms]">{aside}</div>
        </aside>
      )}
    </div>
  );
}
