// SPDX-License-Identifier: BSD-3-Clause

import http from "node:http";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { expect } from "@playwright/test";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const webconsoleDir = path.resolve(scriptDir, "..");
const repoRoot = path.resolve(webconsoleDir, "..");
const staticDir = path.resolve(repoRoot, "pkg", "webconsole", "static");
const outputDir = process.env.ROUTERD_WEB_CONSOLE_SCREENSHOT_DIR
  ? path.resolve(process.env.ROUTERD_WEB_CONSOLE_SCREENSHOT_DIR)
  : path.resolve(repoRoot, "dist", "webconsole-screenshots");

if (!existsSync(path.join(staticDir, "index.html"))) {
  throw new Error(`missing built Web Console at ${staticDir}; run npm run build first`);
}

await mkdir(outputDir, { recursive: true });

let natResourceRevision = 0;
let configRevision = 0;
let observationMode = "normal";
let largeConfig = false;
let recursiveConfig = false;
let currentFileDiffRequests = 0;
let holdGeneration13 = false;
let pendingGenerationResponse = null;
const connectionResourceRequests = [];

const server = http.createServer(async (req, res) => {
  try {
    const url = new URL(req.url ?? "/", "http://127.0.0.1");
    if (url.pathname === "/api/v1/summary") {
      const fixture = summaryFixture();
      if (natResourceRevision > 0) {
        fixture.resources.find(resource => resource.name === "ds-lite-a").status.innerLocalIPv4 = "192.0.0.9";
        fixture.connections.bySNAT = { "192.0.0.9": { total: 702, tcp: 691, udp: 10, other: 1 } };
      }
      if (url.searchParams.get("connections") === "600") connectionResourceRequests.push(url.searchParams.get("resources"));
      if (observationMode === "partial") fixture.connections.count = 936;
      if (observationMode.startsWith("client-")) {
        for (const client of fixture.clients) {
          client.state = observationMode === "client-stale" ? "STALE" : observationMode === "client-mixed" ? "STALE,REACHABLE" : "";
          if (observationMode === "client-zero-accounting") {
            client.bytesOut = 0;
            client.bytesIn = 0;
            if (client.hostname !== "aiseg2") { delete client.bytesOut; delete client.bytesIn; }
          }
          client.sources = ["sticky-history", "traffic", "dns"];
        }
      }
      fixture.interfaces[0].configuredAddresses = ["192.0.2.250/24"];
      if (observationMode === "unknown-health") {
        fixture.gatewayHealth.overall = "unknown";
        fixture.gatewayHealth.components[0].status = "unknown";
        fixture.gatewayHealth.components[0].phase = "NotObserved";
      }
      fixture.resources.push({ apiVersion: "system.routerd.net/v1alpha1", kind: "KernelModule", name: "unobserved-module", declared: true, status: { phase: "NotObserved" } });
      fixture.resources.push({ apiVersion: "net.routerd.net/v1alpha1", kind: "WireGuardPeer", name: "removed-peer", declared: false, status: { phase: "Pending", reason: "WithdrawalFailed", interface: "wg0", configured: true } });
      if (observationMode === "unaccounted-traffic") {
        fixture.trafficFlows = [{ clientAddress: "192.0.2.1", peerAddress: "198.51.100.1", peerPort: 443, protocol: "tcp", resolvedHostname: "unaccounted.example" }];
      }
      if (["interface-failed", "interface-changed", "interface-empty"].includes(observationMode)) {
        delete fixture.interfaces[0].addresses;
        if (observationMode !== "interface-empty") fixture.interfaces[0].observationError = "native address read denied";
        if (observationMode === "interface-changed") fixture.interfaces[0].ifname = "ens99";
      }
      const sections = [
        ["resources", ["resources", "interfaces", "phases", "gatewayHealth"], url.searchParams.get("resources") !== "0"],
        ["events", ["events"], url.searchParams.get("events") !== "-1"],
        ["connections", ["connections"], url.searchParams.get("connections") !== "-1"],
        ["dnsQueries", ["dnsQueries"], url.searchParams.get("dnsQueries") !== "-1"],
        ["trafficFlows", ["trafficFlows"], url.searchParams.get("trafficFlows") !== "-1"],
        ["firewallLogs", ["firewallLogs"], url.searchParams.get("firewallLogs") !== "-1"],
        ["dhcpLeases", ["dhcpLeases"], url.searchParams.get("dhcpLeases") !== "0" || url.searchParams.get("clients") === "1"],
        ["conntrackTuning", ["conntrackTuning"], url.searchParams.get("tuning") === "1"],
        ["clients", ["clients"], url.searchParams.get("clients") === "1"],
        ["neighbors", ["neighbors"], url.searchParams.get("clients") === "1"],
        ["dhcpFingerprints", ["dhcpFingerprints"], url.searchParams.get("clients") === "1"],
        ["vpn", ["vpn"], url.searchParams.get("vpn") !== "0"],
      ];
      fixture.conntrackTuning = { window: "24h", suggestions: [] };
      fixture.collected = [];
      for (const [section, fields, requested] of sections) {
        const failed = (observationMode === "failed" && section === "resources") || observationMode === "unavailable";
        const skipped = observationMode === "partial" && section === "resources";
        if (requested && !failed && !skipped) fixture.collected.push(section);
        if (!requested || failed || skipped || observationMode === "empty") {
          for (const field of fields) delete fixture[field];
        }
      }
      if (!fixture.gatewayHealth) fixture.gatewayHealth = { overall: "unknown" };
      if (observationMode === "failed") fixture.errors = ["resource store unavailable"];
      if (observationMode === "unavailable") fixture.errors = ["requested observation unavailable"];
      if (observationMode === "empty" && url.searchParams.get("connections") !== "-1") fixture.connections = { count: 0, byFamily: {}, entries: [] };
      if (observationMode === "empty" && url.searchParams.get("tuning") === "1") fixture.conntrackTuning = { window: "24h", suggestions: [] };
      return json(res, fixture);
    }
    if (url.pathname === "/api/v1/firewall/deny-timeline") {
      if (observationMode === "unavailable") {
        res.writeHead(503, { "content-type": "text/plain; charset=utf-8" });
        res.end("deny database unavailable");
        return;
      }
      return json(res, []);
    }
    if (url.pathname === "/api/v1/routes") return json(res, routesFixture());
    if (url.pathname === "/api/v1/config") return json(res, configFixture());
    if (url.pathname === "/api/v1/generations") return json(res, generationsFixture());
    if (url.pathname.startsWith("/api/v1/generations/")) return generationResponse(url.pathname, res);

    const requestedPath = url.pathname === "/" ? "/index.html" : url.pathname;
    const filePath = path.resolve(staticDir, `.${requestedPath}`);
    if (!filePath.startsWith(staticDir)) return notFound(res);
    if (!existsSync(filePath)) return notFound(res);
    let data = await readFile(filePath);
    if (filePath.endsWith("index.html")) {
      data = Buffer.from(String(data)
        .replaceAll("__ROUTERD_BASE_PATH__", "/")
        .replaceAll("__ROUTERD_TITLE_TEXT__", "routerd")
        .replaceAll("__ROUTERD_TITLE_JS__", "routerd"));
    }
    res.writeHead(200, {
      "content-type": contentType(filePath),
      "cache-control": "no-store",
    });
    res.end(data);
  } catch (error) {
    res.writeHead(500, { "content-type": "text/plain; charset=utf-8" });
    res.end(String(error));
  }
});

await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
const address = server.address();
const port = typeof address === "object" && address ? address.port : 0;
const baseURL = `http://127.0.0.1:${port}/`;

const browser = await chromium.launch({
  executablePath: process.env.ROUTERD_WEB_CONSOLE_CHROMIUM_PATH,
  args: process.env.ROUTERD_WEB_CONSOLE_CHROMIUM_ARGS ? JSON.parse(process.env.ROUTERD_WEB_CONSOLE_CHROMIUM_ARGS) : undefined,
});
try {
  const refreshContext = await browser.newContext();
  // Start with a fresh page: no Overview/Resources response may seed its state.
  const direct = await refreshContext.newPage();
  await direct.clock.install();
  await direct.goto(`${baseURL}#connections`);
  const directNAT = direct.getByRole("table", { name: "DS-Lite NATエントリー数" });
  await expect(directNAT).toBeVisible();
  await expect(directNAT.getByRole("row").filter({ hasText: "ds-lite-a" })).toContainText("701");
  expect(connectionResourceRequests.at(-1)).toBe("1");
  await expect(direct.getByText("[2606:4700:3030::6815:1001]:443", { exact: true })).toBeVisible();
  const requestsBeforeRefresh = connectionResourceRequests.length;
  natResourceRevision = 1;
  await direct.clock.fastForward(30000);
  await expect(directNAT.getByRole("row").filter({ hasText: "ds-lite-a" })).toContainText("192.0.0.9");
  await expect(directNAT.getByRole("row").filter({ hasText: "ds-lite-a" })).toContainText("702");
  expect(connectionResourceRequests.length).toBeGreaterThan(requestsBeforeRefresh);
  expect(connectionResourceRequests.every(value => value === "1")).toBe(true);
  await direct.close();
  natResourceRevision = 0;

  // Navigate after mounting: polling must request the current view, and config
  // refresh must pick up canonical changes made through another management path.
  const navigated = await refreshContext.newPage();
  await navigated.clock.install();
  await navigated.goto(`${baseURL}#overview`);
  await expect(navigated.locator("main")).toBeVisible();
  await expect(navigated.getByText(/^Updated/)).toBeVisible();
  await navigated.evaluate(() => { window.location.hash = "connections"; });
  await navigated.clock.fastForward(1000);
  const navigatedNAT = navigated.getByRole("table", { name: "DS-Lite NATエントリー数" });
  await expect(navigatedNAT.getByRole("row").filter({ hasText: "ds-lite-a" })).toContainText("701");
  const requestsBeforeNavigationPoll = connectionResourceRequests.length;
  natResourceRevision = 1;
  await navigated.clock.fastForward(30000);
  await expect(navigatedNAT.getByRole("row").filter({ hasText: "ds-lite-a" })).toContainText("702");
  expect(connectionResourceRequests.length).toBeGreaterThan(requestsBeforeNavigationPoll);
  await navigated.evaluate(() => { window.location.hash = "config"; });
  await navigated.clock.fastForward(1000);
  await navigated.getByRole("button", { name: "Raw YAML", exact: true }).click();
  await expect(navigated.locator("main pre")).toContainText("name: homert02");
  configRevision = 1;
  await navigated.clock.fastForward(30000);
  await expect(navigated.locator("main pre")).toContainText("name: refreshed-router");
  largeConfig = true;
  await navigated.clock.fastForward(30000);
  await expect(navigated.locator("main pre")).toContainText("large configuration context");
  await navigated.getByRole("button", { name: "Diff before apply", exact: true }).click();
  await expect(navigated.locator("main")).toContainText("Current file vs latest stored generation");
  await expect(navigated.locator("main")).toContainText("+++ current-file.yaml");
  expect(currentFileDiffRequests).toBe(1);
  configRevision = 2;
  await navigated.clock.fastForward(30000);
  await expect(navigated.locator("main pre")).toContainText("name: third-router");
  await expect(navigated.getByText("Current file vs latest stored generation", { exact: true })).toHaveCount(0);
  configRevision = 1;
  await navigated.evaluate(() => { window.location.hash = "generations"; });
  await navigated.clock.fastForward(1000);
  const generations = navigated.locator("#generations-table");
  const generation13 = generations.getByRole("row").filter({ hasText: "#13" });
  const generation12 = generations.getByRole("row").filter({ hasText: "#12" });
  await expect(generation13).toBeVisible();
  holdGeneration13 = true;
  const firstSettled = new Promise(resolve => {
    const settled = request => {
      if (!request.url().endsWith("/api/v1/generations/13/config")) return;
      navigated.off("requestfinished", settled);
      navigated.off("requestfailed", settled);
      resolve();
    };
    navigated.on("requestfinished", settled);
    navigated.on("requestfailed", settled);
  });
  await generation13.getByRole("button", { name: "View", exact: true }).click();
  await expect.poll(() => pendingGenerationResponse !== null).toBe(true);
  await generation12.getByRole("button", { name: "View", exact: true }).click();
  await expect(navigated.getByText("Generation #12", { exact: true })).toBeVisible();
  const delayed = pendingGenerationResponse;
  pendingGenerationResponse = null;
  delayed.writeHead(200, { "content-type": "text/plain; charset=utf-8" });
  delayed.end("# stored generation 13\n" + configFixture().text);
  await firstSettled;
  await navigated.clock.fastForward(1000);
  await expect(navigated.getByText("Generation #12", { exact: true })).toBeVisible();
  await expect(navigated.locator("#generation-result pre")).toContainText("stored generation 12");
  await expect(navigated.getByText("Generation #13", { exact: true })).toHaveCount(0);
  holdGeneration13 = false;
  await navigated.close();
  largeConfig = false;
  natResourceRevision = 0;
  configRevision = 0;

  // Use the shipped app: omitted rows mean empty only after a successful read.
  const observations = await refreshContext.newPage();
  await observations.clock.install();
  await observations.goto(`${baseURL}#overview`);
  const interfaces = observations.locator("#overview-interfaces");
  await expect(interfaces).toContainText("ens18");
  await expect(observations.locator("#overview-metrics").getByText("0% / 1.00 GiB", { exact: true })).toHaveCount(2);
  await expect(observations.getByText("1 gateway components", { exact: true })).toBeVisible();
  observationMode = "partial";
  await observations.clock.fastForward(30000);
  await expect(observations.locator("#overview-metrics")).toContainText("936/262144");
  await expect(interfaces).toContainText("ens18");
  await expect(observations.getByText("1 gateway components", { exact: true })).toBeVisible();
  observationMode = "interface-failed";
  await observations.clock.fastForward(30000);
  const oldInterface = interfaces.getByRole("group", { name: "Interface ens18", exact: true });
  await expect(oldInterface).toContainText("Previously observed addresses; current read failed");
  await expect(oldInterface).toContainText("192.168.1.249/24");
  await expect(oldInterface).toContainText("Configured addresses");
  await expect(oldInterface).toContainText("192.0.2.250/24");
  observationMode = "interface-changed";
  await observations.clock.fastForward(30000);
  const changedInterface = interfaces.getByRole("group", { name: "Interface ens99", exact: true });
  await expect(changedInterface).toContainText("Current addresses unknown");
  await expect(changedInterface).not.toContainText("192.168.1.249/24");
  observationMode = "normal";
  await observations.clock.fastForward(30000);
  await expect(oldInterface).toContainText("192.168.1.249/24");
  observationMode = "interface-empty";
  await observations.clock.fastForward(30000);
  await expect(oldInterface).toContainText("No address observed");
  await expect(oldInterface).not.toContainText("192.168.1.249/24");
  observationMode = "failed";
  await observations.clock.fastForward(30000);
  await expect(observations.getByRole("alert")).toContainText("resource store unavailable");
  await expect(observations.getByText(/^Partial update/)).toBeVisible();
  await expect(interfaces).toContainText("ens18");
  observationMode = "empty";
  await observations.clock.fastForward(30000);
  await expect(interfaces).not.toContainText("ens18");
  await expect(observations.getByText("No gateway component status observed", { exact: true })).toBeVisible();
  await expect(observations.getByRole("alert")).toHaveCount(0);
  await observations.close();
  observationMode = "normal";

  // The resource inventory includes all declared kinds. Absence of observed
  // effect/controller state must not turn a declaration into healthy/live.
  const inventory = await refreshContext.newPage();
  await inventory.clock.install();
  await inventory.goto(`${baseURL}#resources`);
  const resourceTable = inventory.locator("#resources-table table");
  const unobserved = resourceTable.getByRole("row").filter({ hasText: "unobserved-module" });
  await expect(unobserved).toContainText("NotObserved");
  await expect(unobserved).toContainText("unknown");
  const retained = resourceTable.getByRole("row").filter({ hasText: "removed-peer" });
  await expect(retained).toContainText("retained status; not declared in current config");
  await expect(retained).toContainText("Pending");
  await expect(retained).toContainText("WithdrawalFailed");
  await expect(resourceTable.getByRole("row").filter({ hasText: "Package" }).first()).toBeVisible();
  const unknownPhaseColor = await unobserved.getByText("NotObserved", { exact: true }).evaluate(element => getComputedStyle(element.closest(".fui-Badge")).color);
  const healthyPhaseColor = await resourceTable.getByText("Healthy", { exact: true }).first().evaluate(element => getComputedStyle(element.closest(".fui-Badge")).color);
  expect(unknownPhaseColor).not.toBe(healthyPhaseColor);
  observationMode = "unknown-health";
  await inventory.evaluate(() => { window.location.hash = "overview"; });
  await inventory.clock.fastForward(1000);
  const unknownHealth = inventory.locator("#overview-gateway-health");
  await expect(unknownHealth.getByText("unknown", { exact: true })).toBeVisible();
  await expect(unknownHealth.getByText("unknown 1", { exact: true })).toBeVisible();
  await expect(unknownHealth.getByText("skip 0", { exact: true })).toBeVisible();
  await inventory.close();
  observationMode = "normal";

  observationMode = "unavailable";
  const unknownConnections = await refreshContext.newPage();
  await unknownConnections.clock.install();
  await unknownConnections.goto(`${baseURL}#connections`);
  await expect(unknownConnections.getByRole("alert")).toContainText("requested observation unavailable");
  await expect(unknownConnections.getByText("Connection observation unavailable", { exact: true })).toBeVisible();
  await expect(unknownConnections.getByText("IPv4", { exact: true }).locator("..")).toContainText("-");
  observationMode = "empty";
  await unknownConnections.clock.fastForward(30000);
  await expect(unknownConnections.getByText("Connection observation unavailable", { exact: true })).toHaveCount(0);
  await expect(unknownConnections.getByText("IPv4", { exact: true }).locator("..")).toContainText("0");
  await unknownConnections.close();
  observationMode = "unavailable";
  const unknownDenies = await refreshContext.newPage();
  await unknownDenies.clock.install();
  await unknownDenies.goto(`${baseURL}#firewall/timeline`);
  await expect(unknownDenies.getByText("Firewall deny observation unavailable", { exact: true })).toBeVisible();
  await expect(unknownDenies.getByText("Conntrack tuning observation unavailable", { exact: true })).toBeVisible();
  await expect(unknownDenies.getByText("No denies in the last 24 hours", { exact: true })).toHaveCount(0);
  observationMode = "empty";
  await unknownDenies.clock.fastForward(30000);
  await expect(unknownDenies.getByText("No denies in the last 24 hours", { exact: true })).toBeVisible();
  await expect(unknownDenies.getByText("No tuning suggestions from the current observation window", { exact: true })).toBeVisible();
  await unknownDenies.close();
  observationMode = "normal";

  observationMode = "unaccounted-traffic";
  const passive = await refreshContext.newPage();
  await passive.clock.install();
  await passive.goto(`${baseURL}#overview`);
  const domains = passive.locator(".fui-Card").filter({ has: passive.getByText("Top SNI / domains", { exact: true }) });
  await expect(domains).toContainText("unaccounted.example");
  await expect(domains).toContainText("1 flow");
  await expect(domains).not.toContainText("1 B");
  await expect(passive.locator(".fui-Card").filter({ has: passive.getByText("Top protocols", { exact: true }) })).toContainText("No measured byte volume");
  observationMode = "normal";
  await passive.evaluate(() => { window.location.hash = "firewall/timeline"; });
  await passive.clock.fastForward(1000);
  const ackPacket = passive.getByRole("table", { name: "Deny timeline", exact: true }).getByRole("row").filter({ hasText: "192.0.2.77" });
  await expect(ackPacket).toContainText("unknown");
  await expect(ackPacket).toContainText("ACK packet");
  await expect(ackPacket).not.toContainText("true suspicious");
  await expect(ackPacket).not.toContainText("established follow");
  await passive.close();

  // Real YAML aliases may be recursive. Keep the raw file readable instead
  // of sending a cyclic object graph into the recursive tree renderer.
  recursiveConfig = true;
  const recursive = await refreshContext.newPage();
  await recursive.clock.install();
  const recursiveErrors = [];
  recursive.on("pageerror", error => recursiveErrors.push(String(error)));
  await recursive.goto(`${baseURL}#config`);
  await expect(recursive.locator("main")).toContainText("Recursive YAML aliases cannot be shown as a tree");
  await expect(recursive.locator("main pre")).toContainText("child: *loop");
  recursiveConfig = false;
  await recursive.clock.fastForward(30000);
  await expect(recursive.locator("main")).not.toContainText("Recursive YAML aliases cannot be shown as a tree");
  await expect(recursive.locator("main")).toContainText("homert02");
  expect(recursiveErrors).toEqual([]);
  await recursive.close();

  // Addresses, retained leases and recent traffic identify a device without
  // proving it is reachable now. Exercise the shipped inventory's badges.
  const clientPresence = await refreshContext.newPage();
  await clientPresence.clock.install();
  await clientPresence.goto(`${baseURL}#clients/inventory`);
  const clientRows = clientPresence.locator('[data-client-row="true"]');
  await expect(clientRows).toHaveCount(2);
  await expect(clientRows.first()).toHaveAttribute("data-reachability", "reachable");
  for (const mode of ["client-history", "client-stale"]) {
    observationMode = mode;
    await clientPresence.clock.fastForward(30000);
    await expect(clientRows.first()).toHaveAttribute("data-reachability", "unknown");
    await expect(clientRows.first().getByText("unknown", { exact: true }).last()).toBeVisible();
    await expect(clientRows.first().getByText("offline", { exact: true })).toHaveCount(0);
  }
  observationMode = "client-mixed";
  await clientPresence.clock.fastForward(30000);
  await expect(clientRows.first()).toHaveAttribute("data-reachability", "reachable");
  observationMode = "normal";
  const dualStackClient = clientRows.filter({ hasText: "mcberry" });
  await dualStackClient.getByRole("button", { name: "Expand client details", exact: true }).click();
  await expect(dualStackClient).toContainText("IPv6");
  await expect(dualStackClient).not.toContainText("IPv6 privacy");
  await expect(dualStackClient).not.toContainText("IPv6 stable");
  observationMode = "client-zero-accounting";
  await clientPresence.clock.fastForward(30000);
  const zeroAccountedClient = clientRows.filter({ hasText: "aiseg2" });
  await zeroAccountedClient.getByRole("button", { name: "Expand client details", exact: true }).click();
  await expect(zeroAccountedClient).toContainText("out 0 B / in 0 B");
  await expect(dualStackClient).toContainText("out not collected / in not collected");
  observationMode = "normal";
  await clientPresence.evaluate(() => { window.location.hash = "clients/leases"; });
  await clientPresence.clock.fastForward(1000);
  await expect(clientPresence.locator("#clients-leases").getByRole("columnheader", { name: "MAC / IAID", exact: true })).toBeVisible();
  await expect(clientPresence.locator("#clients-leases").getByRole("cell", { name: "IAID 1", exact: true })).toBeVisible();
  await clientPresence.close();
  observationMode = "normal";

  if (!process.argv.includes("--check-refresh")) {
    const page = await browser.newPage({ viewport: { width: 1440, height: 980 }, deviceScaleFactor: 1 });
    await capture(page, "#overview", "overview-desktop.png");
    await capture(page, "#routes", "routes-desktop.png");
    await capture(page, "#controllers", "controllers-desktop.png");
    await capture(page, "#connections", "connections-desktop.png");
    const natTable = page.getByRole("table", { name: "DS-Lite NATエントリー数" });
    await natTable.waitFor();
    if (!(await natTable.innerText()).includes("701")) throw new Error("NAT summary must use full snapshot, not displayed entries");
    if (!(await natTable.getByRole("row").filter({ hasText: "ds-lite-b" }).innerText()).includes("0")) throw new Error("Successful snapshot must show zero for an idle tunnel");
    if (!(await natTable.getByRole("row").filter({ hasText: "ds-lite-pending" }).innerText()).includes("未取得")) throw new Error("Unresolved tunnel must not show zero");
    await capture(page, "#clients/inventory", "clients-desktop.png");
    await capture(page, "#firewall/timeline", "firewall-desktop.png");
    await capture(page, "#config", "config-desktop.png");

    const mobile = await browser.newPage({
      viewport: { width: 390, height: 844 },
      deviceScaleFactor: 2,
      isMobile: true,
      hasTouch: true,
    });
    await capture(mobile, "#overview", "overview-mobile.png");
    await capture(mobile, "#routes", "routes-mobile.png");
    await capture(mobile, "#connections", "connections-mobile.png");
    await capture(mobile, "#clients/inventory", "clients-mobile.png");
  }
} finally {
  await browser.close();
  server.close();
}

if (!process.argv.includes("--check-refresh")) {
  await writeFile(
    path.join(outputDir, "README.txt"),
    `Generated by webconsole/scripts/screenshot.mjs\nBase URL: ${baseURL}\n`,
  );
}
console.log(process.argv.includes("--check-refresh") ? "Web Console refresh checks passed" : `Wrote Web Console screenshots to ${outputDir}`);

async function capture(page, hash, fileName) {
  // domcontentloaded is enough for the screenshot — the Web Console opens a
  // long-lived /api/v1/events/stream Server-Sent Events connection on mount,
  // which means waitUntil: "networkidle" can stall indefinitely (idle
  // requires 2 s of zero in-flight requests, but SSE keeps the connection
  // open). Bound the goto and the optional networkidle wait so a flaky run
  // cannot hang the release workflow.
  await page.goto(`${baseURL}${hash}`, { waitUntil: "domcontentloaded", timeout: 30000 });
  await page.waitForSelector("main", { timeout: 15000 });
  await page.waitForLoadState("networkidle", { timeout: 5000 }).catch(() => {});
  await page.screenshot({ path: path.join(outputDir, fileName), fullPage: true });
}

function json(res, body) {
  res.writeHead(200, {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "no-store",
  });
  res.end(JSON.stringify(body));
}

function notFound(res) {
  res.writeHead(404, { "content-type": "text/plain; charset=utf-8" });
  res.end("not found");
}

function generationResponse(urlPath, res) {
  if (/^\/api\/v1\/generations\/\d+\/diff\/current$/.test(urlPath)) {
    currentFileDiffRequests++;
    res.writeHead(200, { "content-type": "text/plain; charset=utf-8" });
    res.end("--- generation-13.yaml\n+++ current-file.yaml\n@@ -1,1 +1,1 @@\n-name: previous-router\n+name: refreshed-router\n");
    return;
  }
  const configMatch = urlPath.match(/^\/api\/v1\/generations\/(\d+)\/config$/);
  if (configMatch) {
    if (holdGeneration13 && configMatch[1] === "13") {
      pendingGenerationResponse = res;
      return;
    }
    res.writeHead(200, { "content-type": "text/plain; charset=utf-8" });
    res.end(`# stored generation ${configMatch[1]}\n` + configFixture().text);
    return;
  }
  const diffMatch = urlPath.match(/^\/api\/v1\/generations\/(\d+)\/diff\/(\d+)$/);
  if (diffMatch) {
    res.writeHead(200, { "content-type": "text/plain; charset=utf-8" });
    res.end("--- generation-12.yaml\n+++ generation-13.yaml\n@@\n-  listenAddress: 127.0.0.1\n+  listenAddressFrom:\n+    resource: Interface/mgmt\n+    field: status.addresses[0]\n");
    return;
  }
  notFound(res);
}

function contentType(filePath) {
  if (filePath.endsWith(".html")) return "text/html; charset=utf-8";
  if (filePath.endsWith(".js")) return "text/javascript; charset=utf-8";
  if (filePath.endsWith(".css")) return "text/css; charset=utf-8";
  if (filePath.endsWith(".svg")) return "image/svg+xml";
  return "application/octet-stream";
}

function summaryFixture() {
  const now = new Date().toISOString();
  return {
    generatedAt: now,
    systemUsage: { cpuPercent: 0, memoryUsedPercent: 0, memoryTotalBytes: 1073741824, disks: [{ path: "/", usedPercent: 0, totalBytes: 1073741824 }] },
    gatewayHealth: { overall: "ok", components: [{ kind: "HealthCheck", name: "internet-via-dslite-a", status: "ok", phase: "Healthy" }] },
    status: {
      status: {
        phase: "Healthy",
        generation: 13,
        resourceCount: 42,
      },
    },
    controllers: [
      { name: "address", mode: "live", reason: "Live", message: "controller is applying host state", resourceKinds: ["IPv4StaticAddress", "IPv6DelegatedAddress"] },
      { name: "firewall", mode: "dry-run", reason: "Manual", message: "controller-chain dry-run flag is true; firewall rules are observed but not enforced by this controller", resourceKinds: ["FirewallZone", "FirewallPolicy", "FirewallRule"] },
      { name: "dns-resolver", mode: "live", reason: "Live", message: "controller is applying host state", resourceKinds: ["DNSResolver", "DNSZone"] },
    ],
    phases: { Healthy: 39, "Dry-run": 3 },
    resources: resourceFixture(),
    interfaces: interfaceFixture(),
    events: eventFixture(now),
    connections: connectionFixture(),
    dnsQueries: [
      { questionName: "www.google.com", answers: ["142.250.207.68"] },
      { questionName: "api.github.com", answers: ["140.82.112.6"] },
      { questionName: "routerd.net", answers: ["2606:4700:3030::6815:1001"] },
    ],
    trafficFlows: [
      { clientAddress: "172.18.0.150", peerAddress: "142.250.207.68", resolvedHostname: "www.google.com", bytesOut: 512000, bytesIn: 9400000 },
      { clientAddress: "172.18.1.110", peerAddress: "140.82.112.6", resolvedHostname: "api.github.com", bytesOut: 88000, bytesIn: 1200000 },
    ],
    firewallLogs: [
      { id: 92, ts: now, action: "drop", srcAddress: "192.0.2.77", dstAddress: "198.51.100.77", protocol: "tcp", l3Proto: "ipv4", srcPort: 49152, dstPort: 443, tcpFlags: "ACK" },
      { id: 91, ts: now, action: "drop", srcAddress: "192.168.178.40", dstAddress: "172.18.0.1", protocol: "tcp", l3Proto: "ipv4", dstPort: 8080, inIface: "ens18", outIface: "ens19", packetBytes: 60 },
      { id: 90, ts: now, action: "drop", srcAddress: "35.72.114.176", dstAddress: "172.18.1.110", protocol: "udp", l3Proto: "ipv4", dstPort: 41641, inIface: "ds-lite-a", packetBytes: 92 },
    ],
    dhcpLeases: [
      { expiresAt: now, mac: "18:ec:e7:33:12:6c", ip: "172.18.0.150", hostname: "aiseg2", family: "ipv4", source: "dnsmasq" },
      { expiresAt: now, mac: "4e:20:15:aa:e0:67", ip: "172.18.1.110", hostname: "mcberry", family: "ipv4", source: "dnsmasq" },
      { expiresAt: now, iaid: "1", clientId: "00:03:00:01:00:11:22:33:44:55", ip: "2001:db8::1", hostname: "host6", family: "ipv6", source: "dnsmasq" },
    ],
    neighbors: [
      { ip: "2409:10:3d60:1271:879:3d5a:205d:ba39", ifname: "ens19", mac: "4e:20:15:aa:e0:67", state: "reachable", source: "ip-neigh" },
      { ip: "172.18.0.150", ifname: "ens19", mac: "18:ec:e7:33:12:6c", state: "reachable", source: "arp" },
    ],
    clients: [
      { id: "18:ec:e7:33:12:6c", hostname: "aiseg2", mac: "18:ec:e7:33:12:6c", vendor: "Panasonic", addresses: ["172.18.0.150"], state: "reachable", sources: ["dhcpv4", "neighbor"], bytesOut: 512000, bytesIn: 9400000, peers: ["www.google.com"] },
      { id: "4e:20:15:aa:e0:67", hostname: "mcberry", mac: "4e:20:15:aa:e0:67", addresses: ["172.18.1.110", "2409:10:3d60:1271:879:3d5a:205d:ba39"], state: "reachable", sources: ["dhcpv4", "neighbor"], bytesOut: 88000, bytesIn: 1200000, peers: ["api.github.com"] },
    ],
    vpn: {
      wireGuard: [
        { name: "wg0", publicKey: "wg-interface-public", listenPort: 51820, peers: [{ publicKey: "peer-public", endpoint: "203.0.113.10:51820", allowedIPs: ["10.10.0.2/32"], latestHandshake: now, transferRxBytes: 2048, transferTxBytes: 4096, persistentKeepaliveSec: 25 }] },
      ],
      tailscale: { backendState: "Running", hostName: "homert02", dnsName: "homert02.tailnet.ts.net", tailscaleIPs: ["100.64.87.102"], online: true, active: true, exitNodeOption: true, peers: [{ id: "peer1", hostName: "phone", dnsName: "phone.tailnet.ts.net", online: true, active: true, relay: "tok", rxBytes: 123456, txBytes: 654321 }] },
    },
  };
}

function resourceFixture() {
  return [
    { apiVersion: "net.routerd.net/v1alpha1", kind: "EgressRoutePolicy", name: "ipv4-default", status: { phase: "Healthy", selectedCandidate: "ds-lite-a", selectedDevice: "ds-lite-a" } },
    { apiVersion: "net.routerd.net/v1alpha1", kind: "HealthCheck", name: "internet-via-dslite-a", status: { phase: "Healthy", target: "9.9.9.9" } },
    { apiVersion: "net.routerd.net/v1alpha1", kind: "DSLiteTunnel", name: "ds-lite-a", status: { phase: "Up", address: "192.0.0.2/29", innerLocalIPv4: "192.0.0.2" } },
    { apiVersion: "net.routerd.net/v1alpha1", kind: "DSLiteTunnel", name: "ds-lite-b", status: { phase: "Up", innerLocalIPv4: "192.0.0.3" } },
    { apiVersion: "net.routerd.net/v1alpha1", kind: "DSLiteTunnel", name: "ds-lite-pending", status: { phase: "Pending" } },
    { apiVersion: "net.routerd.net/v1alpha1", kind: "DNSResolver", name: "lan-resolver", status: { phase: "Ready", address: "172.18.0.1" } },
    { apiVersion: "firewall.routerd.net/v1alpha1", kind: "FirewallPolicy", name: "three-role", status: { phase: "Observed", changedFields: "dry-run" } },
    { apiVersion: "system.routerd.net/v1alpha1", kind: "Package", name: "router-tools", status: { phase: "Healthy" } },
  ];
}

function routesFixture() {
  const now = new Date().toISOString();
  return {
    generatedAt: now,
    routes: [
      { source: "kernel", family: "ipv4", destination: "default", gateway: "192.168.123.1", device: "wan0", protocol: "boot", table: "main", metric: "100", phase: "installed", observedAt: now },
      { source: "kernel", family: "ipv4", destination: "172.18.0.0/16", device: "lan0", protocol: "kernel", scope: "link", table: "main", phase: "installed", observedAt: now },
      { source: "static", resource: "IPv4StaticRoute/services", family: "ipv4", destination: "10.96.0.0/12", gateway: "172.18.0.10", device: "lan0", metric: "50", phase: "Applied", observedAt: now },
      { source: "bgp", resource: "BGPRouter/lan", family: "ipv4", destination: "10.250.0.0/24", protocol: "bgp", peer: "192.168.123.111", phase: "Established", observedAt: now },
      { source: "dhcpv4", resource: "DHCPv4Client/wan", family: "ipv4", destination: "default", gateway: "192.168.123.1", device: "wan0", protocol: "dhcp", metric: "100", phase: "Bound", observedAt: now },
    ],
    bgpPeers: [
      { router: "lan", peer: "192.168.123.111", asn: "64513", state: "Established", established: true, prefixesReceived: "12", messages: "82/79", lastEstablishedAt: now },
    ],
  };
}

function interfaceFixture() {
  return [
    { name: "wan", ifname: "ens18", phase: "Up", role: "untrust", zone: "wan", managed: false, mtu: 1454, hardwareAddress: "bc:24:11:aa:bb:01", addresses: ["192.168.1.249/24", "2409:10:3d60:1200::249/64"] },
    { name: "lan", ifname: "ens19", phase: "Up", role: "trust", zone: "lan", managed: true, mtu: 1454, hardwareAddress: "bc:24:11:aa:bb:02", addresses: ["172.18.0.1/16", "2409:10:3d60:1271::11/64", "2409:10:3d60:1271:be24:11ff:fee0:8e3a/64"] },
    { name: "mgmt", ifname: "ens20", phase: "Up", role: "mgmt", zone: "management", managed: false, mtu: 1500, hardwareAddress: "bc:24:11:aa:bb:03", addresses: ["192.168.123.129/24"] },
    { name: "tailscale", ifname: "tailscale0", phase: "Up", role: "trust", zone: "lan", managed: true, mtu: 1280, addresses: ["100.64.87.102/32"] },
  ];
}

function eventFixture(now) {
  return [
    { id: 1001, createdAt: now, severity: "info", topic: "routerd.resource.status.changed", resourceKind: "HealthCheck", resourceName: "internet-via-dslite-a", attributes: { phase: "Healthy", previousPhase: "Pending" } },
    { id: 1000, createdAt: now, severity: "info", topic: "routerd.dhcp.lease.renewed", resourceKind: "IPv4DHCPServer", resourceName: "lan-dhcp4", attributes: { mac: "18:ec:e7:33:12:6c", ip: "172.18.0.150", hostname: "aiseg2" } },
    { id: 999, createdAt: now, severity: "warn", topic: "routerd.firewall.packet.dropped", resourceKind: "FirewallPolicy", resourceName: "three-role", attributes: { action: "drop", source: "35.72.114.176", destination: "172.18.1.110", protocol: "udp" } },
  ];
}

function connectionFixture() {
  return {
    count: 935,
    max: 262144,
    byFamily: { ipv4: 710, ipv6: 225 },
    bySNAT: { "192.0.0.2": { total: 701, tcp: 690, udp: 10, other: 1 } },
    entries: [
      { family: "ipv4", protocol: "tcp", state: "ESTABLISHED", assured: true, timeout: 431999, original: { source: "172.18.1.110", sourcePort: "55124", destination: "140.82.112.6", destinationPort: "443" } },
      { family: "ipv4", protocol: "udp", state: "ASSURED", assured: true, timeout: 179, original: { source: "172.18.0.150", sourcePort: "53210", destination: "1.1.1.1", destinationPort: "53" } },
      { family: "ipv6", protocol: "tcp", state: "ESTABLISHED", assured: true, timeout: 431991, original: { source: "2409:10:3d60:1271:879:3d5a:205d:ba39", sourcePort: "55244", destination: "2606:4700:3030::6815:1001", destinationPort: "443" } },
      { family: "ipv6", protocol: "udp", state: "ASSURED", assured: true, timeout: 150, original: { source: "2409:10:3d60:1271:be24:11ff:fee0:8e3a", sourcePort: "41641", destination: "2606:4700:4700::1111", destinationPort: "53" } },
    ],
  };
}

function configFixture() {
  if (recursiveConfig) return { path: "/etc/routerd/router.yaml", text: "root: &loop\n  child: *loop\n" };
  return {
    path: "/usr/local/etc/routerd/router.yaml",
    text: (largeConfig ? "# large configuration context\n".repeat(10000) : "") + `apiVersion: routerd.net/v1alpha1\nkind: Router\nmetadata:\n  name: ${configRevision === 2 ? "third-router" : configRevision ? "refreshed-router" : "homert02"}\nspec:\n  resources:\n    - apiVersion: net.routerd.net/v1alpha1\n      kind: Interface\n      metadata:\n        name: lan\n      spec:\n        ifname: ens19\n        role: trust\n    - apiVersion: system.routerd.net/v1alpha1\n      kind: WebConsole\n      metadata:\n        name: mgmt-console\n      spec:\n        listenAddressFrom:\n          resource: Interface/mgmt\n          field: status.addresses[0]\n`,
  };
}

function generationsFixture() {
  return [
    { generation: 13, startedAt: "2026-05-11T03:30:00Z", finishedAt: "2026-05-11T03:30:04Z", phase: "Healthy", configHash: "aabbccddeeff001122334455", hasYaml: true },
    { generation: 12, startedAt: "2026-05-11T02:30:00Z", finishedAt: "2026-05-11T02:30:05Z", phase: "Healthy", configHash: "00112233445566778899aabb", hasYaml: true },
  ];
}
