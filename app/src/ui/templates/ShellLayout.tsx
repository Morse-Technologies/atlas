"use client";
import { useRef } from "react";
import { useEnter } from "@/lib/gsapIn";
import type { ReactNode } from "react";

/**
 * Rail on the ground, the working sheet inset beside it. On a phone the rail becomes a bottom bar
 * (rendered by the rail organism) and the sheet runs edge to edge.
 */
export function ShellLayout({ rail, topbar, children, bottomBar }: {
  rail: ReactNode; topbar: ReactNode; children: ReactNode; bottomBar?: ReactNode;
}) {
  return (
    <div className="flex min-h-dvh bg-ground">
      {rail}
      <div className="flex min-w-0 flex-1 flex-col md:py-2 md:pr-2">
        <div className="flex min-h-[calc(100dvh-16px)] flex-1 flex-col bg-surface md:rounded-card md:border md:border-border">
          {topbar}
          <main id="main" className="content-zoom flex min-h-0 flex-1 flex-col pb-16 md:pb-0">{children}</main>
        </div>
      </div>
      {bottomBar}
    </div>
  );
}

/** The slim bar at the top of the sheet: scope on the left, the page's own tools on the right. */
export function TopBar({ scope, children }: { scope: ReactNode; children?: ReactNode }) {
  return (
    <div className="flex h-12 flex-none items-center justify-between gap-4 border-b border-rule px-[max(16px,env(safe-area-inset-left))] pt-[env(safe-area-inset-top)] box-content md:h-12 md:px-6 md:pt-0">
      <div className="flex min-w-0 items-center gap-2 text-body text-muted">{scope}</div>
      <div className="flex items-center gap-2">{children}</div>
    </div>
  );
}

/** Standard page canvas inside the sheet: one gutter, one left edge. */
export function Canvas({ children, className = "" }: { children: ReactNode; className?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  useEnter(ref);
  return <div ref={ref} className={`mx-auto w-full max-w-[1280px] px-4 py-6 md:px-8 md:py-7 ${className}`}>{children}</div>;
}

/** List and detail side by side; on narrow screens the detail stacks under (or the screen pushes it in a sheet). */
export function SplitLayout({ list, detail, listWidth = "md:w-[300px] lg:w-[384px]" }: { list: ReactNode; detail: ReactNode; listWidth?: string }) {
  return (
    <div className="flex min-h-0 flex-1 flex-col md:flex-row">
      <div className={`flex min-h-0 flex-col border-border bg-ground md:flex-none md:border-r ${listWidth}`}>{list}</div>
      <div className="min-h-0 min-w-0 flex-1">{detail}</div>
    </div>
  );
}
