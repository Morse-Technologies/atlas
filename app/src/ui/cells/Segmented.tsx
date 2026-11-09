"use client";
import { useRef, type KeyboardEvent } from "react";
import { useGlide } from "@/lib/gsapIn";
import { cn } from "@/lib/cn";

type Option<T extends string> = { value: T; label: string; count?: number };

/** A segmented radio group. The white pill slides between options on the move curve. */
export function Segmented<T extends string>({
  options,
  value,
  onChange,
  label,
  className,
}: {
  options: Option<T>[];
  value: T;
  onChange: (v: T) => void;
  label: string;
  className?: string;
}) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const pill = useRef<HTMLSpanElement>(null);
  const index = options.findIndex((o) => o.value === value);
  useGlide(pill, () => {
    const el = refs.current[index];
    return el ? { x: el.offsetLeft, width: el.offsetWidth } : null;
  }, [index, options]);

  const onKey = (e: KeyboardEvent) => {
    if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
    e.preventDefault();
    const next = (index + (e.key === "ArrowRight" ? 1 : -1) + options.length) % options.length;
    onChange(options[next].value);
    refs.current[next]?.focus();
  };

  return (
    <div role="radiogroup" aria-label={label} onKeyDown={onKey} className={cn("relative inline-flex h-8 rounded-control bg-active p-0.5", className)}>
      <span ref={pill} aria-hidden className="absolute top-0.5 bottom-0.5 left-0 rounded-[5px] bg-surface opacity-0 shadow-[0_1px_2px_rgb(0_0_0/0.08)]" />
      {options.map((o, i) => (
        <button
          key={o.value}
          ref={(el) => { refs.current[i] = el; }}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          tabIndex={o.value === value ? 0 : -1}
          onClick={() => onChange(o.value)}
          className={cn(
            "relative z-10 inline-flex items-center gap-1.5 rounded-[5px] px-3 text-body whitespace-nowrap transition-colors duration-[var(--dur-1)]",
            o.value === value ? "text-text" : "text-muted hover:text-text",
          )}
        >
          {o.label}
          {o.count !== undefined && <span className="text-small text-muted num">{o.count}</span>}
        </button>
      ))}
    </div>
  );
}
