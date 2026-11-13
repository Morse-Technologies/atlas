import { expect, test } from "@playwright/test";
import { freshTotp, totp } from "./totp";

// RFC 6238 appendix B: the secret "12345678901234567890" is GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ in base32.
test("the one-time code matches the RFC 6238 test vectors", () => {
  const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ";
  expect(totp(secret, 59_000)).toBe("287082");
  expect(totp(secret, 1_111_111_109_000)).toBe("081804");
  expect(totp(secret, 2_000_000_000_000)).toBe("279037");
});

test("a second code in the same 30 s step waits for the next step", async () => {
  const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ";
  let now = 1_000_000_000_000;
  const pause = async (ms: number) => { now += ms * 20; }; // each wait jumps ahead, so the step changes
  const first = await freshTotp(secret, () => now, pause);
  const startOfSecond = now;
  const second = await freshTotp(secret, () => now, pause);
  expect(first).toBe(totp(secret, 1_000_000_000_000));
  expect(now).toBeGreaterThan(startOfSecond);
  expect(second).toBe(totp(secret, now));
  expect(second).not.toBe(first);
});
