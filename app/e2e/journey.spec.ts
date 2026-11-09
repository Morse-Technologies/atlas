import { expect, request, test, type Page } from "@playwright/test";
import { totp } from "./totp";

const ADMIN = { org: "Acme", name: "Ana Pérez", email: "ana@acme.test", password: "a-long-password-1" };
// `dev-env.sh --pro` starts the stack with a signed license; without it the Pro screens must be closed.
const PRO = process.env.ATLAS_E2E_EDITION === "pro";
const INDEXING = { timeout: 3 * 60_000, intervals: [3_000] };
let secret = "";

/** Types the current code. The first of the six boxes takes the focus by itself when the step opens. */
async function typeCode(page: Page) {
  await page.keyboard.type(totp(secret));
}

test.describe.serial("Atlas in a browser, from first setup to a cited answer", () => {
  // One browser for the whole journey: the session created by the first step is what the next ones use.
  let page: Page;
  test.beforeAll(async ({ browser, baseURL }) => {
    page = await (await browser.newContext({ baseURL, ignoreHTTPSErrors: true })).newPage();
  });
  test.afterAll(async () => { await page.context().close(); });

  test("the setup wizard creates the administrator and registers the first folder", async () => {
    await page.goto("/setup");
    await page.getByLabel("Organization").fill(ADMIN.org);
    await page.getByLabel("Full name").fill(ADMIN.name);
    await page.getByLabel("Work email").fill(ADMIN.email);
    await page.getByLabel("Password", { exact: true }).fill(ADMIN.password);
    await page.getByRole("button", { name: /Continue/ }).click();

    await expect(page.getByRole("heading", { name: "Add the second factor" })).toBeVisible();
    secret = ((await page.locator("code").first().textContent()) ?? "").trim();
    expect(secret).toMatch(/^[A-Z2-7]{16,}$/);
    await typeCode(page);

    await expect(page.getByRole("heading", { name: "Where are your documents?" })).toBeVisible();
    await page.getByLabel("Folder path").fill("/sources/docs");
    await page.getByRole("button", { name: "Check" }).click();
    await expect(page.getByText("Atlas can read this folder")).toBeVisible();
    await page.getByRole("button", { name: "Start indexing" }).click();
    await expect(page).toHaveURL(/\/sources$/);
  });

  test("the folder is read and its documents appear", async () => {
    await page.goto("/documents");
    await expect(async () => {
      await page.reload();
      await expect(page.getByText("PNT-014-rev3").first()).toBeVisible({ timeout: 2_000 });
    }).toPass(INDEXING);
  });

  test("a document opens with its PDF, loaded through Caddy", async () => {
    const file = page.waitForResponse((r) => /\/versions\/[^/]+\/file$/.test(r.url()) && [200, 206].includes(r.status()));
    await page.goto("/documents");
    await page.getByText("PNT-014-rev3").first().click();
    expect((await file).headers()["content-type"]).toBe("application/pdf");
    await expect(page.locator("canvas").first()).toBeVisible();
  });

  test("a question streams an answer with a source that opens at its page", async () => {
    await page.goto("/ask");
    await page.locator("#ask-input").fill("¿Cómo se hace el montaje del husillo?");
    await page.getByRole("button", { name: "Ask", exact: true }).click();
    await expect(page.getByText("According to the document").first()).toBeVisible();
    await page.getByRole("link", { name: /Open in viewer/ }).first().click();
    await expect(page).toHaveURL(/\/documents\/[^?]+\?page=\d+&cite=1/);
    await expect(page.locator("canvas").first()).toBeVisible();
  });

  test("a question outside the documents says so and offers nearby ones", async () => {
    await page.goto("/ask");
    await page.locator("#ask-input").fill("qwertyuiop zxcvbnmk lkjhgfdsa");
    await page.getByRole("button", { name: "Ask", exact: true }).click();
    await expect(page.getByText(/couldn.t find this in your documents/i).first()).toBeVisible();
  });

  test("a setting changed on screen is kept by the server", async () => {
    await page.goto("/settings");
    const toggle = page.getByRole("switch", { name: "Always cite a source" });
    await expect(toggle).toHaveAttribute("aria-checked", "true");
    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-checked", "false");
    await page.reload();
    await expect(page.getByRole("switch", { name: "Always cite a source" })).toHaveAttribute("aria-checked", "false");
    await page.getByRole("switch", { name: "Always cite a source" }).click(); // leave it as it was
    await expect(page.getByRole("switch", { name: "Always cite a source" })).toHaveAttribute("aria-checked", "true");
  });

  test("a source is restricted to some people from the Sources screen and opened again", async () => {
    await page.goto("/sources");
    await page.getByRole("button", { name: /Change who can read/ }).first().click();
    await page.getByRole("checkbox", { name: "Everyone who signs in" }).click();
    await page.getByRole("checkbox", { name: ADMIN.name }).click();
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByText("Access saved")).toBeVisible();
    await expect(page.getByText(`Only ${ADMIN.name}`)).toBeVisible();
    await page.getByRole("button", { name: /Change who can read/ }).first().click();
    await page.getByRole("checkbox", { name: "Everyone who signs in" }).click();
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.getByText("Everyone who signs in").first()).toBeVisible();
    await page.goto("/documents"); // the reader never lost sight of the documents
    await expect(page.getByText("PNT-014-rev3").first()).toBeVisible();
  });

  test("Pro screens are closed without a license", async () => {
    test.skip(PRO, "this stack has a Pro license");
    for (const path of ["/alerts", "/map"]) {
      await page.goto(path);
      await expect(page.getByText("Part of Atlas Pro.")).toBeVisible();
      await expect(page.getByRole("link", { name: "Open Settings" })).toBeVisible();
    }
  });

  test("alerts list the real problems and a decision is kept by the server", async () => {
    test.skip(!PRO, "needs a Pro license");
    await page.goto("/alerts");
    await expect(page.getByRole("heading", { name: "Alerts", exact: true })).toBeVisible();
    // Documents without an owner raise NO_OWNER alerts once indexed.
    await expect(async () => {
      await page.reload();
      await expect(page.getByRole("radio", { name: /^Open\s*[1-9]/ })).toBeVisible({ timeout: 2_000 });
    }).toPass(INDEXING);
    const open = page.getByRole("radio", { name: /^Open/ });
    const before = Number(((await open.textContent()) ?? "").replace(/\D/g, ""));
    expect(before).toBeGreaterThan(0);

    const ack = page.getByRole("button", { name: /^Acknowledge/ });
    await ack.hover();
    await page.mouse.down();
    await page.waitForTimeout(1_200); // the button is held, not clicked
    await page.mouse.up();
    await expect(page.getByText(/Acknowledged\. It stays/)).toBeVisible();
    await page.reload();
    await expect(page.getByRole("radio", { name: /^Open/ })).toContainText(String(before - 1));
    await expect(page.getByRole("radio", { name: /^Acknowledged/ })).toContainText("1");
  });

  test("the map shows the documents and opens one", async () => {
    test.skip(!PRO, "needs a Pro license");
    // The map draws links, and the sample documents link to nothing: giving them one code makes them duplicates.
    const docs = (await (await page.request.get("/api/v1/documents?limit=50")).json()) as { id: string }[];
    for (const d of docs) {
      const fixed = await page.request.patch(`/api/v1/documents/${d.id}/metadata`, { data: { code: "PNT-014" } });
      expect(fixed.ok()).toBe(true);
    }
    await expect(async () => {
      await page.goto("/map");
      await expect(page.locator('[aria-label^="document "]').first()).toBeVisible({ timeout: 3_000 });
    }).toPass({ timeout: 60_000 });
    await expect(page.getByRole("heading", { name: "Map", exact: true })).toBeVisible();
    await page.locator('[aria-label^="document "]').first().click();
    await page.getByRole("link", { name: /^Open / }).first().click();
    await expect(page).toHaveURL(/\/documents\/[^/?]+/);
  });

  test("the audit log lists what happened and its chain verifies", async () => {
    test.skip(!PRO, "needs a Pro license");
    // An administrator is not an auditor (separation of duties): the audit log needs the role added.
    const me = (await (await page.request.get("/api/v1/me")).json()) as { id: string };
    const granted = await page.request.patch(`/api/v1/users/${me.id}/roles`, { data: { roles: ["admin", "auditor"] } });
    expect(granted.ok()).toBe(true);
    await page.goto("/audit");
    await page.getByRole("button", { name: "Verify chain" }).click();
    await expect(page.getByText(/The chain is intact: all \d+ events check out/)).toBeVisible();
    await expect(page.getByRole("button", { name: /Export CSV/ })).toBeEnabled();
  });

  test("the dashboard counts the indexed documents", async () => {
    await page.goto("/dashboard");
    await expect(page.getByRole("heading", { name: "Dashboard", exact: true })).toBeVisible();
    await expect(page.getByText(/indexed/i).first()).toBeVisible();
  });

  test("a password is changed from the account menu and the old one stops working", async () => {
    const NEXT = "a-newer-long-password-2";
    await page.goto("/documents");
    await page.getByRole("button", { name: /^Account/ }).click();
    await page.getByRole("menuitem", { name: "Change password" }).click();
    await page.getByLabel("Current password").fill(ADMIN.password);
    await page.getByLabel("New password").fill(NEXT);
    await page.getByRole("button", { name: "Change password" }).click();
    await expect(page.getByText(/Password changed/)).toBeVisible();
    // A 401 clears the cookie of whoever receives it, so sign-in is tried without this browser's session.
    const anonymous = await request.newContext({ baseURL: "https://localhost", ignoreHTTPSErrors: true });
    const login = (password: string) => anonymous.post("/api/v1/auth/login", { data: { email: ADMIN.email, password } });
    expect((await login(ADMIN.password)).status()).toBe(401);
    expect((await login(NEXT)).status()).toBe(200);
    await anonymous.dispose();
    // Put it back so the sign-in test below still uses the first password.
    const back = await page.request.put("/api/v1/me/password", { data: { current_password: NEXT, new_password: ADMIN.password } });
    expect(back.status()).toBe(204);
  });

  test("without a session the app asks to sign in and returns to the page afterwards", async () => {
    await page.goto("about:blank"); // leave the screen first: its own requests would redirect while the test navigates
    await page.context().clearCookies();
    await page.goto("/documents");
    await expect(page).toHaveURL(/\/login\?next=%2Fdocuments/);
    await page.getByLabel("Email").fill(ADMIN.email);
    await page.getByLabel("Password", { exact: true }).fill("not-the-password");
    await page.getByRole("button", { name: "Continue" }).click();
    await expect(page.getByText(/don.t match/)).toBeVisible();
    await page.getByLabel("Password", { exact: true }).fill(ADMIN.password);
    await page.getByRole("button", { name: "Continue" }).click();
    await expect(page.getByRole("heading", { name: "Enter your code" })).toBeVisible();
    await typeCode(page);
    await expect(page).toHaveURL(/\/documents$/);
  });

  test("a session the server no longer knows sends the browser back to sign in", async () => {
    await page.goto("about:blank");
    await page.context().addCookies([{ name: "atlas_session", value: "forged", url: "https://localhost", httpOnly: true, secure: true }]);
    await page.goto("/documents");
    await expect(page).toHaveURL(/\/login\?next=/);
  });
});
