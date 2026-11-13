import { createHmac } from "node:crypto";

const STEP_SECONDS = 30;
const DIGITS = 6;
const BASE32 = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

function base32Decode(text: string): Buffer {
  let bits = "";
  for (const ch of text.replace(/=+$/, "").toUpperCase()) bits += BASE32.indexOf(ch).toString(2).padStart(5, "0");
  const bytes = bits.match(/.{8}/g) ?? [];
  return Buffer.from(bytes.map((b) => parseInt(b, 2)));
}

/** The code an authenticator app shows for `secret` (base32) at `at`: RFC 6238 with SHA-1, 30 s, 6 digits. */
export function totp(secret: string, at = Date.now()): string {
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 1000 / STEP_SECONDS)));
  const hmac = createHmac("sha1", base32Decode(secret)).update(counter).digest();
  const offset = hmac[hmac.length - 1] & 0x0f;
  const value = hmac.readUInt32BE(offset) & 0x7fffffff;
  return String(value % 10 ** DIGITS).padStart(DIGITS, "0");
}

const lastStepUsed = new Map<string, number>();
const stepOf = (at: number) => Math.floor(at / 1000 / STEP_SECONDS);
const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * A code the server has not seen yet. Atlas accepts each code once, so a second sign-in in the same
 * 30 s step waits for the next step instead of failing with a 401.
 */
export async function freshTotp(secret: string, clock = Date.now, pause = sleep): Promise<string> {
  while (stepOf(clock()) <= (lastStepUsed.get(secret) ?? -1)) await pause(250);
  lastStepUsed.set(secret, stepOf(clock()));
  return totp(secret, clock());
}
