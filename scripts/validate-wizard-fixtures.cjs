#!/usr/bin/env node
// SPDX-License-Identifier: BSD-3-Clause

const fs = require("fs");
const path = require("path");
const { createRequire } = require("module");

const requireFromWebsite = createRequire(path.resolve("website/package.json"));
const Ajv2020 = requireFromWebsite("ajv/dist/2020").default;
const yaml = requireFromWebsite("js-yaml");

const [schemaPath, fixtureDir] = process.argv.slice(2);
if (!schemaPath || !fixtureDir) {
  console.error("usage: validate-wizard-fixtures.cjs <schema.json> <fixture-dir>");
  process.exit(2);
}

const schema = JSON.parse(fs.readFileSync(schemaPath, "utf8"));
const ajv = new Ajv2020({ allErrors: true, strict: false });
const validate = ajv.compile(schema);
let failed = false;

for (const fullPath of listYamlFiles(fixtureDir)) {
  const doc = yaml.load(fs.readFileSync(fullPath, "utf8"));
  if (!validate(doc)) {
    failed = true;
    console.error(`${fullPath}: JSON Schema validation failed`);
    for (const err of validate.errors || []) {
      console.error(`  ${err.instancePath || "/"} ${err.message}`);
    }
  }
}

if (failed) {
  process.exit(1);
}

function listYamlFiles(root) {
  const out = [];
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    const fullPath = path.join(root, entry.name);
    if (entry.isDirectory()) {
      out.push(...listYamlFiles(fullPath));
      continue;
    }
    if (entry.isFile() && entry.name.endsWith(".yaml")) {
      out.push(fullPath);
    }
  }
  return out.sort((a, b) => a.localeCompare(b));
}
