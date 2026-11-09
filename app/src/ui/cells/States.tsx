import type { ReactNode } from "react";
import { AlertTriangle } from "lucide-react";
import { cn } from "@/lib/cn";
import { Button, Skeleton } from "@/ui/atoms";

/** An empty block keeps its frame and says one sentence, plus the next step if there is one. */
export function EmptyState({ children, action, className }: { children: ReactNode; action?: ReactNode; className?: string }) {
  return (
    <div className={cn("col-span-full flex min-h-10 flex-wrap items-center justify-between gap-3 rounded-card border border-dashed border-control px-4 py-3 text-body text-muted", className)}>
      <span>{children}</span>
      {action}
    </div>
  );
}

/** An error lives inside the block that failed: what failed, and a retry. */
export function ErrorState({ what, onRetry, className }: { what: string; onRetry?: () => void; className?: string }) {
  return (
    <div role="alert" className={cn("flex flex-wrap items-center gap-3 rounded-card bg-[var(--pill-negative-bg)] px-4 py-3 text-body text-[var(--pill-negative-ink)]", className)}>
      <AlertTriangle aria-hidden className="size-4 flex-none" />
      <span className="flex-1">Couldn&apos;t load {what}. Check that the Atlas server is reachable.</span>
      {onRetry && <Button size="sm" variant="outline" onClick={onRetry}>Try again</Button>}
    </div>
  );
}

/** Loading keeps the final layout: flat bars at their real size, rows at their real pitch. */
export function LoadingRows({ rows = 5, pitch = "h-12", className }: { rows?: number; pitch?: string; className?: string }) {
  return (
    <div aria-busy="true" aria-label="Loading" className={cn("divide-y divide-rule", className)}>
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className={cn("flex items-center gap-4", pitch)}>
          <Skeleton className="h-3.5 w-24" />
          <Skeleton className="h-3.5 flex-1 max-w-80" />
          <Skeleton className="ml-auto h-3.5 w-16" />
        </div>
      ))}
    </div>
  );
}
