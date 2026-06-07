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
