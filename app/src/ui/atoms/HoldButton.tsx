"use client";
import { gsap } from "gsap";
import { Check } from "lucide-react";
import { forwardRef, useCallback, useEffect, useId, useRef, useState, type ReactNode } from "react";
import { cn } from "@/lib/cn";
import { Button, type ButtonProps } from "./Button";
import { Spinner } from "./Avatar";
import { reduced } from "@/lib/motion";

type Phase = "idle" | "hint" | "holding" | "working" | "done";

/**
 * A button for actions that change documents: press and hold, a bar fills the button, then the label
 * becomes `doneLabel` and the background turns green. Letting go early retracts the bar and does nothing.
 * `after` runs `onConfirm` once the done animation has played (for actions that remove the button itself).
 */
export const HoldButton = forwardRef<HTMLButtonElement, Omit<ButtonProps, "onClick"> & {
  onConfirm: () => void | Promise<unknown>; doneLabel: string; holdMs?: number; after?: boolean; children: ReactNode;
}>(function HoldButton({ onConfirm, doneLabel, holdMs = 800, after = false, children, className, variant, size, disabled, "aria-label": ariaLabel, ...props }, ref) {
  const [phase, setPhase] = useState<Phase>("idle");
  const bar = useRef<HTMLSpanElement>(null);
  const tween = useRef<gsap.core.Tween | null>(null);
  const timers = useRef<number[]>([]);
  const doneText = useRef(doneLabel);
  const hintId = useId();
  const isIcon = size === "icon";

  const later = (fn: () => void, ms: number) => { timers.current.push(window.setTimeout(fn, ms)); };
  useEffect(() => () => { tween.current?.kill(); timers.current.forEach(clearTimeout); }, []);

  const finish = useCallback(async () => {
    const run = () => onConfirm();
    if (after) {
      setPhase("done");
      later(() => { void run(); }, 650);
      later(() => { setPhase("idle"); retract(0); }, 1800);
      return;
    }
    const r = run();
    if (r && typeof (r as Promise<unknown>).then === "function") {
      setPhase("working");
      try { await r; } catch { setPhase("idle"); retract(0.2); return; }
    }
    setPhase("done");
    later(() => { setPhase("idle"); retract(0); }, 1600);
  }, [onConfirm, after]);

  const retract = (d: number) => { tween.current?.kill(); tween.current = gsap.to(bar.current, { scaleX: 0, duration: d, ease: "power3.out" }); };

  const press = () => {
    if (disabled || (phase !== "idle" && phase !== "hint")) return;
    doneText.current = doneLabel; // the label belongs to the press: the action may flip what it would say next
    setPhase("holding");
    tween.current?.kill();
    const reduce = reduced();
    tween.current = gsap.fromTo(bar.current, { scaleX: 0 }, {
      scaleX: 1, duration: holdMs / 1000, ease: "none",
      // reduced motion keeps the hold but fills in steps
      ...(reduce ? { ease: "steps(5)" } : {}),
      onComplete: () => { void finish(); },
    });
  };
  const release = () => {
    if (phase !== "holding") return;
    const reached = tween.current?.progress() ?? 0;
    tween.current?.kill();
    retract(0.22);
    if (reached < 0.12) { setPhase("hint"); later(() => setPhase((p) => (p === "hint" ? "idle" : p)), 1300); } else setPhase("idle");
  };

  const done = phase === "done";
  const label = phase === "hint" ? "Hold to confirm" : done ? doneText.current : children;

  return (
    <>
      <Button
        ref={ref}
        {...props}
        variant={variant}
        magnetic={false}
        size={size}
        disabled={disabled || phase === "working"}
        aria-label={isIcon ? ariaLabel : undefined}
        aria-describedby={hintId}
        aria-busy={phase === "working" || undefined}
        data-phase={phase}
        onPointerDown={(e) => { if (e.button === 0) press(); }}
        onPointerUp={release}
        onPointerLeave={release}
        onPointerCancel={release}
        onKeyDown={(e) => { if ((e.key === " " || e.key === "Enter") && !e.repeat) { e.preventDefault(); press(); } }}
        onKeyUp={(e) => { if (e.key === " " || e.key === "Enter") release(); }}
        onBlur={release}
        onContextMenu={(e) => e.preventDefault()}
        className={cn(
          variant === "primary" && "[--hold-fill:rgb(255_255_255/0.24)]",
          "relative touch-none overflow-hidden select-none transition-[background-color,color,border-color,transform] duration-[var(--dur-2)] ease-out",
          done && "!border-transparent !bg-[var(--pill-positive-bg)] !text-[var(--pill-positive-ink)] disabled:!opacity-100",
          className,
        )}
      >
        {/* the bar: GSAP fills it while you hold; a darker tone of the button itself */}
        <span ref={bar} aria-hidden className="pointer-events-none absolute inset-0 origin-left scale-x-0 bg-[var(--hold-fill,rgb(14_18_16/0.16))]"
          style={{ opacity: done ? 0 : 1, transition: "opacity 200ms var(--ease-out)" }} />
        <span key={`${phase === "hint" ? "hint" : done ? "done" : "idle"}`} className="relative inline-flex items-center gap-2 animate-[token-in_200ms_var(--ease-out)_both]">
          {phase === "working" ? <Spinner /> : done ? <Check strokeWidth={2.4} /> : null}
          {isIcon && !done && phase !== "hint" ? children : isIcon ? <span className="sr-only">{done ? doneText.current : "Hold to confirm"}</span> : label}
        </span>
      </Button>
      <span id={hintId} className="sr-only">Press and hold for {(holdMs / 1000).toFixed(1)} seconds to confirm.</span>
      <span role="status" className="sr-only">{done ? doneText.current : ""}</span>
    </>
  );
});
