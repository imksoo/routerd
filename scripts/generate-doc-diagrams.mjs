#!/usr/bin/env node

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import path from 'node:path';

const WIDTH = 1600;
const HEIGHT = 900;
const SVG_DIR = path.resolve('docs/images/diagrams');
const PNG_DIR = path.resolve('website/static/img/diagrams');
const CHECK = process.argv.includes('--check');

function esc(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;');
}

function wrap(text, max = 25) {
  const out = [];
  for (const raw of String(text).split('\n')) {
    let line = '';
    for (const word of raw.split(/\s+/)) {
      if (!word) continue;
      const next = line ? `${line} ${word}` : word;
      if (next.length > max && line) {
        out.push(line);
        line = word;
      } else {
        line = next;
      }
    }
    if (line) out.push(line);
  }
  return out.length ? out : [''];
}

function textBlock({ x, y, w, h, text, size = 27, weight = 600, color = '#14213d', anchor = 'middle', max = 25 }) {
  const lines = wrap(text, max);
  const lineHeight = Math.round(size * 1.22);
  const startY = y + h / 2 - ((lines.length - 1) * lineHeight) / 2 + size * 0.35;
  const tx = anchor === 'start' ? x + 28 : x + w / 2;
  return `<text x="${tx}" y="${startY}" font-size="${size}" font-weight="${weight}" fill="${color}" text-anchor="${anchor}" dominant-baseline="middle">${lines
    .map((line, i) => `<tspan x="${tx}" dy="${i === 0 ? 0 : lineHeight}">${esc(line)}</tspan>`)
    .join('')}</text>`;
}

function box({ x, y, w, h, text, fill = '#ffffff', stroke = '#203a5f', color = '#13233a', size = 23, weight = 700, radius = 18, max = 24, dashed = false }) {
  return `<g>
  <rect x="${x}" y="${y}" width="${w}" height="${h}" rx="${radius}" fill="${fill}" stroke="${stroke}" stroke-width="3"${dashed ? ' stroke-dasharray="10 9"' : ''}/>
  ${textBlock({ x, y, w, h, text, size, weight, color, max })}
</g>`;
}

function pill({ x, y, w, h, text, fill = '#eef6ff', stroke = '#3b82f6', color = '#123d6b', size = 22, max = 28 }) {
  return box({ x, y, w, h, text, fill, stroke, color, size, weight: 700, radius: h / 2, max });
}

function note({ x, y, w, h, text, fill = '#fff8df', stroke = '#d7a725', color = '#5b4400', size = 20, max = 34 }) {
  return box({ x, y, w, h, text, fill, stroke, color, size, weight: 650, radius: 14, max });
}

function arrow({ x1, y1, x2, y2, label, color = '#2f4f6f', width = 4, dashed = false, bend = 0 }) {
  const attrs = `fill="none" stroke="${color}" stroke-width="${width}" marker-end="url(#arrow)"${dashed ? ' stroke-dasharray="9 8"' : ''}`;
  const line = bend
    ? `<path d="M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}" ${attrs}/>`
    : `<line x1="${x1}" y1="${y1}" x2="${x2}" y2="${y2}" ${attrs}/>`;
  if (!label) return line;
  const lx = (x1 + x2) / 2;
  const ly = (y1 + y2) / 2 - 12;
  return `${line}<text x="${lx}" y="${ly}" font-size="22" font-weight="700" fill="${color}" text-anchor="middle">${esc(label)}</text>`;
}

function lane({ x, y, w, h, text, fill = '#f6f8fb' }) {
  return `<g>
  <rect x="${x}" y="${y}" width="${w}" height="${h}" rx="24" fill="${fill}" stroke="#d9e2ef" stroke-width="2"/>
  <text x="${x + 28}" y="${y + 42}" font-size="25" font-weight="800" fill="#45556f">${esc(text)}</text>
</g>`;
}

function svg(title, subtitle, body) {
  return `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="${WIDTH}" height="${HEIGHT}" viewBox="0 0 ${WIDTH} ${HEIGHT}" font-family="Inter, Arial, Helvetica, sans-serif">
<defs>
  <marker id="arrow" markerWidth="12" markerHeight="12" refX="10" refY="5.5" orient="auto">
    <path d="M 0 0 L 11 5.5 L 0 11 z" fill="#2f4f6f"/>
  </marker>
  <filter id="shadow" x="-10%" y="-10%" width="120%" height="130%">
    <feDropShadow dx="0" dy="5" stdDeviation="5" flood-color="#10233f" flood-opacity="0.14"/>
  </filter>
</defs>
<rect width="${WIDTH}" height="${HEIGHT}" fill="#f7fafc"/>
<rect x="28" y="28" width="1544" height="844" rx="30" fill="#ffffff" stroke="#e2e8f0" stroke-width="2"/>
<text x="74" y="92" font-size="42" font-weight="850" fill="#10233f">${esc(title)}</text>
<text x="76" y="130" font-size="23" font-weight="600" fill="#63758d">${esc(subtitle)}</text>
<g filter="url(#shadow)">
${body}
</g>
</svg>
`;
}

const diagrams = [
  {
    name: 'routerd-architecture',
    title: 'routerd architecture',
    subtitle: 'One declarative resource model flows through validation, effective config, controllers, state, and host renderers.',
    body: [
      lane({ x: 72, y: 170, w: 410, h: 610, text: 'Operator intent' }),
      lane({ x: 520, y: 170, w: 500, h: 610, text: 'routerd core' }),
      lane({ x: 1058, y: 170, w: 470, h: 610, text: 'Host runtime' }),
      box({ x: 112, y: 255, w: 315, h: 115, text: 'router.yaml\nresources', fill: '#e9f8ef', stroke: '#2e7d55' }),
      box({ x: 112, y: 448, w: 315, h: 115, text: 'routerctl\nvalidate plan apply', fill: '#eaf2ff', stroke: '#3267b1' }),
      box({ x: 555, y: 235, w: 400, h: 105, text: 'load + validate\nschema and references', fill: '#f5f3ff', stroke: '#6d5bd0' }),
      box({ x: 555, y: 395, w: 400, h: 105, text: 'effective config\nstartup + dynamic - masks', fill: '#fff4e6', stroke: '#c57a1c', max: 26 }),
      box({ x: 555, y: 575, w: 400, h: 115, text: 'SQLite state DB\nobjects events ledger', fill: '#eef8f9', stroke: '#2b8a92' }),
      box({ x: 1096, y: 232, w: 360, h: 115, text: 'controllers\nroutes DNS DHCP firewall BGP SAM', fill: '#edf7ed', stroke: '#347a35', size: 23, max: 24 }),
      box({ x: 1096, y: 412, w: 360, h: 115, text: 'renderers + daemons\nnft dnsmasq GoBGP services', fill: '#fff6e8', stroke: '#bd7515', max: 26 }),
      box({ x: 1096, y: 590, w: 360, h: 115, text: 'owned host artifacts\nroutes addresses tables units', fill: '#fcefee', stroke: '#b64e4a', max: 27 }),
      arrow({ x1: 428, y1: 312, x2: 555, y2: 288 }),
      arrow({ x1: 428, y1: 506, x2: 555, y2: 288, label: 'explicit trigger', bend: 70 }),
      arrow({ x1: 755, y1: 340, x2: 755, y2: 395 }),
      arrow({ x1: 955, y1: 448, x2: 1096, y2: 292 }),
      arrow({ x1: 1276, y1: 347, x2: 1276, y2: 412 }),
      arrow({ x1: 1276, y1: 527, x2: 1276, y2: 590 }),
      arrow({ x1: 1096, y1: 650, x2: 955, y2: 632, label: 'status + events', bend: 55 }),
      arrow({ x1: 755, y1: 500, x2: 755, y2: 575 }),
    ],
  },
  {
    name: 'dynamic-config-provider-actions',
    title: 'dynamic config and provider actions',
    subtitle: 'Trusted plugins can contribute runtime intent; provider mutations stay journaled and gated.',
    body: [
      lane({ x: 72, y: 178, w: 390, h: 592, text: 'Startup owned by operator' }),
      lane({ x: 500, y: 178, w: 465, h: 592, text: 'Dynamic intent' }),
      lane({ x: 1003, y: 178, w: 525, h: 592, text: 'Provider mutation path' }),
      box({ x: 112, y: 260, w: 302, h: 100, text: 'startup config\nfallbacks and policy', fill: '#e9f8ef', stroke: '#2e7d55', max: 26 }),
      box({ x: 112, y: 495, w: 302, h: 100, text: 'DynamicOverridePolicy\nexact masks only', fill: '#f5f3ff', stroke: '#6d5bd0', max: 25 }),
      box({ x: 535, y: 238, w: 382, h: 96, text: 'trusted local plugin\nobserve cloud or local facts', fill: '#eaf2ff', stroke: '#3267b1', max: 27 }),
      box({ x: 535, y: 388, w: 382, h: 96, text: 'DynamicConfigPart\nresources + masks + TTL', fill: '#fff4e6', stroke: '#c57a1c', max: 29 }),
      box({ x: 535, y: 548, w: 382, h: 96, text: 'effective config\nreconcile target', fill: '#eef8f9', stroke: '#2b8a92' }),
      box({ x: 1038, y: 238, w: 420, h: 96, text: 'actionPlans\nstored as inert proposals', fill: '#fff8df', stroke: '#d7a725', max: 29 }),
      box({ x: 1038, y: 388, w: 420, h: 96, text: 'action journal\nimport approve audit', fill: '#f5f3ff', stroke: '#6d5bd0', max: 28 }),
      box({ x: 1038, y: 548, w: 420, h: 96, text: 'executor plugin\nno routerd-held credentials', fill: '#fcefee', stroke: '#b64e4a', max: 30 }),
      note({ x: 1015, y: 680, w: 465, h: 62, text: 'Live execution requires ProviderActionPolicy, approval, allowlists, and dryRunOnly=false.', max: 50 }),
      arrow({ x1: 414, y1: 310, x2: 535, y2: 436 }),
      arrow({ x1: 414, y1: 545, x2: 535, y2: 436 }),
      arrow({ x1: 726, y1: 334, x2: 726, y2: 388 }),
      arrow({ x1: 726, y1: 484, x2: 726, y2: 548 }),
      arrow({ x1: 917, y1: 436, x2: 1038, y2: 286, label: 'propose', bend: 70 }),
      arrow({ x1: 1248, y1: 334, x2: 1248, y2: 388 }),
      arrow({ x1: 1248, y1: 484, x2: 1248, y2: 548 }),
      arrow({ x1: 1038, y1: 596, x2: 917, y2: 596, label: 'outcome events', bend: 55 }),
    ],
  },
  {
    name: 'cloudedge-sam-ipip',
    title: 'CloudEdge SAM transport',
    subtitle: 'SAMTransportProfile generates IPIP delivery and BGP peers; WireGuard is optional endpoint-only encryption underlay.',
    body: [
      lane({ x: 72, y: 170, w: 430, h: 620, text: 'Authoring surface' }),
      lane({ x: 540, y: 170, w: 480, h: 620, text: 'Generated transport' }),
      lane({ x: 1058, y: 170, w: 470, h: 620, text: 'Mobility behavior' }),
      box({ x: 112, y: 245, w: 340, h: 105, text: 'MobilityPool\naddress ownership + capture', fill: '#e9f8ef', stroke: '#2e7d55', max: 28 }),
      box({ x: 112, y: 425, w: 340, h: 105, text: 'SAMTransportProfile\nselfNodeRef topology innerPrefix', fill: '#eaf2ff', stroke: '#3267b1', max: 29 }),
      box({ x: 575, y: 238, w: 385, h: 96, text: 'DynamicConfigPart\nreplace-on-reconcile', fill: '#fff4e6', stroke: '#c57a1c', max: 28 }),
      box({ x: 575, y: 385, w: 385, h: 96, text: 'TunnelInterface\nIPIP or GRE delivery plane', fill: '#eef8f9', stroke: '#2b8a92', max: 28 }),
      box({ x: 575, y: 535, w: 385, h: 96, text: 'BGPPeer + endpoint /32 routes\nmultipath preserved', fill: '#f5f3ff', stroke: '#6d5bd0', max: 29 }),
      box({ x: 1098, y: 225, w: 355, h: 82, text: 'Owner advertises mobile /32', fill: '#e9f8ef', stroke: '#2e7d55', max: 28 }),
      box({ x: 1098, y: 350, w: 355, h: 82, text: 'Non-owner imports BGP best path', fill: '#eaf2ff', stroke: '#3267b1', max: 29 }),
      box({ x: 1098, y: 475, w: 355, h: 82, text: 'FIB can hold ECMP next hops', fill: '#f5f3ff', stroke: '#6d5bd0', max: 28 }),
      box({ x: 1098, y: 600, w: 355, h: 82, text: 'Capture: secondary IP\nor proxy ARP', fill: '#fff4e6', stroke: '#c57a1c', size: 24, max: 28 }),
      note({ x: 555, y: 685, w: 922, h: 62, text: 'WG AllowedIPs: transport endpoints only. Mobile /32s: BGP plus kernel FIB.', max: 84 }),
      arrow({ x1: 452, y1: 298, x2: 575, y2: 286 }),
      arrow({ x1: 452, y1: 478, x2: 575, y2: 286 }),
      arrow({ x1: 768, y1: 334, x2: 768, y2: 385 }),
      arrow({ x1: 768, y1: 481, x2: 768, y2: 535 }),
      arrow({ x1: 960, y1: 433, x2: 1098, y2: 266 }),
      arrow({ x1: 960, y1: 583, x2: 1098, y2: 391 }),
      arrow({ x1: 1275, y1: 432, x2: 1275, y2: 475 }),
      arrow({ x1: 1275, y1: 557, x2: 1275, y2: 600 }),
    ],
  },
  {
    name: 'lifecycle-gc',
    title: 'owner-reference lifecycle GC',
    subtitle: 'The GC planner compares the effective desired set with ledger, status, and host inventory before cleanup.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Inputs' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'Dry-run capable plan' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Teardown execution' }),
      box({ x: 112, y: 235, w: 340, h: 84, text: 'effective config\nwhen + dynamic + SAM', fill: '#e9f8ef', stroke: '#2e7d55', max: 30 }),
      box({ x: 112, y: 355, w: 340, h: 84, text: 'ownership ledger\nartifact owner keys', fill: '#eaf2ff', stroke: '#3267b1', max: 30 }),
      box({ x: 112, y: 475, w: 340, h: 84, text: 'object status\nowner + lifecycle class', fill: '#f5f3ff', stroke: '#6d5bd0', max: 30 }),
      box({ x: 112, y: 595, w: 340, h: 84, text: 'host inventory\nwhat exists now', fill: '#fff4e6', stroke: '#c57a1c', max: 30 }),
      box({ x: 580, y: 315, w: 390, h: 112, text: 'GC planner\nartifact removal\nresource teardown\nstatus + ledger cleanup', fill: '#eef8f9', stroke: '#2b8a92', size: 21, max: 26 }),
      note({ x: 580, y: 505, w: 390, h: 92, text: 'Destructive cleanup records a backup first and emits an audit event.', max: 38 }),
      box({ x: 1088, y: 245, w: 390, h: 96, text: 'artifact teardown registry\nnft table route tunnel unit', fill: '#eaf2ff', stroke: '#3267b1', max: 32 }),
      box({ x: 1088, y: 405, w: 390, h: 96, text: 'ResourceLifecycle.Teardown\nroutes WireGuard SAM capture', fill: '#f5f3ff', stroke: '#6d5bd0', max: 34 }),
      box({ x: 1088, y: 565, w: 390, h: 96, text: 'skip protections\nadopted external unsupported OS', fill: '#fff8df', stroke: '#d7a725', max: 33 }),
      arrow({ x1: 452, y1: 277, x2: 580, y2: 371 }),
      arrow({ x1: 452, y1: 397, x2: 580, y2: 371 }),
      arrow({ x1: 452, y1: 517, x2: 580, y2: 371 }),
      arrow({ x1: 452, y1: 637, x2: 580, y2: 371 }),
      arrow({ x1: 970, y1: 371, x2: 1088, y2: 293 }),
      arrow({ x1: 970, y1: 371, x2: 1088, y2: 453 }),
      arrow({ x1: 970, y1: 552, x2: 1088, y2: 613 }),
    ],
  },
  {
    name: 'config-example-workflow',
    title: 'reading a routerd config example',
    subtitle: 'Example pages connect topology numbers, YAML excerpts, validation, dry-run apply, and runtime checks.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Documentation page' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'Local edits and safety' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Router verification' }),
      box({ x: 112, y: 245, w: 340, h: 96, text: 'Topology diagram\n[1] WAN [2] router [3] LAN', fill: '#eaf2ff', stroke: '#3267b1', max: 30 }),
      box({ x: 112, y: 420, w: 340, h: 96, text: 'Diagram map\nnumber -> resource', fill: '#e9f8ef', stroke: '#2e7d55', max: 30 }),
      box({ x: 112, y: 595, w: 340, h: 96, text: 'YAML excerpts\ncomments match [1] [2] [3]', fill: '#fff4e6', stroke: '#c57a1c', max: 31 }),
      box({ x: 580, y: 250, w: 390, h: 88, text: 'Copy complete example YAML', fill: '#f5f3ff', stroke: '#6d5bd0' }),
      box({ x: 580, y: 385, w: 390, h: 88, text: 'Replace interfaces, addresses, ISP values, secrets paths', fill: '#e9f8ef', stroke: '#2e7d55', max: 39 }),
      box({ x: 580, y: 520, w: 390, h: 88, text: 'validate -> plan -> dry-run apply', fill: '#eef8f9', stroke: '#2b8a92', max: 34 }),
      box({ x: 1088, y: 270, w: 390, h: 96, text: 'apply from release binary', fill: '#fff4e6', stroke: '#c57a1c', max: 31 }),
      box({ x: 1088, y: 430, w: 390, h: 96, text: 'routerctl status / describe / show', fill: '#eaf2ff', stroke: '#3267b1', max: 31 }),
      box({ x: 1088, y: 590, w: 390, h: 96, text: 'confirm management\nand dataplane', fill: '#e9f8ef', stroke: '#2e7d55', max: 30 }),
      arrow({ x1: 452, y1: 293, x2: 580, y2: 294 }),
      arrow({ x1: 452, y1: 468, x2: 580, y2: 429 }),
      arrow({ x1: 452, y1: 643, x2: 580, y2: 564 }),
      arrow({ x1: 775, y1: 338, x2: 775, y2: 385 }),
      arrow({ x1: 775, y1: 473, x2: 775, y2: 520 }),
      arrow({ x1: 970, y1: 564, x2: 1088, y2: 318 }),
      arrow({ x1: 1278, y1: 366, x2: 1278, y2: 430 }),
      arrow({ x1: 1278, y1: 526, x2: 1278, y2: 590 }),
    ],
  },
  {
    name: 'concept-apply-and-render',
    title: 'apply and render operations',
    subtitle: 'Validation, planning, dry-run, apply, and render inspect the same resource graph with different side effects.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Shared inputs' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'Operation mode' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Output or mutation' }),
      box({ x: 112, y: 245, w: 340, h: 96, text: 'router.yaml\nresources and refs', fill: '#e9f8ef', stroke: '#2e7d55' }),
      box({ x: 112, y: 420, w: 340, h: 96, text: 'schema + defaults\nplatform constraints', fill: '#eaf2ff', stroke: '#3267b1', max: 29 }),
      box({ x: 112, y: 595, w: 340, h: 96, text: 'effective view\nwhen + dynamic + SAM', fill: '#fff4e6', stroke: '#c57a1c', max: 30 }),
      box({ x: 580, y: 225, w: 390, h: 82, text: 'validate\nshape and references', fill: '#f5f3ff', stroke: '#6d5bd0', max: 31 }),
      box({ x: 580, y: 345, w: 390, h: 82, text: 'plan\nhost diff preview', fill: '#eef8f9', stroke: '#2b8a92' }),
      box({ x: 580, y: 465, w: 390, h: 82, text: 'dry-run apply\nrun controllers without writes', fill: '#fff8df', stroke: '#d7a725', max: 34 }),
      box({ x: 580, y: 585, w: 390, h: 82, text: 'apply / serve\nwrite owned artifacts', fill: '#e9f8ef', stroke: '#2e7d55', max: 30 }),
      box({ x: 1088, y: 255, w: 390, h: 92, text: 'errors and warnings\nbefore host mutation', fill: '#fcefee', stroke: '#b64e4a', max: 32 }),
      box({ x: 1088, y: 405, w: 390, h: 92, text: 'rendered files\nroutes tables units rules', fill: '#eaf2ff', stroke: '#3267b1', max: 31 }),
      box({ x: 1088, y: 555, w: 390, h: 92, text: 'state DB\nstatus events ledger', fill: '#eef8f9', stroke: '#2b8a92', max: 31 }),
      arrow({ x1: 452, y1: 293, x2: 580, y2: 266 }),
      arrow({ x1: 452, y1: 468, x2: 580, y2: 386 }),
      arrow({ x1: 452, y1: 643, x2: 580, y2: 626 }),
      arrow({ x1: 970, y1: 266, x2: 1088, y2: 301 }),
      arrow({ x1: 970, y1: 386, x2: 1088, y2: 451 }),
      arrow({ x1: 970, y1: 626, x2: 1088, y2: 601 }),
      arrow({ x1: 775, y1: 307, x2: 775, y2: 345 }),
      arrow({ x1: 775, y1: 427, x2: 775, y2: 465 }),
      arrow({ x1: 775, y1: 547, x2: 775, y2: 585 }),
    ],
  },
  {
    name: 'concept-design-philosophy',
    title: 'routerd design philosophy',
    subtitle: 'Small typed resources, daemon status, and OS-specific renderers keep router behavior explicit and observable.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Intent' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'Controller shape' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Safety boundaries' }),
      box({ x: 112, y: 245, w: 340, h: 92, text: 'YAML in git\nhuman readable intent', fill: '#e9f8ef', stroke: '#2e7d55', max: 31 }),
      box({ x: 112, y: 410, w: 340, h: 92, text: 'typed resources\nschema-first API shape', fill: '#eaf2ff', stroke: '#3267b1', max: 30 }),
      box({ x: 112, y: 575, w: 340, h: 92, text: 'explicit ownership\ngenerated artifacts tracked', fill: '#fff4e6', stroke: '#c57a1c', max: 31 }),
      box({ x: 580, y: 245, w: 390, h: 92, text: 'stateful daemons\nDHCP PPPoE health logs', fill: '#f5f3ff', stroke: '#6d5bd0', max: 30 }),
      box({ x: 580, y: 410, w: 390, h: 92, text: 'event-driven convergence\nstatus explains progress', fill: '#eef8f9', stroke: '#2b8a92', max: 32 }),
      box({ x: 580, y: 575, w: 390, h: 92, text: 'platform features\nLinux NixOS FreeBSD gates', fill: '#e9f8ef', stroke: '#2e7d55', max: 33 }),
      box({ x: 1088, y: 245, w: 390, h: 92, text: 'do not advertise broken IPv6', fill: '#fcefee', stroke: '#b64e4a', max: 33 }),
      box({ x: 1088, y: 410, w: 390, h: 92, text: 'plan before mutation\nmanagement path checks', fill: '#fff8df', stroke: '#d7a725', max: 33 }),
      box({ x: 1088, y: 575, w: 390, h: 92, text: 'local router boundary\nno hosted controller', fill: '#eaf2ff', stroke: '#3267b1', max: 31 }),
      arrow({ x1: 452, y1: 291, x2: 580, y2: 291 }),
      arrow({ x1: 452, y1: 456, x2: 580, y2: 456 }),
      arrow({ x1: 452, y1: 621, x2: 580, y2: 621 }),
      arrow({ x1: 970, y1: 291, x2: 1088, y2: 291 }),
      arrow({ x1: 970, y1: 456, x2: 1088, y2: 456 }),
      arrow({ x1: 970, y1: 621, x2: 1088, y2: 621 }),
    ],
  },
  {
    name: 'concept-dns-resolver',
    title: 'DNS resolver resource flow',
    subtitle: 'Zones, forwarders, upstreams, DHCP-derived records, and resolver daemon status converge independently.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'DNS resources' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'Resolver daemon' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Runtime behavior' }),
      box({ x: 112, y: 225, w: 340, h: 82, text: 'DNSZone\nmanual + DHCP records', fill: '#e9f8ef', stroke: '#2e7d55', max: 30 }),
      box({ x: 112, y: 345, w: 340, h: 82, text: 'DNSForwarder\nmatch rule per domain', fill: '#eaf2ff', stroke: '#3267b1', max: 30 }),
      box({ x: 112, y: 465, w: 340, h: 82, text: 'DNSUpstream\nUDP TCP DoT DoH endpoint', fill: '#f5f3ff', stroke: '#6d5bd0', max: 31 }),
      box({ x: 112, y: 585, w: 340, h: 82, text: 'DNSResolver\nlisten cache metrics logs', fill: '#fff4e6', stroke: '#c57a1c', max: 31 }),
      box({ x: 580, y: 270, w: 390, h: 95, text: 'routerd-dns-resolver\none process per resource', fill: '#eef8f9', stroke: '#2b8a92', max: 33 }),
      box({ x: 580, y: 505, w: 390, h: 95, text: 'partial startup\nwait list in Degraded status', fill: '#fff8df', stroke: '#d7a725', max: 34 }),
      box({ x: 1088, y: 235, w: 390, h: 90, text: 'authoritative local answers', fill: '#e9f8ef', stroke: '#2e7d55', max: 33 }),
      box({ x: 1088, y: 385, w: 390, h: 90, text: 'conditional forwarding', fill: '#eaf2ff', stroke: '#3267b1' }),
      box({ x: 1088, y: 535, w: 390, h: 90, text: 'query logs + status\nstate database', fill: '#f5f3ff', stroke: '#6d5bd0', max: 31 }),
      arrow({ x1: 452, y1: 266, x2: 580, y2: 318 }),
      arrow({ x1: 452, y1: 386, x2: 580, y2: 318 }),
      arrow({ x1: 452, y1: 506, x2: 580, y2: 318 }),
      arrow({ x1: 452, y1: 626, x2: 580, y2: 552 }),
      arrow({ x1: 970, y1: 318, x2: 1088, y2: 280 }),
      arrow({ x1: 970, y1: 318, x2: 1088, y2: 430 }),
      arrow({ x1: 970, y1: 552, x2: 1088, y2: 580 }),
    ],
  },
  {
    name: 'concept-egress-route',
    title: 'egress route policy selection',
    subtitle: 'Candidates, health checks, and policy mode produce advisory status or applied route/NAT mark state.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Inputs' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'Selection controller' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Consumers' }),
      box({ x: 112, y: 235, w: 340, h: 86, text: 'candidate routes\nWAN PPPoE DS-Lite static', fill: '#e9f8ef', stroke: '#2e7d55', max: 31 }),
      box({ x: 112, y: 380, w: 340, h: 86, text: 'HealthCheck status\nready degraded failed', fill: '#eaf2ff', stroke: '#3267b1', max: 31 }),
      box({ x: 112, y: 525, w: 340, h: 86, text: 'weights priority enabled flags', fill: '#fff4e6', stroke: '#c57a1c', max: 32 }),
      box({ x: 580, y: 265, w: 390, h: 92, text: 'highest-weight-ready\npriority tie-breaker', fill: '#f5f3ff', stroke: '#6d5bd0', max: 32 }),
      box({ x: 580, y: 455, w: 390, h: 92, text: 'mode omitted: advisory\nmode set: applied policy state', fill: '#eef8f9', stroke: '#2b8a92', max: 36 }),
      box({ x: 1088, y: 235, w: 390, h: 86, text: 'EgressRoutePolicy status\nselected candidate', fill: '#e9f8ef', stroke: '#2e7d55', max: 34 }),
      box({ x: 1088, y: 380, w: 390, h: 86, text: 'IPv4Route / NAT44\nfollow selected egress', fill: '#eaf2ff', stroke: '#3267b1', max: 32 }),
      box({ x: 1088, y: 525, w: 390, h: 86, text: 'events\nroute changed or resource changed', fill: '#fff8df', stroke: '#d7a725', max: 34 }),
      arrow({ x1: 452, y1: 278, x2: 580, y2: 311 }),
      arrow({ x1: 452, y1: 423, x2: 580, y2: 311 }),
      arrow({ x1: 452, y1: 568, x2: 580, y2: 501 }),
      arrow({ x1: 970, y1: 311, x2: 1088, y2: 278 }),
      arrow({ x1: 970, y1: 501, x2: 1088, y2: 423 }),
      arrow({ x1: 970, y1: 501, x2: 1088, y2: 568 }),
    ],
  },
  {
    name: 'concept-firewall',
    title: 'stateful firewall model',
    subtitle: 'Zones, role matrix, explicit rules, client policy, and derived service openings render a separate filter table.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Policy inputs' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'Firewall renderer' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'nftables output' }),
      box({ x: 112, y: 225, w: 340, h: 82, text: 'FirewallZone\ninterface -> role', fill: '#e9f8ef', stroke: '#2e7d55', max: 30 }),
      box({ x: 112, y: 345, w: 340, h: 82, text: 'FirewallPolicy\nimplicit matrix + logging', fill: '#eaf2ff', stroke: '#3267b1', max: 32 }),
      box({ x: 112, y: 465, w: 340, h: 82, text: 'FirewallRule\nCIDR ports sets rate limits', fill: '#f5f3ff', stroke: '#6d5bd0', max: 32 }),
      box({ x: 112, y: 585, w: 340, h: 82, text: 'ClientPolicy\nguest isolation by MAC', fill: '#fff4e6', stroke: '#c57a1c', max: 31 }),
      box({ x: 580, y: 260, w: 390, h: 95, text: 'role matrix\nmgmt trust untrust', fill: '#eef8f9', stroke: '#2b8a92', max: 31 }),
      box({ x: 580, y: 495, w: 390, h: 95, text: 'derived openings\nDHCP DNS DS-Lite PD services', fill: '#fff8df', stroke: '#d7a725', max: 34 }),
      box({ x: 1088, y: 245, w: 390, h: 92, text: 'inet routerd_filter\nstateful filter table', fill: '#e9f8ef', stroke: '#2e7d55', max: 33 }),
      box({ x: 1088, y: 420, w: 390, h: 92, text: 'NAT remains separate\nip routerd_nat', fill: '#eaf2ff', stroke: '#3267b1', max: 32 }),
      box({ x: 1088, y: 595, w: 390, h: 92, text: 'status and logs\naccept drop reject rows', fill: '#f5f3ff', stroke: '#6d5bd0', max: 33 }),
      arrow({ x1: 452, y1: 266, x2: 580, y2: 308 }),
      arrow({ x1: 452, y1: 386, x2: 580, y2: 308 }),
      arrow({ x1: 452, y1: 506, x2: 580, y2: 543 }),
      arrow({ x1: 452, y1: 626, x2: 580, y2: 543 }),
      arrow({ x1: 970, y1: 308, x2: 1088, y2: 291 }),
      arrow({ x1: 970, y1: 543, x2: 1088, y2: 466 }),
      arrow({ x1: 970, y1: 543, x2: 1088, y2: 641 }),
    ],
  },
  {
    name: 'concept-glossary',
    title: 'routerd terminology map',
    subtitle: 'Common documentation terms group around desired state, observed state, host artifacts, and network behavior.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Declarative model' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'Runtime evidence' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Networking words' }),
      box({ x: 112, y: 235, w: 340, h: 86, text: 'Resource\napiVersion kind name spec', fill: '#e9f8ef', stroke: '#2e7d55', max: 31 }),
      box({ x: 112, y: 385, w: 340, h: 86, text: 'Reference\nresource points to resource', fill: '#eaf2ff', stroke: '#3267b1', max: 33 }),
      box({ x: 112, y: 535, w: 340, h: 86, text: 'Effective config\nstartup + dynamic + when', fill: '#fff4e6', stroke: '#c57a1c', max: 33 }),
      box({ x: 580, y: 235, w: 390, h: 86, text: 'Status\nobserved phase details', fill: '#f5f3ff', stroke: '#6d5bd0', max: 32 }),
      box({ x: 580, y: 385, w: 390, h: 86, text: 'Owner reference\nwho owns cleanup', fill: '#eef8f9', stroke: '#2b8a92', max: 32 }),
      box({ x: 580, y: 535, w: 390, h: 86, text: 'Artifact\nroute address table unit file', fill: '#fff8df', stroke: '#d7a725', max: 33 }),
      box({ x: 1088, y: 235, w: 390, h: 86, text: 'Egress / ingress\ntraffic direction', fill: '#e9f8ef', stroke: '#2e7d55', max: 32 }),
      box({ x: 1088, y: 385, w: 390, h: 86, text: 'NAT NAPT firewall\ntranslation and filtering', fill: '#eaf2ff', stroke: '#3267b1', max: 33 }),
      box({ x: 1088, y: 535, w: 390, h: 86, text: 'Prefix delegation\nupstream IPv6 prefix for LAN', fill: '#f5f3ff', stroke: '#6d5bd0', max: 35 }),
      arrow({ x1: 452, y1: 278, x2: 580, y2: 278 }),
      arrow({ x1: 452, y1: 428, x2: 580, y2: 428 }),
      arrow({ x1: 452, y1: 578, x2: 580, y2: 578 }),
      arrow({ x1: 970, y1: 278, x2: 1088, y2: 278 }),
      arrow({ x1: 970, y1: 428, x2: 1088, y2: 428 }),
      arrow({ x1: 970, y1: 578, x2: 1088, y2: 578 }),
    ],
  },
  {
    name: 'concept-log-storage',
    title: 'log storage layout',
    subtitle: 'routerd keeps state, events, DNS queries, traffic flows, and firewall logs in platform-derived local stores.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Writers' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'SQLite stores' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Retention and export' }),
      box({ x: 112, y: 235, w: 340, h: 86, text: 'routerd controllers\nstatus events ledger', fill: '#e9f8ef', stroke: '#2e7d55', max: 32 }),
      box({ x: 112, y: 385, w: 340, h: 86, text: 'DNS resolver\nquery rows', fill: '#eaf2ff', stroke: '#3267b1' }),
      box({ x: 112, y: 535, w: 340, h: 86, text: 'conntrack + firewall\nflows and decisions', fill: '#fff4e6', stroke: '#c57a1c', max: 31 }),
      box({ x: 580, y: 215, w: 390, h: 76, text: 'routerd.db', fill: '#eef8f9', stroke: '#2b8a92' }),
      box({ x: 580, y: 325, w: 390, h: 76, text: 'dns-queries.db', fill: '#eaf2ff', stroke: '#3267b1' }),
      box({ x: 580, y: 435, w: 390, h: 76, text: 'traffic-flows.db', fill: '#f5f3ff', stroke: '#6d5bd0' }),
      box({ x: 580, y: 545, w: 390, h: 76, text: 'firewall-logs.db', fill: '#fff8df', stroke: '#d7a725' }),
      note({ x: 580, y: 655, w: 390, h: 70, text: 'Linux: /var/lib/routerd\nFreeBSD: /var/db/routerd', max: 38 }),
      box({ x: 1088, y: 255, w: 390, h: 92, text: 'LogRetention\nage-based cleanup + vacuum', fill: '#e9f8ef', stroke: '#2e7d55', max: 33 }),
      box({ x: 1088, y: 415, w: 390, h: 92, text: 'OpenTelemetry-shaped columns', fill: '#eaf2ff', stroke: '#3267b1', max: 33 }),
      box({ x: 1088, y: 575, w: 390, h: 92, text: 'routerctl and Web Console\nread-only views', fill: '#f5f3ff', stroke: '#6d5bd0', max: 34 }),
      arrow({ x1: 452, y1: 278, x2: 580, y2: 253 }),
      arrow({ x1: 452, y1: 428, x2: 580, y2: 363 }),
      arrow({ x1: 452, y1: 578, x2: 580, y2: 473 }),
      arrow({ x1: 452, y1: 578, x2: 580, y2: 583 }),
      arrow({ x1: 970, y1: 253, x2: 1088, y2: 301 }),
      arrow({ x1: 970, y1: 473, x2: 1088, y2: 461 }),
      arrow({ x1: 970, y1: 583, x2: 1088, y2: 621 }),
    ],
  },
  {
    name: 'concept-path-mtu',
    title: 'path MTU and TCP MSS',
    subtitle: 'routerd derives tunnel MTU, RA MTU, TCP MSS clamp, and optional IPv4 force-fragmentation from forwarding paths.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Path inputs' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'Derived values' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Rendered behavior' }),
      box({ x: 112, y: 225, w: 340, h: 82, text: 'PPPoE DS-Lite\nWireGuard TunnelInterface', fill: '#e9f8ef', stroke: '#2e7d55', max: 32 }),
      box({ x: 112, y: 345, w: 340, h: 82, text: 'underlay interface MTU\nminus tunnel overhead', fill: '#eaf2ff', stroke: '#3267b1', max: 33 }),
      box({ x: 112, y: 465, w: 340, h: 82, text: 'FirewallZone roles\ntrusted -> untrusted path', fill: '#f5f3ff', stroke: '#6d5bd0', max: 33 }),
      box({ x: 112, y: 585, w: 340, h: 82, text: 'RA / DHCPv6 LAN intent', fill: '#fff4e6', stroke: '#c57a1c', max: 30 }),
      box({ x: 580, y: 255, w: 390, h: 92, text: 'effective path MTU\nmin(source MTU, tunnel MTU)', fill: '#eef8f9', stroke: '#2b8a92', max: 36 }),
      box({ x: 580, y: 445, w: 390, h: 92, text: 'TCP MSS\nIPv4 MTU-40, IPv6 MTU-60', fill: '#fff8df', stroke: '#d7a725', max: 33 }),
      box({ x: 1088, y: 235, w: 390, h: 86, text: 'nft MSS clamp\nonly lowers high SYN MSS', fill: '#e9f8ef', stroke: '#2e7d55', max: 34 }),
      box({ x: 1088, y: 380, w: 390, h: 86, text: 'derived RA MTU\nfor smaller downstream path', fill: '#eaf2ff', stroke: '#3267b1', max: 34 }),
      box({ x: 1088, y: 525, w: 390, h: 86, text: 'optional IPv4 forcefrag\ntrusted overlay fallback', fill: '#f5f3ff', stroke: '#6d5bd0', max: 34 }),
      arrow({ x1: 452, y1: 266, x2: 580, y2: 301 }),
      arrow({ x1: 452, y1: 386, x2: 580, y2: 301 }),
      arrow({ x1: 452, y1: 506, x2: 580, y2: 491 }),
      arrow({ x1: 452, y1: 626, x2: 580, y2: 301 }),
      arrow({ x1: 970, y1: 491, x2: 1088, y2: 278 }),
      arrow({ x1: 970, y1: 301, x2: 1088, y2: 423 }),
      arrow({ x1: 970, y1: 301, x2: 1088, y2: 568 }),
    ],
  },
  {
    name: 'concept-positioning',
    title: 'where routerd fits',
    subtitle: 'routerd is a local declarative router control plane for small and medium networks, not a hosted network OS.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Best fit' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'Local control plane' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Boundaries' }),
      box({ x: 112, y: 235, w: 340, h: 86, text: 'home lab and SOHO routers', fill: '#e9f8ef', stroke: '#2e7d55' }),
      box({ x: 112, y: 385, w: 340, h: 86, text: 'Proxmox KVM cloud edge demos', fill: '#eaf2ff', stroke: '#3267b1', max: 32 }),
      box({ x: 112, y: 535, w: 340, h: 86, text: 'git-reviewed YAML changes', fill: '#fff4e6', stroke: '#c57a1c', max: 32 }),
      box({ x: 580, y: 235, w: 390, h: 86, text: 'router.yaml\nsingle host intent', fill: '#f5f3ff', stroke: '#6d5bd0' }),
      box({ x: 580, y: 385, w: 390, h: 86, text: 'routerd serve/apply\nlocal kernel and daemons', fill: '#eef8f9', stroke: '#2b8a92', max: 34 }),
      box({ x: 580, y: 535, w: 390, h: 86, text: 'routerctl + Web Console\nobserve local state', fill: '#e9f8ef', stroke: '#2e7d55', max: 33 }),
      box({ x: 1088, y: 235, w: 390, h: 86, text: 'not a Linux distribution', fill: '#fcefee', stroke: '#b64e4a' }),
      box({ x: 1088, y: 385, w: 390, h: 86, text: 'not a hosted controller\nfor many routers', fill: '#fff8df', stroke: '#d7a725', max: 34 }),
      box({ x: 1088, y: 535, w: 390, h: 86, text: 'second-tier NixOS FreeBSD\ngroundwork, not parity', fill: '#eaf2ff', stroke: '#3267b1', max: 34 }),
      arrow({ x1: 452, y1: 278, x2: 580, y2: 278 }),
      arrow({ x1: 452, y1: 428, x2: 580, y2: 428 }),
      arrow({ x1: 452, y1: 578, x2: 580, y2: 578 }),
      arrow({ x1: 970, y1: 278, x2: 1088, y2: 278 }),
      arrow({ x1: 970, y1: 428, x2: 1088, y2: 428 }),
      arrow({ x1: 970, y1: 578, x2: 1088, y2: 578 }),
    ],
  },
  {
    name: 'concept-resource-model',
    title: 'routerd resource model',
    subtitle: 'A Router document contains typed resources; references and owners drive apply order, status, and cleanup.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Config document' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'Resource graph' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Observed state' }),
      box({ x: 112, y: 245, w: 340, h: 92, text: 'Router\napiVersion routerd.net/v1alpha1', fill: '#e9f8ef', stroke: '#2e7d55', max: 33 }),
      box({ x: 112, y: 410, w: 340, h: 92, text: 'resources[]\napiVersion kind metadata spec', fill: '#eaf2ff', stroke: '#3267b1', max: 34 }),
      box({ x: 112, y: 575, w: 340, h: 92, text: 'when filters\ndynamic config parts', fill: '#fff4e6', stroke: '#c57a1c', max: 31 }),
      box({ x: 580, y: 245, w: 390, h: 92, text: 'references\nresource fields point to status', fill: '#f5f3ff', stroke: '#6d5bd0', max: 34 }),
      box({ x: 580, y: 410, w: 390, h: 92, text: 'dependency ordering\ncontrollers reconcile safely', fill: '#eef8f9', stroke: '#2b8a92', max: 34 }),
      box({ x: 580, y: 575, w: 390, h: 92, text: 'owner keys\napiVersion/kind/name', fill: '#e9f8ef', stroke: '#2e7d55', max: 31 }),
      box({ x: 1088, y: 245, w: 390, h: 92, text: 'status\nphase conditions outputs', fill: '#eaf2ff', stroke: '#3267b1', max: 31 }),
      box({ x: 1088, y: 410, w: 390, h: 92, text: 'events\nwhy reconciliation changed', fill: '#fff8df', stroke: '#d7a725', max: 32 }),
      box({ x: 1088, y: 575, w: 390, h: 92, text: 'ledger artifacts\nwhat routerd may clean up', fill: '#f5f3ff', stroke: '#6d5bd0', max: 33 }),
      arrow({ x1: 452, y1: 291, x2: 580, y2: 291 }),
      arrow({ x1: 452, y1: 456, x2: 580, y2: 456 }),
      arrow({ x1: 452, y1: 621, x2: 580, y2: 621 }),
      arrow({ x1: 970, y1: 291, x2: 1088, y2: 291 }),
      arrow({ x1: 970, y1: 456, x2: 1088, y2: 456 }),
      arrow({ x1: 970, y1: 621, x2: 1088, y2: 621 }),
    ],
  },
  {
    name: 'concept-sysctl-profile',
    title: 'sysctl derivation and escape hatches',
    subtitle: 'routerd derives router sysctls from resources and reserves explicit Sysctl/Profile resources for narrow overrides.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Resource intent' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'Sysctl controller' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Host writes' }),
      box({ x: 112, y: 225, w: 340, h: 82, text: 'NAT DS-Lite BGP\nPD RA LAN services', fill: '#e9f8ef', stroke: '#2e7d55', max: 31 }),
      box({ x: 112, y: 345, w: 340, h: 82, text: 'Tunnel resources\nrp_filter exceptions', fill: '#eaf2ff', stroke: '#3267b1', max: 31 }),
      box({ x: 112, y: 465, w: 340, h: 82, text: 'SysctlProfile\nrouter-linux escape hatch', fill: '#f5f3ff', stroke: '#6d5bd0', max: 33 }),
      box({ x: 112, y: 585, w: 340, h: 82, text: 'Sysctl\na single explicit key', fill: '#fff4e6', stroke: '#c57a1c', max: 30 }),
      box({ x: 580, y: 260, w: 390, h: 95, text: 'derived profile\nforwarding redirects conntrack', fill: '#eef8f9', stroke: '#2b8a92', max: 35 }),
      box({ x: 580, y: 495, w: 390, h: 95, text: 'platform gate\nLinux runtime vs persistent', fill: '#fff8df', stroke: '#d7a725', max: 34 }),
      box({ x: 1088, y: 245, w: 390, h: 92, text: '/proc/sys runtime write\nserve mode', fill: '#e9f8ef', stroke: '#2e7d55', max: 32 }),
      box({ x: 1088, y: 420, w: 390, h: 92, text: '/etc/sysctl.d\npersistent file', fill: '#eaf2ff', stroke: '#3267b1', max: 31 }),
      box({ x: 1088, y: 595, w: 390, h: 92, text: 'status + GC\nread before write', fill: '#f5f3ff', stroke: '#6d5bd0', max: 31 }),
      arrow({ x1: 452, y1: 266, x2: 580, y2: 308 }),
      arrow({ x1: 452, y1: 386, x2: 580, y2: 308 }),
      arrow({ x1: 452, y1: 506, x2: 580, y2: 543 }),
      arrow({ x1: 452, y1: 626, x2: 580, y2: 543 }),
      arrow({ x1: 970, y1: 308, x2: 1088, y2: 291 }),
      arrow({ x1: 970, y1: 543, x2: 1088, y2: 466 }),
      arrow({ x1: 970, y1: 543, x2: 1088, y2: 641 }),
    ],
  },
  {
    name: 'concept-web-console',
    title: 'Web Console read-only path',
    subtitle: 'The browser observes daemon status, resource status, events, and local diagnostics without editing configuration.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Operator browser' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'routerd local API' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Read-only data' }),
      box({ x: 112, y: 245, w: 340, h: 92, text: 'management network\ntrusted listener only', fill: '#e9f8ef', stroke: '#2e7d55', max: 32 }),
      box({ x: 112, y: 410, w: 340, h: 92, text: 'WebConsole resource\nlistenAddressFrom or literal', fill: '#eaf2ff', stroke: '#3267b1', max: 34 }),
      box({ x: 112, y: 575, w: 340, h: 92, text: 'browser UI\nstatus charts tables logs', fill: '#fff4e6', stroke: '#c57a1c', max: 33 }),
      box({ x: 580, y: 245, w: 390, h: 92, text: 'local HTTP JSON API\nUnix socket backed', fill: '#f5f3ff', stroke: '#6d5bd0', max: 32 }),
      box({ x: 580, y: 410, w: 390, h: 92, text: 'no edit or apply endpoint\nconfiguration stays YAML', fill: '#fcefee', stroke: '#b64e4a', max: 34 }),
      box({ x: 580, y: 575, w: 390, h: 92, text: 'routerctl remains\nchange control path', fill: '#eef8f9', stroke: '#2b8a92', max: 31 }),
      box({ x: 1088, y: 245, w: 390, h: 92, text: 'daemon status\nhealth and apply phase', fill: '#e9f8ef', stroke: '#2e7d55', max: 32 }),
      box({ x: 1088, y: 410, w: 390, h: 92, text: 'SQLite state DB\nresources events logs', fill: '#eaf2ff', stroke: '#3267b1', max: 31 }),
      box({ x: 1088, y: 575, w: 390, h: 92, text: 'diagnostics\nNAPT firewall DNS leases', fill: '#f5f3ff', stroke: '#6d5bd0', max: 32 }),
      arrow({ x1: 452, y1: 456, x2: 580, y2: 291 }),
      arrow({ x1: 452, y1: 621, x2: 580, y2: 291 }),
      arrow({ x1: 970, y1: 291, x2: 1088, y2: 291 }),
      arrow({ x1: 970, y1: 291, x2: 1088, y2: 456 }),
      arrow({ x1: 970, y1: 291, x2: 1088, y2: 621 }),
      arrow({ x1: 580, y1: 456, x2: 452, y2: 621, label: 'no writes', dashed: true, bend: 60 }),
    ],
  },
  {
    name: 'concept-what-is-routerd',
    title: 'what routerd does',
    subtitle: 'routerd turns typed YAML router intent into local host networking, services, state, and observable status.',
    body: [
      lane({ x: 72, y: 176, w: 430, h: 600, text: 'Author intent' }),
      lane({ x: 540, y: 176, w: 470, h: 600, text: 'routerd' }),
      lane({ x: 1048, y: 176, w: 480, h: 600, text: 'Local router host' }),
      box({ x: 112, y: 235, w: 340, h: 86, text: 'router.yaml\nnetwork system firewall resources', fill: '#e9f8ef', stroke: '#2e7d55', max: 34 }),
      box({ x: 112, y: 385, w: 340, h: 86, text: 'routerctl\nvalidate plan dry-run apply', fill: '#eaf2ff', stroke: '#3267b1', max: 33 }),
      box({ x: 112, y: 535, w: 340, h: 86, text: 'Web Console\nobserve only', fill: '#fff4e6', stroke: '#c57a1c' }),
      box({ x: 580, y: 235, w: 390, h: 86, text: 'effective config\nwhen dynamic generated SAM', fill: '#f5f3ff', stroke: '#6d5bd0', max: 34 }),
      box({ x: 580, y: 385, w: 390, h: 86, text: 'controllers + renderers\nroutes DNS DHCP firewall BGP', fill: '#eef8f9', stroke: '#2b8a92', max: 34 }),
      box({ x: 580, y: 535, w: 390, h: 86, text: 'state database\nstatus events owner ledger', fill: '#e9f8ef', stroke: '#2e7d55', max: 33 }),
      box({ x: 1088, y: 225, w: 390, h: 76, text: 'interfaces addresses routes', fill: '#eaf2ff', stroke: '#3267b1' }),
      box({ x: 1088, y: 335, w: 390, h: 76, text: 'dnsmasq GoBGP WireGuard pppd', fill: '#fff4e6', stroke: '#c57a1c', max: 33 }),
      box({ x: 1088, y: 445, w: 390, h: 76, text: 'nftables sysctl systemd logs', fill: '#f5f3ff', stroke: '#6d5bd0', max: 33 }),
      box({ x: 1088, y: 555, w: 390, h: 76, text: 'GC removes owned leftovers', fill: '#fff8df', stroke: '#d7a725', max: 31 }),
      note({ x: 1088, y: 665, w: 390, h: 60, text: 'Runs locally on each router. It is not a hosted controller.', max: 42 }),
      arrow({ x1: 452, y1: 278, x2: 580, y2: 278 }),
      arrow({ x1: 452, y1: 428, x2: 580, y2: 428 }),
      arrow({ x1: 452, y1: 578, x2: 580, y2: 578 }),
      arrow({ x1: 970, y1: 278, x2: 1088, y2: 263 }),
      arrow({ x1: 970, y1: 428, x2: 1088, y2: 373 }),
      arrow({ x1: 970, y1: 428, x2: 1088, y2: 483 }),
      arrow({ x1: 970, y1: 578, x2: 1088, y2: 593 }),
    ],
  },
];

function chromePath() {
  const candidates = [
    process.env.CHROME_BIN,
    'chromium',
    'chromium-browser',
    'google-chrome',
    'google-chrome-stable',
    path.join(homedir(), '.cache/ms-playwright/chromium-1217/chrome-linux64/chrome'),
  ].filter(Boolean);
  for (const candidate of candidates) {
    try {
      execFileSync(candidate, ['--version'], { stdio: 'ignore' });
      return candidate;
    } catch {
      // try the next candidate
    }
  }
  throw new Error('No Chromium-compatible browser found. Set CHROME_BIN or run make webconsole-browser-install.');
}

function pngSize(file) {
  const b = readFileSync(file);
  if (b.length < 24 || b.toString('ascii', 1, 4) !== 'PNG') {
    throw new Error(`${file} is not a PNG file`);
  }
  return { width: b.readUInt32BE(16), height: b.readUInt32BE(20) };
}

function generate() {
  mkdirSync(SVG_DIR, { recursive: true });
  mkdirSync(PNG_DIR, { recursive: true });
  const chrome = chromePath();
  for (const diagram of diagrams) {
    const svgPath = path.join(SVG_DIR, `${diagram.name}.svg`);
    const pngPath = path.join(PNG_DIR, `${diagram.name}.png`);
    writeFileSync(svgPath, svg(diagram.title, diagram.subtitle, diagram.body.join('\n')));
    execFileSync(chrome, [
      '--headless=new',
      '--disable-gpu',
      '--no-sandbox',
      '--hide-scrollbars',
      `--window-size=${WIDTH},${HEIGHT}`,
      `--screenshot=${pngPath}`,
      `file://${svgPath}`,
    ], { stdio: 'ignore' });
    const size = pngSize(pngPath);
    if (size.width !== WIDTH || size.height !== HEIGHT) {
      throw new Error(`${pngPath} is ${size.width}x${size.height}, expected ${WIDTH}x${HEIGHT}`);
    }
    console.log(`generated ${path.relative(process.cwd(), svgPath)} and ${path.relative(process.cwd(), pngPath)} (${WIDTH}x${HEIGHT})`);
  }
}

function check() {
  for (const diagram of diagrams) {
    const svgPath = path.join(SVG_DIR, `${diagram.name}.svg`);
    const pngPath = path.join(PNG_DIR, `${diagram.name}.png`);
    if (!existsSync(svgPath)) throw new Error(`missing ${svgPath}`);
    if (!existsSync(pngPath)) throw new Error(`missing ${pngPath}`);
    const size = pngSize(pngPath);
    if (size.width !== WIDTH || size.height !== HEIGHT) {
      throw new Error(`${pngPath} is ${size.width}x${size.height}, expected ${WIDTH}x${HEIGHT}`);
    }
    console.log(`checked ${path.relative(process.cwd(), pngPath)} (${WIDTH}x${HEIGHT})`);
  }
}

if (CHECK) {
  check();
} else {
  generate();
}
