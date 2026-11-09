import type { ReactNode } from "react";
import { cn } from "@/lib/cn";

/** The title row: a noun h1, one sentence carrying the finding, and real actions only. */
export function PageHeader({
  title,
  subline,
  actions,
  className,
}: {
  title: ReactNode;
  subline?: ReactNode;
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <header className={cn("flex flex-wrap items-start justify-between gap-x-6 gap-y-3", className)}>
      <div className="min-w-0 max-w-3xl">
        <h1 className="text-title font-medium tracking-[-0.01em]">{title}</h1>
        {subline && <p className="mt-1 text-body text-muted">{subline}</p>}
      </div>
      {actions && <div className="flex max-w-full flex-wrap items-center gap-2">{actions}</div>}
    </header>
  );
}

/**
 * A titled block: header, a rule, then the content. `detail` is the smaller variant used inside a
 * detail pane (h3, tighter padding); `flush` drops the body padding for tables that bleed to the edge.
 */
export function Card({ id, title, aside, className, flush, detail, children }: {
  id: string; title: string; aside?: string; className?: string; flush?: boolean; detail?: boolean; children: ReactNode;
}) {
  const Heading = detail ? "h3" : "h2";
  return (
    <section aria-labelledby={id} className={cn("min-w-0 rounded-card border border-border bg-surface", className)}>
      <header className={cn("flex items-baseline justify-between gap-3 border-b border-rule", detail ? "px-4 py-3" : "px-5 py-3.5")}>
        <Heading id={id} className={detail ? "text-body font-medium" : "text-section font-medium"}>{title}</Heading>
        {aside && <span className="text-small text-muted">{aside}</span>}
      </header>
      <div className={flush ? "" : detail ? "px-4 py-4" : "px-5 pb-2"}>{children}</div>
    </section>
  );
}
