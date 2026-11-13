import { NOW } from "@/lib/mocks/plant";

// Reloj de la demo: parte de la fecha de los datos de ejemplo (NOW) y avanza en tiempo real desde que carga la
// página, así "hace 4 min" se lee como se diseñó sea cual sea la fecha real.
const startedAt = Date.now();
export const demoNow = () => new Date(NOW.getTime() + (Date.now() - startedAt));
