import { ArrowLeft, ArrowRight } from "lucide-react";
import type { ReactNode } from "react";
import type { BandKind } from "@/lib/api/types";
import { cn } from "@/lib/cn";

/**
 * The signature mark: a pipe-marking band. Its colour names the kind of source (a fixed four-colour code),
 * its arrow points back to where the evidence comes from.
 */
export const bandBg: Record<BandKind, string> = {
  procedure: "bg-band-procedure",
  manual: "bg-band-manual",
  norm: "bg-band-norm",
  erp: "bg-band-erp",
};
export const bandLabel: Record<BandKind, string> = {
  procedure: "Procedures",
  manual: "Manuals and records",
  norm: "Norms",
  erp: "ERP",
};

type BandProps = {
  kind: BandKind | "muted";
  children: ReactNode;
  size?: "xs" | "md" | "lg";
  arrow?: boolean;
  /** "back" points to the evidence's origin; "forward" shows flow into the index. */
  dir?: "back" | "forward";
  active?: boolean;
  className?: string;
};

export function Band({ kind, children, size = "md", arrow = true, dir = "back", active, className }: BandProps) {
  const Arrow = dir === "forward" ? ArrowRight : ArrowLeft;
  return (
    <span
      className={cn(
        "flex min-w-0 items-stretch overflow-hidden rounded-[4px]",
        kind === "muted" ? "bg-active text-muted" : cn("text-white", bandBg[kind]),
        size === "lg" ? "h-11 text-body" : size === "xs" ? "h-5 text-[11px]" : "h-7 text-small",
        "transition-[transform,box-shadow] duration-[var(--dur-2)] ease-out",
        active && "-translate-x-1 ring-2 ring-accent ring-offset-2 ring-offset-surface",
        className,
      )}
    >
      <span className={cn("flex min-w-0 flex-1 items-center gap-2 font-bold num", size === "xs" ? "px-1.5" : "px-3")}>
        <span className="truncate">{children}</span>
      </span>
      {arrow && (
        <span aria-hidden className={cn("grid place-items-center bg-black/18", size === "lg" ? "w-11" : "w-7", dir === "forward" && "order-last")}>
          <Arrow className={size === "lg" ? "size-[18px]" : "size-3.5"} strokeWidth={2.25} />
        </span>
      )}
    </span>
  );
}

/** Inline citation marker [n] in the band colour of its source. */
export function CiteMark({ n, kind, active }: { n: number; kind: BandKind; active?: boolean }) {
  return (
    <span
      className={cn(
        "mx-0.5 inline-flex h-[18px] min-w-[18px] translate-y-[-1px] items-center justify-center rounded-[3px] px-1",
        "align-middle text-[11px] font-bold text-white num transition-transform duration-[var(--dur-1)] ease-out",
        bandBg[kind],
        active && "scale-110",
      )}
    >
      {n}
    </span>
  );
}

/** A small swatch used in legends and tables. */
export function BandSwatch({ kind, className }: { kind: BandKind; className?: string }) {
  return <span aria-hidden className={cn("inline-block size-2.5 flex-none rounded-[2px]", bandBg[kind], className)} />;
}
