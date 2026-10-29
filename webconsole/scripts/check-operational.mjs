// SPDX-License-Identifier: BSD-3-Clause

import { chromium } from "playwright";
import { expect } from "@playwright/test";

// The Go test serves the real handler and SQLite status, withholding its first
// JSON response until after the next poll has rendered a newer observation.
const baseURL = process.argv[2];
const browser = await chromium.launch({
  executablePath: process.env.ROUTERD_WEB_CONSOLE_CHROMIUM_PATH,
  args: process.env.ROUTERD_WEB_CONSOLE_CHROMIUM_ARGS ? JSON.parse(process.env.ROUTERD_WEB_CONSOLE_CHROMIUM_ARGS) : undefined,
});
try {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.clock.install();
  const firstRequest = page.waitForRequest(request => request.url().endsWith("/api/v1/ingress"));
  const settled = new Set();
  page.on("requestfinished", request => settled.add(request));
  page.on("requestfailed", request => settled.add(request));
  await page.goto(`${baseURL}/ingress`, { waitUntil: "domcontentloaded" });
  const first = await firstRequest;
  const content = page.locator("#operational-content");
  await expect(content).toContainText("OlderObservation");
  await expect(content.getByRole("cell", { name: "Unknown", exact: true })).toBeVisible();
  await expect(content.getByRole("cell", { name: "0/0", exact: true })).toBeVisible();
  await page.clock.fastForward(30000);
  await expect(content).toContainText("NewerObservation");
  await expect(content.getByRole("cell", { name: "Unknown", exact: true })).toBeVisible();
  await expect(content.getByRole("cell", { name: "0/0", exact: true })).toBeVisible();
  await expect(page.locator("#metrics .metric-card").filter({ hasText: "healthy backends" })).toContainText("not observed");
  await expect(page.locator("#metrics .metric-card").filter({ hasText: "active services" })).toContainText("not observed");
  await page.evaluate(async () => { await fetch("/release-old"); });
  await expect.poll(() => settled.has(first)).toBe(true);
  await page.clock.fastForward(1000);
  await expect(content).toContainText("NewerObservation");
  await expect(content).not.toContainText("OlderObservation");
} finally {
  await browser.close();
}
