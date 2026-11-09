"use client";
import { gsap } from "gsap";
import { useLayoutEffect, useRef, type ReactNode } from "react";
import { reduced } from "@/lib/motion";

const NUM = /\d[\d,]*(?:\.\d+)?/;
const group = new Intl.NumberFormat("en-GB");

/** Seconds to count: small figures take their time, big ones are quick (85 takes about 1 s, 20,000 about 0.5 s). */
const countDuration = (n: number) => Math.min(1.5, Math.max(0.45, 1.6 - 0.28 * Math.log10(Math.abs(n) + 1)));

/**
 * A figure that counts up from 0 to its value the first time it shows, and from the old value when it changes.
 * Strings keep their words and units ("85 %", "4.1 s", "4 of 6"): only the first number moves.
 * The final text is always in the DOM (screen readers and no-JS read it); the counting copy is aria-hidden.
 */
export function CountUp({ value, className }: { value: ReactNode; className?: string }) {
  const el = useRef<HTMLSpanElement>(null);
  const last = useRef(0);
  const text = typeof value === "number" ? group.format(value) : typeof value === "string" ? value : null;

  useLayoutEffect(() => {
    const node = el.current;
    const m = text?.match(NUM);
    if (!node || !text || !m || reduced()) return;
    const to = parseFloat(m[0].replace(/,/g, ""));
    const decimals = m[0].includes(".") ? m[0].split(".")[1].length : 0;
    const from = last.current;
    const pre = text.slice(0, m.index), post = text.slice((m.index ?? 0) + m[0].length);
    const state = { v: from };
    const draw = () => {
      const v = decimals ? state.v.toFixed(decimals) : group.format(Math.round(state.v));
      node.textContent = pre + v + post;
    };
    draw();
    const tween = gsap.to(state, { v: to, duration: countDuration(to - from), ease: "power3.out", onUpdate: draw, onComplete: () => { node.textContent = text; last.current = to; } });
    return () => { tween.kill(); last.current = to; node.textContent = text; };
  }, [text]);

  if (text === null) return <>{value}</>;
  return (
    <>
      <span ref={el} aria-hidden className={className}>{text}</span>
      <span className="sr-only">{text}</span>
    </>
  );
}
