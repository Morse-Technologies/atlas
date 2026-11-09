import { defineConfig } from "@playwright/test";

// The browser journey runs against a real stack: Caddy at https://localhost, the Go server behind it and the fake
// model servers (deploy/scripts/e2e.sh starts it empty). Caddy signs with its own CA, hence ignoreHTTPSErrors.
export default defineConfig({
  testDir: "./e2e",
  timeout: 5 * 60_000,
  expect: { timeout: 15_000 },
  workers: 1, // the steps share one installation, in order
  fullyParallel: false,
  reporter: [["list"]],
  use: {
    baseURL: process.env.ATLAS_E2E_URL ?? "https://localhost",
    ignoreHTTPSErrors: true,
    actionTimeout: 15_000,
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
});
