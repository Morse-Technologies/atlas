import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";
import { expect, type Page } from "@playwright/test";
import { waitForSignInSlot } from "./signInPace";
import { freshTotp } from "./totp";

/** The administrator the setup wizard creates; the specs that run after the journey sign in as them. */
export const ADMIN = { org: "Acme", name: "Ana Pérez", email: "ana@acme.test", password: "a-long-password-1" };

// Each spec file is loaded on its own, so the second-factor secret travels between them in a file.
const SECRET_FILE = "test-results/admin-secret.txt";

export function saveAdminSecret(secret: string) {
  mkdirSync(dirname(SECRET_FILE), { recursive: true });
  writeFileSync(SECRET_FILE, secret);
}

/** Signs the administrator in through the login screens, with the second-factor code the journey registered. */
export async function signInAsAdmin(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(ADMIN.email);
  await page.getByLabel("Password", { exact: true }).fill(ADMIN.password);
  await waitForSignInSlot();
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page.getByRole("heading", { name: "Enter your code" })).toBeVisible();
  await page.keyboard.type(await freshTotp(readFileSync(SECRET_FILE, "utf8")));
  await expect(page).toHaveURL(/\/(ask|documents|dashboard)$/);
}
