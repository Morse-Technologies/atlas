import { copyFileSync, mkdirSync, readFileSync } from "node:fs";
import { expect, request, test, type APIRequestContext, type Browser, type Locator, type Page, type Response } from "@playwright/test";
import { ADMIN, saveAdminSecret } from "./admin";
import { waitForSignInSlot } from "./signInPace";
import { freshTotp } from "./totp";

// `dev-env.sh --pro` starts the stack with a signed license; without it the Pro screens must be closed.
const PRO = process.env.ATLAS_E2E_EDITION === "pro";
// The page size the interface asks for (app/src/features/hooks.ts); the paging test creates more people than this.
const LIST_PAGE_SIZE = 50;
const PAGING_PEOPLE = 50;
const INDEXING = { timeout: 3 * 60_000, intervals: [3_000] };
let secret = "";

/** Types a code the server has not accepted yet (it takes each code once). The first of the six boxes takes the focus by itself when the step opens. */
async function typeCode(page: Page, totpSecret = secret) {
  await page.keyboard.type(await freshTotp(totpSecret));
}

type NewPerson = { name: string; email: string; roles: string[]; secondFactor: boolean };

/** Adds an account as the signed-in administrator and returns its one-time password and, if it has one, its second-factor secret. */
async function addPerson(page: Page, person: NewPerson) {
  const data = { name: person.name, email: person.email, roles: person.roles, second_factor: person.secondFactor };
  const created = await page.request.post("/api/v1/users", { data });
  expect(created.status()).toBe(201);
  const body = (await created.json()) as { person: { id: string }; password: string; totp_uri: string | null };
  return { id: body.person.id, password: body.password, secret: body.totp_uri ? (new URL(body.totp_uri).searchParams.get("secret") ?? "") : "" };
}

/** Signs a new person in with a client of their own and has them choose a password, as their first sign-in demands. */
async function signInAsNew(baseURL: string | undefined, person: NewPerson, account: { password: string; secret: string }): Promise<APIRequestContext> {
  const own = "own-long-password-1";
  const api = await request.newContext({ baseURL, ignoreHTTPSErrors: true });
  await waitForSignInSlot();
  const login = await api.post("/api/v1/auth/login", { data: { email: person.email, password: account.password } });
  expect(login.status()).toBe(200);
  const { challenge_id } = (await login.json()) as { challenge_id: string };
  const code = account.secret ? await freshTotp(account.secret) : "000000";
  expect((await api.post("/api/v1/auth/verify", { data: { challenge_id, code } })).status()).toBe(204);
  const changed = await api.put("/api/v1/me/password", { data: { current_password: account.password, new_password: own } });
  expect(changed.status()).toBe(204);
  return api;
}

test.describe.serial("Atlas in a browser, from first setup to a cited answer", () => {
  // One browser for the whole journey: the session created by the first step is what the next ones use.
  let page: Page;
  const policyViolations: string[] = [];
  test.beforeAll(async ({ browser, baseURL }) => {
    page = await (await browser.newContext({ baseURL, ignoreHTTPSErrors: true })).newPage();
    page.on("console", (message) => { if (/Content Security Policy/i.test(message.text())) policyViolations.push(message.text()); });
    page.on("pageerror", (error) => { if (/Content Security Policy/i.test(error.message)) policyViolations.push(error.message); });
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
    saveAdminSecret(secret);
    await typeCode(page);

    await expect(page.getByRole("heading", { name: "Where are your documents?" })).toBeVisible();
    await page.getByLabel("Folder path").fill("/sources/docs");
    await page.getByRole("button", { name: "Check" }).click();
    await expect(page.getByText("Atlas can read this folder")).toBeVisible();
    await page.getByRole("button", { name: "Start indexing" }).click();
    await expect(page).toHaveURL(/\/sources$/);
  });

  test("the Pro license is installed from Settings", async () => {
    test.skip(!PRO, "this stack has no license to install");
    await page.goto("/settings");
    await page.locator('input[type="file"]').setInputFiles(process.env.ATLAS_E2E_LICENSE_FILE ?? "");
    await expect(page.getByText("License installed")).toBeVisible();
    await expect(page.getByText("Atlas Pro", { exact: true })).toBeVisible();
    await expect(page.getByText("Signed audit reports")).toBeVisible();
  });

  test("the folder is read and its documents appear", async () => {
    await page.goto("/documents");
    await expect(async () => {
      await page.reload();
      await expect(page.getByText("PNT-014-rev3").first()).toBeVisible({ timeout: 2_000 });
    }).toPass(INDEXING);
  });

  test("a scan can be requested by hand and shows up as a run without reloading", async () => {
    await page.goto("/sources");
    await page.waitForLoadState("networkidle"); // the layout settles first: the button is held under the pointer
    const runs = page.getByRole("region", { name: "Recent runs" }).locator("tbody tr");
    await expect(runs.first()).toBeVisible();
    const before = await runs.count();
    await page.getByRole("button", { name: "Sync", exact: true }).hover();
    await page.mouse.down();
    await page.waitForTimeout(800); // the button is held, not clicked
    await page.mouse.up();
    await expect(page.getByText("Scan requested")).toBeVisible();
    await expect(async () => expect(await runs.count()).toBeGreaterThan(before)).toPass({ timeout: 90_000 });
    await expect(page.getByText("Running…")).toHaveCount(0, { timeout: 90_000 });
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

  test("the word search language chosen on screen is kept by the server", async () => {
    await page.goto("/settings");
    const choice = (name: string) => page.getByRole("radiogroup", { name: "Word search language" }).getByRole("radio", { name });
    const saved = () => page.waitForResponse((r) => r.url().endsWith("/api/v1/settings") && r.request().method() === "PATCH");
    const spanishSaved = saved();
    await choice("Spanish").click();
    await expect(choice("Spanish")).toHaveAttribute("aria-checked", "true");
    expect((await spanishSaved).ok()).toBe(true); // reloading before the server has it would lose the change
    await page.reload();
    await expect(choice("Spanish")).toHaveAttribute("aria-checked", "true");
    const anySaved = saved();
    await choice("Any (accents ignored)").click(); // leave it as it was
    await expect(choice("Any (accents ignored)")).toHaveAttribute("aria-checked", "true");
    expect((await anySaved).ok()).toBe(true);
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
    // Wait for the server to keep the change: leaving the page earlier would cancel the request.
    const saved = page.waitForResponse((r) => r.request().method() === "PATCH" && r.url().includes("/api/v1/sources/"));
    await page.getByRole("button", { name: "Save" }).click();
    expect((await saved).ok()).toBe(true);
    await page.goto("/documents"); // the reader never lost sight of the documents
    await expect(page.getByText("PNT-014-rev3").first()).toBeVisible();
  });

  test("two people see only what they may read, and only the auditor reads the audit log", async ({ browser, baseURL }) => {
    const reader: NewPerson = { name: "Cleo Reader", email: "cleo@acme.test", roles: ["viewer"], secondFactor: false };
    const auditor: NewPerson = { name: "Dana Auditor", email: "dana@acme.test", roles: ["auditor"], secondFactor: true };
    const cleo = await signInAsNew(baseURL, reader, await addPerson(page, reader));
    const dana = await signInAsNew(baseURL, auditor, await addPerson(page, auditor));
    // Separation of duties: the administrator may not read the audit log, the auditor may.
    expect((await page.request.get("/api/v1/audit/events")).status()).toBe(403);
    expect((await dana.get("/api/v1/audit/events")).status()).toBe(200);

    const docs = (await (await page.request.get("/api/v1/documents?limit=200")).json()) as { id: string; path: string; source_id: string }[];
    const golden = docs.find((d) => d.path.endsWith("PNT-014-rev3.pdf"));
    expect(golden).toBeDefined();
    const me = (await (await page.request.get("/api/v1/me")).json()) as { id: string };
    const restrict = (data: object) => page.request.patch(`/api/v1/sources/${golden?.source_id}`, { data });
    expect((await restrict({ access: "restricted", readers: [me.id] })).ok()).toBe(true);

    const screen = await (await browser.newContext({ baseURL, ignoreHTTPSErrors: true, storageState: await cleo.storageState() })).newPage();
    await screen.goto("/documents");
    await expect(screen.getByText("No documents match these filters.")).toBeVisible(); // the folder is the only source so far
    await expect(screen.getByText("PNT-014-rev3")).toHaveCount(0);
    expect((await cleo.get(`/api/v1/documents/${golden?.id}`)).status()).toBe(404);
    expect((await cleo.get(`/api/v1/documents/${golden?.id}/related`)).status()).toBe(404);

    expect((await restrict({ access: "everyone" })).ok()).toBe(true);
    await screen.reload();
    await expect(screen.getByText("PNT-014-rev3").first()).toBeVisible();
    await screen.context().close();
    await Promise.all([cleo.dispose(), dana.dispose()]);
  });

  test("a second folder is added from the Sources screen and its documents appear", async () => {
    // The stack mounts deploy/compose/sources at /sources: a new subfolder with a copy of the sample PDF.
    const sources = "../deploy/compose/sources";
    mkdirSync(`${sources}/docs2`, { recursive: true });
    copyFileSync(`${sources}/docs/PNT-014-rev3.pdf`, `${sources}/docs2/second-folder.pdf`);
    await page.goto("/sources");
    await page.getByRole("button", { name: "Add folder" }).click();
    await page.getByLabel("Folder path").fill("/sources/docs2");
    await page.getByRole("button", { name: "Check" }).click();
    await expect(page.getByText("Atlas can read this folder")).toBeVisible();
    await page.getByRole("button", { name: "Add", exact: true }).click();
    await expect(page.getByText("docs2 added")).toBeVisible();
    await expect(page.getByRole("region", { name: "Connected sources" }).getByText("docs2", { exact: true })).toBeVisible();
    await expect(async () => {
      await page.goto("/documents");
      await expect(page.getByText("second-folder").first()).toBeVisible({ timeout: 2_000 });
    }).toPass(INDEXING);
  });

  test("a file is uploaded from the Sources screen and appears among the documents", async () => {
    await page.goto("/sources");
    await page.getByRole("button", { name: "Upload files" }).click();
    const buffer = readFileSync("public/samples/PNT-014-rev3.pdf");
    await page.locator('input[type="file"]').setInputFiles({ name: "uploaded-sample.pdf", mimeType: "application/pdf", buffer });
    await page.getByRole("button", { name: "Upload", exact: true }).click();
    await expect(page.getByText(/Atlas is indexing 1 file/)).toBeVisible();
    await expect(async () => {
      await page.goto("/documents");
      await expect(page.getByText("uploaded-sample").first()).toBeVisible({ timeout: 2_000 });
    }).toPass(INDEXING);
  });

  test("a document's code is corrected from its page and marked as corrected by hand", async () => {
    await page.goto("/documents");
    await page.getByText("uploaded-sample").first().click();
    await page.getByRole("button", { name: "Edit details" }).click();
    await page.getByLabel("Code", { exact: true }).fill("UP-001");
    await page.getByRole("button", { name: "Save details" }).click();
    await expect(page.getByText("Details saved")).toBeVisible();
    await expect(page.getByText("Corrected by hand").first()).toBeVisible();
    await page.reload();
    await expect(page.getByText("UP-001").first()).toBeVisible();
  });

  test("people who are not administrators see and pick owners by name", async ({ browser, baseURL }) => {
    const FRAN: NewPerson = { name: "Fran Editor", email: "fran@acme.test", roles: ["editor_metadata"], secondFactor: false };
    const HANA: NewPerson = { name: "Hana Viewer", email: "hana@acme.test", roles: ["viewer"], secondFactor: false };
    const fran = await signInAsNew(baseURL, FRAN, await addPerson(page, FRAN));
    const hana = await signInAsNew(baseURL, HANA, await addPerson(page, HANA));
    const listed = await hana.get("/api/v1/people");
    expect(listed.status()).toBe(200);
    for (const person of (await listed.json()) as object[]) expect(Object.keys(person).sort()).toEqual(["id", "name"]);
    expect((await hana.get("/api/v1/users")).status()).toBe(403);

    const editor = await (await browser.newContext({ baseURL, ignoreHTTPSErrors: true, storageState: await fran.storageState() })).newPage();
    await editor.goto("/documents");
    await editor.getByText("uploaded-sample").first().click();
    await editor.getByRole("button", { name: "Edit details" }).click();
    await editor.getByLabel("Owner").selectOption({ label: ADMIN.name });
    await editor.getByRole("button", { name: "Save details" }).click();
    await expect(editor.getByText("Details saved")).toBeVisible();

    const reader = await (await browser.newContext({ baseURL, ignoreHTTPSErrors: true, storageState: await hana.storageState() })).newPage();
    await reader.goto("/documents");
    await reader.getByText("uploaded-sample").first().click();
    await expect(reader.getByText(ADMIN.name).first()).toBeVisible();
    await expect(reader.getByText("No owner").locator("visible=true")).toHaveCount(0); // the page keeps hidden copies of the header
    await Promise.all([editor.context().close(), reader.context().close(), fran.dispose(), hana.dispose()]);
  });

  test("an uploaded document is deleted from its page and a folder document has no delete", async () => {
    await page.goto("/documents");
    await page.getByText("second-folder").first().click();
    await expect(page.getByRole("heading", { name: "Metadata" })).toBeVisible();
    await expect(page.getByRole("button", { name: /Hold to delete/ })).toHaveCount(0);
    await page.goto("/documents");
    await page.getByText("uploaded-sample").first().click();
    const remove = page.getByRole("button", { name: /Hold to delete/ });
    await remove.hover();
    await page.mouse.down();
    await page.waitForTimeout(1_500); // the button is held, not clicked
    await page.mouse.up();
    await expect(page).toHaveURL(/\/documents$/);
    await expect(async () => { // the document is retired by a job a moment later
      await page.reload();
      await expect(page.getByText("uploaded-sample")).toHaveCount(0, { timeout: 2_000 });
    }).toPass({ timeout: 60_000 });
  });

  test("a folder source is removed from the Sources screen and its documents disappear", async () => {
    await page.goto("/sources");
    await page.waitForLoadState("networkidle");
    const remove = page.getByRole("button", { name: "Remove source docs2" });
    await remove.hover();
    await page.mouse.down();
    await page.waitForTimeout(1_500); // the button is held, not clicked
    await page.mouse.up();
    await expect(page.getByRole("region", { name: "Connected sources" }).getByText("docs2", { exact: true })).toHaveCount(0);
    await expect(async () => { // the documents are retired by a job a moment later
      await page.goto("/documents");
      await expect(page.getByText("PNT-014-rev3").first()).toBeVisible({ timeout: 2_000 });
      await expect(page.getByText("second-folder")).toHaveCount(0, { timeout: 2_000 });
    }).toPass({ timeout: 60_000 });
  });

  test("Pro screens are closed without a license", async () => {
    test.skip(PRO, "this stack has a Pro license");
    const refused: string[] = [];
    const watch = (r: Response) => { if (r.status() === 403) refused.push(r.url()); };
    page.on("response", watch);
    for (const path of ["/alerts", "/map", "/dashboard"]) {
      await page.goto(path);
      await page.waitForLoadState("networkidle");
    }
    page.off("response", watch);
    expect(refused, "no Pro endpoint is asked for without the feature").toEqual([]);
    await page.goto("/settings");
    await page.locator('input[type="file"]').setInputFiles({ name: "forged.lic", mimeType: "text/plain", buffer: Buffer.from("not a license") });
    await expect(page.getByRole("alert").filter({ hasText: "The file is not a license file." })).toBeVisible();
    await expect(page.getByText("Atlas Core (open source)")).toBeVisible();
    for (const path of ["/alerts", "/map"]) {
      await page.goto(path);
      await expect(page.getByText("Part of Atlas Pro.")).toBeVisible();
      await expect(page.getByText("This installation runs Atlas Core.")).toBeVisible();
      await expect(page.getByRole("link", { name: "Open Settings" })).toBeVisible();
    }
  });

  test("a person added by the administrator is made to choose a password at the first sign-in", async ({ browser, baseURL }) => {
    const BEA = { name: "Bea Viewer", email: "bea@acme.test", own: "bea-own-long-password-1" };
    await page.goto("/users");
    await page.getByRole("button", { name: "Add person" }).click();
    await page.getByLabel("Full name").fill(BEA.name);
    await page.getByLabel("Work email").fill(BEA.email);
    await page.getByRole("button", { name: "Create account" }).click();
    await expect(page.getByText("Shown once.")).toBeVisible();
    const oneTime = ((await page.locator("code").first().textContent()) ?? "").trim();
    expect(oneTime.length).toBeGreaterThan(10);
    await page.getByRole("button", { name: "Done" }).click();

    // A new browser context: Bea has her own session and none of the administrator's.
    const bea = await (await browser.newContext({ baseURL, ignoreHTTPSErrors: true })).newPage();
    await bea.goto("/login");
    await bea.getByLabel("Email").fill(BEA.email);
    await bea.getByLabel("Password", { exact: true }).fill(oneTime);
    await waitForSignInSlot();
    await bea.getByRole("button", { name: "Continue" }).click();
    await expect(bea).toHaveURL(/\/password$/);
    await bea.getByLabel("Password you were given").fill(oneTime);
    await bea.getByLabel("New password").fill(BEA.own);
    await bea.getByRole("button", { name: "Continue" }).click();
    await expect(bea).toHaveURL(/\/(ask|dashboard)$/);
    await bea.context().close();
  });

  /** Holds a button down long enough to confirm it, as a person does. */
  async function hold(button: Locator) {
    await button.hover();
    await page.mouse.down();
    await page.waitForTimeout(1_500); // the button is held, not clicked
    await page.mouse.up();
  }

  test("an administrator recovers a person's access from the Users screen", async ({ browser, baseURL }) => {
    const ELI: NewPerson = { name: "Eli Recover", email: "eli@acme.test", roles: ["viewer"], secondFactor: true };
    const NEW_PASSWORD = "eli-own-long-password-2";
    const account = await addPerson(page, ELI);
    const eli = await signInAsNew(baseURL, ELI, account);
    const openRecovery = async () => {
      await page.goto("/users");
      await page.getByRole("button", { name: "Edit roles for Eli Recover" }).click();
    };

    await openRecovery();
    await hold(page.getByRole("button", { name: "Hold to reset password" }));
    await expect(page.getByText("Shown once.")).toBeVisible();
    const oneTime = ((await page.locator("code").first().textContent()) ?? "").trim();
    await page.getByRole("button", { name: "Done" }).click();
    expect((await eli.get("/api/v1/me")).status()).toBe(401); // the old sessions are closed

    const eliPage = await (await browser.newContext({ baseURL, ignoreHTTPSErrors: true })).newPage();
    await eliPage.goto("/login");
    await eliPage.getByLabel("Email").fill(ELI.email);
    await eliPage.getByLabel("Password", { exact: true }).fill(oneTime);
    await waitForSignInSlot();
    await eliPage.getByRole("button", { name: "Continue" }).click();
    await expect(eliPage.getByRole("heading", { name: "Enter your code" })).toBeVisible(); // typing earlier loses the first digits
    await typeCode(eliPage, account.secret);
    await expect(eliPage).toHaveURL(/\/password$/);
    await eliPage.getByLabel("Password you were given").fill(oneTime);
    await eliPage.getByLabel("New password").fill(NEW_PASSWORD);
    await eliPage.getByRole("button", { name: "Continue" }).click();
    await expect(eliPage).toHaveURL(/\/(ask|dashboard)$/);

    await openRecovery();
    await hold(page.getByRole("button", { name: "Hold to reset second factor" }));
    const link = ((await page.locator("code").first().textContent()) ?? "").trim();
    const newSecret = new URL(link).searchParams.get("secret") ?? "";
    expect(newSecret).not.toBe("");
    await page.getByRole("button", { name: "Done" }).click();
    await eliPage.reload();
    await expect(eliPage).toHaveURL(/\/login/);

    const again = await request.newContext({ baseURL, ignoreHTTPSErrors: true });
    await waitForSignInSlot();
    const login = await again.post("/api/v1/auth/login", { data: { email: ELI.email, password: NEW_PASSWORD } });
    const { challenge_id } = (await login.json()) as { challenge_id: string };
    const code = await freshTotp(newSecret);
    expect((await again.post("/api/v1/auth/verify", { data: { challenge_id, code } })).status()).toBe(204);
    await Promise.all([eliPage.context().close(), eli.dispose(), again.dispose()]);
  });

  test("the Users list loads the next page when scrolled to the end", async () => {
    for (let n = 1; n <= PAGING_PEOPLE; n++) {
      const tag = String(n).padStart(2, "0");
      await addPerson(page, { name: `Paging Person ${tag}`, email: `paging-${tag}@acme.test`, roles: ["viewer"], secondFactor: false });
    }
    const next = page.waitForRequest((r) => /\/api\/v1\/users\?.*cursor=/.test(r.url()));
    await page.goto("/users");
    await page.getByText(/\d+ loaded/).scrollIntoViewIfNeeded();
    await next;
    const all = page.getByText(/\d+ loaded, that is all/);
    await expect(all).toBeVisible();
    expect(Number.parseInt((await all.textContent()) ?? "0", 10)).toBeGreaterThan(LIST_PAGE_SIZE);
  });

  /** A browser context signed in as a person the administrator added, with the session of their own client. */
  async function openAs(browser: Browser, baseURL: string | undefined, own: APIRequestContext) {
    return browser.newContext({ baseURL, ignoreHTTPSErrors: true, storageState: await own.storageState() });
  }

  test("a person who only reads sees only the screens they can open", async ({ browser, baseURL }) => {
    const IVO: NewPerson = { name: "Ivo Viewer", email: "ivo@acme.test", roles: ["viewer"], secondFactor: false };
    const ivo = await signInAsNew(baseURL, IVO, await addPerson(page, IVO));
    const context = await openAs(browser, baseURL, ivo);
    const own = await context.newPage();

    await own.goto("/documents");
    const menu = own.getByRole("navigation", { name: "Main" });
    const link = (name: string) => menu.getByRole("link", { name, exact: true });
    for (const name of ["Assistant", "Documents"]) await expect(link(name)).toBeVisible();
    if (PRO) for (const name of ["Dashboard", "Map"]) await expect(link(name)).toBeVisible(); // Core has neither screen
    for (const name of ["Alerts", "Sources", "Audit", "Users"]) await expect(link(name)).toHaveCount(0);
    await expect(own.getByRole("link", { name: "Settings", exact: true })).toHaveCount(0);
    await own.keyboard.press("g");
    await own.keyboard.press("u");
    await expect(own).toHaveURL(/\/documents$/);
    await Promise.all([context.close(), ivo.dispose()]);
  });

  test("an editor's Sources screen offers only the upload", async ({ browser, baseURL }) => {
    const KAI: NewPerson = { name: "Kai Editor", email: "kai@acme.test", roles: ["editor_metadata"], secondFactor: false };
    const kai = await signInAsNew(baseURL, KAI, await addPerson(page, KAI));
    const context = await openAs(browser, baseURL, kai);
    const own = await context.newPage();
    const requested: string[] = [];
    own.on("request", (r) => requested.push(r.url()));

    await own.goto("/sources");
    await expect(own.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Sources", exact: true })).toBeVisible();
    await expect(own.getByRole("button", { name: "Upload files" })).toBeVisible();
    for (const name of ["Connected sources", "Recent runs", "Quarantine"]) await expect(own.getByRole("region", { name })).toHaveCount(0);
    await expect(own.getByRole("button", { name: "Add folder" })).toHaveCount(0);
    expect(requested.filter((url) => /\/api\/v1\/(sources|ingestion\/|users)/.test(url))).toEqual([]);

    await own.getByRole("button", { name: "Upload files" }).click();
    const buffer = readFileSync("public/samples/PNT-014-rev3.pdf");
    await own.locator('input[type="file"]').setInputFiles({ name: "kai-upload.pdf", mimeType: "application/pdf", buffer });
    await own.getByRole("button", { name: "Upload", exact: true }).click();
    await expect(own.getByText(/Atlas is indexing 1 file/)).toBeVisible();
    await Promise.all([context.close(), kai.dispose()]);
  });

  test("a reader sees the organization name and is never refused", async ({ browser, baseURL }) => {
    const JO: NewPerson = { name: "Jo Viewer", email: "jo@acme.test", roles: ["viewer"], secondFactor: false };
    const jo = await signInAsNew(baseURL, JO, await addPerson(page, JO));
    const context = await openAs(browser, baseURL, jo);
    const own = await context.newPage();
    const refused: string[] = [];
    own.on("response", (r) => { if (r.status() === 403 && r.url().includes("/api/v1/")) refused.push(r.url()); });

    const screens = ["/documents", "/ask", ...(PRO ? ["/dashboard", "/map"] : [])]; // Core has neither the dashboard nor the map
    for (const screen of screens) {
      await own.goto(screen);
      await own.waitForLoadState("networkidle");
    }
    await expect(own.getByText("Acme", { exact: true }).first()).toBeVisible();
    expect(refused).toEqual([]);
    await Promise.all([context.close(), jo.dispose()]);
  });

  test("what the administrator changes for a reader shows after a reload, and deleting them ends their session", async ({ browser, baseURL }) => {
    const MEI: NewPerson = { name: "Mei Reader", email: "mei@acme.test", roles: ["viewer"], secondFactor: false };
    const account = await addPerson(page, MEI);
    const mei = await signInAsNew(baseURL, MEI, account);
    const reader = await (await openAs(browser, baseURL, mei)).newPage();
    const sources = reader.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Sources", exact: true });

    await reader.goto("/documents");
    await expect(reader.getByText("PNT-014-rev3").first()).toBeVisible();
    await expect(sources).toHaveCount(0);

    // Role: an editor gets the Sources screen.
    const roles = await page.request.patch(`/api/v1/users/${account.id}/roles`, { data: { roles: ["editor_metadata"] } });
    expect(roles.ok()).toBe(true);
    await reader.reload();
    await expect(sources).toBeVisible();

    // Source access: restricted to the administrator, the reader loses the documents; opened again, they return.
    const docs = (await (await page.request.get("/api/v1/documents?limit=200")).json()) as { path: string; source_id: string }[];
    const sourceId = docs.find((d) => d.path.endsWith("PNT-014-rev3.pdf"))?.source_id;
    const me = (await (await page.request.get("/api/v1/me")).json()) as { id: string };
    const restrict = (data: object) => page.request.patch(`/api/v1/sources/${sourceId}`, { data });
    expect((await restrict({ access: "restricted", readers: [me.id] })).ok()).toBe(true);
    await reader.reload();
    await expect(reader.getByText("PNT-014-rev3")).toHaveCount(0);
    expect((await restrict({ access: "everyone" })).ok()).toBe(true);
    await reader.reload();
    await expect(reader.getByText("PNT-014-rev3").first()).toBeVisible();

    // Deleting the person ends their session at once.
    expect((await page.request.delete(`/api/v1/users/${account.id}`)).status()).toBe(204);
    await reader.reload();
    await expect(reader).toHaveURL(/\/login/);
    expect((await mei.get("/api/v1/me")).status()).toBe(401);
    await Promise.all([reader.context().close(), mei.dispose()]);
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
    const login = async (password: string) => {
      await waitForSignInSlot();
      return anonymous.post("/api/v1/auth/login", { data: { email: ADMIN.email, password } });
    };
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
    await waitForSignInSlot();
    await page.getByRole("button", { name: "Continue" }).click();
    await expect(page.getByText(/don.t match/)).toBeVisible();
    await page.getByLabel("Password", { exact: true }).fill(ADMIN.password);
    await waitForSignInSlot();
    await page.getByRole("button", { name: "Continue" }).click();
    await expect(page.getByRole("heading", { name: "Enter your code" })).toBeVisible();
    await typeCode(page);
    await expect(page).toHaveURL(/\/documents$/);
  });

  test("the interface sends a content security policy and the whole journey broke none of it", async () => {
    const response = await page.request.get("/login");
    const policy = response.headers()["content-security-policy"] ?? "";
    expect(policy).toContain("default-src 'self'");
    expect(policy).toContain("frame-ancestors 'none'");
    expect(policyViolations).toEqual([]);
  });

  test("a session the server no longer knows sends the browser back to sign in", async () => {
    await page.goto("about:blank");
    await page.context().addCookies([{ name: "atlas_session", value: "forged", url: "https://localhost", httpOnly: true, secure: true }]);
    await page.goto("/documents");
    await expect(page).toHaveURL(/\/login\?next=/);
  });
});
