import { expect, test } from "@playwright/test";
import { signInAsAdmin } from "./admin";

// A phone-sized smoke test. It runs after journey.spec.ts, which leaves the installation set up and indexed.
test.use({ viewport: { width: 390, height: 844 } });

test.describe.serial("Atlas on a phone", () => {
  test("the administrator signs in, asks a question and opens the documents", async ({ page }) => {
    await signInAsAdmin(page);

    await page.goto("/ask");
    await page.locator("#ask-input").fill("¿Cómo se hace el montaje del husillo?");
    await page.getByRole("button", { name: "Ask", exact: true }).click();
    await expect(page.getByText("According to the document").first()).toBeVisible();

    await page.goto("/documents");
    // On a phone the table gives way to a list: only a link by role skips the hidden table cells.
    await expect(page.getByRole("link", { name: /PNT-014-rev3/ })).toBeVisible();
  });
});
