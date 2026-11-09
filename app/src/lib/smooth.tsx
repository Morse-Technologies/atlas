"use client";
import { gsap } from "gsap";
import Lenis from "lenis";
import { useEffect } from "react";
import { reduced } from "@/lib/motion";

/** Smooth scrolling for the page, driven by GSAP's ticker so every motion in the app shares one clock. Off under reduced motion. */
export function SmoothScroll() {
  useEffect(() => {
    if (reduced()) return;
    const lenis = new Lenis({ autoRaf: false, lerp: 0.11, wheelMultiplier: 0.95, allowNestedScroll: true });
    const tick = (t: number) => lenis.raf(t * 1000);
    gsap.ticker.add(tick);
    gsap.ticker.lagSmoothing(0);
    return () => { gsap.ticker.remove(tick); lenis.destroy(); };
  }, []);
  return null;
}
