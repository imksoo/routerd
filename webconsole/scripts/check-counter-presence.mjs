// SPDX-License-Identifier: BSD-3-Clause

import { chromium } from "playwright";
import { expect } from "@playwright/test";

// The Go fixture serves the actual Handler, SQLite log, and optional native
// counters. Polling and reconciliation run through the built frontend.
const baseURL = process.argv[2];
const browser = await chromium.launch({
  executablePath: process.env.ROUTERD_WEB_CONSOLE_CHROMIUM_PATH,
  args: process.env.ROUTERD_WEB_CONSOLE_CHROMIUM_ARGS ? JSON.parse(process.env.ROUTERD_WEB_CONSOLE_CHROMIUM_ARGS) : undefined,
});
try {
  const page = await browser.newPage();
  await page.clock.install();
  await page.goto(`${baseURL}/#clients/traffic`, { waitUntil: "domcontentloaded" });
  const row = page.getByRole("table").filter({ hasText: "Bytes out" }).getByRole("row").filter({ hasText: "192.0.2.1" });
  await expect(row).toContainText("≥ 1.00 KiB");
  await expect(row).toContainText("≥ 512 B");
  await page.evaluate(async () => { await fetch("/set-counter-mode?value=partial-update"); });
  await page.clock.fastForward(30000);
  await expect(row).toContainText("≥ 2.00 KiB");
  await expect(row).toContainText("≥ 768 B");
  await page.evaluate(async () => { await fetch("/set-counter-mode?value=zero"); });
  await page.clock.fastForward(30000);
  await expect(row.getByRole("cell", { name: "0 B", exact: true })).toHaveCount(2);
  await expect(row).not.toContainText("not collected");
  await page.evaluate(async () => { await fetch("/set-counter-mode?value=unknown"); });
  await page.clock.fastForward(30000);
  await expect(row.getByRole("cell", { name: "not collected", exact: true })).toHaveCount(2);
  await expect(row).not.toContainText("0 B");
} finally {
  await browser.close();
}
