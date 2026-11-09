// The wire format is snake_case JSON; the app uses camelCase (types.ts). These two functions convert the keys of
// whole payloads, and the types below let the compiler follow the same rule (see contract.ts).

/** Objects whose keys are data (domain attributes, counts per band), not field names: left as they are. */
const OPAQUE_WIRE = new Set(["attributes", "by_band", "checks"]);
const OPAQUE_APP = new Set(["attributes", "byBand", "checks"]);

const toCamel = (key: string) => key.replace(/_([a-z0-9])/g, (_, c: string) => c.toUpperCase());
const toSnake = (key: string) => key.replace(/[A-Z]/g, (c) => `_${c.toLowerCase()}`);

function convert(value: unknown, rename: (k: string) => string, opaque: Set<string>): unknown {
  if (Array.isArray(value)) return value.map((v) => convert(v, rename, opaque));
  if (value === null || typeof value !== "object") return value;
  return Object.fromEntries(
    Object.entries(value).map(([k, v]) => [rename(k), opaque.has(k) ? v : convert(v, rename, opaque)]),
  );
}

/** Server payload to app shape. */
export const camelizeKeys = <T,>(value: unknown): T => convert(value, toCamel, OPAQUE_WIRE) as T;

/** App request body to wire shape. */
export const snakeizeKeys = (value: unknown): unknown => convert(value, toSnake, OPAQUE_APP);

type CamelCase<S extends string> = S extends `${infer Head}_${infer Tail}` ? `${Head}${Capitalize<CamelCase<Tail>>}` : S;

/** The type of a wire value after camelizeKeys. */
export type Camelize<T> = T extends readonly (infer U)[]
  ? Camelize<U>[]
  : T extends object
    ? { [K in keyof T as CamelCase<K & string>]: K extends "attributes" | "by_band" | "checks" ? T[K] : Camelize<T[K]> }
    : T;
