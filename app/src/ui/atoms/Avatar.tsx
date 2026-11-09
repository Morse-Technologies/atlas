import { cn } from "@/lib/cn";

const initials = (name: string) =>
  name.split(/\s+/).filter(Boolean).slice(0, 2).map((p) => p[0]!.toUpperCase()).join("");

/** A person's initials on a neutral disc: identity, never a colour that could read as data. `hue` is accepted and ignored. */
export function Avatar({ name, size = 24, className }: { name: string; hue?: number; size?: 20 | 24 | 32; className?: string }) {
  return (
    <span
      aria-hidden
      style={{ width: size, height: size }}
      className={cn(
        "inline-flex flex-none items-center justify-center rounded-full font-medium",
        "bg-active text-text",
        size === 32 ? "text-body" : "text-[11px]",
        className,
      )}
    >
      {initials(name)}
    </span>
  );
}

/** The Atlas mark, using the exact geometry of design/public/atlas-icon.svg. Outer A in ink (follows the theme), inner A in the brand teal. */
export const MARK_PATHS = ["M1047 2341.261 L1934.555 2341.261 M1761.975 1818.337 L2088.043 2808.033 L2808.023 2808.033 L1872.84 288.168 L1152.661 288.168 L215.797 2808.033 L936.02 2808.033 L1226.624 1818.337 M1761.975 1818.337 L1226.624 1818.337 M1226.624 1818.337 L1497.822 1018.157 L1761.975 1818.337", "M1227.97 2212.553 L1759.241 2212.553 M1655.938 1871.71 L1851.115 2451.087 L2282.079 2520.087 L1722.299 874.342 L1291.216 874.342 L730.43 2520.087 L1161.539 2451.087 L1335.489 1871.71 M1655.938 1871.71 L1335.489 1871.71 M1335.489 1871.71 L1497.821 1350.15 L1655.938 1871.71"];

export function Mark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 3024 3000" aria-hidden className={cn("size-7", className)}>
      <path fill="currentColor" d={MARK_PATHS[0]} />
      <path fill="var(--accent)" d={MARK_PATHS[1]} />
    </svg>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return <span aria-hidden className={cn("block rounded-[4px] bg-active", className)} />;
}

export function Spinner({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 16 16" aria-hidden className={cn("size-4 animate-spin text-muted [animation-duration:900ms]", className)}>
      <circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" strokeOpacity=".25" strokeWidth="2" />
      <path d="M14 8a6 6 0 0 0-6-6" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  );
}
