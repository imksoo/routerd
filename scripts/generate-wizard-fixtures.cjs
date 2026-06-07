#!/usr/bin/env node
// SPDX-License-Identifier: BSD-3-Clause

const fs = require("fs");
const path = require("path");

const [builderPath, outDir] = process.argv.slice(2);
if (!builderPath || !outDir) {
  console.error("usage: generate-wizard-fixtures.cjs <compiled-routerdWizard.js> <out-dir>");
  process.exit(2);
}

const builder = require(path.resolve(builderPath));
const fixtures = builder.buildHomeRouterFixtureYamls();

fs.rmSync(outDir, { recursive: true, force: true });
fs.mkdirSync(outDir, { recursive: true });
for (const [name, yaml] of Object.entries(fixtures).sort(([a], [b]) => a.localeCompare(b))) {
  fs.writeFileSync(path.join(outDir, name), yaml, "utf8");
}
