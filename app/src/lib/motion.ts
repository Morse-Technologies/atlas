/** True when the person asked the OS for less motion. Every JS animation checks this; CSS uses the media query. */
export const reduced = () => typeof window !== "undefined" && matchMedia("(prefers-reduced-motion: reduce)").matches;
