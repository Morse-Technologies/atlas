import { expect, test } from "@playwright/test";
import { totp } from "./totp";

// RFC 6238 appendix B: the secret "12345678901234567890" is GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ in base32.
test("the one-time code matches the RFC 6238 test vectors", () => {
  const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ";
  expect(totp(secret, 59_000)).toBe("287082");
  expect(totp(secret, 1_111_111_109_000)).toBe("081804");
  expect(totp(secret, 2_000_000_000_000)).toBe("279037");
});
