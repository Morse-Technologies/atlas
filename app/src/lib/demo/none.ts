import type { DemoKit } from "./types";

// El build real sustituye "@/lib/demo" por este módulo: nada de la demo llega al navegador.
export const demo: DemoKit | null = null;
