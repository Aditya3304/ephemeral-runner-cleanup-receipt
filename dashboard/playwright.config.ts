import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  timeout: 60000,
  workers: 1,
  fullyParallel: false,
  reporter: [
    ["list"],
    ["json", { outputFile: "../.build/dashboard-test-results.json" }],
  ],
  outputDir: "../.build/dashboard-test-output",
  use: {
    baseURL: "http://127.0.0.1:8080",
    viewport: { width: 1440, height: 1050 },
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  expect: { timeout: 10000 },
});
