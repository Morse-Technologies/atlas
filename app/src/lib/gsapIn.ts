"use client";
import { gsap } from "gsap";
import { Flip } from "gsap/Flip";
import { useEffect, useLayoutEffect, useRef, type RefObject } from "react";

import { reduced } from "@/lib/motion";

gsap.registerPlugin(Flip);
export { Flip };


/** Bars grow from their baseline, one after another. `axis` y grows upward, x grows rightward. */
export function useGrow(ref: RefObject<HTMLElement | null>, bars: { selector: string; axis: "x" | "y" }) {
  const { selector, axis } = bars;
  useLayoutEffect(() => {
    const root = ref.current;
    if (!root || reduced()) return;
    const ctx = gsap.context(() => {
      gsap.from(root.querySelectorAll(selector), {
        [axis === "y" ? "scaleY" : "scaleX"]: 0, transformOrigin: axis === "y" ? "50% 100%" : "0% 50%",
        duration: 0.8, ease: "power3.out", stagger: 0.03, clearProps: "transform",
      });
    }, root);
    return () => ctx.revert();
  }, [ref, selector, axis]);
}

/** A screen's parts rise into place, staggered, once when it mounts. */
export function useEnter(ref: RefObject<HTMLElement | null>) {
  useLayoutEffect(() => {
    const root = ref.current;
    if (!root || reduced()) return;
    const ctx = gsap.context(() => {
      gsap.from(Array.from(root.children), { y: 14, opacity: 0, duration: 0.7, ease: "power3.out", stagger: 0.06, clearProps: "transform,opacity" });
    }, root);
    return () => ctx.revert();
  }, [ref]);
}

/**
 * A highlight that glides to the active item. `measure` returns the target box relative to the pill's
 * container; the first placement is instant, later ones ease.
 */
export function useGlide(pill: RefObject<HTMLElement | null>, measure: () => { x?: number; y?: number; width?: number; height?: number } | null, deps: unknown[]) {
  const placed = useRef(false);
  useLayoutEffect(() => {
    const el = pill.current;
    const box = measure();
    if (!el || !box) return;
    if (!placed.current || reduced()) { gsap.set(el, { ...box, opacity: 1 }); placed.current = true; return; }
    gsap.to(el, { ...box, opacity: 1, duration: 0.45, ease: "power3.out", overwrite: "auto" });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}

/** The content of a primary button leans toward the pointer (about 4px) and springs back on leave. Pointer devices only. */
export function useMagnetic(button: RefObject<HTMLElement | null>, inner: RefObject<HTMLElement | null>, enabled: boolean) {
  useEffect(() => {
    const b = button.current, el = inner.current;
    if (!enabled || !b || !el || reduced() || !matchMedia("(hover: hover)").matches) return;
    const xTo = gsap.quickTo(el, "x", { duration: 0.35, ease: "power3.out" });
    const yTo = gsap.quickTo(el, "y", { duration: 0.35, ease: "power3.out" });
    const move = (e: PointerEvent) => {
      const r = b.getBoundingClientRect();
      xTo(((e.clientX - r.left) / r.width - 0.5) * 8);
      yTo(((e.clientY - r.top) / r.height - 0.5) * 5);
    };
    const leave = () => gsap.to(el, { x: 0, y: 0, duration: 0.7, ease: "elastic.out(1, 0.5)", overwrite: "auto" });
    b.addEventListener("pointermove", move);
    b.addEventListener("pointerleave", leave);
    return () => { b.removeEventListener("pointermove", move); b.removeEventListener("pointerleave", leave); gsap.set(el, { clearProps: "transform" }); };
  }, [button, inner, enabled]);
}

/** Scale pop when a number goes up (a new alert on the badge). */
export function usePopOnIncrease(el: RefObject<HTMLElement | null>, value: number | undefined) {
  const prev = useRef(value);
  useLayoutEffect(() => {
    const node = el.current;
    if (node && value !== undefined && prev.current !== undefined && value > prev.current && !reduced()) {
      gsap.fromTo(node, { scale: 0.6 }, { scale: 1, duration: 0.5, ease: "back.out(3)" });
    }
    prev.current = value;
  }, [el, value]);
}
