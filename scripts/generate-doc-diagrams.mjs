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

const palette = [
  ['#e9f8ef', '#2e7d55'],
  ['#eaf2ff', '#3267b1'],
  ['#f5f3ff', '#6d5bd0'],
  ['#fff4e6', '#c57a1c'],
];

function stackYs(count) {
  if (count <= 2) return { ys: [280, 520], h: 105 };
  if (count === 3) return { ys: [245, 410, 575], h: 92 };
  return { ys: [225, 345, 465, 585], h: 82 };
}

function flowDiagram({ name, title, subtitle, lanes: flowLanes, noteText, arrowLabels = ['intent', 'runtime'] }) {
  const laneX = [72, 540, 1048];
  const laneW = [430, 470, 480];
  const boxX = [112, 580, 1088];
  const boxW = [340, 390, 390];
  const body = flowLanes.flatMap((flowLane, laneIndex) => {
    const { ys, h } = stackYs(flowLane.boxes.length);
    return [
      lane({ x: laneX[laneIndex], y: 176, w: laneW[laneIndex], h: 600, text: flowLane.title }),
      ...flowLane.boxes.map((item, boxIndex) => {
        const [fill, stroke] = palette[boxIndex % palette.length];
        return box({
          x: boxX[laneIndex],
          y: ys[boxIndex],
          w: boxW[laneIndex],
          h,
          text: item.text ?? item,
          fill: item.fill ?? fill,
          stroke: item.stroke ?? stroke,
          max: item.max ?? (laneIndex === 0 ? 31 : 33),
          size: item.size ?? 23,
        });
      }),
    ];
  });
  body.push(
    arrow({ x1: 452, y1: 476, x2: 580, y2: 476, label: arrowLabels[0] }),
    arrow({ x1: 970, y1: 476, x2: 1088, y2: 476, label: arrowLabels[1] }),
  );
  if (noteText) {
    body.push(note({ x: 310, y: 705, w: 980, h: 55, text: noteText, max: 82 }));
  }
  return { name, title, subtitle, body };
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
  flowDiagram({
    name: 'config-example-basic-ipv4-nat',
    title: 'basic IPv4 NAT gateway',
    subtitle: 'A DHCP-acquired WAN address, owned LAN address, DHCPv4 service, NAT44, and zone firewall make the smallest router.',
    lanes: [
      { title: 'Access side', boxes: ['Internet + ISP router', 'wan interface\nDHCPv4 client', 'default upstream route'] },
      { title: 'routerd host', boxes: ['Interface + IPv4StaticAddress\nown LAN gateway', 'DHCPv4Server\nLAN pool + options', 'NAT44Rule + FirewallZone\ntrust to untrust'] },
      { title: 'LAN behavior', boxes: ['clients get 192.168.10.100-199', 'router address becomes gateway and DNS', 'IPv4 traffic masquerades to WAN'] },
    ],
    noteText: 'DNS is intentionally simple here; add DNSResolver and DNSZone after basic routing works.',
  }),
  flowDiagram({
    name: 'config-example-dslite-home',
    title: 'DS-Lite home gateway',
    subtitle: 'IPv6 RA and DHCPv6-PD build the LAN IPv6 path while IPv4 exits through a DS-Lite tunnel with derived MTU and MSS.',
    lanes: [
      { title: 'IPv6-first WAN', boxes: ['WAN IPv6 RA', 'DHCPv6PrefixDelegation\nNTT-style profile', 'AFTR endpoint\nTransix-like placeholder'] },
      { title: 'routerd host', boxes: ['IPv6DelegatedAddress\nLAN prefix slice', 'DSLiteTunnel\nip6tnl IPv4 egress', 'derived NAT44\nMTU/MSS from tunnel'] },
      { title: 'LAN behavior', boxes: ['LAN IPv4 + delegated IPv6', 'RA RDNSS DNSSL + DHCPv4', 'IPv4 clients exit via AFTR'] },
    ],
    noteText: 'Replace AFTR FQDN, DNS servers, and DHCPv6 profile with values from the access line.',
  }),
  flowDiagram({
    name: 'config-example-firewall-rate-limit',
    title: 'firewall rate limits and ICMP rules',
    subtitle: 'Stateful firewall rules combine zone direction, protocol matches, ICMP names, rate limits, and per-source limits.',
    lanes: [
      { title: 'Traffic classes', boxes: ['WAN to router self', 'HTTP HTTPS service ports', 'ICMP echo requests', 'SSH brute-force attempts'] },
      { title: 'FirewallRule set', boxes: ['multi-port allow\n80 and 443', 'ICMP echo allow\nWAN diagnostics', 'SSH over-limit reject\nrate + conn limit'] },
      { title: 'nftables result', boxes: ['inet routerd_filter table', 'stateful established accept', 'over-limit packets rejected and logged'] },
    ],
    noteText: 'RateLimit and ConnLimit match the over-limit traffic; the explicit action decides whether to reject or drop it.',
  }),
  flowDiagram({
    name: 'config-example-guest-isolation',
    title: 'guest and IoT client isolation',
    subtitle: 'ClientPolicy classifies selected MAC addresses on the same LAN and applies guest restrictions through the firewall model.',
    lanes: [
      { title: 'Shared LAN', boxes: ['trusted clients', 'guest / IoT MAC list', 'management network'] },
      { title: 'routerd policy', boxes: ['ClientPolicy include mode', 'FirewallZone lan + mgmt', 'FirewallPolicy default matrix'] },
      { title: 'Allowed paths', boxes: ['guest to internet allowed', 'guest to trusted LAN denied', 'guest to management denied'] },
    ],
    noteText: 'The clients stay on the same layer-2 segment; isolation is policy-driven, not a separate VLAN in this example.',
  }),
  flowDiagram({
    name: 'config-example-kubernetes-api-vip',
    title: 'Kubernetes API VIP with BGP',
    subtitle: 'A routerd edge pair owns a VRRP VIP, forwards API traffic, health-checks backends, and peers with Kubernetes BGP speakers.',
    lanes: [
      { title: 'Cluster edge', boxes: ['routerd-01/02\nVRRP VIP 192.168.70.10', 'control-plane backends\n:6443 /readyz', 'worker BGP speakers\nASN 64513'] },
      { title: 'routerd resources', boxes: ['VirtualAddress\ntracked VRRP role', 'IngressService\nHTTPS health + hairpin SNAT', 'BGPRouter + BGPPeer\nfast timers import allow-list'] },
      { title: 'Runtime checks', boxes: ['stable kube API endpoint', 'Service prefixes imported from k8s', 'routerctl show bgp/vrrp/ingress'] },
    ],
    noteText: 'The VIP is outside the cluster, so cluster bootstrap can depend on the routers without circular ownership.',
  }),
  flowDiagram({
    name: 'config-example-lan-dns-dhcp',
    title: 'LAN DHCP and local DNS',
    subtitle: 'The router owns the LAN address, serves DHCPv4, answers a local DNS zone, and derives client names from leases.',
    lanes: [
      { title: 'LAN segment', boxes: ['router 192.168.30.1', 'DHCP clients\n192.168.30.100-199', 'NAS reservation\n192.168.30.10'] },
      { title: 'routerd services', boxes: ['IPv4StaticAddress\nLAN gateway', 'DHCPv4Server + Reservation', 'DNSZone + DNSResolver\ndhcpDerived names'] },
      { title: 'Client view', boxes: ['gateway and DNS point to router', 'home.example search domain', 'router and NAS names resolve locally'] },
    ],
    noteText: 'The DNS zone can combine static records and lease-derived records from the DHCPv4 server.',
  }),
  flowDiagram({
    name: 'config-example-local-dns-redirect',
    title: 'redirect public DNS to local resolver',
    subtitle: 'LAN clients that send plaintext port 53 to known public DNS names are redirected to the local resolver only.',
    lanes: [
      { title: 'Client attempt', boxes: ['LAN client sends 8.8.8.8:53', 'public DNS names\nexact FQDN set', 'DoH and DoT ports untouched'] },
      { title: 'routerd policy', boxes: ['IPAddressSet\nFQDN refresh', 'LocalServiceRedirect\nLAN prerouting only', 'DNSResolver\nlocal port 53'] },
      { title: 'Result', boxes: ['plaintext DNS lands locally', 'router-origin probes bypass redirect', 'local resolver forwards upstream'] },
    ],
    noteText: 'The redirect is scoped to LAN-client TCP/UDP port 53, not all traffic to those providers.',
  }),
  flowDiagram({
    name: 'config-example-multi-wan-failover',
    title: 'multi-WAN IPv4 failover',
    subtitle: 'Health checks and weighted candidates choose one IPv4 default path across DS-Lite, PPPoE, and direct IPv4 fallback.',
    lanes: [
      { title: 'Candidates', boxes: ['DS-Lite A\nweight 120', 'DS-Lite B\nadditional tunnel', 'PPPoE backup\nweight 60', 'HGW direct IPv4\nweight 40'] },
      { title: 'routerd selection', boxes: ['HealthCheck\ninternet-via-*', 'EgressRoutePolicy\nhighest-weight-ready', 'IPv4Route default\nselected next hop'] },
      { title: 'LAN behavior', boxes: ['clients use one active egress', 'NAT44 follows selected path', 'hysteresis avoids route flapping'] },
    ],
    noteText: 'weighted-ecmp is still reserved; this example selects one ready default route.',
  }),
  flowDiagram({
    name: 'config-example-port-forward-web',
    title: 'port forward to an inside web server',
    subtitle: 'PortForward renders ingress DNAT and optional hairpin rules so external and LAN clients use the same public name.',
    lanes: [
      { title: 'Client paths', boxes: ['Internet to 203.0.113.10:443', 'LAN client to public address', 'inside web server\n192.168.10.20:443'] },
      { title: 'PortForward', boxes: ['listen interface + address\nwan 203.0.113.10', 'target backend\n192.168.10.20:443', 'hairpin enabled\nLAN interfaces'] },
      { title: 'nftables output', boxes: ['WAN DNAT to backend', 'LAN hairpin DNAT/SNAT', 'FirewallZone policy remains separate'] },
    ],
    noteText: 'Hairpin mode needs a concrete listen address or addressFrom so LAN traffic can match before DNAT.',
  }),
  flowDiagram({
    name: 'config-example-pppoe-ipv4-nat',
    title: 'PPPoE IPv4 NAT gateway',
    subtitle: 'Ethernet carries a PPPoE session; LAN IPv4 traffic is masqueraded toward the logical PPP interface.',
    lanes: [
      { title: 'Access line', boxes: ['ONU / provider Ethernet', 'wan interface\nphysical carrier', 'PPPoE credentials\nsecret file'] },
      { title: 'routerd host', boxes: ['PPPoESession\nppp-home MTU 1454', 'IPv4StaticAddress + DHCPv4Server', 'NAT44Rule\nLAN to PPPoE'] },
      { title: 'LAN behavior', boxes: ['clients receive LAN DHCP', 'default internet path is ppp-home', 'firewall zones derive trust/untrust'] },
    ],
    noteText: 'Keep PPPoE secrets in restricted files and validate the plan before replacing a live WAN path.',
  }),
  flowDiagram({
    name: 'config-example-tailscale-subnet-exit',
    title: 'Tailscale subnet and exit node',
    subtitle: 'routerd installs or expects Tailscale, registers the router node, and advertises subnet and exit-node intent.',
    lanes: [
      { title: 'Local networks', boxes: ['LAN 172.18.0.0/16', 'management 192.168.20.0/24', 'internet egress path'] },
      { title: 'routerd resources', boxes: ['Package\ntailscale-runtime', 'TailscaleNode\nhostname edge-router', 'advertiseRoutes + advertiseExitNode'] },
      { title: 'Tailnet behavior', boxes: ['routes appear for admin approval', 'router can act as exit node', 'acceptDNS disabled in example'] },
    ],
    noteText: 'The Tailscale control plane remains external; route and exit-node approval follow the tailnet policy.',
  }),
  flowDiagram({
    name: 'config-example-telemetry-export',
    title: 'telemetry export to OTLP',
    subtitle: 'Telemetry resources attach routerd logs, metrics, and traces to an OpenTelemetry collector endpoint.',
    lanes: [
      { title: 'routerd signals', boxes: ['logs', 'metrics', 'traces', 'service attributes'] },
      { title: 'Telemetry resource', boxes: ['Telemetry/otlp', 'OTLP endpoint\ncollector:4317', 'insecure lab transport\nor TLS in production'] },
      { title: 'Observability path', boxes: ['OpenTelemetry collector', 'metrics/logs/traces backend', 'health checks and apply latency analysis'] },
    ],
    noteText: 'Keep the collector on a trusted management or observability network.',
  }),
  flowDiagram({
    name: 'config-example-wireguard-hub-spoke',
    title: 'WireGuard hub and spoke template',
    subtitle: 'A hub WireGuard interface owns the tunnel address, while peers declare tunnel /32s and routed LAN prefixes.',
    lanes: [
      { title: 'Topology', boxes: ['spoke A\n172.30.11.0/24', 'hub\n10.44.0.1/24', 'spoke B\n172.30.12.0/24'] },
      { title: 'routerd resources', boxes: ['WireGuardInterface\nwg-hub listen 51820', 'IPv4StaticAddress\nhub tunnel IP', 'WireGuardPeer\nallowedIPs per spoke'] },
      { title: 'Routing behavior', boxes: ['spoke tunnel /32s', 'explicit routed LAN prefixes', 'firewall rule for UDP listen port if managed'] },
    ],
    noteText: 'This generic WireGuard template is separate from SAM, where mobile /32s are carried by BGP/FIB, not WG AllowedIPs.',
  }),
  flowDiagram({
    name: 'how-to-aws-provider-action-execution',
    title: 'AWS provider action execution',
    subtitle: 'Provider actions stay inert until policy, journal review, explicit owner approval, and executor dry-run or execute mode line up.',
    lanes: [
      { title: 'Preflight evidence', boxes: ['SAM desired path\ncaptured /32 + ENI target', 'AWS describe calls\nread-only facts', 'ProviderActionPolicy\nallowlist + dryRunOnly gate'] },
      { title: 'Gated journal', boxes: ['actionPlan stored inert', 'operator reviews journal\nand approves', 'aws-provider-executor\nuses instance IAM role'] },
      { title: 'AWS mutation', boxes: ['assign or unassign secondary IP', 'source/dest check toggled\nwith prior state recorded', 'undo uses journaled observed state'] },
    ],
    noteText: 'routerd core passes no cloud credentials; live execute is experimental and requires explicit owner go.',
  }),
  flowDiagram({
    name: 'how-to-cloudedge-autonomous-lab',
    title: 'CloudEdge autonomous lab',
    subtitle: 'cloudedge-labctl wraps SAM lab lifecycle, fault injection, directed connectivity, evidence assembly, TTL, and teardown guards.',
    lanes: [
      { title: 'Lab lifecycle', boxes: ['up\nprovider/onprem topology + run-id', 'deploy\ncommit or release bundle', 'down\nrun-id or expired TTL'] },
      { title: 'Test actions', boxes: ['smoke\nconnectivity matrix', 'failover\nstop drain BGP stop executor fail', 'evidence collect\nschema result + summary'] },
      { title: 'Safety gates', boxes: ['CE_DRY_RUN=1 by default', 'TTL tags and cost guard', 'providerState folded in by lab operator'] },
    ],
    noteText: 'The harness defines the interface; live provider provisioning still depends on approved lab credentials and wiring.',
  }),
  flowDiagram({
    name: 'how-to-cloudedge-mobility-demo',
    title: 'CloudEdge mobility demo',
    subtitle: 'Four sites share one logical /24; SAMTransportProfile generates IPIP delivery and BGP mobility paths over optional WG underlay.',
    lanes: [
      { title: 'Shared address space', boxes: ['on-prem owner\n10.77.60.10/32', 'AWS Azure OCI owners\n10.77.60.11-13/32', 'one logical subnet\n10.77.60.0/24'] },
      { title: 'SAM control plane', boxes: ['MobilityPool\nownership + capture intent', 'SAMTransportProfile\nIPIP tunnels + BGPPeers', 'Event Federation\nobserved client facts'] },
      { title: 'Data-plane proof', boxes: ['provider secondary IP\nor proxy ARP capture', 'BGP /32 delivery\nsource IP preserved', 'D3 12 directed flows\nD5 cloud maintenance move'] },
    ],
    noteText: 'WireGuard AllowedIPs contain transport endpoints only; mobile /32s are carried by BGP and the kernel FIB.',
  }),
  flowDiagram({
    name: 'how-to-cloudedge-protocol-transparency',
    title: 'CloudEdge protocol transparency',
    subtitle: 'The D11 harness validates NAT-less SAM behavior for protocols that depend on dynamic ports, source address, and PMTU.',
    lanes: [
      { title: 'Representative pairs', boxes: ['aws -> azure\ncloud-to-cloud', 'aws -> onprem\nproxy-ARP path', 'shared subnet\n10.77.60.0/24'] },
      { title: 'Protocol probes', boxes: ['FTP active + passive', 'rpcbind + NFSv3', 'bulk transfer + PMTU\nMSS clamp evidence'] },
      { title: 'Acceptance output', boxes: ['source preserved\nno NAT assertion', 'route MTU/advmss\nunknown includes reason', 'protocol result JSON\nfolded into evidence'] },
    ],
    noteText: 'Live packet capture and services run later in the lab; the offline contract fixes the result shape.',
  }),
  flowDiagram({
    name: 'how-to-cloudedge-sam-oci-firewall-bootstrap',
    title: 'OCI SAM firewall bootstrap',
    subtitle: 'OCI Ubuntu image firewall defaults can block WireGuard handshakes and SAM forwarding before provider fabric rules are involved.',
    lanes: [
      { title: 'Symptom layer', boxes: ['OCI security list allows UDP/51820', 'VNIC skip source/dest enabled', 'guest iptables-nft still rejects INPUT/FORWARD'] },
      { title: 'Host bootstrap', boxes: ['allow inbound UDP/51820\nto wg-hybrid', 'allow FORWARD\nVNIC <-> wg-hybrid', 'declare prerequisites\ninstead of ad-hoc rules'] },
      { title: 'Diagnosis', boxes: ['routerctl doctor hybrid', 'check guest firewall first', 'then provider security list\nand source/dest check'] },
    ],
    noteText: 'This is provider-image bootstrap behavior, not a SAM dataplane design change.',
  }),
  flowDiagram({
    name: 'how-to-dhcp-lease-sync',
    title: 'DHCP lease sync for HA routers',
    subtitle: 'Active routers rsync persistent lease files to standby routers, gated by VirtualAddress role and hardened SSH defaults.',
    lanes: [
      { title: 'Active source', boxes: ['DHCPv4Server\nDHCPv6Server\nDHCPv6PD', 'platform-derived lease path', 'when gate\nVirtualAddress role = master'] },
      { title: 'Sync resource', boxes: ['LeaseSync source resource', 'rsync over SSH\nBatchMode + timeout defaults', 'active-to-standby only'] },
      { title: 'Standby result', boxes: ['warm lease database', 'promotion starts from last sync', 'Pending when when=false\nor source not ready'] },
    ],
    noteText: 'Keep target.user non-interactive and validate newline-free target fields before enabling sync.',
  }),
  flowDiagram({
    name: 'how-to-dns-local-zone',
    title: 'local DNS zones',
    subtitle: 'DNSZone combines manual records, DHCP-derived records, reverse zones, and resolver sources for internal names.',
    lanes: [
      { title: 'Name sources', boxes: ['manual A AAAA PTR records', 'DHCPv4/v6 leases\nvia dnsmasq relay events', 'startup lease file reread'] },
      { title: 'routerd DNS', boxes: ['DNSZone\nlocal authoritative data', 'DNSResolver\nserves zone source', 'event bus updates\nin-memory tables'] },
      { title: 'Client lookup', boxes: ['router.lan.example', 'DHCP client hostnames', 'PTR reverse lookups'] },
    ],
    noteText: 'Use a domain you control or a reserved internal suffix; avoid colliding with public DNS names.',
  }),
  flowDiagram({
    name: 'how-to-dns-private-upstream',
    title: 'private DNS upstreams',
    subtitle: 'DNSForwarder and DNSUpstream resources route private zones, provider bootstrap names, and default encrypted DNS.',
    lanes: [
      { title: 'Query classes', boxes: ['local zone queries', 'access-network zones\nAFTR or intranet', 'default internet DNS'] },
      { title: 'Resolver graph', boxes: ['DNSForwarder\nordered match rules', 'DNSUpstream\nUDP TCP DoT DoH', 'addressFrom\nDHCPv6Information status'] },
      { title: 'Runtime behavior', boxes: ['highest-priority healthy upstream', 'fallback through upstream list', 'private endpoints stay out of shared examples'] },
    ],
    noteText: 'Conditional forwarding keeps provider-only names resolvable without exposing account-specific endpoints.',
  }),
  flowDiagram({
    name: 'how-to-event-federation-subscription',
    title: 'federated event to RemoteAddressClaim',
    subtitle: 'A received event matches an EventSubscription, runs a trusted local plugin, and stores a DynamicConfigPart.',
    lanes: [
      { title: 'Sender fact', boxes: ['on-prem observes client /32', 'routerd.client.ipv4.observed', 'EventGroup + EventPeer\npush transport'] },
      { title: 'Receiver trigger', boxes: ['EventSubscription match\nrequired event types', 'plugin event-to-remote-claim', 'PluginResult validated\nas DynamicConfigPart'] },
      { title: 'Effective config', boxes: ['RemoteAddressClaim appears', 'provenance annotations\nevent id/group/source', 'routerctl dynamic render\ninspection path'] },
    ],
    noteText: 'The example plugin is provider-agnostic and does not execute cloud mutations; actionPlans remain separate.',
  }),
  flowDiagram({
    name: 'how-to-firewall-rule',
    title: 'firewall rule exceptions',
    subtitle: 'FirewallRule adds explicit exceptions before the role matrix while routerd-derived service openings stay first.',
    lanes: [
      { title: 'Need an exception', boxes: ['SSH from management subnet', 'service port on router self', 'selected LAN host or WAN path'] },
      { title: 'FirewallRule', boxes: ['fromZone + toZone', 'protocol ports CIDRs\nICMP type names', 'rateLimit connLimit\nsets and actions'] },
      { title: 'Evaluation order', boxes: ['routerd service openings first', 'user exceptions next', 'implicit role matrix last'] },
    ],
    noteText: 'Use destinationCIDRs and sets to scope exceptions; avoid weakening the whole zone role.',
  }),
  flowDiagram({
    name: 'how-to-firewall-zone',
    title: 'firewall zones',
    subtitle: 'FirewallZone maps interfaces to trust roles; the built-in role matrix supplies safe stateful defaults.',
    lanes: [
      { title: 'Interfaces', boxes: ['WAN uplink\nuntrust', 'LAN segment\ntrust', 'management network\nmgmt'] },
      { title: 'FirewallZone model', boxes: ['interface references\nInterface/wan DSLiteTunnel/*', 'role matrix\nself trust mgmt untrust', 'established/related always accepted'] },
      { title: 'Default behavior', boxes: ['WAN cannot reach LAN', 'LAN can reach WAN', 'management can reach everything'] },
    ],
    noteText: 'Add FirewallRule only for exceptions; zones express topology and roles.',
  }),
  flowDiagram({
    name: 'how-to-flets-ipv6-setup',
    title: 'DS-Lite over DHCPv6-PD',
    subtitle: 'IPv6-only access gets a delegated LAN prefix and sends IPv4 through an AFTR-resolved DS-Lite tunnel.',
    lanes: [
      { title: 'Access network', boxes: ['WAN IPv6 RA', 'DHCPv6-PD lease', 'carrier DNS for AFTR FQDN'] },
      { title: 'routerd resources', boxes: ['DHCPv6PrefixDelegation', 'IPv6DelegatedAddress\nLAN ::1', 'DNSForwarder + DSLiteTunnel'] },
      { title: 'LAN service', boxes: ['RA advertises delegated prefix', 'RDNSS points at router resolver', 'IPv4 exits through ip6tnl AFTR'] },
    ],
    noteText: 'If PD is not Bound, routerd pauses stale IPv6 advertisement instead of presenting broken IPv6 to clients.',
  }),
  flowDiagram({
    name: 'how-to-guest-mode',
    title: 'guest mode by MAC address',
    subtitle: 'ClientPolicy narrows selected clients on a shared LAN before the normal trust-zone role matrix can allow traffic.',
    lanes: [
      { title: 'Client classes', boxes: ['visitor devices', 'IoT appliances', 'trusted devices excluded or included'] },
      { title: 'ClientPolicy render', boxes: ['MAC or OUI nft set', 'include or exclude mode', 'self-service allows\nLAN/MGMT/private denies'] },
      { title: 'Firewall effect', boxes: ['internet access allowed', 'lateral LAN blocked', 'management blocked\nbefore trust matrix'] },
    ],
    noteText: 'This is policy isolation on the same L2 segment; use VLANs when you need a separate broadcast domain.',
  }),
  flowDiagram({
    name: 'how-to-hybrid-azure-pve-same-subnet',
    title: 'Azure and PVE same-subnet SAM smoke',
    subtitle: 'Azure provider-secondary-IP capture and on-prem proxy-ARP capture exchange selected /32s over SAM delivery routes.',
    lanes: [
      { title: 'Azure side', boxes: ['NIC secondary IP\nprovider captures on-prem /32', 'guest OS must not own captured /32', 'IP forwarding enabled\nAzure + Linux'] },
      { title: 'On-prem PVE side', boxes: ['proxy ARP capture\nLAN or bridge', 'forwarding + proxy_arp sysctls', 'permit capture <-> wg-hybrid forwarding'] },
      { title: 'Verification', boxes: ['routerctl doctor hybrid', 'delivery route points at tunnel', 'SAM MSS clamp\ncapture-to-tunnel paths'] },
    ],
    noteText: 'SAM delivery is per-/32 routing; it does not change the client default route and it does not NAT.',
  }),
  flowDiagram({
    name: 'how-to-ipv6-dual-stack',
    title: 'IPv6 dual-stack BGP and VIPs',
    subtitle: 'BGPRouter and BGPPeer handle IPv4/IPv6 unicast while VirtualAddress resources publish parallel A and AAAA VIPs.',
    lanes: [
      { title: 'Dual-stack intent', boxes: ['mixed allowedPrefixes\nIPv4 + IPv6', 'peer addresses\nIPv4 and IPv6', 'parallel VIP resources\nfamily ipv4 and ipv6'] },
      { title: 'routerd apply', boxes: ['GoBGP address families\ntyped by prefix', 'keepalived VRRPv2/v3\nor FreeBSD CARP', 'DNSZone gets A + AAAA\nsame hostname'] },
      { title: 'Runtime behavior', boxes: ['BGP TCP/179 opened for both families', 'VRRP protocol 112 allowed', 'routerctl show bgp/vrrp'] },
    ],
    noteText: 'Keep import, export, and redistribute allow-lists explicit for both address families.',
  }),
  flowDiagram({
    name: 'how-to-multi-wan',
    title: 'multi-WAN egress selection',
    subtitle: 'Health-based EgressRoutePolicy picks the highest-weight ready path and updates routes and NAT without flushing conntrack.',
    lanes: [
      { title: 'Candidate paths', boxes: ['DS-Lite primary', 'upstream gateway fallback', 'PPPoE or LTE backup'] },
      { title: 'Selection resources', boxes: ['HealthCheck per candidate', 'EgressRoutePolicy\nhighest-weight-ready', 'NAT44Rule follows selected policy'] },
      { title: 'Operational result', boxes: ['new flows use selected path', 'existing conntrack remains', 'hysteresis damps flapping'] },
    ],
    noteText: 'Disable scarce backup sessions in YAML until you intentionally test them.',
  }),
  flowDiagram({
    name: 'how-to-nat44-session-sync',
    title: 'NAT44 session sync for HA routers',
    subtitle: 'The active router snapshots selected conntrack SNAT sessions and restores them on a standby with degraded/error visibility.',
    lanes: [
      { title: 'Active source', boxes: ['NAT44Rule status\nSNAT addresses', 'conntrack dump\nextended marks', 'when gate\nVIP role = master'] },
      { title: 'Sync controller', boxes: ['NAT44SessionSync\nsnapshot mode', 'delete-then-insert restore script', 'SSH target\nBatchMode + timeout'] },
      { title: 'Standby status', boxes: ['warm conntrack entries', 'ok_ins/ng_ins parsed', 'degraded when inserts fail\nor zero inserted for entries'] },
    ],
    noteText: 'Preserving conntrack marks matters when policy routing keeps established flows on the same egress path.',
  }),
  flowDiagram({
    name: 'how-to-opentelemetry',
    title: 'OpenTelemetry export',
    subtitle: 'routerd daemons use standard OTLP environment variables to send logs, metrics, and traces to an external collector.',
    lanes: [
      { title: 'Signals', boxes: ['routerd control plane\nreconcile traces', 'DHCP PPPoE healthcheck daemons', 'resource attributes\nresource name/site/host'] },
      { title: 'Configuration', boxes: ['OTEL_EXPORTER_OTLP_ENDPOINT', 'per-signal endpoints\nlogs metrics traces', 'systemd drop-ins\nor NixOS service env'] },
      { title: 'Collector path', boxes: ['OTLP/gRPC collector', 'Loki Tempo Mimir\nor managed backend', 'unset variables = telemetry off'] },
    ],
    noteText: 'routerd does not bundle a collector; point each daemon at one you already operate.',
  }),
  flowDiagram({
    name: 'how-to-os-bootstrap',
    title: 'declarative OS bootstrap',
    subtitle: 'router-specific host preparation is derived from resources, while Package and SysctlProfile remain narrow escape hatches.',
    lanes: [
      { title: 'Host prerequisites', boxes: ['OS packages\ndnsmasq nft conntrack WG', 'kernel modules\nderived from resources', 'installer networking\nkept minimal'] },
      { title: 'routerd config', boxes: ['Package override\nwhen dependency not derived', 'derived sysctls\nforwarding rp_filter conntrack', 'Interface/DHCP/DNS\nadoption drop-ins'] },
      { title: 'Runtime target', boxes: ['clean first boot', 'no competing DHCPv6 client\non router-owned WAN', 'router-specific drift stays in YAML'] },
    ],
    noteText: 'The goal is not to replace an installer; it is to keep router-specific drift out of shell history.',
  }),
  flowDiagram({
    name: 'how-to-pve-overlay',
    title: 'PVE overlay replacement',
    subtitle: 'A routed WireGuard underlay plus optional VXLAN segment replaces heavy hypervisor overlay VPNs with observable resources.',
    lanes: [
      { title: 'Existing pain', boxes: ['tap-based overlay VPN', 'poor guest-to-guest throughput', 'MTU mismatch and separate operations'] },
      { title: 'routerd primitives', boxes: ['WireGuardInterface + Peer\nL3 encrypted underlay', 'VXLANTunnel\nonly when L2 stretch needed', 'EgressRoutePolicy + HealthCheck\noptional readiness'] },
      { title: 'Verification', boxes: ['ip -d link show\nwg and vxlan', 'PMTU ping sizes\n1420 / 1370 examples', 'routerctl describe resources'] },
    ],
    noteText: 'Prefer L3 routing by default; stretch L2 only for segments that truly require it.',
  }),
  flowDiagram({
    name: 'how-to-tailscale',
    title: 'Tailscale exit node and subnet router',
    subtitle: 'TailscaleNode declares host-local tailscale up options while the tailnet control plane keeps account and route approval.',
    lanes: [
      { title: 'Host prep', boxes: ['Package tailscale\nmanager-specific names', 'auth key in env file\noutside YAML', 'UDP/41641 reserved\nwhen TailscaleNode exists'] },
      { title: 'TailscaleNode', boxes: ['hostname edge', 'advertiseExitNode', 'advertiseRoutes\nprivate subnets'] },
      { title: 'Tailnet result', boxes: ['subnet routes await approval', 'exit node available by policy', 'acceptDNS/acceptRoutes\nexplicit in config'] },
    ],
    noteText: 'routerd does not replace tailscaled; it owns the local declarative intent for the node.',
  }),
  flowDiagram({
    name: 'how-to-troubleshooting',
    title: 'routerd troubleshooting order',
    subtitle: 'Start with routerd intent and status, then compare with generated host state and daemon-specific evidence.',
    lanes: [
      { title: 'routerd view', boxes: ['routerctl status', 'routerctl describe kind/name', 'routerd apply --once --dry-run'] },
      { title: 'Host evidence', boxes: ['ip nft ss journalctl', 'daemon /v1/status sockets', 'state DB events and logs'] },
      { title: 'Common checks', boxes: ['DHCPv6-PD Bound\nor stale IPv6 paused', 'dnsmasq DHCP/RA only\nDNS resolver separate', 'do not flush conntrack\nor run competing clients'] },
    ],
    noteText: 'Separate intended state from actual host state before changing a live router.',
  }),
  flowDiagram({
    name: 'how-to-vscode-yaml-schema',
    title: 'VS Code YAML schema',
    subtitle: 'The published JSON Schema connects routerd YAML files to completion, hover text, enum validation, and CI-checked contracts.',
    lanes: [
      { title: 'Schema source', boxes: ['Go API types', 'make generate-schema', 'website/static/schemas\npublished routerd.net URL'] },
      { title: 'Editor hookup', boxes: ['yaml-language-server modeline', '.vscode/settings.json\nworkspace mapping', 'examples and wizard fixtures'] },
      { title: 'Feedback loop', boxes: ['completion and hover', 'enum and type diagnostics', 'check-website-schemas\nprevents stale public copy'] },
    ],
    noteText: 'Use the modeline for arbitrary filenames that do not match the workspace mapping.',
  }),
  flowDiagram({
    name: 'tutorial-basic-firewall',
    title: 'basic NAT and firewall policy',
    subtitle: 'A fresh Linux router gets outbound IPv4 masquerade, conservative zone defaults, conntrack observation, and validation checks.',
    lanes: [
      { title: 'Router shape', boxes: ['WAN interface\nuntrusted uplink', 'LAN interface\nprivate clients', 'optional management\nseparate access path'] },
      { title: 'routerd resources', boxes: ['NAT44Rule\nLAN masquerade to WAN', 'FirewallZone\nwan lan mgmt roles', 'FirewallPolicy\ndefault stateful matrix'] },
      { title: 'Verify', boxes: ['routerctl describe NAT44Rule', 'routerctl firewall test', 'nft tables\nrouterd_filter + routerd_nat'] },
    ],
    noteText: 'Managed service openings stay derived so DHCP, DNS, and control traffic continue to work with the firewall enabled.',
  }),
  flowDiagram({
    name: 'tutorial-diskless-minipc-walkthrough',
    title: 'diskless mini PC walkthrough',
    subtitle: 'Boot the live ISO, configure WAN/LAN in the wizard, persist router.yaml on USB, and keep logs compact on removable media.',
    lanes: [
      { title: 'Hardware and media', boxes: ['x86 mini PC\n2+ NICs', 'routerd-live.iso\nconsole or serial', 'USB stick labeled ROUTERD'] },
      { title: 'Live workflow', boxes: ['boot ISO\nverify checksum first', 'setup wizard\nWAN LAN DHCP DNS RA NAT', 'USB persistence\nrouter.yaml + daily log archive'] },
      { title: 'Operational loop', boxes: ['validate and dry-run config', 'boot persistent router', 'recover by editing USB\nor console access'] },
    ],
    noteText: 'Use an isolated LAN bridge during early DHCP and RA tests so a live network is not surprised.',
  }),
  flowDiagram({
    name: 'tutorial-first-router',
    title: 'bring up the first router',
    subtitle: 'The smallest routerd config adopts one DHCPv4 WAN and one static LAN address, then validates before live apply.',
    lanes: [
      { title: 'Interfaces', boxes: ['wan\nens18 DHCPv4', 'lan\nens19 static IPv4', 'management path\nconsole SSH or hypervisor'] },
      { title: 'Minimal resources', boxes: ['Interface/wan + Interface/lan', 'DHCPv4Client/wan\nmanaged daemon', 'IPv4StaticAddress/lan-address'] },
      { title: 'Safe apply loop', boxes: ['routerd validate', 'routerd plan', 'apply --once --dry-run\nthen live apply'] },
    ],
    noteText: 'Confirm the management connection survives before removing dry-run.',
  }),
  flowDiagram({
    name: 'tutorial-freebsd-getting-started',
    title: 'getting started on FreeBSD',
    subtitle: 'The same resource model renders FreeBSD-native rc.d, rc.conf.d, pf, dnsmasq, mpd5, and dynamic gif tunnel artifacts.',
    lanes: [
      { title: 'Install target', boxes: ['FreeBSD 14.x\n/usr/local layout', 'release archive\nrouterd-freebsd-amd64', 'packages\nmpd5 dnsmasq wireguard jq'] },
      { title: 'Review before apply', boxes: ['routerd validate', 'routerd render freebsd\n/tmp output', 'inspect pf.conf\ndnsmasq.conf rc.d scripts'] },
      { title: 'Apply and observe', boxes: ['pfctl -nf before load', 'dnsmasq --test before restart', 'routerctl status\nservice logs'] },
    ],
    noteText: 'FreeBSD is supported through native host surfaces, but platform parity still follows docs/platforms.md.',
  }),
  flowDiagram({
    name: 'tutorial-getting-started',
    title: 'getting started safely',
    subtitle: 'The first routerd loop writes a small config, validates it, inspects the plan, dry-runs apply, and only then runs serve.',
    lanes: [
      { title: 'Prepare', boxes: ['install release archive', 'check interface names\nip link', 'keep management path separate'] },
      { title: 'First config', boxes: ['Package override\nonly if needed', 'Interface resources\nwan lan mgmt', 'derived host runtime\npackages sysctls services'] },
      { title: 'Safety loop', boxes: ['validate', 'plan', 'apply --once --dry-run', 'serve + routerctl status/events'] },
    ],
    noteText: 'The first pass should not change the host network until the plan is understood.',
  }),
  flowDiagram({
    name: 'tutorial-index',
    title: 'tutorial path overview',
    subtitle: 'Pick a first deployment path, then add WAN acquisition, LAN services, firewall policy, and platform-specific startup work.',
    lanes: [
      { title: 'Start here', boxes: ['Install release archive', 'Getting started\nsafe first loop', 'Diskless mini PC\nlive ISO + USB'] },
      { title: 'Build router features', boxes: ['WAN-side services\nDHCP PPPoE DS-Lite PD', 'LAN-side services\nDHCP DNS RA NTP', 'Basic firewall\nNAT44 + zones'] },
      { title: 'Platform paths', boxes: ['NixOS generated module', 'FreeBSD native artifacts', 'reuse same resource model\nas network grows'] },
    ],
    noteText: 'The same YAML resource model spans virtual labs and physical routers.',
  }),
  flowDiagram({
    name: 'tutorial-install',
    title: 'install routerd',
    subtitle: 'Install from a release archive, preserve existing config and state, validate the sample config, then dry-run before live apply.',
    lanes: [
      { title: 'Release archive', boxes: ['download tar.gz + sha256', 'linux amd64/arm64\nfreebsd amd64/arm64', 'no Go toolchain needed'] },
      { title: 'Installer work', boxes: ['install dependencies\nor --no-install-deps', 'copy binaries to /usr/local/sbin', 'install service templates\nand router.yaml.sample'] },
      { title: 'After install', boxes: ['edit /usr/local/etc/routerd/router.yaml', 'validate plan dry-run', 'apply when management access is safe'] },
    ],
    noteText: 'Existing router.yaml and state directories are preserved across install and upgrade.',
  }),
  flowDiagram({
    name: 'tutorial-lan-side-services',
    title: 'LAN-side services',
    subtitle: 'LAN resources publish inside addresses, DHCPv4/v6, RA, NTP options, local DNS zones, and lease-derived names.',
    lanes: [
      { title: 'Inside network', boxes: ['LAN IPv4 address', 'delegated IPv6 prefix\nfrom WAN PD', 'client leases\nreservations and sticky holds'] },
      { title: 'routerd daemons', boxes: ['dnsmasq\nDHCPv4 DHCPv6 RA relay', 'routerd-dns-resolver\nzones forwarders cache logs', 'dhcp-event-relay\nlease updates to DNS'] },
      { title: 'Client experience', boxes: ['gateway DNS NTP options', 'SLAAC + RDNSS for IPv6', 'local names and reverse lookups'] },
    ],
    noteText: 'DNS resolution is not dnsmasq in current routerd; it is handled by routerd-dns-resolver.',
  }),
  flowDiagram({
    name: 'tutorial-nixos-getting-started',
    title: 'getting started on NixOS',
    subtitle: 'routerd keeps NixOS services declarative by generating a module instead of relying on transient systemd units.',
    lanes: [
      { title: 'NixOS host', boxes: ['install binaries from archive', 'keep OS packages in NixOS config', 'install.sh warns\ninstead of nix-env'] },
      { title: 'Generated module', boxes: ['/etc/nixos/routerd-generated.nix', 'systemd units\nRuntimeDirectory StateDirectory caps', 'nftables dnsmasq DHCP PPPoE\nthrough module'] },
      { title: 'Rebuild loop', boxes: ['nixos-rebuild test', 'nixos-rebuild switch', 'rollback attempted\non failed switch'] },
    ],
    noteText: 'NixOS is a secondary target with generated-module coverage; check platforms.md for the current matrix.',
  }),
  flowDiagram({
    name: 'tutorial-wan-side-services',
    title: 'WAN-side services',
    subtitle: 'WAN resources acquire upstream addresses and prefixes, terminate PPPoE or DS-Lite, select egress, and feed downstream services.',
    lanes: [
      { title: 'Provider patterns', boxes: ['native dual-stack\nDHCPv4 + DHCPv6-PD', 'PPPoE IPv4\nplus native IPv6 PD', 'DS-Lite IPv4 over IPv6\nAFTR via DNS'] },
      { title: 'routerd resources', boxes: ['DHCPv4Client\nDHCPv6PrefixDelegation', 'PPPoESession\nDSLiteTunnel', 'HealthCheck + EgressRoutePolicy\nNAT44Rule'] },
      { title: 'Downstream inputs', boxes: ['IPv6DelegatedAddress\nLAN prefix', 'DNS/NTP from DHCP status', 'selected default egress\nfor NAT and routes'] },
    ],
    noteText: 'Pick the subset matching your ISP; routerd publishes status so LAN resources can react.',
  }),
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
