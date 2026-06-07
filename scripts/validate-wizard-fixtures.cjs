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
const samProfilesByDir = new Map();

for (const fullPath of listYamlFiles(fixtureDir)) {
  const doc = yaml.load(fs.readFileSync(fullPath, "utf8"));
  if (!validate(doc)) {
    failed = true;
    console.error(`${fullPath}: JSON Schema validation failed`);
    for (const err of validate.errors || []) {
      console.error(`  ${err.instancePath || "/"} ${err.message}`);
    }
  }
  recordSAMTransportProfile(fullPath, doc);
}

for (const [dir, profiles] of Array.from(samProfilesByDir.entries()).sort(([a], [b]) => a.localeCompare(b))) {
  if (profiles.length < 2) {
    continue;
  }
  const expectedTopology = profiles[0].topology;
  const expectedInnerPrefix = profiles[0].innerPrefix;
  for (const profile of profiles.slice(1)) {
    if (profile.topology !== expectedTopology || profile.innerPrefix !== expectedInnerPrefix) {
      failed = true;
      console.error(`${dir}: generated SAM node bundle is inconsistent`);
      console.error(`  expected topology=${expectedTopology} innerPrefix=${expectedInnerPrefix}`);
      console.error(`  ${profile.file} topology=${profile.topology} innerPrefix=${profile.innerPrefix}`);
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

function recordSAMTransportProfile(fullPath, doc) {
  const resources = doc?.spec?.resources;
  if (!Array.isArray(resources)) {
    return;
  }
  const profile = resources.find((resource) =>
    resource?.apiVersion === "mobility.routerd.net/v1alpha1" &&
    resource?.kind === "SAMTransportProfile"
  );
  if (!profile) {
    return;
  }
  const topology = Array.isArray(profile.spec?.topologyNodeRefs)
    ? [...profile.spec.topologyNodeRefs].sort().join(",")
    : "";
  const innerPrefix = profile.spec?.innerPrefix || "";
  const dir = path.dirname(fullPath);
  const profiles = samProfilesByDir.get(dir) || [];
  profiles.push({ file: path.basename(fullPath), topology, innerPrefix });
  samProfilesByDir.set(dir, profiles);
}
