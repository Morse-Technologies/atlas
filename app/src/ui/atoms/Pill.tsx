import type { ReactNode } from "react";
import { cn } from "@/lib/cn";

export type Tone = "neutral" | "positive" | "negative" | "warning" | "accent";

const tones: Record<Tone, string> = {
  neutral: "bg-[var(--pill-neutral-bg)] text-[var(--pill-neutral-ink)]",
  positive: "bg-[var(--pill-positive-bg)] text-[var(--pill-positive-ink)]",
  negative: "bg-[var(--pill-negative-bg)] text-[var(--pill-negative-ink)]",
  warning: "bg-[var(--pill-warning-bg)] text-[var(--pill-warning-ink)] ring-1 ring-inset ring-[var(--pill-warning-line)]",
  accent: "bg-accent-soft text-accent",
};

/** A status chip: pale tint, dark ink of the same hue. Never squeezed. */
export function Pill({ tone = "neutral", children, className }: { tone?: Tone; children: ReactNode; className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex h-5 flex-none items-center gap-1 rounded-full px-2 text-small font-medium whitespace-nowrap",
        tones[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}

export function Dot({ tone = "neutral", className }: { tone?: Tone; className?: string }) {
  const color = {
    neutral: "bg-faint",
    positive: "bg-positive",
    negative: "bg-negative",
    warning: "bg-[var(--pill-warning-ink)]",
    accent: "bg-accent",
  }[tone];
  return <span aria-hidden className={cn("inline-block size-1.5 flex-none rounded-full", color, className)} />;
}

export function Kbd({ children }: { children: ReactNode }) {
  return (
    <kbd className="inline-flex h-5 min-w-5 items-center justify-center rounded-[4px] border border-border bg-raised px-1 font-sans text-small text-muted">
      {children}
    </kbd>
  );
}
