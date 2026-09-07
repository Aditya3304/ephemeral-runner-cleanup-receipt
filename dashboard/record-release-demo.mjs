import { chromium } from "@playwright/test";
import { createHash } from "node:crypto";
import { mkdir, readFile, rm, stat, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const [outputArg, passReceipt, failureReceipt] = process.argv.slice(2);
if (
  !outputArg ||
  !/^[0-9a-f-]{36}$/.test(passReceipt || "") ||
  !/^[0-9a-f-]{36}$/.test(failureReceipt || "")
) {
  throw new Error(
    "Usage: node record-release-demo.mjs OUTPUT PASS_RECEIPT FAILURE_RECEIPT",
  );
}
const output = resolve(outputArg);
const scratch = resolve(output, ".video-pending");
await mkdir(output, { recursive: true });
await rm(scratch, { recursive: true, force: true });
await mkdir(scratch);
const externalRequests = [];
const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({
  viewport: { width: 1440, height: 900 },
  colorScheme: "dark",
  reducedMotion: "no-preference",
  recordVideo: { dir: scratch, size: { width: 1440, height: 900 } },
});
const page = await context.newPage();
await page.route("**/*", async (route) => {
  const url = new URL(route.request().url());
  if (!["127.0.0.1", "localhost"].includes(url.hostname)) {
    externalRequests.push(url.href);
    return route.abort();
  }
  return route.continue();
});

const startedAt = new Date().toISOString();
await page.goto(`http://127.0.0.1:8080/?receipt=${passReceipt}`);
await page.locator(".detail-panel h2").waitFor();
await page.waitForTimeout(1800);
await page
  .getByRole("button", { name: "Verify this receipt", exact: true })
  .click();
await page
  .locator(".verification-result")
  .getByText("Signature + all artifacts verified")
  .waitFor({ timeout: 50000 });
await page.waitForTimeout(2200);
await page.getByRole("button", { name: /Job logs: verified/ }).click();
await page.waitForTimeout(1800);
await page.getByRole("tab", { name: "Evidence", exact: true }).click();
await page.locator("summary").filter({ hasText: "Signed receipt" }).click();
await page.waitForTimeout(2200);
await page.screenshot({
  path: resolve(output, "verified-pass.png"),
  fullPage: true,
});

await page.goto(
  `http://127.0.0.1:8080/?view=incidents&receipt=${failureReceipt}`,
);
await page.locator(".detail-panel h2").waitFor();
await page.waitForTimeout(1800);
await page.getByRole("button", { name: /Workspace:/ }).click();
await page.waitForTimeout(1800);
await page.locator(".recovery-panel > summary").click();
await page.waitForTimeout(2200);
await page.screenshot({
  path: resolve(output, "verified-incident.png"),
  fullPage: true,
});

const video = page.video();
await page.close();
await context.close();
if (!video) throw new Error("Playwright did not create a recording");
const videoPath = resolve(output, "cleanup-receipt-demo.webm");
await video.saveAs(videoPath);
await browser.close();
await rm(scratch, { recursive: true, force: true });
if (externalRequests.length)
  throw new Error(
    `Unexpected external requests: ${externalRequests.join(", ")}`,
  );

const bytes = await readFile(videoPath);
const info = await stat(videoPath);
const manifest = {
  kind: "local-release-demo-recording/v1",
  started_at: startedAt,
  completed_at: new Date().toISOString(),
  pass_receipt_id: passReceipt,
  failure_receipt_id: failureReceipt,
  video_file: "cleanup-receipt-demo.webm",
  video_bytes: info.size,
  video_sha256: createHash("sha256").update(bytes).digest("hex"),
  screenshots: ["verified-pass.png", "verified-incident.png"],
  external_requests: 0,
};
await writeFile(
  resolve(output, "demo-manifest.json"),
  JSON.stringify(manifest, null, 2) + "\n",
);
console.log(JSON.stringify(manifest, null, 2));
