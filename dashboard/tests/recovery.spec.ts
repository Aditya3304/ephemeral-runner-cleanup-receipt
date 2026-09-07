import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
test("real recovery history, receipt navigation, and accessibility", async ({
  page,
  request,
}) => {
  const data = await (await request.get("/v1/recovery")).json();
  expect(data.items.length).toBeGreaterThan(0);
  await page.goto("/");
  await page.locator(".recovery-panel>summary").click();
  await expect(page.locator(".recovery-content li")).toHaveCount(
    data.items.length,
  );
  const recovered = data.items.find((r: any) => r.result === "succeeded");
  expect(recovered).toBeTruthy();
  await page
    .locator(".recovery-content li")
    .filter({ hasText: recovered.run_id.slice(0, 12) })
    .getByRole("button", { name: "Open receipt" })
    .click();
  await expect(page).toHaveURL(new RegExp("receipt=" + recovered.receipt_id));
  await expect(
    page.getByRole("button", { name: "Verify this receipt", exact: true }),
  ).toBeVisible();
  expect(
    (
      await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
        .analyze()
    ).violations,
  ).toEqual([]);
  await page.screenshot({
    path: "../.build/dashboard-artifacts/recovery.png",
    fullPage: true,
  });
});
test("unresolved and exhausted work never claims cleanup success", async ({
  page,
  request,
}) => {
  const data = await (await request.get("/v1/recovery")).json();
  const real = data.items[0];
  await page.route("**/v1/recovery", (r) =>
    r.fulfill({
      json: {
        items: [
          {
            ...real,
            result: "exhausted",
            receipt_id: null,
            attempt_number: 6,
            stage: "sign",
            next_retry_at: null,
          },
        ],
        limit: 100,
      },
    }),
  );
  await page.goto("/");
  await page.locator(".recovery-panel>summary").click();
  await expect(page.locator(".recovery-panel>summary")).toContainText(
    "1 unresolved",
  );
  await expect(page.locator(".recovery-content")).toContainText(
    "Automatic retries stopped",
  );
  await expect(
    page
      .locator(".recovery-content")
      .getByRole("button", { name: "Open receipt" }),
  ).toHaveCount(0);
  await expect(page.locator(".recovery-content")).not.toContainText(
    "verified receipt stored",
  );
  await page.setViewportSize({ width: 390, height: 844 });
  await expect
    .poll(() =>
      page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
    )
    .toBe(true);
  expect(
    (
      await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
        .analyze()
    ).violations,
  ).toEqual([]);
});
test("recovery API outage is explicit and refresh can recover", async ({
  page,
}) => {
  await page.route("**/v1/recovery", (r) =>
    r.fulfill({ status: 503, json: { error: "dependency_unavailable" } }),
  );
  await page.goto("/");
  await page.locator(".recovery-panel>summary").click();
  await expect(page.locator(".recovery-panel")).toContainText(
    "Status unavailable",
  );
  await expect(
    page.locator(".recovery-panel").getByRole("alert"),
  ).toContainText("retains its retry state locally");
  await page.unroute("**/v1/recovery");
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page.locator(".recovery-panel>summary")).not.toContainText(
    "Status unavailable",
  );
});
