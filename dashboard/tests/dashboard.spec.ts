import { test, expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { mkdir, readFile, writeFile } from "node:fs/promises";
const artifacts = "../.build/dashboard-artifacts";
async function loaded(page: Page) {
  await page.goto("/");
  await expect(
    page.getByRole("button", { name: "Open Isolation check receipt" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Verify this receipt", exact: true }),
  ).toBeVisible();
}
let receiptCount = 0;
test.beforeAll(async ({ request }) => {
  await mkdir(artifacts, { recursive: true });
  const { items } = await (await request.get("/v1/receipts?limit=100")).json();
  receiptCount = items.length;
});

test("real data, local-only assets, visual overview, and genuine verification", async ({
  page,
}) => {
  const external: string[] = [],
    errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", (route) => {
    const u = new URL(route.request().url());
    if (!["127.0.0.1", "localhost"].includes(u.hostname)) {
      external.push(u.href);
      return route.abort();
    }
    return route.continue();
  });
  await loaded(page);
  await page.evaluate(() => document.fonts.ready);
  await expect(page.locator(".ledger-table tbody tr")).toHaveCount(
    receiptCount,
  );
  await page
    .getByRole("button", { name: "Open Isolation check receipt" })
    .click();
  await expect(page.locator(".detail-panel h2")).toHaveText("Isolation check");
  await page.screenshot({ path: `${artifacts}/overview.png`, fullPage: true });
  await page
    .getByRole("button", { name: "Job logs: verified, trusted observer" })
    .click();
  await expect(page.locator(".observation-heading strong")).toHaveText(
    "Job logs",
  );
  await page
    .getByRole("button", { name: "Verify this receipt", exact: true })
    .click();
  await expect(page.locator(".verification-result")).toContainText(
    "Signature + all artifacts verified",
    { timeout: 50000 },
  );
  await expect(page.locator(".verification-result")).not.toContainText(
    "No fresh check",
  );
  await page.screenshot({ path: `${artifacts}/verified.png`, fullPage: true });
  expect(external).toEqual([]);
  expect(errors).toEqual([]);
});

test("incident selection and coverage stay honest", async ({ page }) => {
  await loaded(page);
  await page.getByRole("button", { name: "Incidents", exact: true }).click();
  await expect(page.locator(".ledger-table tbody tr")).toHaveCount(1);
  await expect(page.locator(".detail-panel h2")).toHaveText(
    "Interrupted runner",
  );
  await expect(page.locator(".detail-panel .badge")).toHaveText(
    "Needs attention",
  );
  await expect(page.locator(".incident-note")).toContainText(
    "An incident needs review",
  );
  await expect(page.locator(".map-count")).not.toHaveText("5/5");
  await page
    .getByRole("button", { name: "Workspace: failed, trusted observer" })
    .click();
  await expect(page.locator(".observation-box")).toContainText(
    "This cleanup check failed",
  );
  await page.screenshot({ path: `${artifacts}/incident.png`, fullPage: true });
});

test("search, verdict filters, keyboard shortcut, and browser history", async ({
  page,
}) => {
  await loaded(page);
  await page.keyboard.press("/");
  await expect(
    page.getByRole("textbox", { name: "Search receipts" }),
  ).toBeFocused();
  await page.getByRole("textbox", { name: "Search receipts" }).fill("cancel");
  await expect(page.locator(".ledger-table tbody tr")).toHaveCount(1);
  await expect(page.locator(".detail-panel h2")).toHaveText(
    "Graceful cancellation",
  );
  await expect(page).toHaveURL(/q=cancel/);
  await page.getByRole("button", { name: "Clear search" }).click();
  await expect(page.locator(".ledger-table tbody tr")).toHaveCount(
    receiptCount,
  );
  await page.getByRole("button", { name: "Attention", exact: true }).click();
  await expect(page.locator(".ledger-table tbody tr")).toHaveCount(1);
  await page.goBack();
  await expect(page.locator(".ledger-table tbody tr")).toHaveCount(
    receiptCount,
  );
  await page.getByRole("button", { name: "Incomplete", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "No matching receipts" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Clear all", exact: true }).click();
  await expect(page.locator(".ledger-table tbody tr")).toHaveCount(
    receiptCount,
  );
});

test("repository, incident, inclusive dates, and filter dialog accessibility", async ({
  page,
  request,
}) => {
  const { items } = await (await request.get("/v1/receipts?limit=100")).json();
  const d = new Date(
    items.find((r: any) => r.incident?.state === "open").completed_at,
  );
  const day = await page.evaluate((t) => {
    const d = new Date(t);
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  }, d.toISOString());
  await loaded(page);
  await page.getByRole("button", { name: "Filters", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  expect(
    (
      await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
        .analyze()
    ).violations,
  ).toEqual([]);
  await dialog
    .getByLabel("Repository", { exact: true })
    .selectOption("Aditya3304/ephemeral-runner-cleanup-receipt");
  await dialog
    .getByLabel("Incident state", { exact: true })
    .selectOption("open");
  await dialog.getByLabel("From", { exact: true }).fill(day);
  await dialog.getByLabel("Through", { exact: true }).fill(day);
  await dialog.getByRole("button", { name: "Apply filters" }).click();
  await expect(page.locator(".ledger-table tbody tr")).toHaveCount(1);
  await expect(page).toHaveURL(/incident_state=open/);
  await page.getByRole("button", { name: /^Filters/ }).click();
  await dialog.getByLabel("From", { exact: true }).fill("2027-01-01");
  await expect(dialog.getByRole("alert")).toContainText("end date");
  await expect(
    dialog.getByRole("button", { name: "Apply filters" }),
  ).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole("button", { name: /^Filters/ })).toBeFocused();
});

test("deep link, exact references, metadata export, and recorded timeline", async ({
  page,
  request,
}) => {
  const { items } = await (await request.get("/v1/receipts?limit=100")).json();
  const r = items.find((r: any) => r.job_id === "pass");
  await page.goto(`/?receipt=${r.id}`);
  await expect(page.locator(".detail-panel h2")).toHaveText(
    "Successful command",
  );
  await page.getByRole("tab", { name: "Evidence", exact: true }).click();
  await page.locator("summary").filter({ hasText: "Signed receipt" }).click();
  await expect(page.locator(".reference-body").first()).toContainText(
    r.receipt_object.sha256,
  );
  await expect(page.locator(".reference-body").first()).toContainText(
    r.receipt_object.version_id,
  );
  await page.getByRole("tab", { name: "Timeline", exact: true }).click();
  await expect(page.locator(".timeline-step")).toHaveCount(4);
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export metadata JSON" }).click();
  const file = await download;
  const path = await file.path();
  expect(path).toBeTruthy();
  const metadata = JSON.parse(await readFile(path!, "utf8"));
  expect(metadata.id).toBe(r.id);
  expect(metadata.receipt_object.sha256).toBe(r.receipt_object.sha256);
  await page.reload();
  await expect(page.locator(".detail-panel h2")).toHaveText(
    "Successful command",
  );
});

test("unavailable and rejected verification never look successful", async ({
  page,
}) => {
  await loaded(page);
  await page.route("**/v1/receipts/*/verification", (r) =>
    r.fulfill({
      status: 503,
      contentType: "application/json",
      body: '{"error":"dependency_unavailable"}',
    }),
  );
  await page
    .getByRole("button", { name: "Verify this receipt", exact: true })
    .click();
  await expect(page.locator(".verification-result")).toContainText(
    "Verification is unavailable",
  );
  await expect(page.locator(".verification-result")).not.toHaveClass(/success/);
  await expect(page.locator(".detail-panel .badge")).toHaveText("Clean");
  await page.route("**/v1/receipts/*/verification", (r) =>
    r.fulfill({
      status: 422,
      contentType: "application/json",
      body: '{"error":"invalid_evidence"}',
    }),
  );
  await page
    .getByRole("button", { name: "Verify this receipt", exact: true })
    .click();
  await expect(page.locator(".verification-result")).toContainText(
    "Verification was rejected",
  );
  await page.screenshot({
    path: `${artifacts}/verification-unavailable.png`,
    fullPage: true,
  });
});

test("API outage, retry, and zero-receipt state", async ({ page }) => {
  await page.route("**/v1/receipts?*", (r) =>
    r.fulfill({
      status: 503,
      contentType: "application/json",
      body: '{"error":"dependency_unavailable"}',
    }),
  );
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Waiting for your local API" }),
  ).toBeVisible();
  await expect(page.getByRole("alert")).toContainText("unavailable");
  await page.unroute("**/v1/receipts?*");
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(page.locator(".ledger-table tbody tr")).toHaveCount(
    receiptCount,
  );
  await page.route("**/v1/receipts?*", (r) =>
    r.fulfill({
      status: 200,
      contentType: "application/json",
      body: '{"items":[]}',
    }),
  );
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "Your evidence starts here" }),
  ).toBeVisible();
  await expect(page.locator(".detail-panel")).toHaveCount(0);
});

test("pagination does not mistake one page for the complete ledger", async ({
  page,
  request,
}) => {
  const { items } = await (await request.get("/v1/receipts?limit=100")).json();
  await page.route("**/v1/receipts?*", (r) =>
    r.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        r.request().url().includes("cursor=")
          ? { items: items.slice(2) }
          : { items: items.slice(0, 2), next_cursor: "test-cursor" },
      ),
    }),
  );
  await page.goto("/");
  await expect(page.locator(".ledger-table tbody tr")).toHaveCount(2);
  await expect(page.locator(".overview-caption")).toHaveText(
    "loaded · more available",
  );
  await page.getByRole("button", { name: "Load more", exact: true }).click();
  await expect(page.locator(".ledger-table tbody tr")).toHaveCount(
    receiptCount,
  );
  await expect(
    page.getByRole("button", { name: "Load more", exact: true }),
  ).toHaveCount(0);
});

test("dark appearance and explanatory interactions", async ({ page }) => {
  await loaded(page);
  await page.getByRole("button", { name: "Switch to dark theme" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  expect(
    (
      await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
        .analyze()
    ).violations,
  ).toEqual([]);
  await page.screenshot({ path: `${artifacts}/dark.png`, fullPage: true });
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.getByRole("button", { name: "How it works", exact: true }).click();
  await page.getByRole("tab", { name: /04.*Verify independently/ }).click();
  await expect(page.locator("#journey-panel")).toContainText(
    "The API checks the signature",
  );
  await page.screenshot({ path: `${artifacts}/guide.png`, fullPage: true });
});

test("desktop accessibility", async ({ page }) => {
  await loaded(page);
  const result = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(result.violations).toEqual([]);
});

test("mobile layout, coverage interaction, and reduced motion", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await loaded(page);
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
  await page
    .getByRole("button", { name: "Open Interrupted runner receipt" })
    .click();
  await expect(page.locator(".detail-panel h2")).toHaveText(
    "Interrupted runner",
  );
  await page
    .getByRole("button", { name: "Workspace: failed, trusted observer" })
    .click();
  await page.screenshot({ path: `${artifacts}/mobile.png`, fullPage: true });
  const result = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(result.violations).toEqual([]);
});

test("320px reflow keeps controls reachable", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 740 });
  await loaded(page);
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
  await page.getByRole("button", { name: "Filters", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Apply filters" }),
  ).toBeInViewport();
});

test("keyboard tabs, invalid links, and invalid calendar filters", async ({
  page,
}) => {
  await loaded(page);
  await page.getByRole("tab", { name: "Cleanup map" }).focus();
  await page.keyboard.press("ArrowRight");
  await expect(
    page.getByRole("tab", { name: "Evidence", exact: true }),
  ).toBeFocused();
  await expect(
    page.getByRole("tab", { name: "Evidence", exact: true }),
  ).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("End");
  await expect(page.getByRole("tab", { name: "Timeline" })).toBeFocused();
  await page.goto("/?receipt=invalid");
  await expect(
    page.getByRole("heading", { name: "Receipt unavailable" }),
  ).toBeVisible();
  await page.goto("/?from=2026-02-31");
  await expect(page.getByRole("alert")).toContainText("valid completion dates");
  await page.getByRole("button", { name: "Reset filters" }).click();
  await expect(page.locator(".ledger-table tbody tr")).toHaveCount(
    receiptCount,
  );
});

test("latest genuine success is visible and freshly verified", async ({
  page,
  request,
}) => {
  const { items } = await (await request.get("/v1/receipts?limit=100")).json();
  const receipt = items.find((r: any) => r.job_id === "pass");
  await page.goto(`/?receipt=${receipt.id}`);
  await expect(page.locator(".detail-panel h2")).toHaveText(
    "Successful command",
  );
  const response = page.waitForResponse((r) =>
    r.url().endsWith(`/v1/receipts/${receipt.id}/verification`),
  );
  await page
    .getByRole("button", { name: "Verify this receipt", exact: true })
    .click();
  const result = await response;
  expect(result.status()).toBe(200);
  const verification = await result.json();
  expect(verification.receipt_sha256).toBe(receipt.receipt_object.sha256);
  await expect(page.locator(".verification-result")).toContainText(
    "Signature + all artifacts verified",
  );
  await writeFile(
    `${artifacts}/fresh-run-verification.json`,
    JSON.stringify(
      {
        receipt_id: receipt.id,
        run_id: receipt.run_id,
        verdict: receipt.verdict,
        verification,
      },
      null,
      2,
    ) + "\n",
  );
  await page.screenshot({ path: `${artifacts}/fresh-run.png`, fullPage: true });
});
