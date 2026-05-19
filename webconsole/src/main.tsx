// SPDX-License-Identifier: BSD-3-Clause

import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Badge,
  Button,
  Card,
  CardHeader,
  DrawerBody,
  DrawerHeader,
  DrawerHeaderTitle,
  FluentProvider,
  Input,
  OverlayDrawer,
  Select,
  Spinner,
  Table,
  TableBody,
  TableCell,
  TableHeader,
  TableHeaderCell,
  TableRow,
  Text,
  makeStyles,
  tokens,
  webDarkTheme,
} from "@fluentui/react-components";
import {
  ArrowClockwiseRegular,
  ArrowUpRegular,
  CameraRegular,
  ChevronDownRegular,
  ChevronRightRegular,
  DatabaseRegular,
  DesktopRegular,
  DismissRegular,
  DocumentTextRegular,
  FilterRegular,
  GamesRegular,
  HomeRegular,
  LaptopRegular,
  NavigationRegular,
  PeopleRegular,
  PhoneRegular,
  PlugConnectedRegular,
  PrintRegular,
  ServerRegular,
  ShieldRegular,
  Speaker2Regular,
  TabletRegular,
  TvRegular,
  VehicleCarRegular,
} from "@fluentui/react-icons";
import { parseDocument } from "yaml";
import "./styles.css";

type RouterdConfig = {
  basePath: string;
  title: string;
};

declare global {
  interface Window {
    __ROUTERD_WEB_CONSOLE__?: RouterdConfig;
  }
}

type Summary = {
  generatedAt?: string;
  status?: { status?: Record<string, unknown> };
  controllers?: ControllerStatus[];
  phases?: Record<string, number>;
  resources?: ResourceStatus[];
  interfaces?: InterfaceSummary[];
  events?: RouterEvent[];
  connections?: ConnectionTable;
  dnsQueries?: DNSQuery[];
  trafficFlows?: TrafficFlow[];
  firewallLogs?: FirewallLog[];
  conntrackTuning?: ConntrackTuningSummary;
  dhcpFingerprints?: DHCPFingerprint[];
  dhcpLeases?: DHCPLease[];
  neighbors?: NeighborEntry[];
  clients?: ClientEntry[];
  vpn?: VPNStatus;
  dpi?: DPIStatus;
  systemUsage?: SystemUsage;
  errors?: string[];
};

type SystemUsage = {
  cpuPercent?: number;
  load1?: number;
  memoryUsedBytes?: number;
  memoryTotalBytes?: number;
  memoryUsedPercent?: number;
  disks?: DiskUsage[];
};

type DiskUsage = {
  path?: string;
  usedBytes?: number;
  totalBytes?: number;
  usedPercent?: number;
};

type DPIStatus = {
  classifier?: DPIServiceStatus;
  agent?: DPIServiceStatus;
};

type DPIServiceStatus = {
  available?: boolean;
  socket?: string;
  engine?: string;
  activeEngine?: string;
  libndpiLoaded?: boolean;
  libndpiVersion?: string;
  reason?: string;
  error?: string;
  stats?: Record<string, unknown>;
};

type ResourceStatus = {
  apiVersion?: string;
  kind?: string;
  name?: string;
  owner?: string;
  managedBy?: string;
  management?: string;
  status?: Record<string, unknown>;
};

type RoutesStatus = {
  generatedAt?: string;
  routes?: RouteEntry[];
  bgpPeers?: RouteBGPPeer[];
  errors?: string[];
};

type RouteEntry = {
  source?: string;
  resource?: string;
  family?: string;
  destination?: string;
  gateway?: string;
  device?: string;
  protocol?: string;
  table?: string;
  metric?: string;
  scope?: string;
  type?: string;
  peer?: string;
  phase?: string;
  observedAt?: string;
};

type RouteBGPPeer = {
  router?: string;
  peer?: string;
  asn?: string;
  state?: string;
  established?: boolean;
  prefixesReceived?: string;
  messages?: string;
  lastEstablishedAt?: string;
  lastErrorReason?: string;
};

type ControllerStatus = {
  name?: string;
  mode?: string;
  reason?: string;
  message?: string;
  resourceKinds?: string[];
  interval?: string;
  lastTrigger?: string;
  lastReconcileTime?: string;
  lastSuccessTime?: string;
  nextReconcileTime?: string;
  reconcileCount?: number;
  reconcileErrorCount?: number;
  lastDuration?: string;
  maxDuration?: string;
  averageDuration?: string;
  lastDurationMillis?: number;
  maxDurationMillis?: number;
  averageDurationMillis?: number;
  lastError?: string;
};

type RouterEvent = {
  id?: number;
  createdAt?: string;
  severity?: string;
  topic?: string;
  type?: string;
  reason?: string;
  message?: string;
  resourceKind?: string;
  resourceName?: string;
  kind?: string;
  name?: string;
  attributes?: Record<string, unknown>;
};

type StreamEvent = {
  cursor?: string;
  time?: string;
  type?: string;
  severity?: string;
  reason?: string;
  message?: string;
  resource?: {
    apiVersion?: string;
    kind?: string;
    name?: string;
  };
  attributes?: Record<string, string>;
};

type ScrollSnapshot = {
  capturedAt: number;
  windowX: number;
  windowY: number;
  anchor?: { id: string; top: number };
  elements: { key: string; top: number; left: number }[];
};

type ConnectionTable = {
  count?: number;
  max?: number;
  byFamily?: Record<string, number>;
  entries?: ConnectionEntry[];
};

type ConnTuple = {
  source?: string;
  sourcePort?: string;
  destination?: string;
  destinationPort?: string;
  sourceHostname?: string;
  destinationHostname?: string;
  sourceService?: string;
  destinationService?: string;
  packets?: number;
  bytes?: number;
  accounting?: boolean;
};

type ConnectionEntry = {
  family?: string;
  protocol?: string;
  state?: string;
  assured?: boolean;
  timeout?: number;
  mark?: string;
  original?: ConnTuple;
  reply?: ConnTuple;
  localRedirect?: LocalRedirect;
  appName?: string;
  appCategory?: string;
  appConfidence?: number;
  tlsSNI?: string;
  httpHost?: string;
  dnsQuery?: string;
};

type LocalRedirect = {
  resourceName?: string;
  ruleName?: string;
  destinationSetRef?: string;
  originalAddress?: string;
  redirectAddress?: string;
  redirectPort?: number;
  match?: string;
};

type DNSQuery = {
  questionName?: string;
  answers?: string[];
};

type TrafficFlow = {
  clientAddress?: string;
  peerAddress?: string;
  peerPort?: number;
  resolvedHostname?: string;
  tlsSNI?: string;
  protocol?: string;
  appName?: string;
  appCategory?: string;
  appConfidence?: number;
  detectedProtocol?: string;
  masterProtocol?: string;
  applicationProtocol?: string;
  category?: string;
  risk?: string[];
  confidence?: number;
  metadata?: Record<string, string>;
  engine?: string;
  source?: string;
  httpHost?: string;
  dnsQuery?: string;
  accounting?: boolean;
  bytesOut?: number;
  bytesIn?: number;
};

type FirewallLog = {
  id?: number;
  ts?: string;
  action?: string;
  srcAddress?: string;
  srcPort?: number;
  dstAddress?: string;
  dstPort?: number;
  srcHostname?: string;
  dstHostname?: string;
  srcService?: string;
  dstService?: string;
  protocol?: string;
  tcpFlags?: string;
  l3Proto?: string;
  ruleName?: string;
  inIface?: string;
  outIface?: string;
  packetBytes?: number;
  hint?: string;
  dpiApp?: string;
  dpiCategory?: string;
  dpiTlsSNI?: string;
  dpiHttpHost?: string;
  dpiDnsQuery?: string;
  dpiConfidence?: number;
  correlation?: string;
  correlationDetail?: string;
  expiredAgeSeconds?: number;
  expiredBytes?: number;
  destinationSetMatches?: AddressSetMatch[];
};

type AddressSetMatch = {
  resourceName?: string;
  setName?: string;
  source?: string;
  current?: boolean;
};

type FirewallDenyTimelineBucket = {
  start?: string;
  end?: string;
  count?: number;
};

type ConntrackTuningSummary = {
  generatedAt?: string;
  window?: string;
  applyMode?: string;
  autoApply?: boolean;
  suggestions?: ConntrackTuningSuggestion[];
};

type ConntrackTuningSuggestion = {
  application?: string;
  protocol?: string;
  sysctlKey?: string;
  recommendedSeconds?: number;
  baselineSeconds?: number;
  observedFlows?: number;
  expiredFlows?: number;
  orphanReturns?: number;
  denyEvents?: number;
  averageDurationSeconds?: number;
  orphanRate?: number;
  rationale?: string;
  productionApplyGuard?: string;
};

type DHCPLease = {
  expiresAt?: string;
  mac?: string;
  ip?: string;
  hostname?: string;
  clientId?: string;
  vendor?: string;
  family?: string;
  source?: string;
  stickyUntil?: string;
  stickyState?: string;
};

type DHCPFingerprint = {
  mac?: string;
  hostname?: string;
  osFamily?: string;
  deviceClass?: string;
  confidence?: number;
  signal?: string;
  observedAt?: string;
  source?: string;
};

type NeighborEntry = {
  ip?: string;
  ifname?: string;
  mac?: string;
  state?: string;
  source?: string;
  vendor?: string;
};

type ClientEntry = {
  id?: string;
  hostname?: string;
  mac?: string;
  vendor?: string;
  addresses?: string[];
  state?: string;
  sources?: string[];
  peers?: string[];
  bytesOut?: number;
  bytesIn?: number;
  primaryActivity?: string;
  lastProtocol?: string;
  lastProtocolDetail?: string;
  protocolMix?: string[];
  inferredOSFamily?: string;
  inferredDeviceClass?: string;
  fingerprintConfidence?: number;
  fingerprintSignals?: string[];
  stickyUntil?: string;
  stickyState?: string;
  clientPolicy?: string;
  clientPolicyMode?: string;
  isolationPolicy?: string[];
};

type ClientIdentity = {
  label: string;
  compactLabel: string;
  searchText: string;
};

type InterfaceSummary = {
  name?: string;
  ifname?: string;
  phase?: string;
  role?: string;
  zone?: string;
  managed?: boolean;
  owner?: string;
  mtu?: number;
  hardwareAddress?: string;
  flags?: string;
  addresses?: string[];
};

type VPNStatus = {
  wireGuard?: WireGuardInterfaceStatus[];
  tailscale?: TailscaleStatus;
  errors?: string[];
};

type WireGuardInterfaceStatus = {
  name?: string;
  publicKey?: string;
  listenPort?: number;
  fwmark?: string;
  peers?: WireGuardPeerStatus[];
};

type WireGuardPeerStatus = {
  publicKey?: string;
  endpoint?: string;
  allowedIPs?: string[];
  latestHandshake?: string;
  transferRxBytes?: number;
  transferTxBytes?: number;
  persistentKeepaliveSec?: number;
};

type TailscaleStatus = {
  backendState?: string;
  tailnetName?: string;
  magicDNSSuffix?: string;
  magicDNSEnabled?: boolean;
  certDomains?: string[];
  hostName?: string;
  dnsName?: string;
  tailscaleIPs?: string[];
  allowedIPs?: string[];
  online?: boolean;
  active?: boolean;
  exitNode?: boolean;
  exitNodeOption?: boolean;
  peers?: TailscalePeerStatus[];
};

type TailscalePeerStatus = {
  id?: string;
  hostName?: string;
  dnsName?: string;
  tailscaleIPs?: string[];
  allowedIPs?: string[];
  online?: boolean;
  active?: boolean;
  exitNode?: boolean;
  exitNodeOption?: boolean;
  relay?: string;
  lastSeen?: string;
  rxBytes?: number;
  txBytes?: number;
};

type ConfigSnapshot = {
  path?: string;
  text?: string;
};

type GenerationRecord = {
  generation: number;
  startedAt?: string;
  finishedAt?: string;
  phase?: string;
  configHash?: string;
  hasYaml?: boolean;
};

type MetricSample = {
  time: string;
  generation: number;
  healthy: number;
  warning: number;
  danger: number;
  healthHealthy: number;
  healthUnhealthy: number;
};

type ConnectionFilters = {
  query: string;
  client: string;
  family: string;
  protocol: string;
  app: string;
  source: string;
  state: string;
  sort: string;
  direction: string;
};

type FirewallFilters = {
  query: string;
  source: string;
  destination: string;
  port: string;
  protocol: string;
};

type EventFilters = {
  query: string;
  severity: string;
  resourceKind: string;
  range: string;
  customHours: string;
};

type ClientRow = {
  id?: string;
  ip: string;
  addresses: Set<string>;
  hostname: string;
  mac: string;
  vendor: string;
  state?: string;
  sources: Set<string>;
  expiresAt: string;
  bytesOut?: number;
  bytesIn?: number;
  peers: Set<string>;
  primaryActivity: string;
  lastProtocol: string;
  lastProtocolDetail: string;
  protocolMix: Set<string>;
  inferredOSFamily: string;
  inferredDeviceClass: string;
  fingerprintConfidence?: number;
  fingerprintSignals: Set<string>;
  stickyUntil: string;
  stickyState: string;
  clientPolicy: string;
  clientPolicyMode: string;
  isolationPolicy: Set<string>;
};

type ViewKey = "overview" | "resources" | "routes" | "controllers" | "clients" | "connections" | "vpn" | "events" | "firewall" | "config" | "generations";
type NavSubItem = { key: string; label: string; count?: number; view: ViewKey; targetID: string };

const cfg = window.__ROUTERD_WEB_CONSOLE__ ?? { basePath: "/", title: "routerd" };
const basePath = normalizeBasePath(cfg.basePath);
const defaultConnectionPageSize = 25;
const connectionPageSizeOptions = [25, 50, 100];
const collapsedStorageKey = "routerd.webconsole.collapsed";
const clientSectionsCollapsedStorageKey = "routerd.webconsole.clientSectionsCollapsed";
const connectionPagesStorageKey = "routerd.webconsole.connectionPages";
const connectionPageSizesStorageKey = "routerd.webconsole.connectionPageSizes";
const navItems: { key: ViewKey; label: string; description: string; icon: React.ReactNode }[] = [
  { key: "overview", label: "Overview", description: "Status and interfaces", icon: <HomeRegular /> },
  { key: "resources", label: "Resources", description: "Resource phases and status detail", icon: <ServerRegular /> },
  { key: "routes", label: "Routes", description: "Kernel, static, DHCP, and BGP routes", icon: <DatabaseRegular /> },
  { key: "clients", label: "Clients", description: "Leases and endpoint traffic", icon: <PeopleRegular /> },
  { key: "connections", label: "Connections", description: "conntrack and live flows", icon: <PlugConnectedRegular /> },
  { key: "vpn", label: "VPN", description: "WireGuard and Tailscale peers", icon: <PlugConnectedRegular /> },
  { key: "events", label: "Events", description: "Bus events and resource changes", icon: <ServerRegular /> },
  { key: "firewall", label: "Firewall", description: "Deny ranking and timeline", icon: <ShieldRegular /> },
  { key: "controllers", label: "Controllers", description: "Live and dry-run controller modes", icon: <ServerRegular /> },
  { key: "config", label: "Config", description: "Read-only YAML tree", icon: <DocumentTextRegular /> },
  { key: "generations", label: "Generations", description: "Applied YAML history and diffs", icon: <DocumentTextRegular /> },
];
const viewKeys = new Set<string>(navItems.map(item => item.key));

function useMediaQuery(query: string) {
  const [matches, setMatches] = useState(() => {
    if (typeof window === "undefined" || !window.matchMedia) return false;
    return window.matchMedia(query).matches;
  });
  useEffect(() => {
    if (typeof window === "undefined" || !window.matchMedia) return;
    const media = window.matchMedia(query);
    const onChange = () => setMatches(media.matches);
    onChange();
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, [query]);
  return matches;
}

const useStyles = makeStyles({
  shell: {
    minHeight: "100vh",
    width: "min(100%, clamp(64rem, 94vw, 96rem))",
    margin: "0 auto",
    backgroundColor: "#0b1118",
    color: tokens.colorNeutralForeground1,
    boxShadow: "0 0 0 1px rgba(255,255,255,0.03)",
    "@media (max-width: 860px)": {
      width: "100%",
      boxShadow: "none",
    },
  },
  header: {
    position: "sticky",
    top: 0,
    zIndex: 20,
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: "12px",
    minHeight: "48px",
    padding: "0 16px",
    borderBottom: "1px solid #243041",
    backgroundColor: "#111827",
    boxShadow: "0 1px 0 rgba(255,255,255,0.04)",
    "@media print": {
      display: "none",
    },
  },
  productArea: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    minWidth: 0,
  },
  title: {
    minWidth: 0,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  navToggle: {
    flexShrink: 0,
  },
  productTitleBlock: {
    display: "grid",
    gridTemplateRows: "auto auto",
    gap: "1px",
    minWidth: 0,
    lineHeight: 1.1,
  },
  productTitleText: {
    minWidth: 0,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
    lineHeight: 1.2,
  },
  subtitle: {
    color: tokens.colorNeutralForeground3,
    lineHeight: 1.2,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  layout: {
    display: "grid",
    gridTemplateColumns: "clamp(11rem, 16vw, 14rem) minmax(0, 1fr)",
    minHeight: "calc(100vh - 49px)",
    "@media (max-width: 860px)": {
      gridTemplateColumns: "1fr",
    },
  },
  layoutCollapsed: {
    gridTemplateColumns: "3.5rem minmax(0, 1fr)",
    "@media (max-width: 860px)": {
      gridTemplateColumns: "1fr",
    },
  },
  sidebar: {
    position: "sticky",
    top: "49px",
    alignSelf: "start",
    height: "calc(100vh - 49px)",
    overflowY: "auto",
    borderRight: "1px solid #243041",
    backgroundColor: "#0f1722",
    padding: "12px 8px",
    "@media print": {
      display: "none",
    },
    "@media (max-width: 860px)": {
      display: "none",
    },
  },
  sidebarCollapsed: {
    padding: "12px 6px",
    "@media (max-width: 860px)": {
      display: "none",
    },
  },
  navSection: {
    display: "grid",
    gap: "2px",
    "@media (max-width: 860px)": {
      gap: "4px",
      minWidth: 0,
    },
  },
  mobileDrawer: {
    backgroundColor: "#0f1722",
    color: tokens.colorNeutralForeground1,
  },
  mobileDrawerBody: {
    padding: "8px",
  },
  navButton: {
    width: "100%",
    justifyContent: "flex-start",
    borderRadius: tokens.borderRadiusMedium,
    padding: "9px 10px",
    color: tokens.colorNeutralForeground2,
    backgroundColor: "transparent",
    border: "1px solid transparent",
    ":hover": {
      backgroundColor: "#172235",
      color: tokens.colorNeutralForeground1,
    },
    "@media (max-width: 860px)": {
      width: "100%",
      minWidth: 0,
    },
  },
  navButtonCollapsed: {
    padding: "9px 8px",
    justifyContent: "center",
    minWidth: 0,
  },
  navButtonActive: {
    backgroundColor: "#1b2a40",
    color: tokens.colorNeutralForeground1,
    borderLeft: "3px solid #60cdff",
    ":hover": {
      backgroundColor: "#22324a",
    },
    "@media (max-width: 860px)": {
      borderLeft: "3px solid #60cdff",
      borderBottom: "1px solid #2f4664",
    },
  },
  navButtonInner: {
    display: "grid",
    gridTemplateColumns: "20px minmax(0, 1fr)",
    gap: "10px",
    alignItems: "start",
    width: "100%",
  },
  navIcon: {
    display: "grid",
    placeItems: "center",
    color: "#60cdff",
    fontSize: "18px",
    paddingTop: "1px",
  },
  navText: {
    display: "grid",
    gap: "2px",
    minWidth: 0,
  },
  navTextCollapsed: {
    display: "none",
  },
  navDescription: {
    color: tokens.colorNeutralForeground3,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
    "@media (max-width: 860px)": {
      display: "none",
    },
  },
  navSubMenu: {
    display: "grid",
    gap: "2px",
    margin: "4px 0 8px 30px",
    "@media (max-width: 860px)": {
      display: "none",
    },
  },
  navSubButton: {
    width: "100%",
    justifyContent: "space-between",
    borderRadius: tokens.borderRadiusMedium,
    padding: "5px 8px",
    color: tokens.colorNeutralForeground3,
    backgroundColor: "transparent",
    ":hover": {
      color: tokens.colorNeutralForeground1,
      backgroundColor: "#172235",
    },
  },
  navSubButtonActive: {
    color: tokens.colorNeutralForeground1,
    backgroundColor: "#1b2a40",
  },
  sectionBar: {
    display: "flex",
    flexWrap: "wrap",
    gap: "8px",
    padding: "10px 20px",
    borderBottom: "1px solid #243041",
    backgroundColor: "#0b1118",
    "@media (max-width: 640px)": {
      flexWrap: "nowrap",
      overflowX: "auto",
      overscrollBehaviorX: "contain",
      padding: "8px 12px",
    },
  },
  sectionButton: {
    borderRadius: tokens.borderRadiusMedium,
    "@media (max-width: 640px)": {
      minWidth: "max-content",
      minHeight: "44px",
    },
  },
  jumpBar: {
    display: "flex",
    flexWrap: "wrap",
    gap: "6px",
    marginBottom: "12px",
  },
  anchorDotBar: {
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    gap: "6px",
    marginBottom: "12px",
    "@media (max-width: 860px)": {
      display: "none",
    },
  },
  connectionJumpBar: {
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    gap: "8px",
    marginBottom: "12px",
  },
  activeFilterBanner: {
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    justifyContent: "space-between",
    gap: "10px",
    padding: "10px 12px",
    marginBottom: "12px",
    border: `1px solid ${tokens.colorBrandStroke2}`,
    borderRadius: tokens.borderRadiusMedium,
    backgroundColor: "#102238",
  },
  connectionCardList: {
    display: "grid",
    gap: "6px",
  },
  connectionCard: {
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: tokens.borderRadiusMedium,
    backgroundColor: tokens.colorNeutralBackground2,
    overflow: "hidden",
  },
  connectionCardExpanded: {
    backgroundColor: tokens.colorNeutralBackground3,
    border: `1px solid ${tokens.colorBrandStroke2}`,
  },
  connectionCardToggle: {
    width: "100%",
    background: "transparent",
    border: "none",
    color: "inherit",
    textAlign: "left",
    padding: "8px 12px",
    cursor: "pointer",
    display: "grid",
    gap: "4px",
    "&:hover": {
      backgroundColor: tokens.colorNeutralBackground3Hover,
    },
  },
  connectionCardLine: {
    display: "grid",
    gridTemplateColumns: "minmax(0, 1fr) max-content",
    alignItems: "center",
    gap: "10px",
    minWidth: 0,
    "@media (max-width: 860px)": {
      gridTemplateColumns: "1fr",
      alignItems: "start",
    },
  },
  connectionCardRoute: {
    display: "grid",
    gridTemplateColumns: "minmax(10rem, 18rem) 16px minmax(12rem, 1fr)",
    alignItems: "center",
    gap: "6px",
    minWidth: 0,
    "@media (max-width: 640px)": {
      gridTemplateColumns: "minmax(0, 1fr) 16px minmax(0, 1fr)",
    },
  },
  connectionCardEndpoint: {
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
    minWidth: 0,
    "@media (max-width: 860px)": {
      maxWidth: "none",
    },
  },
  connectionCardArrow: {
    color: tokens.colorNeutralForeground3,
    textAlign: "center",
  },
  connectionCardMeta: {
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    justifyContent: "flex-end",
    gap: "4px",
    minWidth: 0,
    "@media (max-width: 860px)": {
      justifyContent: "flex-start",
    },
  },
  connectionCardDetail: {
    padding: "10px 12px 12px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
  },
  anchorDot: {
    minWidth: "16px",
    width: "16px",
    height: "16px",
    padding: 0,
    borderRadius: "50%",
  },
  anchorDotActive: {
    boxShadow: `0 0 0 2px ${tokens.colorBrandStroke1}`,
  },
  content: {
    minWidth: 0,
    backgroundColor: "#0b1118",
  },
  bladeHeader: {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "flex-start",
    gap: "16px",
    padding: "18px 20px 14px",
    borderBottom: "1px solid #243041",
    backgroundColor: "#0d1420",
    "@media (max-width: 640px)": {
      flexDirection: "column",
    },
  },
  bladeTitleBlock: {
    minWidth: 0,
    display: "grid",
    gap: "4px",
  },
  bladeTitleLine: {
    display: "flex",
    alignItems: "center",
    gap: "10px",
    minWidth: 0,
  },
  bladeIcon: {
    display: "grid",
    placeItems: "center",
    width: "32px",
    height: "32px",
    borderRadius: tokens.borderRadiusMedium,
    backgroundColor: "#12395b",
    color: "#60cdff",
    fontSize: "19px",
    flexShrink: 0,
  },
  bladeSubtitle: {
    color: tokens.colorNeutralForeground3,
    paddingLeft: "42px",
    "@media (max-width: 640px)": {
      paddingLeft: 0,
    },
  },
  bladeActions: {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    flexWrap: "wrap",
  },
  streamBadge: {
    transition: "background-color 160ms ease, color 160ms ease, border-color 160ms ease",
    "@media (max-width: 860px)": {
      transition: "none",
    },
  },
  softUpdate: {
    transition: "background-color 180ms ease, opacity 180ms ease, color 180ms ease",
    "@media (max-width: 860px)": {
      transition: "none",
    },
  },
  main: {
    padding: "16px 20px 24px",
    display: "grid",
    gap: "16px",
    "@media (max-width: 640px)": {
      padding: "12px 10px 20px",
      gap: "12px",
    },
    "@media print": {
      padding: "0",
      gap: "12px",
    },
  },
  dryRunBanner: {
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    justifyContent: "space-between",
    gap: "10px",
    padding: "12px",
    border: "1px solid #8a5a00",
    borderRadius: tokens.borderRadiusMedium,
    backgroundColor: "#332300",
  },
  grid: {
    display: "grid",
    containerType: "inline-size",
    gridTemplateColumns: "repeat(auto-fit, minmax(170px, 1fr))",
    gap: "12px",
    "@container (max-width: 430px)": {
      gridTemplateColumns: "1fr",
    },
  },
  metric: {
    minWidth: 0,
    borderRadius: tokens.borderRadiusMedium,
    border: "1px solid #243041",
    backgroundColor: "#101a28",
  },
  metricValue: {
    display: "block",
    marginTop: "4px",
    whiteSpace: "normal",
    overflowWrap: "anywhere",
    wordBreak: "break-word",
    lineHeight: tokens.lineHeightBase400,
  },
  sectionGrid: {
    display: "grid",
    containerType: "inline-size",
    gridTemplateColumns: "minmax(0, 1.4fr) minmax(min-content, 0.8fr)",
    gap: "16px",
    "@container (max-width: 720px)": {
      gridTemplateColumns: "1fr",
    },
    "@media (max-width: 860px)": {
      gridTemplateColumns: "1fr",
    },
  },
  alertList: {
    display: "grid",
    gap: "8px",
  },
  alertRow: {
    display: "grid",
    gridTemplateColumns: "110px minmax(0, 1fr)",
    gap: "10px",
    alignItems: "start",
    minHeight: "46px",
    padding: "8px 0",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    transition: "none",
    "@media (max-width: 640px)": {
      gridTemplateColumns: "1fr",
      minHeight: "58px",
    },
  },
  chartGrid: {
    display: "grid",
    containerType: "inline-size",
    gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 16rem), 1fr))",
    gap: "12px",
  },
  chartCard: {
    minWidth: 0,
    minHeight: "124px",
    display: "grid",
    gap: "8px",
    borderRadius: tokens.borderRadiusMedium,
    border: "1px solid #243041",
    backgroundColor: "#101a28",
    padding: "10px",
  },
  dpiInsightGrid: {
    display: "grid",
    containerType: "inline-size",
    gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 17rem), 1fr))",
    gap: "12px",
  },
  rankList: {
    display: "grid",
    gap: "8px",
  },
  rankRow: {
    display: "grid",
    gap: "4px",
    minWidth: 0,
    minHeight: "36px",
    transition: "none",
  },
  rankLine: {
    display: "grid",
    gridTemplateColumns: "minmax(0, 1fr) max-content",
    gap: "8px",
    alignItems: "center",
  },
  barTrack: {
    height: "6px",
    borderRadius: "999px",
    backgroundColor: tokens.colorNeutralBackground5,
    overflow: "hidden",
  },
  barFill: {
    height: "100%",
    borderRadius: "999px",
    backgroundColor: tokens.colorBrandBackground,
  },
  classificationStack: {
    display: "grid",
    gap: "6px",
  },
  classificationMeter: {
    display: "flex",
    height: "8px",
    borderRadius: "999px",
    overflow: "hidden",
    backgroundColor: tokens.colorNeutralBackground5,
  },
  classificationSegmentDPI: {
    backgroundColor: "#54b054",
  },
  classificationSegmentGuess: {
    backgroundColor: "#9aa4b2",
  },
  classificationSegmentUnknown: {
    backgroundColor: "#6b7280",
  },
  guessText: {
    color: tokens.colorNeutralForeground3,
    fontStyle: "italic",
  },
  identifyingText: {
    color: tokens.colorNeutralForeground3,
    fontStyle: "italic",
  },
  chartSvg: {
    width: "100%",
    height: "86px",
    display: "block",
  },
  resourceFilters: {
    display: "grid",
    gridTemplateColumns: "minmax(min(100%, 14rem), 1fr) max-content",
    gap: "8px",
    alignItems: "end",
    marginBottom: "12px",
    "@media (max-width: 640px)": {
      gridTemplateColumns: "1fr",
    },
  },
  routeFilters: {
    display: "grid",
    gridTemplateColumns: "minmax(min(100%, 16rem), 1.4fr) repeat(2, minmax(min-content, 12rem))",
    gap: "8px",
    alignItems: "end",
    marginBottom: "12px",
    "@media (max-width: 860px)": {
      gridTemplateColumns: "1fr",
    },
  },
  singleSearchRow: {
    display: "grid",
    gridTemplateColumns: "minmax(min(100%, 14rem), 26rem)",
    gap: "8px",
    marginBottom: "12px",
    "@media (max-width: 640px)": {
      gridTemplateColumns: "1fr",
    },
  },
  clientFilters: {
    display: "grid",
    gridTemplateColumns: "minmax(min(100%, 14rem), 1.4fr) minmax(min-content, 12rem)",
    gap: "8px",
    alignItems: "end",
    marginBottom: "12px",
    "@media (max-width: 640px)": {
      gridTemplateColumns: "1fr",
    },
  },
  highlight: {
    backgroundColor: "#6b4b00",
    color: "#fff7d6",
    borderRadius: "2px",
    padding: "0 2px",
  },
  eventsGrid: {
    display: "grid",
    gridTemplateColumns: "minmax(0, 1.25fr) minmax(min-content, 0.75fr)",
    gap: "16px",
    alignItems: "start",
    "@media (max-width: 860px)": {
      gridTemplateColumns: "1fr",
    },
  },
  eventFilters: {
    display: "grid",
    gridTemplateColumns: "minmax(min(100%, 14rem), 1.4fr) repeat(4, minmax(min-content, 1fr))",
    gap: "8px",
    alignItems: "end",
    marginBottom: "12px",
    "@media (max-width: 860px)": {
      gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 9.5rem), 1fr))",
    },
  },
  firewallStack: {
    display: "grid",
    gap: "16px",
  },
  resourceDesktopTable: {
    "@media (max-width: 860px)": {
      display: "none",
    },
  },
  resourceMobileList: {
    display: "none",
    "@media (max-width: 860px)": {
      display: "grid",
      gap: "10px",
    },
  },
  resourceMobileCard: {
    display: "grid",
    containerType: "inline-size",
    gap: "6px",
    padding: "10px 12px",
    border: "1px solid #243041",
    borderRadius: "6px",
    backgroundColor: "#101a28",
  },
  resourceMobileHeader: {
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: "8px",
  },
  resourceMobileMeta: {
    display: "grid",
    gap: "4px",
  },
  routeMobileCard: {
    display: "grid",
    gap: "8px",
    padding: "10px 12px",
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: tokens.borderRadiusMedium,
    backgroundColor: tokens.colorNeutralBackground2,
  },
  routeMobileHeader: {
    display: "grid",
    gridTemplateColumns: "minmax(0, 1fr) max-content",
    gap: "8px",
    alignItems: "start",
  },
  routeMobileDetails: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 9rem), 1fr))",
    gap: "6px",
  },
  routePeerSection: {
    marginTop: "12px",
  },
  clientsGrid: {
    display: "grid",
    containerType: "inline-size",
    gap: "16px",
    gridTemplateColumns: "1fr",
  },
  clientSections: {
    display: "grid",
    gap: "12px",
  },
  clientSection: {
    display: "grid",
    gap: "8px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    paddingTop: "10px",
  },
  clientSectionHeader: {
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    justifyContent: "space-between",
    gap: "8px",
  },
  clientSectionToggle: {
    appearance: "none",
    border: 0,
    padding: 0,
    margin: 0,
    background: "transparent",
    color: "inherit",
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    gap: "8px",
    textAlign: "left",
    cursor: "pointer",
    minWidth: 0,
    "@media (min-width: 861px)": {
      cursor: "default",
    },
  },
  clientSectionTitle: {
    display: "flex",
    alignItems: "center",
    gap: "6px",
    minWidth: 0,
    fontWeight: tokens.fontWeightSemibold,
  },
  clientDesktopOnly: {
    "@media (max-width: 860px)": {
      display: "none",
    },
  },
  clientMobileOnly: {
    display: "none",
    "@media (max-width: 860px)": {
      display: "flex",
    },
  },
  clientDeviceList: {
    display: "grid",
    containerType: "inline-size",
    gap: "8px",
    "@media (max-width: 860px)": {
      gap: "6px",
    },
  },
  clientDeviceRow: {
    display: "grid",
    containerType: "inline-size",
    gridTemplateColumns: "2rem minmax(12rem, 1.1fr) minmax(11rem, 1fr) minmax(9rem, 0.7fr) minmax(10rem, 0.8fr) max-content",
    gap: "10px",
    alignItems: "start",
    padding: "10px",
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: tokens.borderRadiusMedium,
    backgroundColor: tokens.colorNeutralBackground2,
    minHeight: "74px",
    transition: "none",
    "@media (max-width: 860px)": {
      gridTemplateColumns: "minmax(0, 1fr)",
      gap: 0,
      padding: 0,
      overflow: "hidden",
      minHeight: "68px",
      transition: "none",
    },
  },
  clientDeviceRowOffline: {
    opacity: 0.68,
  },
  clientDeviceDetails: {
    gridColumn: "2 / -1",
    display: "grid",
    gap: "8px",
    paddingTop: "8px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    "@media (max-width: 860px)": {
      gridColumn: "1 / -1",
      padding: "8px",
    },
  },
  clientMobileSummary: {
    appearance: "none",
    border: 0,
    margin: 0,
    padding: "8px 10px",
    width: "100%",
    background: "transparent",
    color: "inherit",
    display: "none",
    textAlign: "left",
    cursor: "pointer",
    "@media (max-width: 860px)": {
      display: "grid",
      gap: "5px",
      height: "68px",
      minHeight: "68px",
      transition: "none",
    },
  },
  clientMobileMainLine: {
    display: "grid",
    gridTemplateColumns: "24px minmax(0, 1fr) auto",
    gap: "8px",
    alignItems: "center",
    minWidth: 0,
  },
  clientMobileIcon: {
    width: "24px",
    height: "24px",
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    color: tokens.colorNeutralForeground2,
  },
  clientMobileName: {
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
    minWidth: 0,
  },
  clientMobileStatus: {
    display: "inline-flex",
    alignItems: "center",
    gap: "4px",
    color: tokens.colorNeutralForeground3,
    whiteSpace: "nowrap",
    transition: "none",
  },
  clientOnlineDot: {
    width: "8px",
    height: "8px",
    borderRadius: "999px",
    backgroundColor: tokens.colorPaletteGreenForeground1,
    flex: "0 0 auto",
    transition: "none",
  },
  clientOfflineDot: {
    width: "8px",
    height: "8px",
    borderRadius: "999px",
    backgroundColor: tokens.colorNeutralForegroundDisabled,
    flex: "0 0 auto",
    transition: "none",
  },
  clientMobileSubLine: {
    display: "flex",
    alignItems: "center",
    gap: "6px",
    minWidth: 0,
    paddingLeft: "32px",
    color: tokens.colorNeutralForeground3,
    whiteSpace: "nowrap",
  },
  clientMobileIP: {
    flex: "0 0 auto",
    minWidth: "15ch",
    maxWidth: "18ch",
    overflow: "hidden",
    textOverflow: "ellipsis",
    fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace",
    color: tokens.colorNeutralForeground2,
  },
  clientMobileMeta: {
    minWidth: 0,
    overflow: "hidden",
    textOverflow: "ellipsis",
  },
  vpnGrid: {
    display: "grid",
    containerType: "inline-size",
    gap: "16px",
    gridTemplateColumns: "1fr",
  },
  vpnSummaryGrid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 13rem), 1fr))",
    gap: "10px",
    marginBottom: "12px",
  },
  interfaceGrid: {
    display: "grid",
    containerType: "inline-size",
    gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 14rem), 1fr))",
    gap: "10px",
  },
  interfaceCard: {
    display: "grid",
    gap: "8px",
    padding: "10px",
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: tokens.borderRadiusMedium,
    backgroundColor: tokens.colorNeutralBackground2,
    minWidth: 0,
    minHeight: "148px",
    transition: "none",
  },
  interfaceHeader: {
    display: "flex",
    gap: "8px",
    alignItems: "flex-start",
    justifyContent: "space-between",
    minWidth: 0,
  },
  interfaceName: {
    minWidth: 0,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  interfaceLine: {
    display: "flex",
    flexWrap: "wrap",
    gap: "6px",
    alignItems: "flex-start",
  },
  addressList: {
    display: "grid",
    gap: "3px",
  },
  tableWrap: {
    overflowX: "auto",
    containerType: "inline-size",
    overscrollBehaviorX: "contain",
    maxWidth: "100%",
    WebkitOverflowScrolling: "touch",
    "@media (max-width: 640px)": {
      border: `1px solid ${tokens.colorNeutralStroke2}`,
      borderRadius: tokens.borderRadiusMedium,
    },
  },
  dataTable: {
    width: "max-content",
    minWidth: "min(100%, 45rem)",
    tableLayout: "auto",
  },
  resourceTable: {
    width: "max-content",
    minWidth: "min(100%, 56rem)",
    tableLayout: "auto",
    "@media (max-width: 640px)": {
      minWidth: "100%",
    },
  },
  routeTable: {
    width: "max-content",
    minWidth: "min(100%, 74rem)",
    tableLayout: "auto",
  },
  controllerTable: {
    width: "max-content",
    minWidth: "min(100%, 74rem)",
    tableLayout: "auto",
  },
  eventTable: {
    width: "max-content",
    minWidth: "min(100%, 48rem)",
    tableLayout: "auto",
    "@media (max-width: 640px)": {
      minWidth: "100%",
    },
  },
  connectionTable: {
    width: "max-content",
    minWidth: "min(100%, 68rem)",
    tableLayout: "auto",
    "@media (max-width: 640px)": {
      minWidth: "100%",
    },
  },
  clientInventoryTable: {
    width: "max-content",
    minWidth: "min(100%, 62rem)",
    tableLayout: "auto",
    "@media (max-width: 640px)": {
      minWidth: "100%",
    },
  },
  clientTrafficTable: {
    width: "max-content",
    minWidth: "min(100%, 48rem)",
    tableLayout: "auto",
  },
  dhcpLeaseTable: {
    width: "max-content",
    minWidth: "min(100%, 56rem)",
    tableLayout: "auto",
    "@media (max-width: 640px)": {
      minWidth: "100%",
    },
  },
  vpnPeerTable: {
    width: "max-content",
    minWidth: "min(100%, 56rem)",
    tableLayout: "auto",
  },
  code: {
    fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace",
    whiteSpace: "nowrap",
    wordBreak: "normal",
    overflowWrap: "normal",
    "@media (max-width: 640px)": {
      whiteSpace: "normal",
      overflowWrap: "anywhere",
      wordBreak: "break-word",
    },
  },
  wrapCode: {
    fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace",
    whiteSpace: "normal",
    overflowWrap: "anywhere",
    wordBreak: "break-word",
  },
  muted: {
    color: tokens.colorNeutralForeground3,
  },
  consoleLegal: {
    color: tokens.colorNeutralForeground3,
    padding: "8px 2px 0",
    textAlign: "center",
  },
  navTextHeader: {
    display: "flex",
    alignItems: "center",
    gap: "6px",
  },
  consoleLegalLink: {
    color: tokens.colorBrandForegroundLink,
    textDecoration: "none",
    "&:hover": {
      textDecoration: "underline",
    },
  },
  chips: {
    display: "flex",
    flexWrap: "wrap",
    gap: "4px",
  },
  badges: {
    display: "flex",
    flexWrap: "wrap",
    gap: "4px",
    alignItems: "center",
  },
  toolbar: {
    display: "flex",
    alignItems: "center",
    gap: "8px",
  },
  connectionFilters: {
    display: "grid",
    gridTemplateColumns: "minmax(min(100%, 14rem), 1.4fr) repeat(7, minmax(min-content, 1fr))",
    gap: "8px",
    alignItems: "end",
    marginBottom: "12px",
    "@media (max-width: 860px)": {
      gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 9.5rem), 1fr))",
    },
  },
  firewallFilters: {
    display: "grid",
    gridTemplateColumns: "minmax(min(100%, 14rem), 1.4fr) repeat(4, minmax(min-content, 1fr))",
    gap: "8px",
    alignItems: "end",
    marginTop: "12px",
    marginBottom: "8px",
    "@media (max-width: 860px)": {
      gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 9.5rem), 1fr))",
    },
  },
  filterControl: {
    display: "grid",
    gap: "4px",
    minWidth: 0,
  },
  filterInput: {
    minWidth: 0,
  },
  filterClearButton: {
    minWidth: "24px",
    width: "24px",
    height: "24px",
  },
  connectionGroup: {
    display: "grid",
    gap: "8px",
  },
  connectionAnchor: {
    scrollMarginTop: "96px",
  },
  connectionHeader: {
    display: "flex",
    flexWrap: "wrap",
    gap: "8px",
    alignItems: "center",
    justifyContent: "space-between",
    marginBottom: "8px",
  },
  connectionSummaryGrid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 11rem), 1fr))",
    gap: "10px",
    marginBottom: "12px",
  },
  connectionSummaryCard: {
    minWidth: 0,
    minHeight: "104px",
    display: "grid",
    gap: "8px",
    padding: "10px",
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: tokens.borderRadiusMedium,
    backgroundColor: tokens.colorNeutralBackground2,
  },
  connectionFlow: {
    display: "grid",
    gap: "2px",
    minWidth: 0,
  },
  connectionPeerIdentity: {
    color: tokens.colorNeutralForeground3,
    display: "block",
    maxWidth: "min(100%, 54rem)",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  connectionInlineIdentity: {
    color: tokens.colorNeutralForeground3,
    display: "inline-block",
    maxWidth: "min(100%, 22rem)",
    overflow: "hidden",
    textOverflow: "ellipsis",
    verticalAlign: "bottom",
    whiteSpace: "nowrap",
  },
  connectionDetailIdentity: {
    color: tokens.colorNeutralForeground3,
    display: "block",
    maxWidth: "100%",
    whiteSpace: "normal",
    overflowWrap: "anywhere",
    wordBreak: "break-word",
  },
  clientDetailStack: {
    display: "grid",
    gap: "7px",
    minWidth: 0,
  },
  clientAddressGroup: {
    display: "grid",
    gap: "3px",
    minWidth: 0,
  },
  clientAddressList: {
    display: "flex",
    flexWrap: "wrap",
    gap: "4px 8px",
    minWidth: 0,
    maxWidth: "100%",
    overscrollBehaviorX: "contain",
    "@media (max-width: 640px)": {
      display: "grid",
      gridTemplateColumns: "1fr",
      gap: "5px",
    },
  },
  clientAddressCode: {
    display: "block",
    minWidth: 0,
    maxWidth: "100%",
    fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace",
    whiteSpace: "normal",
    overflowWrap: "anywhere",
    wordBreak: "break-word",
    lineHeight: tokens.lineHeightBase300,
  },
  clientPrimaryIPCell: {
    display: "grid",
    gap: "2px",
    minWidth: "15ch",
    maxWidth: "100%",
  },
  clientPrimaryIPCode: {
    display: "block",
    minWidth: "15ch",
    maxWidth: "100%",
    fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace",
    whiteSpace: "nowrap",
    overflow: "hidden",
    textOverflow: "ellipsis",
    lineHeight: tokens.lineHeightBase300,
  },
  clientMetaLine: {
    display: "flex",
    flexWrap: "wrap",
    gap: "6px 10px",
    alignItems: "center",
    minWidth: 0,
  },
  firewallTable: {
    display: "grid",
    gap: "6px",
    maxWidth: "100%",
    overflowX: "auto",
    overscrollBehaviorX: "contain",
    paddingBottom: "4px",
    WebkitOverflowScrolling: "touch",
  },
  firewallChartWrap: {
    display: "grid",
    gap: "8px",
  },
  firewallTopN: {
    display: "grid",
    gap: "8px",
  },
  firewallTopRow: {
    display: "grid",
    gridTemplateColumns: "4rem minmax(0, 1fr) max-content",
    gap: "10px",
    alignItems: "center",
    minHeight: "56px",
    padding: "8px 10px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    transition: "none",
    "@media (max-width: 640px)": {
      gridTemplateColumns: "3rem minmax(0, 1fr)",
    },
  },
  firewallBar: {
    height: "8px",
    borderRadius: "999px",
    backgroundColor: "#d13438",
    minWidth: "3px",
  },
  firewallRankHeader: {
    display: "grid",
    gridTemplateColumns: "3.5rem minmax(13rem, 1.2fr) minmax(13rem, 1.2fr) max-content minmax(5rem, max-content) minmax(12rem, 0.9fr) minmax(8rem, 0.7fr) minmax(9rem, 0.8fr)",
    gap: "10px",
    width: "max-content",
    minWidth: "100%",
    padding: "0 10px 6px",
    color: tokens.colorNeutralForeground3,
    fontSize: "12px",
    fontWeight: 600,
    "@media (max-width: 860px)": {
      display: "none",
    },
  },
  firewallTimelineHeader: {
    display: "grid",
    gridTemplateColumns: "6rem max-content minmax(13rem, 1.3fr) minmax(13rem, 1.3fr) max-content minmax(5rem, max-content) minmax(12rem, 0.9fr) minmax(8rem, 0.75fr) minmax(10rem, 0.9fr) minmax(7rem, 0.7fr)",
    gap: "10px",
    width: "max-content",
    minWidth: "100%",
    padding: "0 10px 6px",
    color: tokens.colorNeutralForeground3,
    fontSize: "12px",
    fontWeight: 600,
    "@media (max-width: 860px)": {
      display: "none",
    },
  },
  firewallRankRow: {
    display: "grid",
    gridTemplateColumns: "3.5rem minmax(13rem, 1.2fr) minmax(13rem, 1.2fr) max-content minmax(5rem, max-content) minmax(12rem, 0.9fr) minmax(8rem, 0.7fr) minmax(9rem, 0.8fr)",
    gap: "10px",
    width: "max-content",
    minWidth: "100%",
    alignItems: "start",
    minHeight: "54px",
    padding: "8px 10px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    transition: "none",
    "@media (max-width: 860px)": {
      gridTemplateColumns: "1fr",
      gap: "8px",
      minHeight: "128px",
      padding: "10px",
      border: `1px solid ${tokens.colorNeutralStroke2}`,
      borderRadius: tokens.borderRadiusMedium,
      backgroundColor: tokens.colorNeutralBackground2,
    },
  },
  firewallTimelineRow: {
    display: "grid",
    gridTemplateColumns: "6rem max-content minmax(13rem, 1.3fr) minmax(13rem, 1.3fr) max-content minmax(5rem, max-content) minmax(12rem, 0.9fr) minmax(8rem, 0.75fr) minmax(10rem, 0.9fr) minmax(7rem, 0.7fr)",
    gap: "10px",
    width: "max-content",
    minWidth: "100%",
    alignItems: "start",
    minHeight: "54px",
    padding: "8px 10px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    transition: "none",
    "@media (max-width: 860px)": {
      gridTemplateColumns: "1fr",
      gap: "8px",
      minHeight: "148px",
      padding: "10px",
      border: `1px solid ${tokens.colorNeutralStroke2}`,
      borderRadius: tokens.borderRadiusMedium,
      backgroundColor: tokens.colorNeutralBackground2,
    },
  },
  firewallCell: {
    minWidth: 0,
    overflow: "hidden",
    "@media (max-width: 860px)": {
      display: "grid",
      gridTemplateColumns: "92px minmax(0, 1fr)",
      gap: "8px",
      alignItems: "start",
    },
  },
  firewallCellLabel: {
    display: "none",
    color: tokens.colorNeutralForeground3,
    fontSize: "12px",
    "@media (max-width: 860px)": {
      display: "block",
    },
  },
  firewallCellValue: {
    minWidth: 0,
  },
  tuningGrid: {
    display: "grid",
    containerType: "inline-size",
    gap: "8px",
  },
  tuningHeader: {
    display: "grid",
    gridTemplateColumns: "minmax(8rem, 0.9fr) max-content max-content max-content max-content minmax(12rem, 1fr)",
    gap: "10px",
    color: tokens.colorNeutralForeground3,
    fontSize: "12px",
    fontWeight: 600,
    padding: "0 10px 6px",
    "@container (max-width: 760px)": {
      display: "none",
    },
    "@media (max-width: 860px)": {
      display: "none",
    },
  },
  tuningRow: {
    display: "grid",
    gridTemplateColumns: "minmax(8rem, 0.9fr) max-content max-content max-content max-content minmax(12rem, 1fr)",
    gap: "10px",
    alignItems: "start",
    minHeight: "54px",
    padding: "8px 10px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    transition: "none",
    "@container (max-width: 760px)": {
      gridTemplateColumns: "1fr",
      gap: "8px",
      minHeight: "148px",
      padding: "10px",
      border: `1px solid ${tokens.colorNeutralStroke2}`,
      borderRadius: tokens.borderRadiusMedium,
      backgroundColor: tokens.colorNeutralBackground2,
    },
    "@media (max-width: 860px)": {
      gridTemplateColumns: "1fr",
      gap: "8px",
      minHeight: "148px",
      padding: "10px",
      border: `1px solid ${tokens.colorNeutralStroke2}`,
      borderRadius: tokens.borderRadiusMedium,
      backgroundColor: tokens.colorNeutralBackground2,
    },
  },
  pager: {
    display: "flex",
    flexWrap: "wrap",
    gap: "8px",
    alignItems: "center",
    justifyContent: "flex-end",
  },
  pageSize: {
    width: "5.5rem",
  },
  scrollTopButton: {
    position: "fixed",
    right: "18px",
    bottom: "18px",
    zIndex: 30,
    boxShadow: "0 8px 24px rgba(0,0,0,0.35)",
    "@media (max-width: 640px)": {
      right: "12px",
      bottom: "12px",
    },
  },
  detailPanel: {
    position: "sticky",
    top: "78px",
    display: "grid",
    gap: "12px",
    "@media (max-width: 860px)": {
      position: "static",
    },
  },
  detailList: {
    display: "grid",
    gridTemplateColumns: "max-content minmax(0, 1fr)",
    columnGap: "10px",
    rowGap: "8px",
    alignItems: "start",
    "@media (max-width: 640px)": {
      gridTemplateColumns: "1fr",
      rowGap: "4px",
    },
  },
  detailKey: {
    color: tokens.colorNeutralForeground3,
    fontSize: "12px",
  },
  eventRowSelected: {
    backgroundColor: tokens.colorNeutralBackground2Selected,
  },
  stableTableRow: {
    height: "44px",
    minHeight: "44px",
    transition: "none",
    "@media (max-width: 640px)": {
      height: "52px",
      minHeight: "52px",
    },
  },
  stableTallTableRow: {
    height: "64px",
    minHeight: "64px",
    transition: "none",
    "@media (max-width: 640px)": {
      height: "72px",
      minHeight: "72px",
    },
  },
  config: {
    maxHeight: "66vh",
    overflow: "auto",
    overscrollBehavior: "contain",
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: tokens.borderRadiusMedium,
    padding: "10px",
    backgroundColor: tokens.colorNeutralBackground2,
  },
  configToolbar: {
    display: "flex",
    flexWrap: "wrap",
    gap: "8px",
    alignItems: "center",
    justifyContent: "space-between",
    marginBottom: "10px",
  },
  configModeButtons: {
    display: "flex",
    gap: "6px",
  },
  configError: {
    marginBottom: "10px",
    padding: "8px",
    border: `1px solid ${tokens.colorPaletteRedBorder2}`,
    borderRadius: tokens.borderRadiusMedium,
    backgroundColor: tokens.colorPaletteRedBackground2,
  },
  generationTable: {
    width: "max-content",
    minWidth: "min(100%, 60rem)",
    tableLayout: "auto",
  },
  generationActions: {
    display: "flex",
    flexWrap: "wrap",
    gap: "8px",
    alignItems: "center",
    marginBottom: "12px",
  },
  generationRowActions: {
    display: "flex",
    flexWrap: "wrap",
    gap: "6px",
    alignItems: "center",
  },
  generationSelect: {
    width: "11rem",
  },
  diffPanel: {
    position: "relative",
    maxHeight: "62vh",
    overflow: "auto",
    overscrollBehavior: "contain",
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: tokens.borderRadiusMedium,
    backgroundColor: tokens.colorNeutralBackground2,
    padding: "10px 18px 10px 10px",
  },
  diffLine: {
    display: "block",
    fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace",
    fontSize: "12px",
    lineHeight: 1.45,
    whiteSpace: "pre",
  },
  diffAdded: {
    color: "#8ee68e",
    backgroundColor: "rgba(37, 113, 37, 0.24)",
  },
  diffRemoved: {
    color: "#ffb3ad",
    backgroundColor: "rgba(150, 48, 44, 0.28)",
  },
  diffRuler: {
    position: "sticky",
    top: 0,
    float: "right",
    width: "6px",
    height: "62vh",
    maxHeight: "100%",
    marginRight: "-12px",
    marginLeft: "6px",
    borderRadius: "999px",
    backgroundColor: "rgba(255,255,255,0.08)",
    pointerEvents: "none",
  },
  diffRulerMark: {
    position: "absolute",
    left: 0,
    width: "100%",
    minHeight: "3px",
    borderRadius: "999px",
  },
  diffRulerAdded: {
    backgroundColor: "#6ccb5f",
  },
  diffRulerRemoved: {
    backgroundColor: "#d13438",
  },
  tree: {
    display: "grid",
    gap: "2px",
    fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace",
    fontSize: "12px",
    lineHeight: 1.45,
  },
  treeNode: {
    minWidth: 0,
  },
  treeSummary: {
    cursor: "pointer",
    minWidth: 0,
    padding: "2px 0",
  },
  treeRow: {
    display: "inline-flex",
    gap: "8px",
    alignItems: "baseline",
    minWidth: 0,
    maxWidth: "100%",
  },
  treeKey: {
    color: tokens.colorNeutralForeground1,
    overflowWrap: "anywhere",
  },
  treeMeta: {
    color: tokens.colorNeutralForeground3,
    whiteSpace: "nowrap",
  },
  treeChildren: {
    display: "grid",
    gap: "2px",
    marginLeft: "18px",
    paddingLeft: "10px",
    borderLeft: `1px solid ${tokens.colorNeutralStroke2}`,
  },
  treeLeaf: {
    display: "grid",
    gridTemplateColumns: "minmax(8rem, 0.42fr) minmax(0, 1fr)",
    gap: "10px",
    minWidth: 0,
    padding: "2px 0",
    "@media (max-width: 640px)": {
      gridTemplateColumns: "1fr",
      gap: "2px",
    },
  },
  treeValue: {
    minWidth: 0,
    overflowWrap: "anywhere",
    wordBreak: "break-word",
    color: tokens.colorNeutralForeground2,
  },
  pre: {
    margin: 0,
    fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace",
    fontSize: "12px",
    lineHeight: 1.45,
    whiteSpace: "pre",
  },
});

function App() {
  const styles = useStyles();
  const initialLocation = parseLocationHash();
  const [summary, setSummary] = useState<Summary | null>(null);
  const [config, setConfig] = useState<ConfigSnapshot | null>(null);
  const [routesStatus, setRoutesStatus] = useState<RoutesStatus | null>(null);
  const [generations, setGenerations] = useState<GenerationRecord[]>([]);
  const [firewallDenyTimeline, setFirewallDenyTimeline] = useState<FirewallDenyTimelineBucket[]>([]);
  const [generationDiff, setGenerationDiff] = useState<string>("");
  const [configPlanDiff, setConfigPlanDiff] = useState<string>("");
  const [generationConfig, setGenerationConfig] = useState<{ generation: number; text: string } | null>(null);
  const [generationFrom, setGenerationFrom] = useState<string>("");
  const [generationTo, setGenerationTo] = useState<string>("");
  const [error, setError] = useState<string>("");
  const [clientQuery, setClientQuery] = useState("");
  const [clientActivityFilter, setClientActivityFilter] = useState("all");
  const [generationQuery, setGenerationQuery] = useState("");
  const [selected, setSelected] = useState<ViewKey>(initialLocation.view);
  const [selectedTargetID, setSelectedTargetID] = useState<string | undefined>(initialLocation.targetID);
  const [navCollapsed, setNavCollapsed] = useState(() => {
    try {
      return window.localStorage?.getItem("routerd:nav:collapsed") === "1";
    } catch {
      return false;
    }
  });
  const isMobileNav = useMediaQuery("(max-width: 860px)");
  const mobileNavInitialized = useRef(false);
  useEffect(() => {
    try {
      window.localStorage?.setItem("routerd:nav:collapsed", navCollapsed ? "1" : "0");
    } catch {}
  }, [navCollapsed]);
  useEffect(() => {
    if (!isMobileNav) {
      mobileNavInitialized.current = false;
      return;
    }
    if (!mobileNavInitialized.current) {
      mobileNavInitialized.current = true;
      setNavCollapsed(true);
    }
  }, [isMobileNav]);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(() => readStoredRecord(collapsedStorageKey));
  const [connectionPages, setConnectionPages] = useState<Record<string, number>>(() => readStoredRecord(connectionPagesStorageKey));
  const [connectionPageSizes, setConnectionPageSizes] = useState<Record<string, number>>(() => readStoredRecord(connectionPageSizesStorageKey));
  const [connectionSortRevision, setConnectionSortRevision] = useState(0);
  const [connectionFilters, setConnectionFilters] = useState<ConnectionFilters>({
    query: "",
    client: "",
    family: "all",
    protocol: "all",
    app: "all",
    source: "all",
    state: "all",
    sort: "traffic",
    direction: "desc",
  });
  const [firewallFilters, setFirewallFilters] = useState<FirewallFilters>({
    query: "",
    source: "",
    destination: "",
    port: "",
    protocol: "all",
  });
  const [eventFilters, setEventFilters] = useState<EventFilters>({
    query: "",
    severity: "all",
    resourceKind: "all",
    range: "24h",
    customHours: "24",
  });
  const [selectedEventKey, setSelectedEventKey] = useState<string>("");
  const [metricSamples, setMetricSamples] = useState<MetricSample[]>([]);
  const [loading, setLoading] = useState(true);
  const [streamState, setStreamState] = useState<"connecting" | "live" | "polling">("connecting");
  const [lastStreamEvent, setLastStreamEvent] = useState<StreamEvent | null>(null);
  const refreshInFlight = useRef(false);
  const queuedRefresh = useRef(false);
  const refreshTimer = useRef<number | null>(null);
  const configRef = useRef<ConfigSnapshot | null>(null);
  const pendingScrollSnapshot = useRef<ScrollSnapshot | null>(null);
  const connectionOrderRef = useRef<{ signature: string; keys: string[] }>({ signature: "", keys: [] });
  const connectionGroupOrderRef = useRef<{ signature: string; keys: string[] }>({ signature: "", keys: [] });
  const detailRefreshMounted = useRef(false);
  const overviewDetailsLoaded = useRef(false);

  async function refresh() {
    if (refreshInFlight.current) {
      queuedRefresh.current = true;
      return;
    }
    refreshInFlight.current = true;
    const scrollSnapshot = captureScrollSnapshot();
    const eventLimit = selected === "events" ? 200 : 50;
    const connectionLimit = selected === "connections" ? 600 : -1;
    const firewallLogLimit = selected === "firewall" ? 200 : -1;
    const generationLimit = selected === "generations" ? 200 : 50;
    const shouldFetchConfig = selected === "config";
    const shouldFetchGenerations = selected === "config" || selected === "generations";
    const shouldFetchRoutes = selected === "routes";
    const includeClients = selected === "clients" || selected === "connections";
    const includeTuning = selected === "firewall";
    const includeVPN = selected === "vpn";
    const includeDPI = selected === "connections" || selected === "clients" || selected === "firewall";
    const trafficFlowLimit = selected === "clients" ? 200 : selected === "connections" ? 600 : -1;
    const includeResources = selected === "resources";
    const includeEvents = selected === "events";
    const includeDHCPLeases = selected === "clients" || selected === "connections";
    const summaryQuery = new URLSearchParams({
      events: includeEvents ? String(eventLimit) : "-1",
      connections: String(connectionLimit),
      firewallLogs: String(firewallLogLimit),
      dnsQueries: includeClients ? "200" : "-1",
      trafficFlows: String(trafficFlowLimit),
      fingerprintQueries: includeClients ? "1000" : "50",
      dhcpFingerprints: includeClients ? "1000" : "200",
      clients: includeClients ? "1" : "0",
      dpi: includeDPI ? "1" : "0",
      tuning: includeTuning ? "1" : "0",
      vpn: includeVPN ? "1" : "0",
      resources: includeResources ? "1" : "0",
      dhcpLeases: includeDHCPLeases ? "1" : "0",
    });
    try {
      const [summaryResponse, configResponse, generationResponse, denyTimelineResponse, routesResponse] = await Promise.all([
        fetchJSON<Summary>(`api/v1/summary?${summaryQuery.toString()}`),
        shouldFetchConfig ? (configRef.current ? Promise.resolve(configRef.current) : fetchJSON<ConfigSnapshot>("api/v1/config")) : Promise.resolve(null),
        shouldFetchGenerations ? fetchJSON<GenerationRecord[]>(`api/v1/generations?limit=${generationLimit}`) : Promise.resolve(null),
        selected === "firewall" ? fetchJSON<FirewallDenyTimelineBucket[]>("api/v1/firewall/deny-timeline?range=24h&bucket=5min") : Promise.resolve(null),
        shouldFetchRoutes ? fetchJSON<RoutesStatus>("api/v1/routes") : Promise.resolve(null),
      ]);
      pendingScrollSnapshot.current = scrollSnapshot;
      setSummary(current => reconcileSummary(current, summaryResponse));
      if (selected === "overview" && !overviewDetailsLoaded.current) {
        overviewDetailsLoaded.current = true;
        window.setTimeout(loadOverviewDetails, 250);
      }
      if (Array.isArray(denyTimelineResponse)) {
        setFirewallDenyTimeline(denyTimelineResponse);
      }
      if (summaryResponse.resources !== undefined) {
        setMetricSamples(current => appendMetricSample(current, summaryResponse));
      }
      if (configResponse && !configRef.current) {
        configRef.current = configResponse as ConfigSnapshot;
        setConfig(configResponse as ConfigSnapshot);
      }
      if (Array.isArray(generationResponse)) {
        setGenerations(current => reconcileRecords(current, generationResponse, row => String(row.generation)));
      }
      if (routesResponse) {
        setRoutesStatus(routesResponse as RoutesStatus);
      }
      setError("");
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
      refreshInFlight.current = false;
      if (queuedRefresh.current) {
        queuedRefresh.current = false;
        scheduleRefresh(150);
      }
    }
  }

  async function loadOverviewDetails() {
    const query = new URLSearchParams({
      events: "50",
      connections: "200",
      firewallLogs: "-1",
      dnsQueries: "200",
      trafficFlows: "200",
      clients: "0",
      dpi: "0",
      tuning: "0",
      vpn: "0",
      resources: "1",
      dhcpLeases: "0",
    });
    try {
      const response = await fetchJSON<Summary>(`api/v1/summary?${query.toString()}`);
      setSummary(current => reconcileSummary(current, response));
      if (response.resources !== undefined) {
        setMetricSamples(current => appendMetricSample(current, response));
      }
      setError("");
    } catch (err) {
      overviewDetailsLoaded.current = false;
      setError(String(err));
    }
  }

  function scheduleRefresh(delay = 350) {
    if (refreshTimer.current !== null) {
      window.clearTimeout(refreshTimer.current);
    }
    refreshTimer.current = window.setTimeout(() => {
      refreshTimer.current = null;
      refresh();
    }, delay);
  }

  useEffect(() => {
    const onScroll = () => {
      const now = performance.now();
      if (now < programmaticScrollUntil) return;
      lastUserWindowScrollAt = now;
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  useEffect(() => installHorizontalScrollTouchCoordinator(), []);

  useEffect(() => {
    refresh();
    const pollID = window.setInterval(refresh, 30000);
    let source: EventSource | null = null;
    if ("EventSource" in window) {
      source = new EventSource(basePath + "api/v1/events/stream");
      source.addEventListener("connected", () => {
        setStreamState("live");
      });
      source.addEventListener("routerd-event", event => {
        try {
          const parsed = JSON.parse((event as MessageEvent).data) as StreamEvent;
          setLastStreamEvent(parsed);
        } catch {
          setLastStreamEvent(null);
        }
        setStreamState("live");
        scheduleRefresh(250);
      });
      source.onerror = () => {
        setStreamState("polling");
      };
    } else {
      setStreamState("polling");
    }
    return () => {
      window.clearInterval(pollID);
      if (refreshTimer.current !== null) window.clearTimeout(refreshTimer.current);
      source?.close();
    };
  }, []);

  useEffect(() => {
    if (!detailRefreshMounted.current) {
      detailRefreshMounted.current = true;
      return;
    }
    scheduleRefresh(0);
  }, [selected]);

  useLayoutEffect(() => {
    const snapshot = pendingScrollSnapshot.current;
    if (!snapshot) return;
    pendingScrollSnapshot.current = null;
    restoreScrollSnapshot(snapshot);
    restoreScrollAfterRender(snapshot);
  });

  useEffect(() => {
    const withYaml = generations.filter(row => row.hasYaml);
    if (!generationTo && withYaml[0]) setGenerationTo(String(withYaml[0].generation));
    if (!generationFrom && withYaml[1]) setGenerationFrom(String(withYaml[1].generation));
    if (!generationFrom && withYaml.length === 1) setGenerationFrom(String(withYaml[0].generation));
  }, [generations, generationFrom, generationTo]);

  const connections = summary?.connections?.entries ?? [];
  const dnsLabels = useMemo(() => dnsLabelMap(summary?.dnsQueries ?? []), [summary?.dnsQueries]);
  const leaseMap = useMemo(() => dhcpLeaseMap(summary?.dhcpLeases ?? []), [summary?.dhcpLeases]);
  const clientIdentities = useMemo(() => clientIdentityMap(summary?.clients ?? []), [summary?.clients]);
  const connectionCandidates = useMemo(
    () => filterConnections(connections, dnsLabels, clientIdentities, connectionFilters),
    [connections, dnsLabels, clientIdentities, connectionFilters],
  );
  const connectionSortSignature = [
    connectionFilters.query,
    connectionFilters.client,
    connectionFilters.family,
    connectionFilters.protocol,
    connectionFilters.app,
    connectionFilters.source,
    connectionFilters.state,
    connectionFilters.sort,
    connectionFilters.direction,
    connectionSortRevision,
  ].join("\u0001");
  const filteredConnections = useMemo(() => {
    if (connectionOrderRef.current.signature !== connectionSortSignature) {
      connectionOrderRef.current = {
        signature: connectionSortSignature,
        keys: sortConnectionEntries(connectionCandidates, dnsLabels, connectionFilters).map(connectionStableKey),
      };
    }
    return applyFrozenConnectionOrder(connectionCandidates, connectionOrderRef.current.keys);
  }, [connectionCandidates, connectionFilters, connectionSortSignature, dnsLabels]);
  const connectionGroupsList = useMemo(() => {
    const groups = connectionGroups(filteredConnections);
    if (connectionGroupOrderRef.current.signature !== connectionSortSignature) {
      connectionGroupOrderRef.current = { signature: connectionSortSignature, keys: groups.map(group => group.key) };
    }
    return applyFrozenGroupOrder(groups, connectionGroupOrderRef.current.keys);
  }, [filteredConnections, connectionSortSignature]);
  const connectionFacets = useMemo(() => connectionFilterFacets(connections), [connections]);
  const navSubItems = useMemo(() => navigationSubItems(selected, connectionGroupsList, summary), [selected, connectionGroupsList, summary]);
  const resources = useMemo(() => importantResources(summary?.resources ?? []), [summary?.resources]);
  const controllers = summary?.controllers ?? (summary?.status?.status?.controllers as ControllerStatus[] | undefined) ?? [];
  const dryRunControllers = useMemo(() => controllers.filter(controller => controller.mode === "dry-run"), [controllers]);
  const events = summary?.events ?? [];
  const filteredEvents = useMemo(() => filterEvents(events, eventFilters), [events, eventFilters]);
  const eventFacets = useMemo(() => eventFilterFacets(events), [events]);
  const firewallLogs = summary?.firewallLogs ?? [];
  const filteredFirewallLogs = useMemo(() => filterFirewallLogs(firewallLogs, firewallFilters, dnsLabels), [firewallLogs, firewallFilters, dnsLabels]);
  const firewallProtocols = useMemo(() => firewallProtocolFacets(firewallLogs), [firewallLogs]);
  const selectedEvent = useMemo(() => {
    if (filteredEvents.length === 0) return undefined;
    return filteredEvents.find(event => eventKey(event) === selectedEventKey) ?? filteredEvents[0];
  }, [filteredEvents, selectedEventKey]);
  const clientActivityOptions = useMemo(() => clientActivityFacets(summary?.clients ?? []), [summary?.clients]);
  const filteredClients = useMemo(
    () => filterClients(summary?.clients ?? [], clientQuery, clientActivityFilter),
    [summary?.clients, clientQuery, clientActivityFilter],
  );
  const filteredGenerations = useMemo(() => filterGenerations(generations, generationQuery), [generations, generationQuery]);

  useEffect(() => {
    if (filteredEvents.length > 0 && !filteredEvents.some(event => eventKey(event) === selectedEventKey)) {
      setSelectedEventKey(eventKey(filteredEvents[0]));
    }
  }, [filteredEvents, selectedEventKey]);

  useEffect(() => {
    setConnectionPages({});
  }, [connectionFilters]);

  useEffect(() => {
    writeStoredRecord(collapsedStorageKey, collapsed);
  }, [collapsed]);

  useEffect(() => {
    writeStoredRecord(connectionPagesStorageKey, connectionPages);
  }, [connectionPages]);

  useEffect(() => {
    writeStoredRecord(connectionPageSizesStorageKey, connectionPageSizes);
  }, [connectionPageSizes]);

  useEffect(() => {
    const onHashChange = () => {
      const next = parseLocationHash();
      setSelected(next.view);
      setSelectedTargetID(next.targetID);
      const targetID = next.targetID;
      if (targetID) {
        window.setTimeout(() => scrollToElement(targetID), 80);
      }
    };
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  function updateConnectionFilter<K extends keyof ConnectionFilters>(key: K, value: ConnectionFilters[K]) {
    setConnectionFilters(current => ({ ...current, [key]: value }));
  }

  function scrollToTop() {
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  function showConnectionsGroup(key: string) {
    showSection({ key, label: key, view: "connections", targetID: connectionGroupID(key) });
    setCollapsed(current => ({ ...current, [key]: false }));
  }

  function showClientConnections(row: ClientRow) {
    const addresses = clientConnectionAddresses(row);
    if (addresses.length === 0) return;
    setConnectionFilters(current => ({ ...current, query: "", client: addresses.join(",") }));
    navigateTo("connections");
  }

  function showAddressConnections(address: string) {
    const normalized = normalizeAddressKey(address);
    if (!normalized) return;
    setConnectionFilters(current => ({ ...current, query: "", client: normalized }));
    navigateTo("connections");
  }

  function showClientForAddress(address?: string) {
    const normalized = normalizeAddressKey(address);
    if (!normalized) return;
    setClientQuery(normalized);
    setClientActivityFilter("all");
    navigateTo("clients", "clients-inventory");
  }

  function showSection(item: NavSubItem) {
    navigateTo(item.view, item.targetID);
  }

  function sectionActive(item: NavSubItem) {
    return item.view === selected && selectedTargetID === item.targetID;
  }

  function navigateTo(view: ViewKey, targetID?: string) {
    setSelected(view);
    setSelectedTargetID(targetID);
    if (isMobileNav) setNavCollapsed(true);
    const nextHash = hashForView(view, targetID);
    if (window.location.hash !== nextHash) {
      window.history.pushState(null, "", nextHash);
    }
    window.setTimeout(() => {
      if (targetID) {
        scrollToElement(targetID);
      } else {
        scrollToTop();
      }
    }, 80);
  }

  async function loadGenerationConfig(generation: number) {
    try {
      const text = await fetchText(`api/v1/generations/${generation}/config`);
      setGenerationConfig({ generation, text });
      setGenerationDiff("");
      setError("");
      scrollToGenerationResult();
    } catch (err) {
      setError(String(err));
    }
  }

  async function loadGenerationDiff() {
    const from = Number(generationFrom);
    const to = Number(generationTo);
    if (!from || !to) return;
    try {
      const text = await fetchText(`api/v1/generations/${from}/diff/${to}`);
      setGenerationDiff(text);
      setGenerationConfig(null);
      setError("");
      scrollToGenerationResult();
    } catch (err) {
      setError(String(err));
    }
  }

  async function loadAdjacentGenerationDiff(from: number, to: number) {
    try {
      const text = await fetchText(`api/v1/generations/${from}/diff/${to}`);
      setGenerationFrom(String(from));
      setGenerationTo(String(to));
      setGenerationDiff(text);
      setGenerationConfig(null);
      setError("");
      scrollToGenerationResult();
    } catch (err) {
      setError(String(err));
    }
  }

  async function loadConfigPlanDiff() {
    const latest = generations.find(row => row.hasYaml);
    if (!latest || !config?.text) return;
    try {
      const previous = await fetchText(`api/v1/generations/${latest.generation}/config`);
      setConfigPlanDiff(unifiedLineDiff(`generation-${latest.generation}.yaml`, "current-file.yaml", previous, config.text));
      setError("");
    } catch (err) {
      setError(String(err));
    }
  }

  const selectedNav = navItems.find(item => item.key === selected) ?? navItems[0];
  const activeClientTargetID = clientSectionID(selectedTargetID);
  useEffect(() => {
    const base = cfg.title || "routerd";
    document.title = selectedNav.label ? `${selectedNav.label} - ${base}` : base;
  }, [selectedNav.label, cfg.title]);
  const renderNavigation = (compact: boolean) => (
    <div className={styles.navSection}>
      {navItems.map(item => (
        <React.Fragment key={item.key}>
          <Button
            appearance="subtle"
            className={`${styles.navButton} ${compact ? "" : navCollapsed ? styles.navButtonCollapsed : ""} ${selected === item.key ? styles.navButtonActive : ""}`}
            onClick={() => navigateTo(item.key)}
            aria-label={item.label}
          >
            <span className={styles.navButtonInner}>
              <span className={styles.navIcon}>{item.icon}</span>
              <span className={`${styles.navText} ${!compact && navCollapsed ? styles.navTextCollapsed : ""}`}>
                <span className={styles.navTextHeader}>
                  <Text weight={selected === item.key ? "semibold" : "regular"}>{item.label}</Text>
                  {item.key === "resources" && dryRunControllers.length > 0 ? <Badge size="small" appearance="tint" color="warning">{dryRunControllers.length}</Badge> : null}
                  {item.key === "resources" && resources.filter(r => ["danger","warning"].includes(phaseColor(r.status?.phase))).length > 0 ? <Badge size="small" appearance="tint" color="danger">{resources.filter(r => ["danger","warning"].includes(phaseColor(r.status?.phase))).length}</Badge> : null}
                </span>
                <Text size={200} className={styles.navDescription}>{item.description}</Text>
              </span>
            </span>
          </Button>
          {!compact && !navCollapsed && item.key === selected && navSubItems.length > 0 && selected !== "connections" ? (
            <div className={styles.navSubMenu}>
              {navSubItems.map(sub => (
                <Button
                  key={sub.key}
                  size="small"
                  appearance="subtle"
                  className={`${styles.navSubButton} ${sectionActive(sub) ? styles.navSubButtonActive : ""}`}
                  onClick={() => showSection(sub)}
                >
                  <span>{sub.label}</span>
                  {sub.count !== undefined ? <span>{sub.count}</span> : null}
                </Button>
              ))}
            </div>
          ) : null}
        </React.Fragment>
      ))}
    </div>
  );

  return (
    <FluentProvider theme={webDarkTheme} className={styles.shell}>
      <header className={styles.header}>
        <div className={styles.productArea}>
          <Button
            className={styles.navToggle}
            appearance="subtle"
            icon={<NavigationRegular />}
            aria-label={navCollapsed ? "Open navigation" : "Close navigation"}
            onClick={() => setNavCollapsed(value => !value)}
          />
          <div className={styles.productTitleBlock}>
            <Text size={500} weight="semibold" className={styles.productTitleText}>{cfg.title || "routerd"}</Text>
            <Text size={200} className={styles.subtitle}>Local router control plane</Text>
          </div>
        </div>
        <div className={styles.toolbar}>
          {loading ? <Spinner size="tiny" /> : null}
        </div>
      </header>
      {isMobileNav ? (
        <OverlayDrawer
          className={styles.mobileDrawer}
          modalType="modal"
          open={!navCollapsed}
          position="start"
          onOpenChange={(_, data) => setNavCollapsed(!data.open)}
        >
          <DrawerHeader>
            <DrawerHeaderTitle action={<Button appearance="subtle" aria-label="Close navigation" icon={<ChevronRightRegular />} onClick={() => setNavCollapsed(true)} />}>
              Navigation
            </DrawerHeaderTitle>
          </DrawerHeader>
          <DrawerBody className={styles.mobileDrawerBody}>
            {renderNavigation(true)}
          </DrawerBody>
        </OverlayDrawer>
      ) : null}
      <div className={`${styles.layout} ${navCollapsed ? styles.layoutCollapsed : ""}`}>
        <aside className={`${styles.sidebar} ${navCollapsed ? styles.sidebarCollapsed : ""}`} aria-label="Web console navigation">
          {renderNavigation(false)}
        </aside>
        <section className={styles.content}>
          <div className={styles.bladeHeader}>
            <div className={styles.bladeTitleBlock}>
              <div className={styles.bladeTitleLine}>
                <span className={styles.bladeIcon}>{selectedNav.icon}</span>
                <Text size={700} weight="semibold" className={styles.title}>{selectedNav.label}</Text>
              </div>
              <Text className={styles.bladeSubtitle}>{selectedNav.description}</Text>
            </div>
            <div className={styles.bladeActions}>
              <Badge appearance="tint" color={phaseColor(summary?.status?.status?.phase)}>{String(summary?.status?.status?.phase ?? "Unknown")}</Badge>
              <Badge appearance="outline" color={streamState === "live" ? "success" : streamState === "polling" ? "warning" : "subtle"} className={styles.streamBadge}>
                {streamState === "live" ? "Live updates" : streamState === "polling" ? "Polling fallback" : "Connecting"}
              </Badge>
              <Text size={200} className={styles.muted}>{summary?.generatedAt ? <>Updated <RelativeTime value={summary.generatedAt} /></> : ""}</Text>
              {lastStreamEvent?.type ? <Text size={200} className={styles.muted}>{lastStreamEvent.type}</Text> : null}
              <Button appearance="primary" icon={<ArrowClockwiseRegular />} onClick={refresh}>Refresh</Button>
            </div>
          </div>
          {navSubItems.length > 0 && selected !== "connections" ? (
            <div className={styles.sectionBar} aria-label={`${selectedNav.label} sections`}>
              {navSubItems.map(sub => (
                <Button
                  key={sub.key}
                  size="small"
                  appearance={sectionActive(sub) ? "primary" : "secondary"}
                  className={styles.sectionButton}
                  onClick={() => showSection(sub)}
                >
                  {`${sub.label}${sub.count !== undefined ? ` ${sub.count}` : ""}`}
                </Button>
              ))}
            </div>
          ) : null}
          <main className={styles.main}>
            {error ? <Card><Text role="alert">Web console error: {error}</Text></Card> : null}
            {selected === "overview" ? (
              <>
                {dryRunControllers.length > 0 ? (
                  <div className={styles.dryRunBanner}>
                    <div className={styles.badges}>
                      <Badge appearance="tint" color="warning">dry-run</Badge>
                      <Text weight="semibold">{dryRunControllers.length} controllers are running in dry-run mode</Text>
                      <Text className={styles.muted}>{dryRunControllers.map(controller => controller.name).join(", ")}</Text>
                    </div>
                    <Button appearance="secondary" onClick={() => navigateTo("controllers")}>View controllers</Button>
                  </div>
                ) : null}
                <div id="overview-metrics" className={styles.connectionAnchor}>
                  <div className={styles.grid}>
                    <Metric label="phase" value={String(summary?.status?.status?.phase ?? "Unknown")} />
                    <Metric label="generation" value={String(summary?.status?.status?.generation ?? "-")} />
                    <Metric label="resources" value={String(summary?.status?.status?.resourceCount ?? resources.length)} />
                    <Metric label="conntrack" value={conntrackLabel(summary?.connections)} />
                    <Metric label="families" value={connectionFamilyCounts(summary?.connections)} />
                    <Metric label="CPU" value={systemCPULabel(summary?.systemUsage)} />
                    <Metric label="memory" value={systemMemoryLabel(summary?.systemUsage)} />
                    <Metric label="disk" value={systemDiskLabel(summary?.systemUsage)} />
                    <Metric label="DPI latency" value={dpiLatencyLabel(summary?.dpi)} />
                  </div>
                </div>
                <MetricCharts samples={metricSamples} />
                <OverviewDPIInsights
                  flows={summary?.trafficFlows}
                  clients={summary?.clients}
                  connections={summary?.connections}
                  dpi={summary?.dpi}
                />
                <Card id="overview-interfaces" className={styles.connectionAnchor}>
                  <CardHeader header={<Text weight="semibold">Interfaces</Text>} description={<Text className={styles.muted}>Role, link state, MTU, and assigned addresses</Text>} />
                  <InterfaceOverview interfaces={summary?.interfaces ?? []} />
                </Card>
                <OverviewActivity resources={summary?.resources ?? []} events={events} navigateTo={navigateTo} />
              </>
            ) : null}
            {selected === "resources" ? (
              <Card id="resources-table" className={styles.connectionAnchor}>
                <CardHeader
                  header={<Text weight="semibold">Resources</Text>}
                  description={<Text className={styles.muted}>Resource phase, controller mode, and status detail</Text>}
                />
                <div className={styles.grid}>
                  <Metric label="total" value={String(resources.length)} />
                  <Metric label="healthy" value={String(resources.filter(r => phaseColor(r.status?.phase) === "success").length)} />
                  <Metric label="warning" value={String(resources.filter(r => phaseColor(r.status?.phase) === "warning").length)} />
                  <Metric label="danger" value={String(resources.filter(r => phaseColor(r.status?.phase) === "danger").length)} />
                  <Metric label="dry-run kinds" value={String(controllers.filter(c => c.mode === "dry-run").length)} />
                </div>
                <ResourceTable resources={resources} controllers={controllers} navigateTo={navigateTo} />
              </Card>
            ) : null}
            {selected === "routes" ? (
              <RoutesView status={routesStatus} />
            ) : null}
            {selected === "controllers" ? (
              <Card id="controllers-table" className={styles.connectionAnchor}>
                <CardHeader
                  header={<Text weight="semibold">Controllers</Text>}
                  description={<Text className={styles.muted}>Controller mode, reason, and resource ownership surface</Text>}
                />
                <div className={styles.grid}>
                  <Metric label="total" value={String(controllers.length)} />
                  <Metric label="live" value={String(controllers.filter(c => c.mode === "live").length)} />
                  <Metric label="dry-run" value={String(controllers.filter(c => c.mode === "dry-run").length)} />
                  <Metric label="errors" value={String(controllers.reduce((sum, c) => sum + Number(c.reconcileErrorCount ?? 0), 0))} />
                  <Metric label="slowest" value={slowestControllerLabel(controllers)} />
                </div>
                <ControllerTable controllers={controllers} />
              </Card>
            ) : null}
            {selected === "clients" ? (
              <div className={styles.clientsGrid}>
                {activeClientTargetID === "clients-inventory" ? (
                  <Card id="clients-inventory" className={styles.connectionAnchor}>
                    <CardHeader
                      header={<Text weight="semibold">Client inventory</Text>}
                      description={<Text className={styles.muted}>DHCP leases, neighbors, and observed traffic grouped by client. Showing {filteredClients.length} of {summary?.clients?.length ?? 0}</Text>}
                    />
                    <div className={styles.clientFilters}>
                      <SearchControl label="Search clients" value={clientQuery} placeholder="name, MAC, address, vendor, peer" onChange={setClientQuery} />
                      <div className={styles.filterControl}>
                        <Text size={200} className={styles.muted}>Activity</Text>
                        <Select size="small" value={clientActivityFilter} onChange={event => setClientActivityFilter(event.target.value)}>
                          <option value="all">All activity</option>
                          {clientActivityOptions.map(activity => <option key={activity} value={activity}>{formatClientActivity(activity)}</option>)}
                        </Select>
                      </div>
                    </div>
                    <ClientInventory clients={filteredClients} onShowConnections={showClientConnections} />
                  </Card>
                ) : null}
                {activeClientTargetID === "clients-traffic" ? (
                  <Card id="clients-traffic" className={styles.connectionAnchor}>
                    <CardHeader header={<Text weight="semibold">Client traffic</Text>} description={<Text className={styles.muted}>Traffic grouped by client address</Text>} />
                    <ClientTraffic flows={summary?.trafficFlows ?? []} onShowConnectionsForAddress={showAddressConnections} />
                  </Card>
                ) : null}
                {activeClientTargetID === "clients-leases" ? (
                  <Card id="clients-leases" className={styles.connectionAnchor}>
                    <CardHeader header={<Text weight="semibold">DHCP leases</Text>} description={<Text className={styles.muted}>dnsmasq lease file entries</Text>} />
                    <DHCPLeaseTable leases={summary?.dhcpLeases ?? []} />
                  </Card>
                ) : null}
              </div>
            ) : null}
            {selected === "connections" ? (
              <Card>
            <CardHeader
              header={<Text weight="semibold">Connections</Text>}
              description={<Text className={styles.muted}>conntrack flows grouped by family and protocol</Text>}
            />
            <div className={styles.grid}>
              <Metric label="IPv4" value={String(connectionFamilyCount(summary?.connections, "ipv4"))} />
              <Metric label="IPv6" value={String(connectionFamilyCount(summary?.connections, "ipv6"))} />
              <Metric label="Showing" value={connectionShowingValue(summary?.connections, filteredConnections.length)} />
              <Metric label="Groups" value={String(connectionGroupsList.length)} />
            </div>
            <div className={styles.connectionJumpBar}>
              <Button size="small" appearance="secondary" icon={<ArrowUpRegular />} onClick={scrollToTop}>Top</Button>
              {connectionGroupsList.length > 0 ? (
                <Select
                  size="small"
                  value=""
                  aria-label="Jump to connection group"
                  onChange={event => {
                    const key = event.target.value;
                    if (key) showConnectionsGroup(key);
                  }}
                >
                  <option value="">Jump to group...</option>
                  {connectionGroupsList.map(group => {
                    const label = connectionGroupLabel(group.key);
                    return (
                      <option key={group.key} value={group.key}>
                        {formatConnectionGroupTitle(label)} ({group.rows.length})
                      </option>
                    );
                  })}
                </Select>
              ) : null}
            </div>
            <ConnectionClassificationSummary entries={filteredConnections} />
            {connectionFilters.client ? (
              <div className={styles.activeFilterBanner}>
                <div className={styles.connectionFlow}>
                  <Text weight="semibold">Client connections</Text>
                  <Text size={200} className={styles.muted}>{connectionClientFilterLabel(connectionFilters.client, clientIdentities)}</Text>
                </div>
                <div className={styles.badges}>
                  <Button size="small" appearance="secondary" icon={<PeopleRegular />} onClick={() => showClientForAddress(splitConnectionClientFilter(connectionFilters.client)[0])}>Client</Button>
                  <Button size="small" appearance="subtle" onClick={() => updateConnectionFilter("client", "")}>Clear</Button>
                </div>
              </div>
            ) : null}
            <div className={styles.connectionFilters}>
              <SearchControl label="Filter" value={connectionFilters.query} placeholder="address, port, state, label" onChange={value => updateConnectionFilter("query", value)} />
              <div className={styles.filterControl}>
                <Text size={200} className={styles.muted}>Family</Text>
                <Select size="small" value={connectionFilters.family} onChange={event => updateConnectionFilter("family", event.target.value)}>
                  <option value="all">All</option>
                  {connectionFacets.families.map(value => <option key={value} value={value}>{formatFacet(value)}</option>)}
                </Select>
              </div>
              <div className={styles.filterControl}>
                <Text size={200} className={styles.muted}>Protocol</Text>
                <Select size="small" value={connectionFilters.protocol} onChange={event => updateConnectionFilter("protocol", event.target.value)}>
                  <option value="all">All</option>
                  {connectionFacets.protocols.map(value => <option key={value} value={value}>{formatFacet(value)}</option>)}
                </Select>
              </div>
              <div className={styles.filterControl}>
                <Text size={200} className={styles.muted}>App</Text>
                <Select size="small" value={connectionFilters.app} onChange={event => updateConnectionFilter("app", event.target.value)}>
                  <option value="all">All</option>
                  {connectionFacets.apps.map(value => <option key={value} value={value}>{formatConnectionApp(value)}</option>)}
                </Select>
              </div>
              <div className={styles.filterControl}>
                <Text size={200} className={styles.muted}>Source</Text>
                <Select size="small" value={connectionFilters.source} onChange={event => updateConnectionFilter("source", event.target.value)}>
                  <option value="all">All</option>
                  {connectionFacets.sources.map(value => <option key={value} value={value}>{formatTrafficSourceLabel(value)}</option>)}
                </Select>
              </div>
              <div className={styles.filterControl}>
                <Text size={200} className={styles.muted}>State</Text>
                <Select size="small" value={connectionFilters.state} onChange={event => updateConnectionFilter("state", event.target.value)}>
                  <option value="all">All</option>
                  {connectionFacets.states.map(value => <option key={value} value={value}>{formatFacet(value)}</option>)}
                </Select>
              </div>
              <div className={styles.filterControl}>
                <Text size={200} className={styles.muted}>Sort</Text>
                <Select size="small" value={connectionFilters.sort} onChange={event => updateConnectionFilter("sort", event.target.value)}>
                  <option value="stable">Stable key</option>
                  <option value="observed">Observed order</option>
                  <option value="traffic">Traffic</option>
                  <option value="state">State</option>
                  <option value="source">Source</option>
                  <option value="destination">Destination</option>
                  <option value="label">Label</option>
                  <option value="app">App</option>
                  <option value="timeout">Timeout</option>
                </Select>
              </div>
              <div className={styles.filterControl}>
                <Text size={200} className={styles.muted}>Order</Text>
                <Select size="small" value={connectionFilters.direction} onChange={event => updateConnectionFilter("direction", event.target.value)}>
                  <option value="asc">Ascending</option>
                  <option value="desc">Descending</option>
                </Select>
              </div>
              <div className={styles.filterControl}>
                <Text size={200} className={styles.muted}>Apply</Text>
                <Button size="small" appearance="secondary" icon={<ArrowClockwiseRegular />} onClick={() => setConnectionSortRevision(value => value + 1)}>
                  Re-sort
                </Button>
              </div>
            </div>
            <div className={styles.connectionGroup}>
              {connectionGroupsList.map(group => (
                <ConnectionGroup
                  key={group.key}
                  group={group}
                  dnsLabels={dnsLabels}
                  clientIdentities={clientIdentities}
                  collapsed={collapsed[group.key] ?? false}
                  toggle={() => setCollapsed(current => ({ ...current, [group.key]: !(current[group.key] ?? false) }))}
                  page={connectionPages[group.key] ?? 0}
                  pageSize={connectionPageSizes[group.key] ?? defaultConnectionPageSize}
                  setPage={page => setConnectionPages(current => ({ ...current, [group.key]: page }))}
                  setPageSize={size => {
                    setConnectionPageSizes(current => ({ ...current, [group.key]: size }));
                    setConnectionPages(current => ({ ...current, [group.key]: 0 }));
                  }}
                  onShowClient={showClientForAddress}
                />
              ))}
            </div>
            <Button className={styles.scrollTopButton} appearance="primary" icon={<ArrowUpRegular />} onClick={scrollToTop}>Top</Button>
              </Card>
            ) : null}
            {selected === "vpn" ? (
              <div className={styles.vpnGrid}>
                <Card id="vpn-tailscale" className={styles.connectionAnchor}>
                  <CardHeader
                    header={<Text weight="semibold">Tailscale</Text>}
                    description={<Text className={styles.muted}>Local node, advertised routes, and peers from tailscale status</Text>}
                  />
                  <TailscalePanel status={summary?.vpn?.tailscale} errors={summary?.vpn?.errors ?? []} />
                </Card>
                <Card id="vpn-wireguard" className={styles.connectionAnchor}>
                  <CardHeader
                    header={<Text weight="semibold">WireGuard</Text>}
                    description={<Text className={styles.muted}>Kernel WireGuard interfaces and peers from wg show</Text>}
                  />
                  <WireGuardPanel interfaces={summary?.vpn?.wireGuard ?? []} errors={summary?.vpn?.errors ?? []} />
                </Card>
              </div>
            ) : null}
            {selected === "events" ? (
              <div className={styles.eventsGrid}>
                <Card id="events-list" className={styles.connectionAnchor}>
                  <CardHeader header={<Text weight="semibold">Events</Text>} description={<Text className={styles.muted}>Bus events from routerd controllers and daemons</Text>} />
                  <div className={styles.grid}>
                    <Metric label="total" value={String(events.length)} />
                    <Metric label="filter matched" value={String(filteredEvents.length)} />
                    <Metric label="severities" value={String(eventFacets.severities.length)} />
                    <Metric label="kinds" value={String(eventFacets.kinds.length)} />
                  </div>
                  <div className={styles.eventFilters}>
                    <SearchControl label="Search" value={eventFilters.query} placeholder="topic, reason, message, resource" onChange={value => setEventFilters(current => ({ ...current, query: value }))} />
                    <div className={styles.filterControl}>
                      <Text size={200} className={styles.muted}>Severity</Text>
                      <Select size="small" value={eventFilters.severity} onChange={event => setEventFilters(current => ({ ...current, severity: event.target.value }))}>
                        <option value="all">All</option>
                        {eventFacets.severities.map(value => <option key={value} value={value}>{value}</option>)}
                      </Select>
                    </div>
                    <div className={styles.filterControl}>
                      <Text size={200} className={styles.muted}>Kind</Text>
                      <Select size="small" value={eventFilters.resourceKind} onChange={event => setEventFilters(current => ({ ...current, resourceKind: event.target.value }))}>
                        <option value="all">All</option>
                        {eventFacets.kinds.map(value => <option key={value} value={value}>{value}</option>)}
                      </Select>
                    </div>
                    <div className={styles.filterControl}>
                      <Text size={200} className={styles.muted}>Range</Text>
                      <Select size="small" value={eventFilters.range} onChange={event => setEventFilters(current => ({ ...current, range: event.target.value }))}>
                        <option value="1h">Last 1h</option>
                        <option value="6h">Last 6h</option>
                        <option value="24h">Last 24h</option>
                        <option value="custom">Custom</option>
                      </Select>
                    </div>
                    {eventFilters.range === "custom" ? (
                      <div className={styles.filterControl}>
                        <Text size={200} className={styles.muted}>Hours</Text>
                        <Input className={styles.filterInput} size="small" value={eventFilters.customHours} onChange={(_, data) => setEventFilters(current => ({ ...current, customHours: data.value }))} />
                      </div>
                    ) : null}
                  </div>
                  <EventTable events={filteredEvents} selectedKey={eventKey(selectedEvent)} onSelect={event => setSelectedEventKey(eventKey(event))} query={eventFilters.query} />
                </Card>
                <EventDetail event={selectedEvent} id="events-detail" />
              </div>
            ) : null}
            {selected === "firewall" ? (
              <div className={styles.firewallStack}>
                <Card>
                  <CardHeader
                    header={<Text weight="semibold">Deny activity</Text>}
                    description={<Text className={styles.muted}>Drop/reject rate over the last 24 hours. Filters below narrow the ranking and timeline.</Text>}
                  />
                  <div className={styles.grid}>
                    <Metric label="total denies (24h)" value={String(denyTimelineTotal(firewallDenyTimeline))} />
                    <Metric label="peak / bucket" value={String(denyTimelinePeak(firewallDenyTimeline))} />
                    <Metric label="unique sources" value={String(new Set(firewallLogs.map(l => l.srcAddress).filter(Boolean)).size)} />
                    <Metric label="DPI identified" value={`${firewallLogs.filter(l => l.dpiApp).length} / ${firewallLogs.length}`} />
                    <Metric label="orphan returns" value={String(firewallLogs.filter(l => l.correlation === "orphan_return").length)} />
                    <Metric label="filter matched" value={`${filteredFirewallLogs.length} / ${firewallLogs.length}`} />
                  </div>
                  <DenyRateChart timeline={firewallDenyTimeline} />
                  <div className={styles.firewallFilters}>
                    <SearchControl label="Search" value={firewallFilters.query} placeholder="rule, interface, address, protocol, DPI, orphan" onChange={value => setFirewallFilters(current => ({ ...current, query: value }))} />
                    <div className={styles.filterControl}>
                      <Text size={200} className={styles.muted}>Source</Text>
                      <Input className={styles.filterInput} size="small" value={firewallFilters.source} placeholder="source IP" onChange={(_, data) => setFirewallFilters(current => ({ ...current, source: data.value }))} />
                    </div>
                    <div className={styles.filterControl}>
                      <Text size={200} className={styles.muted}>Destination</Text>
                      <Input className={styles.filterInput} size="small" value={firewallFilters.destination} placeholder="destination IP" onChange={(_, data) => setFirewallFilters(current => ({ ...current, destination: data.value }))} />
                    </div>
                    <div className={styles.filterControl}>
                      <Text size={200} className={styles.muted}>Port</Text>
                      <Input className={styles.filterInput} size="small" value={firewallFilters.port} placeholder="dst/src port" onChange={(_, data) => setFirewallFilters(current => ({ ...current, port: data.value }))} />
                    </div>
                    <div className={styles.filterControl}>
                      <Text size={200} className={styles.muted}>Protocol</Text>
                      <Select size="small" value={firewallFilters.protocol} onChange={event => setFirewallFilters(current => ({ ...current, protocol: event.target.value }))}>
                        <option value="all">All</option>
                        {firewallProtocols.map(value => <option key={value} value={value}>{value.toUpperCase()}</option>)}
                      </Select>
                    </div>
                  </div>
                  <Text size={200} className={styles.muted}>Showing {filteredFirewallLogs.length} of {firewallLogs.length} deny log rows</Text>
                </Card>
                <Card>
                  <CardHeader header={<Text weight="semibold">Source IP top-N</Text>} description={<Text className={styles.muted}>Top denied sources in the filtered view</Text>} />
                  <FirewallSourceTopN logs={filteredFirewallLogs} dnsLabels={dnsLabels} leases={leaseMap} onSourceClick={ip => setFirewallFilters(current => ({ ...current, source: ip }))} />
                </Card>
                <Card id="firewall-tuning" className={styles.connectionAnchor}>
                  <CardHeader
                    header={<Text weight="semibold">Tuning Suggestions</Text>}
                    description={<Text className={styles.muted}>Read-only conntrack timeout recommendations from DPI flows and expired-return observations</Text>}
                  />
                  <ConntrackTuningView tuning={summary?.conntrackTuning} />
                </Card>
                <Card id="firewall-ranking" className={styles.connectionAnchor}>
                  <CardHeader header={<Text weight="semibold">Deny ranking</Text>} description={<Text className={styles.muted}>Grouped by source, destination, and protocol</Text>} />
                  <RecentDeny logs={filteredFirewallLogs} dnsLabels={dnsLabels} leases={leaseMap} />
                </Card>
                <Card id="firewall-timeline" className={styles.connectionAnchor}>
                  <CardHeader header={<Text weight="semibold">Deny timeline</Text>} description={<Text className={styles.muted}>Newest firewall log rows</Text>} />
                  <FirewallTimeline logs={filteredFirewallLogs} dnsLabels={dnsLabels} leases={leaseMap} />
                </Card>
              </div>
            ) : null}
            {selected === "config" ? (
              <Card id="config-view" className={styles.connectionAnchor}>
                <CardHeader header={<Text weight="semibold">Config</Text>} description={<Text className={styles.muted}>{config?.path ?? ""}</Text>} />
                <ConfigView config={config} latestGeneration={generations.find(row => row.hasYaml)} planDiff={configPlanDiff} loadPlanDiff={loadConfigPlanDiff} />
              </Card>
            ) : null}
            {selected === "generations" ? (
              <GenerationsView
                generations={filteredGenerations}
                totalGenerations={generations.length}
                query={generationQuery}
                setQuery={setGenerationQuery}
                from={generationFrom}
                to={generationTo}
                setFrom={setGenerationFrom}
                setTo={setGenerationTo}
                diff={generationDiff}
                config={generationConfig}
                loadDiff={loadGenerationDiff}
                loadAdjacentDiff={loadAdjacentGenerationDiff}
                loadConfig={loadGenerationConfig}
              />
            ) : null}
            <Text size={200} className={styles.consoleLegal}>
              Powered by{" "}
              <a href="https://github.com/imksoo/routerd" target="_blank" rel="noopener noreferrer" className={styles.consoleLegalLink}>routerd</a>.
            </Text>
          </main>
        </section>
      </div>
    </FluentProvider>
  );
}

function ConfigView({
  config,
  latestGeneration,
  planDiff,
  loadPlanDiff,
}: {
  config: ConfigSnapshot | null;
  latestGeneration?: GenerationRecord;
  planDiff: string;
  loadPlanDiff: () => void;
}) {
  const styles = useStyles();
  const [mode, setMode] = useState<"tree" | "raw">("tree");
  const [query, setQuery] = useState(() => {
    try {
      const stored = window.localStorage?.getItem("routerd:config:initialQuery") ?? "";
      if (stored) window.localStorage.removeItem("routerd:config:initialQuery");
      return stored;
    } catch {
      return "";
    }
  });
  const [copied, setCopied] = useState(false);
  const parsed = useMemo(() => parseConfig(config?.text), [config?.text]);
  const kindTargets = useMemo(() => configKindTargets(parsed.value), [parsed.value]);
  async function copyRawYAML() {
    if (!config?.text || !navigator.clipboard) return;
    await navigator.clipboard.writeText(config.text);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1600);
  }
  return (
    <>
      <div className={styles.configToolbar}>
        <Text className={styles.muted}>Read-only view of the active routerd YAML. Edit the source file and run routerd apply from the CLI.</Text>
        <div className={styles.configModeButtons}>
          <Button size="small" appearance="secondary" disabled={!latestGeneration || !config?.text} onClick={loadPlanDiff}>
            Diff before apply
          </Button>
          <Button size="small" appearance={mode === "tree" ? "primary" : "secondary"} onClick={() => setMode("tree")}>Tree</Button>
          <Button size="small" appearance={mode === "raw" ? "primary" : "secondary"} onClick={() => setMode("raw")}>Raw YAML</Button>
        </div>
      </div>
      <div className={styles.configToolbar}>
        <SearchControl label="Search config" value={query} placeholder="kind, name, field, value" onChange={setQuery} />
        <div className={styles.configModeButtons}>
          {kindTargets.slice(0, 8).map(target => (
            <Button key={target.id} size="small" appearance="secondary" onClick={() => scrollToElement(target.id)}>{target.label}</Button>
          ))}
          {mode === "raw" ? <Button size="small" appearance="secondary" disabled={!config?.text} onClick={copyRawYAML}>{copied ? "Copied" : "Copy YAML"}</Button> : null}
        </div>
      </div>
      {parsed.errors.length > 0 ? (
        <div className={styles.configError}>
          <Text weight="semibold">YAML parse warning</Text>
          <div className={styles.tree}>
            {parsed.errors.map((error, index) => <code className={styles.wrapCode} key={index}>{error}</code>)}
          </div>
        </div>
      ) : null}
      <div className={styles.config}>
        {mode === "tree" && parsed.value !== undefined ? (
          <div className={styles.tree}>
            <ConfigTreeNode label="config" value={parsed.value} depth={0} query={query} />
          </div>
        ) : (
          <pre className={styles.pre}>{highlightYAML(config?.text ?? "Config is unavailable", query).map((part, index) => part.match ? <mark className={styles.highlight} key={index}>{part.text}</mark> : <span key={index}>{part.text}</span>)}</pre>
        )}
      </div>
      {planDiff ? (
        <div style={{ marginTop: "12px" }}>
          <Text weight="semibold">Current file vs latest applied generation</Text>
          <Text className={styles.muted}>Review only. The Web Console does not edit or apply YAML.</Text>
          <DiffView diff={planDiff} />
        </div>
      ) : null}
    </>
  );
}

function GenerationsView({
  generations,
  totalGenerations,
  query,
  setQuery,
  from,
  to,
  setFrom,
  setTo,
  diff,
  config,
  loadDiff,
  loadAdjacentDiff,
  loadConfig,
}: {
  generations: GenerationRecord[];
  totalGenerations: number;
  query: string;
  setQuery: (value: string) => void;
  from: string;
  to: string;
  setFrom: (value: string) => void;
  setTo: (value: string) => void;
  diff: string;
  config: { generation: number; text: string } | null;
  loadDiff: () => void;
  loadAdjacentDiff: (from: number, to: number) => void;
  loadConfig: (generation: number) => void;
}) {
  const styles = useStyles();
  const diffable = generations.filter(row => row.hasYaml);
  return (
    <>
      <Card id="generations-table" className={styles.connectionAnchor}>
        <CardHeader
          header={<Text weight="semibold">Generations</Text>}
          description={<Text className={styles.muted}>Applied router YAML snapshots. Older rows without YAML cannot be diffed.</Text>}
        />
        <div className={styles.grid}>
          <Metric label="total" value={String(totalGenerations)} />
          <Metric label="showing" value={String(generations.length)} />
          <Metric label="diffable" value={String(generations.filter(g => g.hasYaml).length)} />
          <Metric label="latest phase" value={String(generations[0]?.phase || "-")} />
        </div>
        <div className={styles.singleSearchRow}>
          <SearchControl label="Search generations" value={query} placeholder="generation, phase, hash, time" onChange={setQuery} />
        </div>
        <div className={styles.generationActions}>
          <Text size={200} className={styles.muted}>From</Text>
          <Select className={styles.generationSelect} size="small" value={from} onChange={event => setFrom(event.target.value)}>
            {diffable.map(row => <option key={row.generation} value={row.generation}>#{row.generation}</option>)}
          </Select>
          <Text size={200} className={styles.muted}>To</Text>
          <Select className={styles.generationSelect} size="small" value={to} onChange={event => setTo(event.target.value)}>
            {diffable.map(row => <option key={row.generation} value={row.generation}>#{row.generation}</option>)}
          </Select>
          <Button appearance="primary" disabled={!from || !to} onClick={loadDiff}>Diff</Button>
        </div>
        {diff ? (
          <Card id="generation-result" tabIndex={-1} aria-live="polite">
            <CardHeader header={<Text weight="semibold">Diff</Text>} description={<Text className={styles.muted}>Unified diff between selected generations</Text>} />
            <DiffView diff={diff} />
          </Card>
        ) : null}
        {config ? (
          <Card id="generation-result" tabIndex={-1} aria-live="polite">
            <CardHeader header={<Text weight="semibold">Generation #{config.generation}</Text>} description={<Text className={styles.muted}>Stored YAML snapshot</Text>} />
            <div className={styles.diffPanel} data-routerd-scroll-key={`generation-${config.generation}-yaml`}><pre className={styles.pre}>{config.text}</pre></div>
          </Card>
        ) : null}
        <div className={`${styles.tableWrap} ${styles.resourceDesktopTable}`} data-routerd-scroll-key="generations-table">
          <Table size="small" className={styles.generationTable}>
            <colgroup>
              <col style={{ width: "92px" }} />
              <col style={{ width: "150px" }} />
              <col style={{ width: "150px" }} />
              <col style={{ width: "104px" }} />
              <col />
              <col style={{ width: "96px" }} />
              <col style={{ width: "230px" }} />
            </colgroup>
            <TableHeader>
              <TableRow>
                <TableHeaderCell>Generation</TableHeaderCell>
                <TableHeaderCell>Started</TableHeaderCell>
                <TableHeaderCell>Finished</TableHeaderCell>
                <TableHeaderCell>Phase</TableHeaderCell>
                <TableHeaderCell>Hash</TableHeaderCell>
                <TableHeaderCell>YAML</TableHeaderCell>
                <TableHeaderCell>Actions</TableHeaderCell>
              </TableRow>
            </TableHeader>
            <TableBody>
              {generations.map((row, index) => {
                const previous = generations[index + 1];
                const canDiffPrevious = !!previous?.hasYaml && !!row.hasYaml;
                return (
                <TableRow key={row.generation} className={styles.stableTableRow}>
                  <TableCell><code className={styles.code}>#{row.generation}</code></TableCell>
                  <TableCell><RelativeTime value={row.startedAt} /></TableCell>
                  <TableCell><RelativeTime value={row.finishedAt} /></TableCell>
                  <TableCell><Badge appearance="tint" color={phaseColor(row.phase)}>{row.phase || "Unknown"}</Badge></TableCell>
                  <TableCell><code className={styles.wrapCode}>{shortHash(row.configHash)}</code></TableCell>
                  <TableCell>{row.hasYaml ? <Badge appearance="tint" color="success">stored</Badge> : <Badge appearance="outline">unavailable</Badge>}</TableCell>
                  <TableCell>
                    <div className={styles.generationRowActions}>
                      <Button size="small" appearance="subtle" disabled={!row.hasYaml} onClick={() => loadConfig(row.generation)}>View</Button>
                      <Button size="small" appearance="subtle" disabled={!canDiffPrevious} onClick={() => previous && loadAdjacentDiff(previous.generation, row.generation)}>Diff prev</Button>
                    </div>
                  </TableCell>
                </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
        <div className={styles.resourceMobileList}>
          {generations.map((row, index) => {
            const previous = generations[index + 1];
            const canDiffPrevious = !!previous?.hasYaml && !!row.hasYaml;
            return (
              <div className={styles.resourceMobileCard} key={`mg-${row.generation}`}>
                <div className={styles.resourceMobileHeader}>
                  <Text weight="semibold"><code className={styles.code}>#{row.generation}</code></Text>
                  <Badge appearance="tint" color={phaseColor(row.phase)}>{row.phase || "Unknown"}</Badge>
                </div>
                <div className={styles.resourceMobileMeta}>
                  <Text size={200} className={styles.muted}>started <RelativeTime value={row.startedAt} /> · finished <RelativeTime value={row.finishedAt} /></Text>
                  <code className={styles.wrapCode}>{shortHash(row.configHash)}</code>
                  <div className={styles.generationRowActions}>
                    {row.hasYaml ? <Badge appearance="tint" color="success">stored</Badge> : <Badge appearance="outline">unavailable</Badge>}
                    <Button size="small" appearance="subtle" disabled={!row.hasYaml} onClick={() => loadConfig(row.generation)}>View</Button>
                    <Button size="small" appearance="subtle" disabled={!canDiffPrevious} onClick={() => previous && loadAdjacentDiff(previous.generation, row.generation)}>Diff prev</Button>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </Card>
    </>
  );
}

function DiffView({ diff }: { diff: string }) {
  const styles = useStyles();
  const lines = diff.split(/\n/);
  const markers = diffOverviewMarkers(lines);
  return (
    <div className={styles.diffPanel} data-routerd-scroll-key="generation-diff">
      {markers.length > 0 ? (
        <div className={styles.diffRuler} aria-hidden="true">
          {markers.map((marker, index) => (
            <span
              key={`${marker.kind}-${marker.top}-${index}`}
              className={`${styles.diffRulerMark} ${marker.kind === "added" ? styles.diffRulerAdded : styles.diffRulerRemoved}`}
              style={{ top: `${marker.top}%`, height: `${marker.height}%` }}
            />
          ))}
        </div>
      ) : null}
      {lines.map((line, index) => (
        <span key={index} className={`${styles.diffLine} ${line.startsWith("+") && !line.startsWith("+++") ? styles.diffAdded : ""} ${line.startsWith("-") && !line.startsWith("---") ? styles.diffRemoved : ""}`}>
          {line}
        </span>
      ))}
    </div>
  );
}

function diffOverviewMarkers(lines: string[]) {
  const total = Math.max(lines.length, 1);
  const markers: { kind: "added" | "removed"; top: number; height: number }[] = [];
  let current: { kind: "added" | "removed"; start: number; end: number } | null = null;
  lines.forEach((line, index) => {
    const kind = line.startsWith("+") && !line.startsWith("+++") ? "added" : line.startsWith("-") && !line.startsWith("---") ? "removed" : "";
    if (!kind) {
      if (current) {
        markers.push(markerFromRange(current, total));
        current = null;
      }
      return;
    }
    if (current && current.kind === kind && index <= current.end + 1) {
      current.end = index;
      return;
    }
    if (current) markers.push(markerFromRange(current, total));
    current = { kind, start: index, end: index };
  });
  if (current) markers.push(markerFromRange(current, total));
  return markers.slice(0, 200);
}

function markerFromRange(marker: { kind: "added" | "removed"; start: number; end: number }, total: number) {
  const top = Math.max(0, Math.min(100, marker.start / total * 100));
  const height = Math.max(0.7, ((marker.end - marker.start + 1) / total) * 100);
  return { kind: marker.kind, top, height };
}

function ConfigTreeNode({ label, value, depth, query }: { label: string; value: unknown; depth: number; query: string }) {
  const styles = useStyles();
  const nodeID = configNodeID(label, value);
  if (Array.isArray(value)) {
    return (
      <details id={nodeID} className={styles.treeNode} open={depth < 2 || configNodeMatches(label, value, query)}>
        <summary className={styles.treeSummary}>
          <span className={styles.treeRow}>
            <span className={styles.treeKey}><Highlighted text={label} query={query} /></span>
            <span className={styles.treeMeta}>[{value.length} items]</span>
          </span>
        </summary>
        <div className={styles.treeChildren}>
          {value.map((item, index) => (
            <ConfigTreeNode key={`${index}-${configNodeLabel(index, item)}`} label={configNodeLabel(index, item)} value={item} depth={depth + 1} query={query} />
          ))}
        </div>
      </details>
    );
  }
  if (isRecord(value)) {
    const entries = Object.entries(value);
    return (
      <details id={nodeID} className={styles.treeNode} open={depth < 2 || configNodeMatches(label, value, query)}>
        <summary className={styles.treeSummary}>
          <span className={styles.treeRow}>
            <span className={styles.treeKey}><Highlighted text={label} query={query} /></span>
            <span className={styles.treeMeta}>{entries.length} keys</span>
          </span>
        </summary>
        <div className={styles.treeChildren}>
          {entries.map(([key, item]) => (
            <ConfigTreeNode key={key} label={key} value={item} depth={depth + 1} query={query} />
          ))}
        </div>
      </details>
    );
  }
  return (
    <div className={styles.treeLeaf}>
      <span className={styles.treeKey}><Highlighted text={label} query={query} /></span>
      <code className={styles.treeValue}><Highlighted text={formatConfigScalar(value)} query={query} /></code>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  const styles = useStyles();
  return (
    <Card className={styles.metric}>
      <Text size={200} className={styles.muted}>{label}</Text>
      <Text size={600} weight="semibold" className={styles.metricValue}>{value}</Text>
    </Card>
  );
}

function MetricCharts({ samples }: { samples: MetricSample[] }) {
  const styles = useStyles();
  return (
    <div className={styles.chartGrid}>
      <div className={styles.chartCard}>
        <Text weight="semibold">Generation trend</Text>
        <Sparkline samples={samples.map(sample => sample.generation)} color="#60cdff" />
        <Text size={200} className={styles.muted}>{samples.length ? `${samples.length} samples / latest #${samples[samples.length - 1].generation}` : "Waiting for samples"}</Text>
      </div>
      <div className={styles.chartCard}>
        <Text weight="semibold">Resource phases</Text>
        <StackBars samples={samples.map(sample => [sample.healthy, sample.warning, sample.danger])} colors={["#54b054", "#f7b955", "#d13438"]} />
        <Text size={200} className={styles.muted}>Healthy / pending / unhealthy over the current browser session</Text>
      </div>
      <div className={styles.chartCard}>
        <Text weight="semibold">Health checks</Text>
        <StackBars samples={samples.map(sample => [sample.healthHealthy, sample.healthUnhealthy])} colors={["#54b054", "#d13438"]} />
        <Text size={200} className={styles.muted}>Healthy and unhealthy HealthCheck resources</Text>
      </div>
    </div>
  );
}

function OverviewDPIInsights({ flows, clients, connections, dpi }: { flows?: TrafficFlow[]; clients?: ClientEntry[]; connections?: ConnectionTable; dpi?: DPIStatus }) {
  const styles = useStyles();
  const flowRows = flows ?? [];
  const clientRows = clients ?? [];
  const connectionRows = connections?.entries ?? [];
  const flowEmpty = flows === undefined ? "Loading observed flow data" : "No observed flow data";
  const connectionEmpty = connections === undefined ? "Loading connection data" : "No active connection classes";
  const protocols = topTrafficProtocols(flowRows);
  const sources = topTrafficSources(flowRows);
  const talkers = topTalkers(clientRows, flowRows);
  const domains = topDomains(flowRows);
  const classes = connectionClassSummary(connectionRows);
  const classification = connectionClassificationStats(connectionRows);
  return (
    <div id="overview-dpi" className={`${styles.dpiInsightGrid} ${styles.connectionAnchor}`}>
      <Card className={styles.chartCard}>
        <CardHeader header={<Text weight="semibold">Classification</Text>} description={<Text className={styles.muted}>Active flow identification ratio from DPI and port fallback</Text>} />
        <ConnectionClassificationMeter stats={classification} />
      </Card>
      <Card className={styles.chartCard}>
        <CardHeader header={<Text weight="semibold">DPI engine</Text>} description={<Text className={styles.muted}>Classifier and nDPI agent service status</Text>} />
        <DPIServiceSummary dpi={dpi} />
      </Card>
      <Card className={styles.chartCard}>
        <CardHeader header={<Text weight="semibold">Top protocols</Text>} description={<Text className={styles.muted}>DPI protocols and weak port guesses from recent observed flows</Text>} />
        <RankList rows={protocols} empty={flowEmpty} formatLabel={formatProtocolRankLabel} formatValue={formatBytes} />
      </Card>
      <Card className={styles.chartCard}>
        <CardHeader header={<Text weight="semibold">DPI sources</Text>} description={<Text className={styles.muted}>Observed flow volume by classifier source</Text>} />
        <RankList rows={sources} empty={flowEmpty} formatLabel={formatTrafficSourceLabel} formatValue={formatBytes} />
      </Card>
      <Card className={styles.chartCard}>
        <CardHeader header={<Text weight="semibold">Top talkers</Text>} description={<Text className={styles.muted}>Clients with DPI-classified traffic volume</Text>} />
        <RankList rows={talkers} empty={flowEmpty} formatValue={formatBytes} />
      </Card>
      <Card className={styles.chartCard}>
        <CardHeader header={<Text weight="semibold">Top SNI / domains</Text>} description={<Text className={styles.muted}>Recent TLS SNI, HTTP host, DNS, or reverse DNS labels</Text>} />
        <RankList rows={domains} empty={flowEmpty} formatValue={formatBytes} />
      </Card>
      <Card className={styles.chartCard}>
        <CardHeader header={<Text weight="semibold">Traffic classes</Text>} description={<Text className={styles.muted}>Active connection visibility from conntrack and DPI enrichment</Text>} />
        <RankList rows={classes} empty={connectionEmpty} />
      </Card>
    </div>
  );
}

function DPIServiceSummary({ dpi }: { dpi?: DPIStatus }) {
  const styles = useStyles();
  if (!dpi?.classifier && !dpi?.agent) return <Text className={styles.muted}>No DPI service sockets found</Text>;
  const classifier = dpi.classifier;
  const agent = dpi.agent;
  const classified = Number(classifier?.stats?.agentClassified ?? 0) + Number(classifier?.stats?.builtinClassified ?? 0) || Number(agent?.stats?.classifiedPackets ?? agent?.stats?.classified ?? 0);
  const errors = Number(classifier?.stats?.agentErrors ?? 0) + Number(agent?.stats?.errorPackets ?? agent?.stats?.errors ?? 0);
  const timeouts = Number(classifier?.stats?.timeoutErrors ?? 0);
  return (
    <div className={styles.grid}>
      <Metric label="classifier" value={classifier?.activeEngine || classifier?.engine || (classifier?.error ? "error" : "-")} />
      <Metric label="nDPI" value={agent?.libndpiLoaded ? "loaded" : agent?.error ? "error" : agent ? "unavailable" : "-"} />
      <Metric label="classified" value={classified ? String(classified) : "-"} />
      <Metric label="errors" value={errors ? String(errors) : "0"} />
      <Metric label="timeouts" value={timeouts ? String(timeouts) : "0"} />
      {classifier?.reason || classifier?.error || agent?.reason || agent?.error ? (
        <Text size={200} className={styles.muted}>{classifier?.reason || classifier?.error || agent?.reason || agent?.error}</Text>
      ) : null}
    </div>
  );
}

function RankList({
  rows,
  empty,
  formatLabel = value => value,
  formatValue = value => String(value),
}: {
  rows: { label: string; value: number }[];
  empty: string;
  formatLabel?: (value: string) => string;
  formatValue?: (value: number) => string;
}) {
  const styles = useStyles();
  const stableRows = useFrozenRowOrder(rows, row => row.label);
  const max = Math.max(1, ...stableRows.map(row => row.value));
  if (stableRows.length === 0) return <Text className={styles.muted}>{empty}</Text>;
  return (
    <div className={styles.rankList}>
      {stableRows.map(row => (
        <div className={styles.rankRow} key={row.label}>
          <div className={styles.rankLine}>
            <Text><code className={styles.wrapCode}>{formatLabel(row.label)}</code></Text>
            <Text size={200} className={styles.muted}>{formatValue(row.value)}</Text>
          </div>
          <div className={styles.barTrack} aria-hidden="true">
            <div className={styles.barFill} style={{ width: `${Math.max(4, (row.value / max) * 100)}%` }} />
          </div>
        </div>
      ))}
    </div>
  );
}

function Sparkline({ samples, color }: { samples: number[]; color: string }) {
  const styles = useStyles();
  const values = samples.length ? samples : [0];
  const max = Math.max(...values);
  const min = Math.min(...values);
  const span = Math.max(1, max - min);
  const points = values.map((value, index) => {
    const x = values.length === 1 ? 50 : (index / (values.length - 1)) * 100;
    const y = 76 - ((value - min) / span) * 62;
    return `${x.toFixed(2)},${y.toFixed(2)}`;
  }).join(" ");
  return (
    <svg className={styles.chartSvg} viewBox="0 0 100 86" preserveAspectRatio="none" aria-hidden="true">
      <polyline fill="none" stroke={color} strokeWidth="2.5" points={points} />
    </svg>
  );
}

function StackBars({ samples, colors }: { samples: number[][]; colors: string[] }) {
  const styles = useStyles();
  const rows = samples.length ? samples : [[0]];
  const width = 100 / Math.max(1, rows.length);
  return (
    <svg className={styles.chartSvg} viewBox="0 0 100 86" preserveAspectRatio="none" aria-hidden="true">
      {rows.map((row, index) => {
        const total = Math.max(1, row.reduce((sum, value) => sum + value, 0));
        let y = 86;
        return row.map((value, part) => {
          const height = (value / total) * 72;
          y -= height;
          return <rect key={`${index}-${part}`} x={index * width + 1} y={y} width={Math.max(1, width - 2)} height={height} fill={colors[part] ?? "#777"} />;
        });
      })}
    </svg>
  );
}

function ConnectionSummaryCharts({ groups }: { groups: { key: string; rows: ConnectionEntry[] }[] }) {
  const styles = useStyles();
  const max = Math.max(1, ...groups.map(group => group.rows.length));
  if (groups.length === 0) return <Text className={styles.muted}>No active connections reported</Text>;
  return (
    <div className={styles.connectionSummaryGrid}>
      {groups.map(group => {
        const label = connectionGroupLabel(group.key);
        return (
          <div className={styles.connectionSummaryCard} key={group.key}>
            <div className={styles.interfaceHeader}>
              <Text weight="semibold">{label.family}/{label.protocol.toUpperCase()}</Text>
              <Badge appearance="tint" color={label.family === "IPv6" ? "brand" : "success"}>{group.rows.length}</Badge>
            </div>
            <div className={styles.firewallBar} style={{ width: `${Math.max(3, (group.rows.length / max) * 100)}%` }} />
            <Text size={200} className={styles.muted}>{connectionStateSummary(group.rows).map(row => `${row.label} ${row.count}`).join(" / ") || "stateless"}</Text>
          </div>
        );
      })}
    </div>
  );
}

function ConnectionClassificationSummary({ entries }: { entries: ConnectionEntry[] }) {
  const styles = useStyles();
  const stats = connectionClassificationStats(entries);
  return (
    <div className={styles.connectionSummaryGrid}>
      <div className={styles.connectionSummaryCard}>
        <div className={styles.interfaceHeader}>
          <Text weight="semibold">Classification</Text>
          <Badge appearance="tint" color={stats.classifiedRatio >= 70 ? "success" : stats.classifiedRatio >= 40 ? "warning" : "danger"}>
            {stats.classifiedRatio}%
          </Badge>
        </div>
        <ConnectionClassificationMeter stats={stats} />
      </div>
      <div className={styles.connectionSummaryCard}>
        <Text weight="semibold">Identified</Text>
        <div className={styles.grid}>
          <Metric label="DPI" value={String(stats.dpi)} />
          <Metric label="Port guess" value={String(stats.guessed)} />
          <Metric label="Identifying" value={String(stats.identifying)} />
          <Metric label="Unclassified" value={String(stats.unclassified)} />
        </div>
        <Text size={200} className={styles.muted}>{stats.classified} of {stats.total} active rows carry a protocol label; port guesses are lower confidence</Text>
      </div>
    </div>
  );
}

function ConnectionClassificationMeter({ stats }: { stats: ConnectionClassificationStats }) {
  const styles = useStyles();
  const total = Math.max(1, stats.total);
  const dpiWidth = (stats.dpi / total) * 100;
  const guessWidth = (stats.guessed / total) * 100;
  const unknownWidth = Math.max(0, 100 - dpiWidth - guessWidth);
  return (
    <div className={styles.classificationStack}>
      <div className={styles.classificationMeter} aria-label={`classified ${stats.classifiedRatio}%`}>
        <div className={styles.classificationSegmentDPI} style={{ width: `${dpiWidth}%` }} />
        <div className={styles.classificationSegmentGuess} style={{ width: `${guessWidth}%` }} />
        <div className={styles.classificationSegmentUnknown} style={{ width: `${unknownWidth}%` }} />
      </div>
      <Text size={200} className={styles.muted}>
        Classified {stats.classified}/{stats.total} / DPI {stats.dpi} / Port guess {stats.guessed} / Identifying {stats.identifying} / Unclassified {stats.unclassified}
      </Text>
    </div>
  );
}

function parseConfig(text?: string): { value?: unknown; errors: string[] } {
  if (!text) return { value: undefined, errors: [] };
  try {
    const document = parseDocument(text);
    const errors = [...document.errors, ...document.warnings].map(error => error.message || String(error));
    return { value: document.toJS(), errors };
  } catch (error) {
    return { value: undefined, errors: [String(error)] };
  }
}

function configNodeLabel(index: number, value: unknown) {
  if (!isRecord(value)) return `[${index}]`;
  const kind = stringValue(value.kind);
  const name = stringValue(value.name) || (isRecord(value.metadata) ? stringValue(value.metadata.name) : "");
  if (kind && name) return `${index}: ${kind}/${name}`;
  if (kind) return `${index}: ${kind}`;
  return `[${index}]`;
}

function configNodeID(label: string, value: unknown) {
  if (!isRecord(value)) return undefined;
  const kind = stringValue(value.kind);
  const name = stringValue(value.name) || (isRecord(value.metadata) ? stringValue(value.metadata.name) : "");
  if (!kind) return undefined;
  return `config-${kind}-${name || label}`.replace(/[^a-zA-Z0-9_-]+/g, "-");
}

function configNodeMatches(label: string, value: unknown, query: string) {
  const needle = query.trim().toLowerCase();
  if (!needle) return false;
  return `${label} ${JSON.stringify(value)}`.toLowerCase().includes(needle);
}

function configKindTargets(value: unknown) {
  const targets: { id: string; label: string }[] = [];
  const visit = (item: unknown, label: string) => {
    if (Array.isArray(item)) {
      item.forEach((child, index) => visit(child, configNodeLabel(index, child)));
      return;
    }
    if (!isRecord(item)) return;
    const id = configNodeID(label, item);
    const kind = stringValue(item.kind);
    const name = stringValue(item.name) || (isRecord(item.metadata) ? stringValue(item.metadata.name) : "");
    if (id && kind) targets.push({ id, label: name ? `${kind}/${name}` : kind });
    Object.entries(item).forEach(([key, child]) => {
      if (key !== "metadata" && key !== "spec" && key !== "status") visit(child, key);
    });
  };
  visit(value, "config");
  const seen = new Set<string>();
  return targets.filter(target => {
    if (seen.has(target.id)) return false;
    seen.add(target.id);
    return true;
  });
}

function highlightYAML(text: string, query: string) {
  const needle = query.trim();
  if (!needle) return [{ text, match: false }];
  const lower = text.toLowerCase();
  const lowerNeedle = needle.toLowerCase();
  const parts: { text: string; match: boolean }[] = [];
  let cursor = 0;
  for (;;) {
    const index = lower.indexOf(lowerNeedle, cursor);
    if (index < 0) break;
    if (index > cursor) parts.push({ text: text.slice(cursor, index), match: false });
    parts.push({ text: text.slice(index, index + needle.length), match: true });
    cursor = index + needle.length;
  }
  if (cursor < text.length) parts.push({ text: text.slice(cursor), match: false });
  return parts;
}

function formatConfigScalar(value: unknown) {
  if (value === null) return "null";
  if (value === undefined) return "";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  if (value instanceof Date) return value.toISOString();
  return JSON.stringify(value);
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value : "";
}

function shortHash(value?: string) {
  if (!value) return "-";
  return value.length > 16 ? `${value.slice(0, 16)}...` : value;
}

function handshakeFresh(value?: string) {
  if (!value) return false;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return false;
  return Date.now() - date.getTime() < 5 * 60 * 1000;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value) && !(value instanceof Date);
}

function ResourceTable({ resources, controllers, navigateTo }: { resources: ResourceStatus[]; controllers: ControllerStatus[]; navigateTo?: (view: ViewKey, targetID?: string) => void }) {
  const styles = useStyles();
  const dryRunByKind = useMemo(() => dryRunControllerByKind(controllers), [controllers]);
  const [query, setQuery] = useState("");
  const [phase, setPhase] = useState("all");
  const [kind, setKind] = useState("all");
  const phases = useMemo(() => {
    const values = new Set<string>();
    for (const resource of resources) values.add(String(resource.status?.phase ?? "Unknown"));
    return Array.from(values).sort(facetSort);
  }, [resources]);
  const kinds = useMemo(() => {
    const values = new Set<string>();
    for (const resource of resources) values.add(String(resource.kind ?? "Unknown"));
    return Array.from(values).sort(facetSort);
  }, [resources]);
  const filtered = resources.filter(resource => {
    const resourcePhase = String(resource.status?.phase ?? "Unknown");
    if (phase !== "all" && resourcePhase !== phase) return false;
    const resourceKind = String(resource.kind ?? "Unknown");
    if (kind !== "all" && resourceKind !== kind) return false;
    if (!query.trim()) return true;
    return resourceSearchText(resource).includes(query.trim().toLowerCase());
  });
  return (
    <>
      <div className={styles.resourceFilters}>
        <SearchControl label="Search resources" value={query} placeholder="kind, name, phase, status detail" onChange={setQuery} />
        <div className={styles.filterControl}>
          <Text size={200} className={styles.muted}>Kind</Text>
          <Select size="small" value={kind} onChange={event => setKind(event.target.value)}>
            <option value="all">All kinds</option>
            {kinds.map(value => <option key={value} value={value}>{value}</option>)}
          </Select>
        </div>
        <div className={styles.filterControl}>
          <Text size={200} className={styles.muted}>Phase</Text>
          <Select size="small" value={phase} onChange={event => setPhase(event.target.value)}>
            <option value="all">All phases</option>
            {phases.map(value => <option key={value} value={value}>{value}</option>)}
          </Select>
        </div>
      </div>
      <Text size={200} className={styles.muted}>Showing {Math.min(filtered.length, 120)} of {filtered.length} matched resources / {resources.length} total</Text>
      <div className={`${styles.tableWrap} ${styles.resourceDesktopTable}`} data-routerd-scroll-key="resources-table">
        <Table size="small" className={styles.resourceTable}>
          <colgroup>
            <col style={{ width: "170px" }} />
            <col style={{ width: "220px" }} />
            <col style={{ width: "120px" }} />
            <col style={{ width: "130px" }} />
            <col />
          </colgroup>
          <TableHeader>
            <TableRow>
              <TableHeaderCell>Kind</TableHeaderCell>
              <TableHeaderCell>Name</TableHeaderCell>
              <TableHeaderCell>Phase</TableHeaderCell>
              <TableHeaderCell>Mode</TableHeaderCell>
              <TableHeaderCell>Detail</TableHeaderCell>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.slice(0, 120).map(resource => {
              const status = resource.status ?? {};
              const dryRunController = dryRunByKind.get(String(resource.kind ?? ""));
              return (
                <TableRow key={`${resource.apiVersion}/${resource.kind}/${resource.name}`} className={styles.stableTableRow}>
                  <TableCell><Highlighted text={resource.kind ?? ""} query={query} /></TableCell>
                  <TableCell><code className={styles.code}><Highlighted text={resource.name ?? ""} query={query} /></code></TableCell>
                  <TableCell><Badge appearance="tint" color={phaseColor(status.phase)}><Highlighted text={String(status.phase ?? "Unknown")} query={query} /></Badge></TableCell>
                  <TableCell>{dryRunController ? <Badge appearance="tint" color="warning">dry-run</Badge> : <Text size={200} className={styles.muted}>live</Text>}</TableCell>
                  <TableCell>
                    <div className={styles.connectionFlow}>
                      <OwnershipLine resource={resource} />
                      <code className={styles.wrapCode}><Highlighted text={resourceDetail(status)} query={query} /></code>
                      <ResourceStatusExtra resource={resource} />
                      {navigateTo ? <Button size="small" appearance="outline" icon={<DocumentTextRegular />} onClick={() => { try { window.localStorage?.setItem("routerd:config:initialQuery", String(resource.name ?? "")); } catch {} navigateTo("config"); }}>YAML</Button> : null}
                    </div>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
      <div className={styles.resourceMobileList}>
        {filtered.slice(0, 120).map(resource => {
          const status = resource.status ?? {};
          const dryRunController = dryRunByKind.get(String(resource.kind ?? ""));
          return (
            <div className={styles.resourceMobileCard} key={`m-${resource.apiVersion}/${resource.kind}/${resource.name}`}>
              <div className={styles.resourceMobileHeader}>
                <Text weight="semibold"><Highlighted text={resource.kind ?? ""} query={query} /></Text>
                <Badge appearance="tint" color={phaseColor(status.phase)}>{String(status.phase ?? "Unknown")}</Badge>
              </div>
              <code className={styles.code}><Highlighted text={resource.name ?? ""} query={query} /></code>
              <div className={styles.resourceMobileMeta}>
                {dryRunController ? <Badge appearance="tint" color="warning">dry-run</Badge> : <Text size={200} className={styles.muted}>live</Text>}
                <OwnershipLine resource={resource} />
                <code className={styles.wrapCode}><Highlighted text={resourceDetail(status)} query={query} /></code>
                <ResourceStatusExtra resource={resource} />
                {navigateTo ? <Button size="small" appearance="subtle" onClick={() => navigateTo("config")}>View YAML</Button> : null}
              </div>
            </div>
          );
        })}
      </div>
    </>
  );
}

function RoutesView({ status }: { status: RoutesStatus | null }) {
  const styles = useStyles();
  const routes = status?.routes ?? [];
  const peers = status?.bgpPeers ?? [];
  const [query, setQuery] = useState("");
  const [protocol, setProtocol] = useState("all");
  const [family, setFamily] = useState("all");
  const protocols = useMemo(() => routeProtocolOptions(routes), [routes]);
  const families = useMemo(() => routeFamilyOptions(routes), [routes]);
  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return routes.filter(route => {
      if (protocol !== "all" && routeProtocolBucket(route) !== protocol) return false;
      if (family !== "all" && String(route.family || "unknown") !== family) return false;
      if (!needle) return true;
      return routeSearchText(route).includes(needle);
    });
  }, [routes, query, protocol, family]);
  const latest = latestRouteObservedAt(status);
  return (
    <Card id="routes-table" className={styles.connectionAnchor}>
      <CardHeader
        header={<Text weight="semibold">Routes</Text>}
        description={<Text className={styles.muted}>Kernel, configured, DHCP, and BGP routes with next-hop and observation state</Text>}
      />
      <div className={styles.grid}>
        <Metric label="total routes" value={String(routes.length)} />
        <Metric label="BGP" value={String(routes.filter(route => routeProtocolBucket(route) === "bgp").length)} />
        <Metric label="static" value={String(routes.filter(route => routeProtocolBucket(route) === "static").length)} />
        <Metric label="connected" value={String(routes.filter(route => routeProtocolBucket(route) === "connected").length)} />
        <Metric label="kernel" value={String(routes.filter(route => routeProtocolBucket(route) === "kernel").length)} />
        <Metric label="last reload" value={latest ? relativeTimeText(latest) : "-"} />
      </div>
      <div className={styles.routeFilters}>
        <SearchControl label="Search routes" value={query} placeholder="prefix, next-hop, device, peer" onChange={setQuery} />
        <div className={styles.filterControl}>
          <Text size={200} className={styles.muted}>Protocol</Text>
          <Select size="small" value={protocol} onChange={event => setProtocol(event.target.value)}>
            <option value="all">All protocols</option>
            {protocols.map(value => <option key={value} value={value}>{routeProtocolLabel(value)}</option>)}
          </Select>
        </div>
        <div className={styles.filterControl}>
          <Text size={200} className={styles.muted}>Family</Text>
          <Select size="small" value={family} onChange={event => setFamily(event.target.value)}>
            <option value="all">All families</option>
            {families.map(value => <option key={value} value={value}>{formatFacet(value)}</option>)}
          </Select>
        </div>
      </div>
      <Text size={200} className={styles.muted}>Showing {filtered.length} of {routes.length} routes</Text>
      {status?.errors?.length ? (
        <div className={styles.activeFilterBanner}>
          <Text weight="semibold">Collection errors</Text>
          <Text size={200} className={styles.muted}>{status.errors.join("; ")}</Text>
        </div>
      ) : null}
      <div className={`${styles.tableWrap} ${styles.resourceDesktopTable}`} data-routerd-scroll-key="routes-table">
        <Table size="small" className={styles.routeTable}>
          <colgroup>
            <col style={{ width: "108px" }} />
            <col style={{ width: "92px" }} />
            <col style={{ width: "220px" }} />
            <col style={{ width: "160px" }} />
            <col style={{ width: "120px" }} />
            <col style={{ width: "112px" }} />
            <col style={{ width: "86px" }} />
            <col style={{ width: "80px" }} />
            <col style={{ width: "150px" }} />
            <col style={{ width: "118px" }} />
          </colgroup>
          <TableHeader>
            <TableRow>
              <TableHeaderCell>Protocol</TableHeaderCell>
              <TableHeaderCell>Family</TableHeaderCell>
              <TableHeaderCell>Destination</TableHeaderCell>
              <TableHeaderCell>Next hop</TableHeaderCell>
              <TableHeaderCell>Device</TableHeaderCell>
              <TableHeaderCell>Table</TableHeaderCell>
              <TableHeaderCell>Metric</TableHeaderCell>
              <TableHeaderCell>Phase</TableHeaderCell>
              <TableHeaderCell>Resource</TableHeaderCell>
              <TableHeaderCell>Observed</TableHeaderCell>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.map(route => (
              <TableRow key={routeKey(route)} className={styles.stableTableRow}>
                <TableCell><Badge appearance="tint" color={routeProtocolColor(routeProtocolBucket(route))}>{routeProtocolLabel(routeProtocolBucket(route))}</Badge></TableCell>
                <TableCell>{formatFacet(route.family || "-")}</TableCell>
                <TableCell><code className={styles.wrapCode}><Highlighted text={route.destination || "default"} query={query} /></code></TableCell>
                <TableCell><code className={styles.wrapCode}><Highlighted text={route.gateway || route.peer || "-"} query={query} /></code></TableCell>
                <TableCell><code className={styles.code}><Highlighted text={route.device || "-"} query={query} /></code></TableCell>
                <TableCell><code className={styles.code}>{route.table || "-"}</code></TableCell>
                <TableCell>{route.metric || "-"}</TableCell>
                <TableCell><Text size={200} className={styles.muted}>{route.phase || "-"}</Text></TableCell>
                <TableCell><code className={styles.wrapCode}><Highlighted text={route.resource || route.source || "-"} query={query} /></code></TableCell>
                <TableCell>{route.observedAt ? <RelativeTime value={route.observedAt} /> : <Text size={200} className={styles.muted}>-</Text>}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <div className={styles.resourceMobileList}>
        {filtered.map(route => (
          <div className={styles.routeMobileCard} key={`mobile-${routeKey(route)}`}>
            <div className={styles.routeMobileHeader}>
              <div className={styles.connectionFlow}>
                <code className={styles.wrapCode}><Highlighted text={route.destination || "default"} query={query} /></code>
                <Text size={200} className={styles.muted}>{route.gateway || route.peer ? <>via <Highlighted text={route.gateway || route.peer || ""} query={query} /></> : "direct"}</Text>
              </div>
              <Badge appearance="tint" color={routeProtocolColor(routeProtocolBucket(route))}>{routeProtocolLabel(routeProtocolBucket(route))}</Badge>
            </div>
            <div className={styles.routeMobileDetails}>
              <RouteDetail label="family" value={formatFacet(route.family || "-")} />
              <RouteDetail label="device" value={route.device || "-"} code />
              <RouteDetail label="table" value={route.table || "-"} code />
              <RouteDetail label="metric" value={route.metric || "-"} />
              <RouteDetail label="phase" value={route.phase || "-"} />
              <div className={styles.connectionFlow}>
                <Text size={200} className={styles.muted}>observed</Text>
                {route.observedAt ? <RelativeTime value={route.observedAt} /> : <Text size={200}>-</Text>}
              </div>
            </div>
            <Text size={200} className={styles.muted}>{route.resource || route.source || "-"}</Text>
          </div>
        ))}
      </div>
      <RoutePeers peers={peers} />
    </Card>
  );
}

function RouteDetail({ label, value, code }: { label: string; value: string; code?: boolean }) {
  const styles = useStyles();
  return (
    <div className={styles.connectionFlow}>
      <Text size={200} className={styles.muted}>{label}</Text>
      {code ? <code className={styles.code}>{value}</code> : <Text>{value}</Text>}
    </div>
  );
}

function RoutePeers({ peers }: { peers: RouteBGPPeer[] }) {
  const styles = useStyles();
  if (peers.length === 0) return null;
  return (
    <div className={styles.routePeerSection}>
      <CardHeader header={<Text weight="semibold">BGP peers</Text>} description={<Text className={styles.muted}>Peer state associated with BGP route observations</Text>} />
      <div className={`${styles.tableWrap} ${styles.resourceDesktopTable}`} data-routerd-scroll-key="routes-bgp-peers">
        <Table size="small" className={styles.resourceTable}>
          <TableHeader>
            <TableRow>
              <TableHeaderCell>Router</TableHeaderCell>
              <TableHeaderCell>Peer</TableHeaderCell>
              <TableHeaderCell>ASN</TableHeaderCell>
              <TableHeaderCell>State</TableHeaderCell>
              <TableHeaderCell>Prefixes</TableHeaderCell>
              <TableHeaderCell>Messages</TableHeaderCell>
              <TableHeaderCell>Last established</TableHeaderCell>
            </TableRow>
          </TableHeader>
          <TableBody>
            {peers.map(peer => (
              <TableRow key={routePeerKey(peer)} className={styles.stableTableRow}>
                <TableCell><code className={styles.code}>{peer.router || "-"}</code></TableCell>
                <TableCell><code className={styles.code}>{peer.peer || "-"}</code></TableCell>
                <TableCell>{peer.asn || "-"}</TableCell>
                <TableCell><Badge appearance="tint" color={peer.established ? "success" : "warning"}>{peer.state || "Unknown"}</Badge></TableCell>
                <TableCell>{peer.prefixesReceived || "-"}</TableCell>
                <TableCell>{peer.messages || "-"}</TableCell>
                <TableCell>{peer.lastEstablishedAt ? <RelativeTime value={peer.lastEstablishedAt} /> : <Text size={200} className={styles.muted}>-</Text>}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <div className={styles.resourceMobileList}>
        {peers.map(peer => (
          <div className={styles.routeMobileCard} key={`mobile-${routePeerKey(peer)}`}>
            <div className={styles.routeMobileHeader}>
              <code className={styles.code}>{peer.peer || "-"}</code>
              <Badge appearance="tint" color={peer.established ? "success" : "warning"}>{peer.state || "Unknown"}</Badge>
            </div>
            <div className={styles.routeMobileDetails}>
              <RouteDetail label="router" value={peer.router || "-"} code />
              <RouteDetail label="ASN" value={peer.asn || "-"} />
              <RouteDetail label="prefixes" value={peer.prefixesReceived || "-"} />
              <RouteDetail label="messages" value={peer.messages || "-"} />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function ResourceStatusExtra({ resource }: { resource: ResourceStatus }) {
  const styles = useStyles();
  if (resource.kind !== "TailscaleNode") return null;
  const peers = arrayValue(resource.status?.peers).slice(0, 8);
  if (peers.length === 0) return null;
  return (
    <div className={styles.badges}>
      {peers.map((peer, index) => {
        const row = isRecord(peer) ? peer : {};
        const label = stringValue(row.hostName) || stringValue(row.dnsName) || stringValue(row.id) || `peer-${index + 1}`;
        const online = Boolean(row.online);
        return <Badge key={`${label}-${index}`} appearance="outline" color={online ? "success" : "subtle"}>{label}</Badge>;
      })}
    </div>
  );
}

function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function OwnershipLine({ resource }: { resource: ResourceStatus }) {
  const styles = useStyles();
  const status = resource.status ?? {};
  const owner = String(resource.owner ?? status.owner ?? "").trim();
  const managedBy = String(resource.managedBy ?? status.managedBy ?? "").trim();
  const management = String(resource.management ?? status.management ?? "").trim();
  const parts = [
    owner ? `owner ${owner}` : "",
    managedBy ? `managed-by ${managedBy}` : "",
    management ? management : "",
  ].filter(Boolean);
  if (parts.length === 0) return null;
  return <Text size={200} className={styles.muted}>{parts.join(" / ")}</Text>;
}

function SearchControl({
  label,
  value,
  placeholder,
  onChange,
}: {
  label: string;
  value: string;
  placeholder: string;
  onChange: (value: string) => void;
}) {
  const styles = useStyles();
  return (
    <div className={styles.filterControl}>
      <Text size={200} className={styles.muted}>{label}</Text>
      <Input
        className={styles.filterInput}
        size="small"
        value={value}
        placeholder={placeholder}
        onChange={(_, data) => onChange(data.value)}
        contentAfter={value ? (
          <Button
            aria-label={`Clear ${label}`}
            appearance="subtle"
            className={styles.filterClearButton}
            icon={<DismissRegular />}
            size="small"
            onClick={() => onChange("")}
          />
        ) : null}
      />
    </div>
  );
}

function ControllerTable({ controllers }: { controllers: ControllerStatus[] }) {
  const styles = useStyles();
  const rows = controllers;
  return (
    <div className={styles.tableWrap} data-routerd-scroll-key="controllers-table">
      <Table size="small" className={styles.controllerTable}>
        <colgroup>
          <col style={{ width: "170px" }} />
          <col style={{ width: "110px" }} />
          <col style={{ width: "150px" }} />
          <col style={{ width: "240px" }} />
          <col style={{ width: "180px" }} />
          <col />
        </colgroup>
        <TableHeader>
          <TableRow>
            <TableHeaderCell>Controller</TableHeaderCell>
            <TableHeaderCell>Mode</TableHeaderCell>
            <TableHeaderCell>Reconcile</TableHeaderCell>
            <TableHeaderCell>Timing</TableHeaderCell>
            <TableHeaderCell>Resource kinds</TableHeaderCell>
            <TableHeaderCell>Reason</TableHeaderCell>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map(controller => (
            <TableRow key={controller.name} className={styles.stableTallTableRow}>
              <TableCell><code className={styles.code}>{controller.name}</code></TableCell>
              <TableCell><Badge appearance="tint" color={controller.mode === "dry-run" ? "warning" : "success"}>{controller.mode ?? "unknown"}</Badge></TableCell>
              <TableCell>
                <div className={styles.connectionFlow}>
                  <Text>{controller.reconcileCount ?? 0} runs</Text>
                  <Text size={200} className={controller.reconcileErrorCount ? "" : styles.muted}>
                    {controller.reconcileErrorCount ?? 0} errors
                  </Text>
                  {controller.lastTrigger ? <Text size={200} className={styles.muted}>{controller.lastTrigger}</Text> : null}
                </div>
              </TableCell>
              <TableCell>
                <div className={styles.connectionFlow}>
                  <Text>{controller.interval || "-"}</Text>
                  <Text size={200} className={styles.muted}>last {durationLabel(controller.lastDuration, controller.lastDurationMillis)}</Text>
                  <Text size={200} className={styles.muted}>avg {durationLabel(controller.averageDuration, controller.averageDurationMillis)}</Text>
                  {controller.lastReconcileTime ? <Text size={200} className={styles.muted}>ran <RelativeTime value={controller.lastReconcileTime} /></Text> : null}
                </div>
              </TableCell>
              <TableCell>
                <Text size={200} className={styles.muted}>{(controller.resourceKinds ?? []).join(", ") || "-"}</Text>
              </TableCell>
              <TableCell>
                <div className={styles.connectionFlow}>
                  <Text>{controller.reason || "-"}</Text>
                  {controller.message ? <Text size={200} className={styles.muted}>{controller.message}</Text> : null}
                  {controller.lastError ? <Text size={200}>{controller.lastError}</Text> : null}
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function Highlighted({ text, query }: { text: string; query: string }) {
  const styles = useStyles();
  const needle = query.trim();
  if (!needle) return <>{text}</>;
  const index = text.toLowerCase().indexOf(needle.toLowerCase());
  if (index < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, index)}
      <mark className={styles.highlight}>{text.slice(index, index + needle.length)}</mark>
      {text.slice(index + needle.length)}
    </>
  );
}

function RelativeTime({ value }: { value?: string }) {
  const absolute = absoluteTime(value);
  const relative = relativeTimeText(value);
  if (!value) return null;
  return <span title={absolute}>{relative || absolute}</span>;
}

function EventTable({ events, selectedKey, onSelect, query }: { events: RouterEvent[]; selectedKey: string; onSelect: (event: RouterEvent) => void; query?: string }) {
  const styles = useStyles();
  return (
    <div className={styles.tableWrap} data-routerd-scroll-key="events-table">
      <Table size="small" className={styles.eventTable}>
        <colgroup>
          <col style={{ width: "104px" }} />
          <col style={{ width: "78px" }} />
          <col />
          <col style={{ width: "170px" }} />
        </colgroup>
        <TableHeader>
          <TableRow>
            <TableHeaderCell>Time</TableHeaderCell>
            <TableHeaderCell>Severity</TableHeaderCell>
            <TableHeaderCell>Topic</TableHeaderCell>
            <TableHeaderCell>Resource</TableHeaderCell>
          </TableRow>
        </TableHeader>
        <TableBody>
          {events.slice(0, 100).map(event => {
            const key = eventKey(event);
            return (
              <TableRow key={key} className={`${styles.stableTableRow} ${key === selectedKey ? styles.eventRowSelected : ""}`} onClick={() => onSelect(event)}>
                <TableCell><RelativeTime value={event.createdAt} /></TableCell>
                <TableCell><Highlighted text={event.severity ?? ""} query={query ?? ""} /></TableCell>
                <TableCell><code className={styles.wrapCode}><Highlighted text={event.topic ?? event.type ?? ""} query={query ?? ""} /></code></TableCell>
                <TableCell><Highlighted text={resourceName(event)} query={query ?? ""} /></TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

function EventDetail({ event, id }: { event?: RouterEvent; id?: string }) {
  const styles = useStyles();
  if (!event) {
    return (
      <Card id={id} className={styles.detailPanel}>
        <CardHeader header={<Text weight="semibold">Detail</Text>} />
        <Text className={styles.muted}>No event selected</Text>
      </Card>
    );
  }
  const baseRows: [string, unknown][] = [
    ["time", absoluteTime(event.createdAt)],
    ["severity", event.severity ?? ""],
    ["topic", event.topic ?? event.type ?? ""],
    ["resource", resourceName(event)],
    ["reason", event.reason ?? ""],
    ["message", event.message ?? ""],
  ];
  const rows = [...baseRows, ...eventAttributeEntries(event)].filter(([, value]) => value !== undefined && value !== "");
  return (
    <Card id={id} className={styles.detailPanel}>
      <CardHeader header={<Text weight="semibold">Detail</Text>} description={<Text className={styles.muted}>Event {event.id ?? "-"}</Text>} />
      <div className={styles.detailList}>
        {rows.map(([key, value]) => (
          <React.Fragment key={key}>
            <Text className={styles.detailKey}>{key}</Text>
            <code className={styles.wrapCode}>{formatDetailValue(value)}</code>
          </React.Fragment>
        ))}
      </div>
    </Card>
  );
}

function ConnectionGroup({
  group,
  dnsLabels,
  clientIdentities,
  collapsed,
  toggle,
  page,
  pageSize,
  setPage,
  setPageSize,
  onShowClient,
}: {
  group: { key: string; rows: ConnectionEntry[] };
  dnsLabels: Record<string, string>;
  clientIdentities: Map<string, ClientIdentity>;
  collapsed: boolean;
  toggle: () => void;
  page: number;
  pageSize: number;
  setPage: (page: number) => void;
  setPageSize: (size: number) => void;
  onShowClient: (address?: string) => void;
}) {
  const styles = useStyles();
  const label = connectionGroupLabel(group.key);
  const states = connectionStateSummary(group.rows);
  const apps = connectionAppSummary(group.rows);
  const totalPages = Math.max(1, Math.ceil(group.rows.length / pageSize));
  const currentPage = Math.min(Math.max(page, 0), totalPages - 1);
  const start = currentPage * pageSize;
  const visibleRows = group.rows.slice(start, start + pageSize);
  return (
    <Card id={connectionGroupID(group.key)} className={styles.connectionAnchor}>
      <CardHeader
        header={<Text weight="semibold">{formatConnectionGroupTitle(label)} {group.rows.length}</Text>}
        description={!collapsed ? <Text className={styles.muted}>Showing {visibleRows.length ? start + 1 : 0}-{start + visibleRows.length} of {group.rows.length}</Text> : undefined}
        action={<Button appearance="subtle" icon={collapsed ? <ChevronRightRegular /> : <ChevronDownRegular />} onClick={toggle}>{collapsed ? "Open" : "Close"}</Button>}
      />
      <div className={styles.badges}>
        {states.map(state => <Badge key={state.label} appearance="outline" color={stateColor(state.label)}>{state.label} {state.count}</Badge>)}
        {apps.map(app => <Badge key={app.label} appearance="outline" color={connectionAppColor(app.label)}>{formatConnectionApp(app.label)} {app.count}</Badge>)}
      </div>
      {!collapsed ? (
        <>
          <div className={styles.connectionHeader}>
            <Text className={styles.muted}>Page {currentPage + 1} of {totalPages} / {pageSize} rows per page</Text>
            <div className={styles.pager}>
              <Text className={styles.muted}>Rows</Text>
              <Select className={styles.pageSize} size="small" value={String(pageSize)} onChange={event => setPageSize(Number(event.target.value))}>
                {connectionPageSizeOptions.map(size => <option key={size} value={size}>{size}</option>)}
              </Select>
              <Button size="small" appearance="subtle" disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}>Prev</Button>
              <Button size="small" appearance="subtle" disabled={currentPage >= totalPages - 1} onClick={() => setPage(currentPage + 1)}>Next</Button>
            </div>
          </div>
          <div className={styles.connectionCardList} data-routerd-scroll-key={`connections-${group.key}`}>
            {visibleRows.map(entry => (
              <ConnectionCard key={flowKey(entry)} entry={entry} dnsLabels={dnsLabels} clientIdentities={clientIdentities} onShowClient={onShowClient} />
            ))}
          </div>
        </>
      ) : null}
    </Card>
  );
}

function ConnectionCard({
  entry,
  dnsLabels,
  clientIdentities,
  onShowClient,
}: {
  entry: ConnectionEntry;
  dnsLabels: Record<string, string>;
  clientIdentities: Map<string, ClientIdentity>;
  onShowClient: (address?: string) => void;
}) {
  const styles = useStyles();
  const [expanded, setExpanded] = useState(false);
  const classification = connectionClassification(entry, dnsLabels);
  const proto = (entry.protocol || "?").toUpperCase();
  const family = entry.family || "";
  const orig = entry.original ?? {};
  const sourceLabel = orig.sourceHostname || clientIdentities.get(normalizeAddressKey(orig.source))?.label || (orig.source ? `${orig.source}${orig.sourcePort ? ":" + orig.sourcePort : ""}` : "-");
  const destLabel = orig.destinationHostname || (orig.destination ? `${orig.destination}${orig.destinationPort ? ":" + orig.destinationPort : ""}` : "-");
  const destService = orig.destinationService || "";
  const destServiceApp = connectionServiceApp(destService);
  const traffic = connectionTrafficBytes(entry);
  const provider = connectionDestinationProvider(entry, dnsLabels);
  return (
    <div className={`${styles.connectionCard} ${expanded ? styles.connectionCardExpanded : ""}`}>
      <button type="button" className={styles.connectionCardToggle} aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>
        <div className={styles.connectionCardLine}>
          <div className={styles.connectionCardRoute}>
            <Text className={styles.connectionCardEndpoint} title={sourceLabel}>{sourceLabel}</Text>
            <Text className={styles.connectionCardArrow}>→</Text>
            <Text className={styles.connectionCardEndpoint} title={destLabel}>{destLabel}</Text>
          </div>
          <div className={styles.connectionCardMeta}>
            <Badge appearance="outline" color={family === "ipv6" ? "brand" : "informative"}>{family.toUpperCase() || "L3"}/{proto}</Badge>
            <Badge appearance="tint" color={stateColor(entry.state)}>{entry.state || "stateless"}</Badge>
            {entry.localRedirect ? <Badge appearance="tint" color="warning" title={localRedirectTitle(entry.localRedirect)}>Local redirect</Badge> : null}
            {classification.app ? <Badge appearance={classification.source === "port-fallback" ? "outline" : "tint"} color={connectionAppColor(classification.app)} title={classification.source === "port-fallback" ? "Port-based guess" : classification.cacheHit ? "DPI cache hit" : "DPI"}>{formatConnectionApp(classification.app)}</Badge> : null}
            {provider ? <Badge appearance="outline" color="subtle" title="Destination provider">{formatProviderLabel(provider)}</Badge> : null}
            {destServiceApp && destServiceApp !== classification.app ? <ServiceBadge service={destService} app={destServiceApp} /> : null}
            <Text size={200} className={styles.muted}>
              {hasConnectionAccounting(entry) ? `${formatBytes(traffic)}, ${entry.timeout ?? 0}s` : `${entry.timeout ?? 0}s`}
            </Text>
          </div>
        </div>
      </button>
      {expanded ? (
        <div className={styles.connectionCardDetail}>
          <div className={styles.detailList}>
            <Text className={styles.detailKey}>state</Text>
            <Text>{entry.state || "stateless"}{entry.assured ? " (assured)" : ""} · class {connectionClass(entry)}</Text>
            <Text className={styles.detailKey}>source</Text>
            <div className={styles.connectionFlow}>
              <code className={styles.wrapCode}>{endpoint(entry.original)}</code>
              <ConnectionRemoteIdentity entry={entry} dnsLabels={dnsLabels} clientIdentities={clientIdentities} />
              <ConnectionClientAction address={entry.original?.source} clientIdentities={clientIdentities} assumeLocal={true} onShowClient={onShowClient} />
            </div>
            <Text className={styles.detailKey}>destination</Text>
            <div className={styles.connectionFlow}>
              <RemoteIdentityLabel entry={entry} dnsLabels={dnsLabels} clientIdentities={clientIdentities} />
              <ConnectionClientAction address={entry.original?.destination} clientIdentities={clientIdentities} onShowClient={onShowClient} />
              {destServiceApp || provider ? (
                <div className={styles.badges}>
                  {destServiceApp ? <ServiceBadge service={destService} app={destServiceApp} prefix="service" /> : null}
                  {provider ? <Badge appearance="outline" color="subtle">provider {formatProviderLabel(provider)}</Badge> : null}
                </div>
              ) : null}
            </div>
            {entry.localRedirect ? (
              <>
                <Text className={styles.detailKey}>local redirect</Text>
                <LocalRedirectDetail redirect={entry.localRedirect} />
              </>
            ) : null}
            <Text className={styles.detailKey}>DPI</Text>
            <ConnectionDPI entry={entry} dnsLabels={dnsLabels} />
            <Text className={styles.detailKey}>traffic</Text>
            <Text size={200} className={styles.muted}>
              {hasConnectionAccounting(entry)
                ? `${formatBytes(entry.original?.bytes)} out / ${formatBytes(entry.reply?.bytes)} in / ${formatBytes(traffic)} total`
                : "not accounted"}
            </Text>
            <Text className={styles.detailKey}>flow</Text>
            <Text size={200} className={styles.muted}>protocol {family.toUpperCase() || "?"}/{proto} · timeout {entry.timeout ?? 0}s{entry.mark ? ` · mark ${entry.mark}` : ""}</Text>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function ConnectionClientAction({
  address,
  clientIdentities,
  assumeLocal = false,
  onShowClient,
}: {
  address?: string;
  clientIdentities: Map<string, ClientIdentity>;
  assumeLocal?: boolean;
  onShowClient: (address?: string) => void;
}) {
  const styles = useStyles();
  const normalized = normalizeAddressKey(address);
  const identity = normalized ? clientIdentities.get(normalized) : undefined;
  const canSearch = normalized && (assumeLocal || isLikelyLocalClientAddress(normalized));
  if (!identity && !canSearch) return null;
  return (
    <div className={styles.badges}>
      <Button size="small" appearance={identity ? "secondary" : "subtle"} icon={<PeopleRegular />} onClick={() => onShowClient(normalized)}>
        {identity ? "Client" : "Search clients"}
      </Button>
      <Text size={200} className={styles.muted}>{identity?.compactLabel ?? normalized}</Text>
    </div>
  );
}

function LocalRedirectDetail({ redirect }: { redirect: LocalRedirect }) {
  const styles = useStyles();
  return (
    <div className={styles.connectionFlow}>
      <div className={styles.badges}>
        <Badge appearance="tint" color="warning">Local redirect</Badge>
        {redirect.destinationSetRef ? <Badge appearance="outline">set {redirect.destinationSetRef}</Badge> : null}
        {redirect.ruleName ? <Badge appearance="outline">rule {redirect.ruleName}</Badge> : null}
      </div>
      <Text size={200} className={styles.muted}>
        {[redirect.originalAddress, redirect.redirectAddress ? `router ${redirect.redirectAddress}${redirect.redirectPort ? `:${redirect.redirectPort}` : ""}` : ""].filter(Boolean).join(" -> ")}
      </Text>
    </div>
  );
}

function localRedirectTitle(redirect: LocalRedirect) {
  return [
    "LocalServiceRedirect",
    redirect.resourceName,
    redirect.ruleName,
    redirect.destinationSetRef ? `set ${redirect.destinationSetRef}` : "",
    redirect.match ? `match ${redirect.match}` : "",
  ].filter(Boolean).join(" / ");
}

function ConnectionDPI({ entry, dnsLabels }: { entry: ConnectionEntry; dnsLabels: Record<string, string> }) {
  const styles = useStyles();
  const classification = connectionClassification(entry, dnsLabels);
  if (classification.source === "identifying") {
    return <Text className={styles.identifyingText}>Identifying...</Text>;
  }
  if (classification.source === "none") {
    return <Text className={styles.muted}>-</Text>;
  }
  return (
    <div className={styles.connectionFlow}>
      <div className={styles.badges}>
        <Badge
          appearance={classification.source === "port-fallback" ? "outline" : "tint"}
          color={connectionAppColor(classification.app)}
          title={classification.source === "dpi" ? (classification.cacheHit ? "DPI cache hit" : "DPI") : classification.source === "port-fallback" ? "Port-based guess" : undefined}
        >
          {formatConnectionApp(classification.app)}
        </Badge>
        {classification.confidence ? (
          <Badge appearance="outline" color={classification.source === "port-fallback" || classification.confidence < 50 ? "subtle" : "success"}>
            {classification.confidence}% {classification.source === "port-fallback" ? "guess" : "confidence"}
          </Badge>
        ) : null}
      </div>
      {classification.detail ? <code className={`${styles.wrapCode} ${classification.source === "port-fallback" ? styles.guessText : ""}`}>{classification.detail}</code> : null}
      {classification.category && classification.category !== "port-fallback" ? <Text size={200} className={styles.muted}>{classification.category}</Text> : null}
    </div>
  );
}

function ServiceBadge({ service, app, prefix = "" }: { service: string; app: string; prefix?: string }) {
  const label = formatConnectionService(service, app);
  return <Badge appearance="outline" color={connectionAppColor(app)}>{prefix ? `${prefix} ${label}` : label}</Badge>;
}

function ConnectionRemoteIdentity({ entry, dnsLabels, clientIdentities }: { entry: ConnectionEntry; dnsLabels: Record<string, string>; clientIdentities: Map<string, ClientIdentity> }) {
  const styles = useStyles();
  const identity = connectionInlineIdentity(entry, dnsLabels, clientIdentities);
  if (!identity) return null;
  return (
    <Text size={200} className={styles.connectionDetailIdentity} title={identity}>
      {identity}
    </Text>
  );
}

function RemoteIdentityLabel({ entry, dnsLabels, clientIdentities }: { entry: ConnectionEntry; dnsLabels: Record<string, string>; clientIdentities: Map<string, ClientIdentity> }) {
  const styles = useStyles();
  const identity = destinationIdentity(entry, dnsLabels, clientIdentities);
  if (!identity) return <Text className={styles.muted}>-</Text>;
  const isClientIdentity = Boolean(clientIdentities.get(normalizeAddressKey(entry.original?.destination)));
  return <code className={`${styles.wrapCode} ${styles.connectionDetailIdentity} ${isClientIdentity ? "" : styles.guessText}`} title={identity}>{identity}</code>;
}

function InterfaceOverview({ interfaces }: { interfaces: InterfaceSummary[] }) {
  const styles = useStyles();
  if (interfaces.length === 0) {
    return <Text className={styles.muted}>No interface status is available</Text>;
  }
  return (
    <div className={styles.interfaceGrid}>
      {interfaces.map(item => (
        <div className={styles.interfaceCard} key={`${item.name}-${item.ifname}`}>
          <div className={styles.interfaceHeader}>
            <div className={styles.interfaceName}>
              <Text weight="semibold">{item.name || item.ifname || "-"}</Text>
              <Text size={200} className={styles.muted}> {item.ifname || ""}</Text>
            </div>
            <Badge appearance="tint" color={roleColor(item.role)}>{item.role || "unknown"}</Badge>
          </div>
          <div className={styles.interfaceLine}>
            <Badge appearance="tint" color={phaseColor(item.phase)}>{item.phase || "Unknown"}</Badge>
            {item.zone ? <Badge appearance="outline">{item.zone}</Badge> : null}
            {item.mtu ? <Text size={200} className={styles.muted}>MTU {item.mtu}</Text> : null}
          </div>
          <div className={styles.addressList}>
            {(item.addresses ?? []).map(address => (
              <code className={styles.wrapCode} key={address}>{address}</code>
            ))}
            {(item.addresses ?? []).length === 0 ? <Text size={200} className={styles.muted}>No address observed</Text> : null}
          </div>
          <div className={styles.interfaceLine}>
            {item.hardwareAddress ? <Text size={200} className={styles.muted}>{item.hardwareAddress}</Text> : null}
            {item.owner ? <Text size={200} className={styles.muted}>owner {item.owner}</Text> : null}
            <Text size={200} className={styles.muted}>{item.managed ? "managed by routerd" : "adopted (external)"}</Text>
          </div>
        </div>
      ))}
    </div>
  );
}

function OverviewActivity({
  resources,
  events,
  navigateTo,
}: {
  resources: ResourceStatus[];
  events: RouterEvent[];
  navigateTo: (view: ViewKey, targetID?: string) => void;
}) {
  const styles = useStyles();
  const alerts = resources
    .filter(resource => ["danger", "warning"].includes(phaseColor(resource.status?.phase)))
    .slice(0, 8);
  const recent = events.slice(0, 8);
  return (
    <div id="overview-activity" className={`${styles.sectionGrid} ${styles.connectionAnchor}`}>
      <Card>
        <CardHeader
          header={<Text weight="semibold">Active alerts</Text>}
          description={<Text className={styles.muted}>Resources that are not currently in a healthy applied phase</Text>}
          action={<Button size="small" appearance="secondary" onClick={() => navigateTo("resources")}>Resources</Button>}
        />
        {alerts.length === 0 ? (
          <Text className={styles.muted}>No active resource alerts</Text>
        ) : (
          <div className={styles.alertList}>
            {alerts.map(resource => (
              <div className={styles.alertRow} key={`${resource.kind}/${resource.name}`}>
                <Badge appearance="tint" color={phaseColor(resource.status?.phase)}>{String(resource.status?.phase ?? "Unknown")}</Badge>
                <div className={styles.connectionFlow}>
                  <Text><code className={styles.code}>{resource.kind}/{resource.name}</code></Text>
                  <Text size={200} className={styles.muted}>{resourceDetail(resource.status ?? {}) || "No detail"}</Text>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
      <Card>
        <CardHeader
          header={<Text weight="semibold">Recent changes</Text>}
          description={<Text className={styles.muted}>Latest bus events observed by routerd</Text>}
          action={<Button size="small" appearance="secondary" onClick={() => navigateTo("events")}>Events</Button>}
        />
        {recent.length === 0 ? (
          <Text className={styles.muted}>No recent events</Text>
        ) : (
          <div className={styles.alertList}>
            {recent.map(event => (
              <div className={styles.alertRow} key={eventKey(event)}>
                <Text size={200} className={styles.muted}><RelativeTime value={event.createdAt} /></Text>
                <div className={styles.connectionFlow}>
                  <Text><code className={styles.wrapCode}>{event.topic ?? event.type ?? "-"}</code></Text>
                  <Text size={200} className={styles.muted}>{resourceName(event)}</Text>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

function TailscalePanel({ status, errors }: { status?: TailscaleStatus; errors: string[] }) {
  const styles = useStyles();
  if (!status) {
    return (
      <div className={styles.connectionFlow}>
        <Text className={styles.muted}>Tailscale status is unavailable.</Text>
        {errors.filter(error => error.includes("tailscale")).map(error => <code className={styles.wrapCode} key={error}>{error}</code>)}
      </div>
    );
  }
  const peers = status.peers ?? [];
  return (
    <>
      <div className={styles.vpnSummaryGrid}>
        <Metric label="backend" value={status.backendState || "Unknown"} />
        <Metric label="tailnet" value={status.tailnetName || "-"} />
        <Metric label="MagicDNS" value={status.magicDNSSuffix ? `${status.magicDNSSuffix} (${status.magicDNSEnabled ? "on" : "off"})` : "-"} />
        <Metric label="node" value={status.hostName || status.dnsName || "-"} />
        <Metric label="tailnet ip" value={(status.tailscaleIPs ?? []).join(" / ") || "-"} />
        <Metric label="peers" value={`${peers.filter(peer => peer.online).length} online / ${peers.length} total`} />
      </div>
      <div className={styles.badges}>
        <Badge appearance="tint" color={status.online ? "success" : "danger"}>{status.online ? "online" : "offline"}</Badge>
        {status.active ? <Badge appearance="outline" color="success">active</Badge> : null}
        {status.exitNodeOption ? <Badge appearance="outline" color="brand">exit node</Badge> : null}
        {(status.allowedIPs ?? []).slice(0, 6).map(route => <Badge key={route} appearance="outline">{route}</Badge>)}
        {(status.certDomains ?? []).slice(0, 4).map(domain => <Badge key={domain} appearance="outline" color="informative">{domain}</Badge>)}
      </div>
      <div className={styles.tableWrap} data-routerd-scroll-key="tailscale-peers">
        <Table size="small" className={styles.vpnPeerTable}>
          <colgroup>
            <col style={{ width: "180px" }} />
            <col style={{ width: "118px" }} />
            <col style={{ width: "230px" }} />
            <col />
            <col style={{ width: "110px" }} />
            <col style={{ width: "140px" }} />
          </colgroup>
          <TableHeader>
            <TableRow>
              <TableHeaderCell>Peer</TableHeaderCell>
              <TableHeaderCell>Status</TableHeaderCell>
              <TableHeaderCell>Tailscale IP</TableHeaderCell>
              <TableHeaderCell>Allowed routes</TableHeaderCell>
              <TableHeaderCell>Relay</TableHeaderCell>
              <TableHeaderCell>Last seen</TableHeaderCell>
            </TableRow>
          </TableHeader>
          <TableBody>
            {peers.map(peer => (
              <TableRow key={peer.id || peer.dnsName || peer.hostName} className={styles.stableTallTableRow}>
                <TableCell>
                  <div className={styles.connectionFlow}>
                    <Text>{peer.hostName || "-"}</Text>
                    <Text size={200} className={styles.muted}>{peer.dnsName || peer.id || ""}</Text>
                  </div>
                </TableCell>
                <TableCell>
                  <div className={styles.badges}>
                    <Badge appearance="tint" color={peer.online ? "success" : "subtle"}>{peer.online ? "online" : "offline"}</Badge>
                    {peer.active ? <Badge appearance="outline" color="success">active</Badge> : null}
                    {peer.exitNode || peer.exitNodeOption ? <Badge appearance="outline" color="brand">exit</Badge> : null}
                  </div>
                </TableCell>
                <TableCell><code className={styles.wrapCode}>{(peer.tailscaleIPs ?? []).join(", ") || "-"}</code></TableCell>
                <TableCell><code className={styles.wrapCode}>{(peer.allowedIPs ?? []).join(", ") || "-"}</code></TableCell>
                <TableCell>{peer.relay || "-"}</TableCell>
                <TableCell><RelativeTime value={peer.lastSeen} /></TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </>
  );
}

function WireGuardPanel({ interfaces, errors }: { interfaces: WireGuardInterfaceStatus[]; errors: string[] }) {
  const styles = useStyles();
  if (interfaces.length === 0) {
    return (
      <div className={styles.connectionFlow}>
        <Text className={styles.muted}>No WireGuard interface is currently reported.</Text>
        {errors.filter(error => error.includes("wg show")).map(error => <code className={styles.wrapCode} key={error}>{error}</code>)}
      </div>
    );
  }
  const peerCount = interfaces.reduce((total, item) => total + (item.peers?.length ?? 0), 0);
  const freshPeers = interfaces.reduce((total, item) => total + (item.peers ?? []).filter(peer => handshakeFresh(peer.latestHandshake)).length, 0);
  return (
    <div className={styles.vpnGrid}>
      <div className={styles.vpnSummaryGrid}>
        <Metric label="interfaces" value={String(interfaces.length)} />
        <Metric label="peers" value={String(peerCount)} />
        <Metric label="recent handshakes" value={`${freshPeers}/${peerCount}`} />
      </div>
      {interfaces.map(item => (
        <div key={item.name} className={styles.connectionGroup}>
          <div className={styles.badges}>
            <Badge appearance="tint" color="brand">{item.name || "wg"}</Badge>
            {item.listenPort ? <Badge appearance="outline">udp/{item.listenPort}</Badge> : null}
            {item.fwmark ? <Badge appearance="outline">fwmark {item.fwmark}</Badge> : null}
            <Text size={200} className={styles.muted}>public key {shortHash(item.publicKey)}</Text>
          </div>
          <div className={styles.tableWrap} data-routerd-scroll-key={`wireguard-${item.name || item.publicKey || "interface"}`}>
            <Table size="small" className={styles.vpnPeerTable}>
              <colgroup>
                <col style={{ width: "190px" }} />
                <col style={{ width: "190px" }} />
                <col />
                <col style={{ width: "142px" }} />
                <col style={{ width: "110px" }} />
                <col style={{ width: "110px" }} />
              </colgroup>
              <TableHeader>
                <TableRow>
                  <TableHeaderCell>Peer key</TableHeaderCell>
                  <TableHeaderCell>Endpoint</TableHeaderCell>
                  <TableHeaderCell>Allowed IPs</TableHeaderCell>
                  <TableHeaderCell>Handshake</TableHeaderCell>
                  <TableHeaderCell>RX</TableHeaderCell>
                  <TableHeaderCell>TX</TableHeaderCell>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(item.peers ?? []).map(peer => (
                  <TableRow key={peer.publicKey} className={styles.stableTableRow}>
                    <TableCell><code className={styles.wrapCode}>{shortHash(peer.publicKey)}</code></TableCell>
                    <TableCell><code className={styles.wrapCode}>{peer.endpoint || "-"}</code></TableCell>
                    <TableCell><code className={styles.wrapCode}>{(peer.allowedIPs ?? []).join(", ") || "-"}</code></TableCell>
                    <TableCell><RelativeTime value={peer.latestHandshake} /></TableCell>
                    <TableCell>{formatBytes(peer.transferRxBytes)}</TableCell>
                    <TableCell>{formatBytes(peer.transferTxBytes)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </div>
      ))}
    </div>
  );
}

function ClientInventory({ clients, onShowConnections }: { clients: ClientEntry[]; onShowConnections: (row: ClientRow) => void }) {
  const styles = useStyles();
  const rows = clients.map(clientEntryToRow);
  const online = rows.filter(row => clientOnline(row)).length;
  const addressCount = rows.reduce((sum, row) => sum + row.addresses.size, 0);
  const activeActivities = new Set(rows.map(row => row.primaryActivity).filter(Boolean));
  const sections = clientSections(rows);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [collapsedSections, setCollapsedSections] = useState<Record<string, boolean>>(() => readStoredRecord(clientSectionsCollapsedStorageKey));
  useEffect(() => {
    writeStoredRecord(clientSectionsCollapsedStorageKey, collapsedSections);
  }, [collapsedSections]);
  const toggleExpanded = (key: string) => setExpanded(current => ({ ...current, [key]: !current[key] }));
  const toggleSection = (key: string) => setCollapsedSections(current => ({ ...current, [key]: !current[key] }));
  return (
    <>
      <div className={styles.vpnSummaryGrid}>
        <Metric label="devices" value={String(rows.length)} />
        <Metric label="online" value={`${online}/${rows.length}`} />
        <Metric label="addresses" value={String(addressCount)} />
        <Metric label="activity types" value={String(activeActivities.size)} />
      </div>
      <div className={styles.clientSections}>
        {sections.map(section => {
          const sectionOnline = section.rows.filter(row => clientOnline(row)).length;
          const isSectionCollapsed = !!collapsedSections[section.key];
          return (
            <section className={styles.clientSection} key={section.key}>
              <div className={styles.clientSectionHeader}>
                <button
                  type="button"
                  className={styles.clientSectionToggle}
                  aria-expanded={!isSectionCollapsed}
                  onClick={() => toggleSection(section.key)}
                >
                  <span className={`${styles.clientSectionTitle} ${styles.clientMobileOnly}`}>
                    {isSectionCollapsed ? <ChevronRightRegular /> : <ChevronDownRegular />}
                    <ClientSectionIcon family={section.label} />
                    <span>{section.label}</span>
                    <span className={styles.muted}>({section.rows.length} devices, {sectionOnline} online)</span>
                  </span>
                  <span className={`${styles.clientSectionTitle} ${styles.clientDesktopOnly}`}>
                    {isSectionCollapsed ? <ChevronRightRegular /> : <ChevronDownRegular />}
                    <ClientSectionIcon family={section.label} />
                    <Text weight="semibold">{section.label}</Text>
                    <Text size={200} className={styles.muted}>{section.rows.length} devices · {sectionOnline} online</Text>
                  </span>
                </button>
                <Text size={200} className={styles.muted}>{section.addressCount} addresses</Text>
              </div>
              <div className={styles.clientDeviceList} hidden={isSectionCollapsed}>
                {section.rows.map(row => {
                  const key = clientRowKey(row);
                  const groups = groupedClientAddresses(Array.from(row.addresses));
                  const isExpanded = !!expanded[key];
                  const primaryAddress = primaryClientAddress(row) || "-";
                  return (
                    <div className={`${styles.clientDeviceRow} ${clientOnline(row) ? "" : styles.clientDeviceRowOffline}`} data-client-row="true" key={key}>
                      <Button
                        className={styles.clientDesktopOnly}
                        appearance="subtle"
                        size="small"
                        icon={isExpanded ? <ChevronDownRegular /> : <ChevronRightRegular />}
                        aria-label={isExpanded ? "Collapse client details" : "Expand client details"}
                        onClick={() => toggleExpanded(key)}
                      />
                      <button
                        type="button"
                        className={styles.clientMobileSummary}
                        aria-expanded={isExpanded}
                        aria-label={`${isExpanded ? "Collapse" : "Expand"} ${row.hostname || primaryAddress || "client"} details`}
                        onClick={() => toggleExpanded(key)}
                      >
                        <span className={styles.clientMobileMainLine}>
                          <span className={styles.clientMobileIcon}><ClientDeviceIcon row={row} /></span>
                          <Text weight="semibold" className={styles.clientMobileName}>{row.hostname || row.vendor || row.mac || "unknown client"}</Text>
                          <span className={styles.clientMobileStatus}>
                            <span className={clientOnline(row) ? styles.clientOnlineDot : styles.clientOfflineDot} />
                            <Text size={200}>{clientLastSeen(row)}</Text>
                          </span>
                        </span>
                        <span className={styles.clientMobileSubLine}>
                          <code className={styles.clientMobileIP} title={primaryAddress}>{formatPrimaryClientAddress(primaryAddress)}</code>
                          <span>·</span>
                          <span className={styles.clientMobileMeta}>{formatClientOSFamily(clientOSFamily(row))}</span>
                          {row.clientPolicy ? <><span>·</span><span>{formatClientPolicyMode(row.clientPolicyMode)}</span></> : null}
                          <span>·</span>
                          <span>{row.addresses.size} IPs</span>
                        </span>
                      </button>
                      <div className={`${styles.connectionFlow} ${styles.clientDesktopOnly}`}>
                        <Text weight="semibold">{row.hostname || "unknown client"}</Text>
                        <Text size={200} className={styles.muted}>{row.vendor || row.mac || "-"}</Text>
                        <div className={styles.badges}>
                          <Badge appearance="tint" color={clientOnline(row) ? "success" : "subtle"}>{clientOnline(row) ? "online" : "offline"}</Badge>
                          {row.state ? <Badge appearance="outline">{row.state}</Badge> : null}
                          {row.clientPolicy ? <Badge appearance="tint" color={row.clientPolicyMode === "trusted" ? "success" : "warning"}>{formatClientPolicyMode(row.clientPolicyMode)}</Badge> : null}
                        </div>
                      </div>
                      <div className={`${styles.clientPrimaryIPCell} ${styles.clientDesktopOnly}`}>
                        <Text size={200} className={styles.muted}>Primary IP</Text>
                        <code className={styles.clientPrimaryIPCode} title={primaryAddress}>
                          {formatPrimaryClientAddress(primaryAddress)}
                        </code>
                      </div>
                      <div className={styles.clientDesktopOnly}><ClientOSBadge row={row} /></div>
                      <div className={styles.clientDesktopOnly}><ClientActivityBadge row={row} /></div>
                      <div className={`${styles.connectionFlow} ${styles.clientDesktopOnly}`}>
                        <Text size={200} className={styles.muted}>Addresses</Text>
                        <Text>{row.addresses.size}</Text>
                        <Button size="small" appearance="subtle" icon={<PlugConnectedRegular />} onClick={() => onShowConnections(row)}>Connections</Button>
                      </div>
                      {isExpanded ? (
                        <div className={styles.clientDeviceDetails}>
                          <div className={styles.clientDetailStack}>
                            <ClientAddressGroup label="IPv4" addresses={groups.ipv4} />
                            <ClientAddressGroup label="IPv6 stable" addresses={groups.ipv6Stable} />
                            <ClientAddressGroup label="IPv6 privacy" addresses={groups.ipv6Privacy} />
                            <div className={styles.clientMetaLine}>
                              <code className={styles.wrapCode}>{row.mac || "-"}</code>
                              <Text size={200} className={styles.muted}>last seen {clientLastSeen(row)}</Text>
                              {row.primaryActivity ? <Text size={200} className={styles.muted}>activity {formatClientActivity(row.primaryActivity)}</Text> : null}
                              {row.lastProtocol ? <Text size={200} className={styles.muted}>last protocol {formatConnectionApp(row.lastProtocol)}{row.lastProtocolDetail ? ` ${row.lastProtocolDetail}` : ""}</Text> : null}
                              {row.protocolMix.size > 0 ? <Text size={200} className={styles.muted}>protocol mix {Array.from(row.protocolMix).map(formatConnectionApp).join(", ")}</Text> : null}
                              <Text size={200} className={styles.muted}>out {formatBytes(row.bytesOut)} / in {formatBytes(row.bytesIn)}</Text>
                              {row.stickyUntil ? <Text size={200} className={styles.muted}>sticky until <RelativeTime value={row.stickyUntil} /></Text> : null}
                              {row.clientPolicy ? <Text size={200} className={styles.muted}>policy {row.clientPolicy}: {Array.from(row.isolationPolicy).join(", ")}</Text> : null}
                              {row.sources.size > 0 ? <Text size={200} className={styles.muted}>sources {Array.from(row.sources).join(", ")}</Text> : null}
                              {row.fingerprintSignals.size > 0 ? <Text size={200} className={styles.muted}>signals {Array.from(row.fingerprintSignals).slice(0, 5).join(", ")}</Text> : null}
                              {row.peers.size > 0 ? <Text size={200} className={styles.muted}>peers {Array.from(row.peers).slice(0, 5).join(", ")}</Text> : null}
                              <Button size="small" appearance="secondary" icon={<PlugConnectedRegular />} onClick={() => onShowConnections(row)}>Connections</Button>
                            </div>
                          </div>
                        </div>
                      ) : null}
                    </div>
                  );
                })}
              </div>
            </section>
          );
        })}
      </div>
    </>
  );
}

function ClientActivityBadge({ row }: { row: ClientRow }) {
  const styles = useStyles();
  if (!row.primaryActivity && row.protocolMix.size === 0) return <Text className={styles.muted}>-</Text>;
  return (
    <div className={styles.connectionFlow}>
      <Text size={200} className={styles.muted}>Activity</Text>
      <div className={styles.badges}>
        {row.primaryActivity ? <Badge appearance="tint" color={clientActivityColor(row.primaryActivity)}>{formatClientActivity(row.primaryActivity)}</Badge> : null}
        {Array.from(row.protocolMix).slice(0, 3).map(protocol => (
          <Badge appearance="outline" color={connectionAppColor(protocol)} key={protocol}>{formatConnectionApp(protocol)}</Badge>
        ))}
      </div>
    </div>
  );
}

function ClientOSBadge({ row }: { row: ClientRow }) {
  const styles = useStyles();
  const family = clientOSFamily(row);
  if (family === "-") return <Text className={styles.muted}>-</Text>;
  return (
    <div className={styles.badges}>
      <Badge appearance="tint" color={clientOSBadgeColor(family)}>{formatClientOSFamily(family)}</Badge>
      {row.inferredDeviceClass ? <Badge appearance="outline">{row.inferredDeviceClass}</Badge> : null}
      {row.fingerprintConfidence ? <Text size={200} className={styles.muted}>{row.fingerprintConfidence}%</Text> : null}
    </div>
  );
}

function ClientSectionIcon({ family }: { family: string }) {
  const normalized = family.trim().toLowerCase();
  let icon: React.ReactNode = <ServerRegular />;
  if (normalized === "nintendo" || normalized === "playstation" || normalized === "xbox" || normalized === "steamos") icon = <GamesRegular />;
  else if (normalized === "printer") icon = <PrintRegular />;
  else if (normalized === "nas") icon = <DatabaseRegular />;
  else if (normalized === "voip") icon = <PhoneRegular />;
  else if (normalized === "iot" || normalized === "embedded") icon = <HomeRegular />;
  else if (normalized === "android" || normalized === "apple") icon = <PhoneRegular />;
  else if (normalized === "windows" || normalized === "linux") icon = <DesktopRegular />;
  return <span style={{ color: clientOSIconColor(normalized), display: "inline-flex" }} aria-hidden="true">{icon}</span>;
}

function clientOSIconColor(family: string): string {
  switch (family) {
    case "apple": return tokens.colorBrandForeground1;
    case "windows": return tokens.colorPaletteTealForeground2;
    case "linux": return tokens.colorPaletteYellowForeground1;
    case "android": return tokens.colorPaletteLightGreenForeground1;
    case "nintendo": return tokens.colorPaletteRedForeground1;
    case "playstation": return tokens.colorPaletteBlueForeground2;
    case "xbox": return tokens.colorPaletteGreenForeground1;
    case "steamos":
    case "steam-os": return tokens.colorPaletteDarkOrangeForeground1;
    case "iot":
    case "embedded": return tokens.colorPaletteLightGreenForeground1;
    case "printer":
    case "nas":
    case "voip": return tokens.colorPalettePurpleForeground2;
    default: return tokens.colorNeutralForeground3;
  }
}

function ClientDeviceIcon({ row }: { row: ClientRow }) {
  const deviceClass = row.inferredDeviceClass.trim().toLowerCase();
  const family = clientOSFamily(row).trim().toLowerCase();
  switch (deviceClass) {
    case "phone":
      return <PhoneRegular />;
    case "tablet":
      return <TabletRegular />;
    case "laptop":
      return <LaptopRegular />;
    case "desktop":
    case "computer":
      return <DesktopRegular />;
    case "smart-tv":
    case "media":
      return <TvRegular />;
    case "smart-speaker":
      return <Speaker2Regular />;
    case "gaming-console":
      return <GamesRegular />;
    case "printer":
      return <PrintRegular />;
    case "camera":
      return <CameraRegular />;
    case "nas":
      return <DatabaseRegular />;
    case "ev":
      return <VehicleCarRegular />;
    case "iot":
    case "lighting":
    case "vacuum":
      return <HomeRegular />;
    case "voip":
      return <PhoneRegular />;
    default:
      if (family === "nintendo" || family === "playstation" || family === "xbox" || family === "steam-os") return <GamesRegular />;
      if (family === "printer") return <PrintRegular />;
      if (family === "nas") return <DatabaseRegular />;
      if (family === "voip") return <PhoneRegular />;
      if (family === "iot" || family === "embedded") return <HomeRegular />;
      if (family === "android" || family === "apple") return <PhoneRegular />;
      if (family === "windows" || family === "linux") return <DesktopRegular />;
      return <ServerRegular />;
  }
}

function ClientAddressGroup({ label, addresses }: { label: string; addresses: string[] }) {
  const styles = useStyles();
  if (addresses.length === 0) return null;
  return (
    <div className={styles.clientAddressGroup}>
      <Text size={200} className={styles.muted}>{label}</Text>
      <div className={styles.clientAddressList}>
        {addresses.slice(0, 6).map(address => <code className={styles.clientAddressCode} key={address}>{address}</code>)}
        {addresses.length > 6 ? <Badge appearance="outline">+{addresses.length - 6}</Badge> : null}
      </div>
    </div>
  );
}

function ClientTraffic({ flows, onShowConnectionsForAddress }: { flows: TrafficFlow[]; onShowConnectionsForAddress: (address: string) => void }) {
  const styles = useStyles();
  const [source, setSource] = useState("all");
  const sourceOptions = useMemo(() => {
    const values = new Set<string>();
    for (const flow of flows) values.add(trafficFlowClassification(flow).source);
    return Array.from(values).sort(facetSort);
  }, [flows]);
  const filtered = useMemo(() => {
    if (source === "all") return flows;
    return flows.filter(flow => trafficFlowClassification(flow).source === source);
  }, [flows, source]);
  return (
    <>
      <div className={styles.clientFilters}>
        <div className={styles.filterControl}>
          <Text size={200} className={styles.muted}>Source</Text>
          <Select size="small" value={source} onChange={event => setSource(event.target.value)}>
            <option value="all">All</option>
            {sourceOptions.map(value => <option key={value} value={value}>{formatTrafficSourceLabel(value)}</option>)}
          </Select>
        </div>
      </div>
      <div className={styles.tableWrap} data-routerd-scroll-key="client-traffic">
      <Table size="small" className={styles.clientTrafficTable}>
        <colgroup>
          <col style={{ width: "170px" }} />
          <col style={{ width: "96px" }} />
          <col style={{ width: "96px" }} />
          <col style={{ width: "190px" }} />
          <col />
        </colgroup>
        <TableHeader>
          <TableRow>
            <TableHeaderCell>Client</TableHeaderCell>
            <TableHeaderCell>Bytes out</TableHeaderCell>
            <TableHeaderCell>Bytes in</TableHeaderCell>
            <TableHeaderCell>Protocol mix</TableHeaderCell>
            <TableHeaderCell>Peers</TableHeaderCell>
          </TableRow>
        </TableHeader>
        <TableBody>
          {clientTrafficRows(filtered).map(row => (
            <TableRow key={row.client} className={styles.stableTableRow}>
              <TableCell>
                <div className={styles.connectionFlow}>
                  <code className={styles.code}>{row.client}</code>
                  <Button size="small" appearance="subtle" icon={<PlugConnectedRegular />} onClick={() => onShowConnectionsForAddress(row.client)}>Connections</Button>
                </div>
              </TableCell>
              <TableCell>{formatBytes(row.bytesOut)}</TableCell>
              <TableCell>{formatBytes(row.bytesIn)}</TableCell>
              <TableCell>
                <div className={styles.badges}>
                  {Array.from(row.protocols).slice(0, 3).map(protocol => (
                    <Badge appearance="outline" color={connectionAppColor(protocol)} key={protocol}>{formatConnectionApp(protocol)}</Badge>
                  ))}
                  {row.protocols.size === 0 ? <Text className={styles.muted}>-</Text> : null}
                </div>
              </TableCell>
              <TableCell><code className={styles.wrapCode}>{Array.from(row.peers).slice(0, 3).join(", ") || "-"}</code></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
    </>
  );
}

function DHCPLeaseTable({ leases }: { leases: DHCPLease[] }) {
  const styles = useStyles();
  const rows = [...leases].sort((a, b) => stringSort(a.ip ?? "", b.ip ?? ""));
  return (
    <div className={styles.tableWrap} data-routerd-scroll-key="dhcp-leases">
      <Table size="small" className={styles.dhcpLeaseTable}>
        <colgroup>
          <col style={{ width: "82px" }} />
          <col style={{ width: "250px" }} />
          <col />
          <col style={{ width: "150px" }} />
          <col style={{ width: "170px" }} />
          <col style={{ width: "112px" }} />
          <col style={{ width: "132px" }} />
        </colgroup>
        <TableHeader>
          <TableRow>
            <TableHeaderCell>Family</TableHeaderCell>
            <TableHeaderCell>IP</TableHeaderCell>
            <TableHeaderCell>Hostname</TableHeaderCell>
            <TableHeaderCell>MAC</TableHeaderCell>
            <TableHeaderCell>Vendor</TableHeaderCell>
            <TableHeaderCell>Expires</TableHeaderCell>
            <TableHeaderCell>Sticky</TableHeaderCell>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map(lease => (
            <TableRow key={`${lease.ip}-${lease.mac}`} className={styles.stableTableRow}>
              <TableCell><Badge appearance="tint" color={lease.family === "ipv6" ? "brand" : "success"}>{lease.family || "-"}</Badge></TableCell>
              <TableCell><code className={styles.wrapCode}>{lease.ip || "-"}</code></TableCell>
              <TableCell>{lease.hostname || "-"}</TableCell>
              <TableCell><code className={styles.wrapCode}>{lease.mac || "-"}</code></TableCell>
              <TableCell>{lease.vendor || "-"}</TableCell>
              <TableCell><RelativeTime value={lease.expiresAt} /></TableCell>
              <TableCell>{lease.stickyUntil ? <Text size={200} className={styles.muted}>until <RelativeTime value={lease.stickyUntil} /></Text> : "-"}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function ConntrackTuningView({ tuning }: { tuning?: ConntrackTuningSummary }) {
  const styles = useStyles();
  const suggestions = tuning?.suggestions ?? [];
  if (suggestions.length === 0) {
    return <Text className={styles.muted}>No tuning suggestions from the current observation window</Text>;
  }
  return (
    <div className={styles.tuningGrid} data-routerd-scroll-key="firewall-tuning-table">
      <div className={styles.badges}>
        <Badge appearance="tint" color={tuning?.autoApply ? "warning" : "success"}>{tuning?.applyMode === "auto" ? "auto apply enabled" : "manual apply only"}</Badge>
        <Badge appearance="outline">window {tuning?.window || "24h"}</Badge>
      </div>
      <div className={styles.tuningHeader}>
        <span>Application</span>
        <span>Recommended</span>
        <span>Baseline</span>
        <span>Orphan</span>
        <span>Flows</span>
        <span>Reason</span>
      </div>
      {suggestions.map(row => (
        <div className={styles.tuningRow} key={`${row.protocol}-${row.application}-${row.sysctlKey}`}>
          <FirewallCell label="Application">
            <div className={styles.connectionFlow}>
              <Badge appearance="tint" color={connectionAppColor(row.application || "unidentified")}>{formatConnectionApp(row.application || "unidentified")}</Badge>
              <code className={styles.wrapCode}>{row.sysctlKey || "-"}</code>
            </div>
          </FirewallCell>
          <FirewallCell label="Recommended">{formatSeconds(row.recommendedSeconds)}</FirewallCell>
          <FirewallCell label="Baseline">{formatSeconds(row.baselineSeconds)}</FirewallCell>
          <FirewallCell label="Orphan">{formatPercent(row.orphanRate)} / {row.orphanReturns ?? 0}</FirewallCell>
          <FirewallCell label="Flows">{row.observedFlows ?? 0} seen / {row.expiredFlows ?? 0} expired</FirewallCell>
          <FirewallCell label="Reason">
            <div className={styles.connectionFlow}>
              <Text>{row.rationale || "-"}</Text>
              <Text size={200} className={styles.muted}>{row.productionApplyGuard || "manual approval required before sysctl apply"}</Text>
            </div>
          </FirewallCell>
        </div>
      ))}
    </div>
  );
}

function RecentDeny({
  logs,
  dnsLabels,
  leases,
}: {
  logs: FirewallLog[];
  dnsLabels: Record<string, string>;
  leases: Record<string, DHCPLease>;
}) {
  const styles = useStyles();
  const rows = useFrozenRowOrder(denyRows(logs), row => row.key);
  return (
    <div className={styles.firewallTable} role="table" aria-label="Deny ranking" data-routerd-scroll-key="firewall-ranking-table">
      <div className={styles.firewallRankHeader} role="row">
        <span>Count</span>
        <span>Source</span>
        <span>Destination</span>
        <span>Proto</span>
        <span>Flags</span>
        <span>Traffic</span>
        <span>Class</span>
        <span>DPI</span>
      </div>
      {rows.map(row => (
        <div className={styles.firewallRankRow} role="row" key={`${row.key}-${row.dpi}`}>
          <FirewallCell label="Count">{row.count}</FirewallCell>
          <FirewallCell label="Source"><EndpointDetail address={row.src} dnsLabels={dnsLabels} leases={leases} /></FirewallCell>
          <FirewallCell label="Destination">
            <EndpointDetail address={row.dst} dnsLabels={dnsLabels} leases={leases} />
            <FirewallDestinationSetBadges matches={row.example?.destinationSetMatches} />
          </FirewallCell>
          <FirewallCell label="Proto">{row.proto}</FirewallCell>
          <FirewallCell label="Flags"><code className={styles.wrapCode}>{row.tcpFlags || "-"}</code></FirewallCell>
          <FirewallCell label="Traffic"><FirewallTrafficClassBadge value={row.trafficClass} /></FirewallCell>
          <FirewallCell label="Class"><FirewallCorrelationBadge correlation={row.correlation} /></FirewallCell>
          <FirewallCell label="DPI"><FirewallDPI log={row.example} dnsLabels={dnsLabels} /></FirewallCell>
        </div>
      ))}
    </div>
  );
}

function DenyRateChart({ timeline }: { timeline: FirewallDenyTimelineBucket[] }) {
  const styles = useStyles();
  const samples = denyTimelineSamples(timeline);
  const max = Math.max(0, ...samples);
  const total = samples.reduce((sum, value) => sum + value, 0);
  return (
    <div className={styles.firewallChartWrap}>
      <Sparkline samples={samples} color="#ff8a80" />
      <Text size={200} className={styles.muted}>{total} denies / peak {max} per 5 min bucket</Text>
    </div>
  );
}

function FirewallSourceTopN({
  logs,
  dnsLabels,
  leases,
  onSourceClick,
}: {
  logs: FirewallLog[];
  dnsLabels: Record<string, string>;
  leases: Record<string, DHCPLease>;
  onSourceClick?: (ip: string) => void;
}) {
  const styles = useStyles();
  const rows = useFrozenRowOrder(sourceTopRows(logs), row => row.source);
  const max = Math.max(1, ...rows.map(row => row.count));
  if (rows.length === 0) return <Text className={styles.muted}>No deny rows match the current filters</Text>;
  return (
    <div className={styles.firewallTopN}>
      {rows.map((row, index) => (
        <div className={styles.firewallTopRow} key={row.source}>
          <Text weight="semibold">#{index + 1}</Text>
          <div className={styles.connectionFlow}>
            <EndpointDetail address={row.source} dnsLabels={dnsLabels} leases={leases} />
            <div className={styles.firewallBar} style={{ width: `${Math.max(3, (row.count / max) * 100)}%` }} />
          </div>
          <Text>{row.count}</Text>
          {onSourceClick ? <Button size="small" appearance="subtle" icon={<FilterRegular />} aria-label={`Filter to source ${row.source}`} onClick={() => onSourceClick(row.source)}>Filter</Button> : null}
        </div>
      ))}
    </div>
  );
}

function FirewallTimeline({
  logs,
  dnsLabels,
  leases,
}: {
  logs: FirewallLog[];
  dnsLabels: Record<string, string>;
  leases: Record<string, DHCPLease>;
}) {
  const styles = useStyles();
  return (
    <div className={styles.firewallTable} role="table" aria-label="Deny timeline" data-routerd-scroll-key="firewall-timeline-table">
      <div className={styles.firewallTimelineHeader} role="row">
        <span>Time</span>
        <span>Action</span>
        <span>Source</span>
        <span>Destination</span>
        <span>Proto</span>
        <span>Flags</span>
        <span>Traffic</span>
        <span>Class</span>
        <span>DPI</span>
        <span>Rule</span>
      </div>
      {logs.slice(0, 50).map((log, index) => (
        <div className={styles.firewallTimelineRow} role="row" key={log.id ?? `${log.ts}-${log.srcAddress}-${log.dstAddress}-${index}`}>
          <FirewallCell label="Time"><RelativeTime value={log.ts} /></FirewallCell>
          <FirewallCell label="Action"><Badge appearance="tint" color={firewallActionColor(log.action)}>{log.action || "-"}</Badge></FirewallCell>
          <FirewallCell label="Source"><EndpointDetail address={log.srcAddress} port={log.srcPort} hostname={log.srcHostname} service={log.srcService} dnsLabels={dnsLabels} leases={leases} /></FirewallCell>
          <FirewallCell label="Destination">
            <EndpointDetail address={log.dstAddress} port={log.dstPort} hostname={log.dstHostname} service={log.dstService} dnsLabels={dnsLabels} leases={leases} />
            <FirewallDestinationSetBadges matches={log.destinationSetMatches} />
          </FirewallCell>
          <FirewallCell label="Proto">{[log.l3Proto, log.protocol].filter(Boolean).join("/") || "-"}</FirewallCell>
          <FirewallCell label="Flags"><code className={styles.wrapCode}>{firewallTCPFlags(log) || "-"}</code></FirewallCell>
          <FirewallCell label="Traffic"><FirewallTrafficClassBadge value={firewallTrafficClass(log)} /></FirewallCell>
          <FirewallCell label="Class"><FirewallCorrelationBadge correlation={firewallCorrelation(log)} /></FirewallCell>
          <FirewallCell label="DPI"><FirewallDPI log={log} dnsLabels={dnsLabels} /></FirewallCell>
          <FirewallCell label="Rule"><code className={styles.wrapCode}>{log.ruleName || "-"}</code></FirewallCell>
        </div>
      ))}
    </div>
  );
}

function FirewallCell({ label, children }: { label: string; children: React.ReactNode }) {
  const styles = useStyles();
  return (
    <div className={styles.firewallCell} role="cell">
      <Text className={styles.firewallCellLabel}>{label}</Text>
      <div className={styles.firewallCellValue}>{children}</div>
    </div>
  );
}

function FirewallDestinationSetBadges({ matches }: { matches?: AddressSetMatch[] }) {
  const styles = useStyles();
  const visible = (matches ?? []).filter(match => match.resourceName || match.setName).slice(0, 3);
  if (visible.length === 0) return null;
  return (
    <div className={styles.badges}>
      {visible.map((match, index) => (
        <Badge
          key={`${match.setName ?? match.resourceName ?? "set"}-${index}`}
          appearance={match.source === "firewall-rule" ? "tint" : "outline"}
          color={match.source === "firewall-rule" ? "brand" : "subtle"}
          title={firewallDestinationSetTitle(match)}
        >
          FQDN set {match.resourceName || match.setName}
        </Badge>
      ))}
      {(matches ?? []).length > visible.length ? <Badge appearance="outline">+{(matches ?? []).length - visible.length}</Badge> : null}
    </div>
  );
}

function firewallDestinationSetTitle(match: AddressSetMatch) {
  return [
    match.source === "firewall-rule" ? "Firewall rule destinationSetRefs" : "Destination currently in IPAddressSet",
    match.resourceName || match.setName,
    match.current ? "current address match" : "",
  ].filter(Boolean).join(" / ");
}

function FirewallCorrelationBadge({ correlation }: { correlation?: string }) {
  const value = correlation || "true_suspicious";
  if (value === "orphan_return") return <Badge appearance="tint" color="warning">orphan return</Badge>;
  if (value === "true_suspicious") return <Badge appearance="tint" color="danger">true suspicious</Badge>;
  return <Badge appearance="outline">{value}</Badge>;
}

function FirewallTrafficClassBadge({ value }: { value: string }) {
  return <Badge appearance="tint" color={firewallTrafficClassColor(value)}>{value || "unclassified"}</Badge>;
}

function FirewallDPI({ log, dnsLabels }: { log?: FirewallLog; dnsLabels: Record<string, string> }) {
  const styles = useStyles();
  if (!log) return <Text className={styles.muted}>-</Text>;
  const classification = firewallDPIClassification(log, dnsLabels);
  if (classification.source === "port-fallback") {
    return (
      <div className={styles.connectionFlow}>
        <div className={styles.badges}>
          <Badge appearance="outline" color="subtle">Port guess</Badge>
          <Badge appearance="outline" color="subtle">{classification.confidence || 30}% guess</Badge>
        </div>
        <code className={`${styles.wrapCode} ${styles.guessText}`}>{classification.detail}</code>
      </div>
    );
  }
  if (!classification.detail) return <Text className={styles.muted}>-</Text>;
  return (
    <div className={styles.connectionFlow}>
      <div className={styles.badges}>
        <Badge appearance="outline" color="success">{classification.cacheHit ? "DPI cache" : "DPI"}</Badge>
        {classification.confidence ? <Badge appearance="outline" color={classification.confidence < 50 ? "warning" : "success"}>{classification.confidence}% confidence</Badge> : null}
      </div>
      <code className={styles.wrapCode}>{classification.detail}</code>
    </div>
  );
}

function EndpointDetail({
  address,
  port,
  hostname,
  service,
  dnsLabels,
  leases,
}: {
  address?: string;
  port?: number;
  hostname?: string;
  service?: string;
  dnsLabels: Record<string, string>;
  leases: Record<string, DHCPLease>;
}) {
  const styles = useStyles();
  const lease = address ? leases[address] : undefined;
  const label = lease?.hostname || hostname || (address ? dnsLabels[address] : "");
  const vendor = lease?.vendor || "";
  const serviceLabel = service || serviceNameForPort(port ? String(port) : undefined);
  return (
    <div className={styles.connectionFlow}>
      <code className={styles.wrapCode}>{firewallEndpoint(address, port)}</code>
      {label || serviceLabel || lease?.mac || vendor ? (
        <Text size={200} className={styles.muted}>
          {[label, serviceLabel ? `service ${serviceLabel}` : "", lease?.mac, vendor].filter(Boolean).join(" / ")}
        </Text>
      ) : null}
    </div>
  );
}

async function fetchJSON<T>(path: string): Promise<T> {
  const response = await fetch(basePath + path, { cache: "no-store" });
  if (!response.ok) throw new Error(`${path}: ${response.status}`);
  return response.json() as Promise<T>;
}

async function fetchText(path: string): Promise<string> {
  const response = await fetch(basePath + path, { cache: "no-store" });
  if (!response.ok) throw new Error(`${path}: ${response.status}`);
  return response.text();
}

function reconcileSummary(current: Summary | null, next: Summary): Summary {
  if (!current) return next;
  return {
    ...next,
    controllers: reconcileRecords(current.controllers, next.controllers, row => row.name ?? ""),
    resources: next.resources === undefined ? current.resources : reconcileRecords(current.resources, next.resources, row => `${row.apiVersion ?? ""}/${row.kind ?? ""}/${row.name ?? ""}`),
    interfaces: next.interfaces === undefined ? current.interfaces : reconcileRecords(current.interfaces, next.interfaces, row => row.ifname ?? row.name ?? ""),
    events: next.events === undefined ? current.events : reconcileRecords(current.events, next.events, row => eventKey(row)),
    connections: next.connections === undefined ? current.connections : reconcileConnectionTable(current.connections, next.connections),
    dnsQueries: next.dnsQueries === undefined ? current.dnsQueries : reconcileRecords(current.dnsQueries, next.dnsQueries, row => `${row.questionName ?? ""}/${(row.answers ?? []).join(",")}`),
    trafficFlows: next.trafficFlows === undefined ? current.trafficFlows : reconcileRecords(current.trafficFlows, next.trafficFlows, row => `${row.clientAddress ?? ""}/${row.peerAddress ?? ""}/${row.resolvedHostname ?? ""}/${row.tlsSNI ?? ""}`),
    firewallLogs: next.firewallLogs === undefined ? current.firewallLogs : reconcileRecords(current.firewallLogs, next.firewallLogs, row => String(row.id ?? `${row.ts ?? ""}/${row.srcAddress ?? ""}/${row.dstAddress ?? ""}/${row.protocol ?? ""}`)),
    conntrackTuning: next.conntrackTuning ?? current.conntrackTuning,
    dhcpLeases: next.dhcpLeases === undefined ? current.dhcpLeases : reconcileRecords(current.dhcpLeases, next.dhcpLeases, row => `${row.family ?? ""}/${row.ip ?? ""}/${row.mac ?? ""}`),
    dhcpFingerprints: next.dhcpFingerprints === undefined ? current.dhcpFingerprints : reconcileRecords(current.dhcpFingerprints, next.dhcpFingerprints, row => `${row.mac ?? ""}/${row.observedAt ?? ""}`),
    neighbors: next.neighbors === undefined ? current.neighbors : reconcileRecords(current.neighbors, next.neighbors, row => `${row.ip ?? ""}/${row.mac ?? ""}/${row.ifname ?? ""}`),
    clients: next.clients === undefined ? current.clients : reconcileRecords(current.clients, next.clients, row => row.id ?? row.mac ?? row.hostname ?? (row.addresses ?? []).join(",")),
    vpn: next.vpn === undefined ? current.vpn : reconcileVPNStatus(current.vpn, next.vpn),
  };
}

function reconcileConnectionTable(current?: ConnectionTable, next?: ConnectionTable): ConnectionTable | undefined {
  if (!next) return next;
  return {
    ...next,
    entries: reconcileRecords(current?.entries, next.entries, row => flowKey(row)),
  };
}

function reconcileVPNStatus(current?: VPNStatus, next?: VPNStatus): VPNStatus | undefined {
  if (!next) return next;
  return {
    ...next,
    wireGuard: reconcileRecords(current?.wireGuard, next.wireGuard, row => row.name ?? ""),
    tailscale: next.tailscale
      ? {
          ...next.tailscale,
          peers: reconcileRecords(current?.tailscale?.peers, next.tailscale.peers, row => row.id ?? row.dnsName ?? row.hostName ?? (row.tailscaleIPs ?? []).join(",")),
        }
      : next.tailscale,
  };
}

function reconcileRecords<T>(current: T[] | undefined, next: T[] | undefined, keyFn: (row: T) => string): T[] {
  if (!next) return [];
  if (!current || current.length === 0) return next;
  const previous = new Map<string, T>();
  const incoming = new Map<string, T>();
  const used = new Set<string>();
  for (const row of current) {
    const key = keyFn(row);
    if (key) previous.set(key, row);
  }
  for (const row of next) {
    const key = keyFn(row);
    if (key) incoming.set(key, row);
  }
  const rows: T[] = [];
  for (const oldRow of current) {
    const key = keyFn(oldRow);
    const row = key ? incoming.get(key) : undefined;
    if (!key || !row) continue;
    used.add(key);
    rows.push(stableJSON(oldRow) === stableJSON(row) ? oldRow : row);
  }
  for (const row of next) {
    const key = keyFn(row);
    if (key && used.has(key)) continue;
    rows.push(row);
  }
  return rows;
}

function stableJSON(value: unknown): string {
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function normalizeBasePath(value: string) {
  let base = value || "/";
  if (!base.startsWith("/")) base = `/${base}`;
  if (!base.endsWith("/")) base = `${base}/`;
  return base;
}

function phaseColor(phase: unknown): "success" | "warning" | "danger" | "informative" | "subtle" {
  const text = String(phase ?? "");
  if (/Disabled|Standby|NotApplicable/.test(text)) return "subtle";
  if (/Healthy|Applied|Active|Bound|Installed|Ready|Running|Up|Observed/.test(text)) return "success";
  if (/Pending|Drifted|Unknown/.test(text)) return "warning";
  if (/Error|Failed|Down|Unhealthy/.test(text)) return "danger";
  return "informative";
}

function neutralPhase(phase: unknown) {
  return /Disabled|Standby|NotApplicable/.test(String(phase ?? ""));
}

function stateColor(state: unknown): "success" | "warning" | "informative" | "subtle" {
  const text = String(state ?? "").toLowerCase();
  if (/established|assured/.test(text)) return "success";
  if (/syn|unreplied/.test(text)) return "warning";
  if (/time_wait|close/.test(text)) return "subtle";
  return "informative";
}

function firewallActionColor(action: unknown): "success" | "warning" | "danger" | "informative" | "subtle" {
  const text = String(action ?? "").toLowerCase();
  if (text === "accept") return "success";
  if (text === "reject") return "warning";
  if (text === "drop" || text === "deny") return "danger";
  return "informative";
}

function roleColor(role: unknown): "success" | "warning" | "danger" | "informative" | "brand" {
  const text = String(role ?? "").toLowerCase();
  if (text === "trust") return "success";
  if (text === "untrust") return "danger";
  if (text === "mgmt") return "brand";
  return "informative";
}

function importantResources(resources: ResourceStatus[]) {
  return resources.filter(resource => /EgressRoutePolicy|HealthCheck|DNSResolver|DHCP|DSLiteTunnel|NAT44Rule|IPv4Route|Firewall|WireGuard|VXLAN/.test(resource.kind ?? ""));
}

function conntrackLabel(table?: ConnectionTable) {
  if (!table) return "-";
  if (table.max) return `${table.count ?? 0}/${table.max}`;
  return String(table.count ?? "-");
}

function connectionFamilyCount(table: ConnectionTable | undefined, family: string): number {
  if (!table) return 0;
  const want = family.toLowerCase();
  if (table.byFamily && Object.keys(table.byFamily).length > 0) {
    let total = 0;
    for (const [key, value] of Object.entries(table.byFamily)) {
      if (key.toLowerCase() === want) total += Number(value || 0);
    }
    return total;
  }
  return (table.entries ?? []).filter(entry => String(entry.family ?? "").toLowerCase() === want).length;
}

function connectionFamilyCounts(table?: ConnectionTable) {
  if (!table) return "-";
  const counts = { ipv4: 0, ipv6: 0, other: 0 };
  if (table?.byFamily && Object.keys(table.byFamily).length > 0) {
    for (const [family, value] of Object.entries(table.byFamily)) {
      const key = family.toLowerCase();
      if (key === "ipv4") counts.ipv4 += Number(value || 0);
      else if (key === "ipv6") counts.ipv6 += Number(value || 0);
      else counts.other += Number(value || 0);
    }
  } else {
    for (const entry of table?.entries ?? []) {
      const family = String(entry.family ?? "").toLowerCase();
      if (family === "ipv4") counts.ipv4++;
      else if (family === "ipv6") counts.ipv6++;
      else counts.other++;
    }
  }
  return `IPv4 ${counts.ipv4} / IPv6 ${counts.ipv6}${counts.other ? ` / Other ${counts.other}` : ""}`;
}

function connectionShowingValue(table: ConnectionTable | undefined, filtered: number) {
  if (!table) return "0 / 0";
  const loaded = table.entries?.length ?? 0;
  const observedTotal = Math.max(Number(table.count ?? 0), loaded);
  if (observedTotal > loaded) {
    return `${filtered} / ${loaded} loaded (${observedTotal} total)`;
  }
  return `${filtered} / ${loaded}`;
}

function dnsLabelMap(rows: DNSQuery[]) {
  const labels: Record<string, string> = {};
  for (const row of rows) {
    for (const answer of row.answers ?? []) if (!labels[answer]) labels[answer] = row.questionName ?? "";
  }
  return labels;
}

function dhcpLeaseMap(rows: DHCPLease[]) {
  const leases: Record<string, DHCPLease> = {};
  for (const row of rows) {
    if (row.ip) leases[row.ip] = row;
  }
  return leases;
}

function eventFilterFacets(events: RouterEvent[]) {
  const severities = new Set<string>();
  const kinds = new Set<string>();
  for (const event of events) {
    if (event.severity) severities.add(event.severity);
    const kind = event.resourceKind || event.kind;
    if (kind) kinds.add(kind);
  }
  return {
    severities: Array.from(severities).sort(facetSort),
    kinds: Array.from(kinds).sort(facetSort),
  };
}

function filterEvents(events: RouterEvent[], filters: EventFilters) {
  const query = filters.query.trim().toLowerCase();
  const severity = filters.severity.trim();
  const kind = filters.resourceKind.trim();
  const since = eventRangeSince(filters);
  return events.filter(event => {
    if (since) {
      const ts = Date.parse(event.createdAt ?? "");
      if (Number.isNaN(ts) || ts < since) return false;
    }
    if (severity && severity !== "all" && event.severity !== severity) return false;
    if (kind && kind !== "all" && (event.resourceKind || event.kind) !== kind) return false;
    if (!query) return true;
    return eventSearchText(event).includes(query);
  });
}

function eventRangeSince(filters: EventFilters) {
  const hours = filters.range === "custom" ? Number(filters.customHours) : Number(filters.range.replace(/h$/, ""));
  if (!Number.isFinite(hours) || hours <= 0) return 0;
  return Date.now() - hours * 60 * 60 * 1000;
}

function eventSearchText(event: RouterEvent) {
  return [
    event.severity,
    event.topic,
    event.type,
    event.reason,
    event.message,
    event.resourceKind,
    event.resourceName,
    event.kind,
    event.name,
    JSON.stringify(event.attributes ?? {}),
  ].filter(Boolean).join(" ").toLowerCase();
}

function firewallProtocolFacets(logs: FirewallLog[]) {
  const values = new Set<string>();
  for (const log of logs) {
    const proto = normalizeFacet(log.protocol || log.l3Proto, "");
    if (proto) values.add(proto);
  }
  return Array.from(values).sort(facetSort);
}

function filterFirewallLogs(logs: FirewallLog[], filters: FirewallFilters, dnsLabels: Record<string, string>) {
  const query = filters.query.trim().toLowerCase();
  const source = filters.source.trim().toLowerCase();
  const destination = filters.destination.trim().toLowerCase();
  const port = filters.port.trim().toLowerCase();
  const protocol = filters.protocol.trim().toLowerCase();
  return logs.filter(log => {
    if (source && !String(log.srcAddress ?? "").toLowerCase().includes(source)) return false;
    if (destination && !String(log.dstAddress ?? "").toLowerCase().includes(destination)) return false;
    if (port && !String(log.srcPort ?? "").includes(port) && !String(log.dstPort ?? "").includes(port)) return false;
    if (protocol && protocol !== "all" && normalizeFacet(log.protocol || log.l3Proto, "") !== protocol) return false;
    if (!query) return true;
    return firewallSearchText(log, dnsLabels).includes(query);
  });
}

function firewallSearchText(log: FirewallLog, dnsLabels: Record<string, string>) {
  return [
    log.action,
    log.srcAddress,
    log.srcPort,
    log.dstAddress,
    log.dstPort,
    log.protocol,
    log.tcpFlags,
    log.l3Proto,
    log.ruleName,
    log.inIface,
    log.outIface,
    log.hint,
    log.dpiApp,
    log.dpiCategory,
    log.dpiTlsSNI,
    log.dpiHttpHost,
    log.dpiDnsQuery,
    log.dpiConfidence,
    ...(log.destinationSetMatches ?? []).flatMap(match => [
      "fqdn set",
      match.resourceName,
      match.setName,
      match.source,
      match.current ? "current" : "",
    ]),
    firewallDPIText(log),
    firewallDPIClassification(log, dnsLabels).detail,
    firewallDPIClassification(log, dnsLabels).source,
    firewallTCPFlags(log),
    firewallTrafficClass(log),
    firewallCorrelation(log),
    log.correlationDetail,
    log.expiredAgeSeconds,
    log.expiredBytes,
  ].filter(value => value !== undefined && value !== "").join(" ").toLowerCase();
}

function denyTimelineSamples(timeline: FirewallDenyTimelineBucket[]) {
  return timeline.map(bucket => Math.max(0, Math.trunc(Number(bucket.count ?? 0)) || 0));
}

function denyTimelineTotal(timeline: FirewallDenyTimelineBucket[]) {
  return denyTimelineSamples(timeline).reduce((sum, value) => sum + value, 0);
}

function denyTimelinePeak(timeline: FirewallDenyTimelineBucket[]) {
  return Math.max(0, ...denyTimelineSamples(timeline));
}

function sourceTopRows(logs: FirewallLog[]) {
  const counts = new Map<string, number>();
  for (const log of logs) {
    const source = log.srcAddress || "-";
    counts.set(source, (counts.get(source) ?? 0) + 1);
  }
  return Array.from(counts.entries())
    .map(([source, count]) => ({ source, count }))
    .sort((a, b) => b.count - a.count || stringSort(a.source, b.source))
    .slice(0, 10);
}

function appendMetricSample(current: MetricSample[], summary: Summary) {
  const next = metricSample(summary);
  const last = current[current.length - 1];
  if (last && last.time === next.time) return current;
  return [...current.slice(-35), next];
}

function metricSample(summary: Summary): MetricSample {
  let healthy = 0;
  let warning = 0;
  let danger = 0;
  let healthHealthy = 0;
  let healthUnhealthy = 0;
  for (const resource of summary.resources ?? []) {
    const phase = resource.status?.phase;
    if (neutralPhase(phase)) continue;
    const color = phaseColor(phase);
    if (color === "success") healthy++;
    else if (color === "danger") danger++;
    else warning++;
    if (resource.kind === "HealthCheck") {
      if (color === "success") healthHealthy++;
      if (color === "danger") healthUnhealthy++;
    }
  }
  return {
    time: summary.generatedAt ?? new Date().toISOString(),
    generation: Number(summary.status?.status?.generation ?? 0),
    healthy,
    warning,
    danger,
    healthHealthy,
    healthUnhealthy,
  };
}

function connectionFilterFacets(entries: ConnectionEntry[]) {
  const families = new Set<string>();
  const protocols = new Set<string>();
  const apps = new Set<string>();
  const sources = new Set<string>();
  const states = new Set<string>();
  for (const entry of entries) {
    families.add(normalizeFacet(entry.family, "other"));
    protocols.add(normalizeFacet(entry.protocol, "other"));
    apps.add(connectionApp(entry));
    sources.add(connectionAppSource(entry));
    states.add(normalizeFacet(entry.state, "stateless"));
  }
  return {
    families: Array.from(families).sort(facetSort),
    protocols: Array.from(protocols).sort(facetSort),
    apps: Array.from(apps).sort(facetSort),
    sources: Array.from(sources).sort(facetSort),
    states: Array.from(states).sort(facetSort),
  };
}

function resourceSearchText(resource: ResourceStatus) {
  return [
    resource.apiVersion,
    resource.kind,
    resource.name,
    resource.owner,
    resource.managedBy,
    resource.management,
    resource.status?.phase,
    resourceDetail(resource.status ?? {}),
    JSON.stringify(resource.status ?? {}),
  ].filter(Boolean).join(" ").toLowerCase();
}

function filterConnections(entries: ConnectionEntry[], dnsLabels: Record<string, string>, clientIdentities: Map<string, ClientIdentity>, filters: ConnectionFilters) {
  const query = filters.query.trim().toLowerCase();
  const clientAddresses = splitConnectionClientFilter(filters.client);
  return entries.filter(entry => {
    if (clientAddresses.length > 0 && !connectionTouchesAnyAddress(entry, clientAddresses)) return false;
    if (filters.family !== "all" && normalizeFacet(entry.family, "other") !== filters.family) return false;
    if (filters.protocol !== "all" && normalizeFacet(entry.protocol, "other") !== filters.protocol) return false;
    if (filters.app !== "all" && connectionApp(entry) !== filters.app) return false;
    if (filters.source !== "all" && connectionAppSource(entry) !== filters.source) return false;
    if (filters.state !== "all" && normalizeFacet(entry.state, "stateless") !== filters.state) return false;
    if (!query) return true;
    return connectionSearchText(entry, dnsLabels, clientIdentities).includes(query);
  });
}

function splitConnectionClientFilter(value: string) {
  return Array.from(new Set(
    String(value ?? "")
      .split(",")
      .map(part => normalizeAddressKey(part))
      .filter(Boolean),
  ));
}

function connectionTouchesAnyAddress(entry: ConnectionEntry, addresses: string[]) {
  const wanted = new Set(addresses);
  return connectionEndpointAddresses(entry).some(address => wanted.has(address));
}

function connectionEndpointAddresses(entry: ConnectionEntry) {
  return [
    entry.original?.source,
    entry.original?.destination,
    entry.reply?.source,
    entry.reply?.destination,
  ].map(address => normalizeAddressKey(address)).filter(Boolean);
}

function connectionClientFilterLabel(value: string, clientIdentities: Map<string, ClientIdentity>) {
  const addresses = splitConnectionClientFilter(value);
  const labels = Array.from(new Set(addresses.map(address => clientIdentities.get(address)?.compactLabel ?? "").filter(Boolean)));
  const addressLabel = addresses.slice(0, 3).join(", ");
  const addressSuffix = addresses.length > 3 ? `, +${addresses.length - 3}` : "";
  if (labels.length > 0) {
    return `${labels[0]} · ${addresses.length} address${addresses.length === 1 ? "" : "es"}${addressLabel ? ` (${addressLabel}${addressSuffix})` : ""}`;
  }
  return `${addressLabel}${addressSuffix}`;
}

function sortConnectionEntries(entries: ConnectionEntry[], dnsLabels: Record<string, string>, filters: ConnectionFilters) {
  const indexed = entries.map((entry, index) => ({ entry, index }));
  const multiplier = filters.direction === "desc" ? -1 : 1;
  return indexed
    .sort((a, b) => {
      if (filters.sort === "observed") return (a.index - b.index) * multiplier;
      const primary = compareConnectionSortValue(a.entry, b.entry, filters.sort, dnsLabels) * multiplier;
      return primary || a.index - b.index;
    })
    .map(row => row.entry);
}

function applyFrozenConnectionOrder(entries: ConnectionEntry[], keys: string[]) {
  const byKey = new Map<string, ConnectionEntry[]>();
  for (const entry of entries) {
    const key = connectionStableKey(entry);
    byKey.set(key, [...(byKey.get(key) ?? []), entry]);
  }
  const ordered: ConnectionEntry[] = [];
  for (const key of keys) {
    const rows = byKey.get(key);
    if (!rows || rows.length === 0) continue;
    ordered.push(rows.shift() as ConnectionEntry);
    if (rows.length === 0) byKey.delete(key);
  }
  const remaining = Array.from(byKey.values()).flat().sort(compareConnectionStable);
  return [...ordered, ...remaining];
}

function applyFrozenGroupOrder<T extends { key: string }>(groups: T[], keys: string[]) {
  const byKey = new Map(groups.map(group => [group.key, group]));
  const ordered: T[] = [];
  for (const key of keys) {
    const group = byKey.get(key);
    if (!group) continue;
    ordered.push(group);
    byKey.delete(key);
  }
  return [...ordered, ...Array.from(byKey.values())];
}

function useFrozenRowOrder<T>(rows: T[], keyFn: (row: T) => string): T[] {
  const orderRef = useRef<string[]>([]);
  const keys = rows.map(keyFn);
  const known = new Set(keys);
  const nextOrder = orderRef.current.filter(key => known.has(key));
  for (const key of keys) {
    if (!nextOrder.includes(key)) nextOrder.push(key);
  }
  orderRef.current = nextOrder;

  const byKey = new Map<string, T[]>();
  for (const row of rows) {
    const key = keyFn(row);
    byKey.set(key, [...(byKey.get(key) ?? []), row]);
  }
  const ordered: T[] = [];
  for (const key of nextOrder) {
    const candidates = byKey.get(key);
    if (!candidates || candidates.length === 0) continue;
    ordered.push(candidates.shift() as T);
    if (candidates.length === 0) byKey.delete(key);
  }
  return [...ordered, ...Array.from(byKey.values()).flat()];
}

function connectionSearchText(entry: ConnectionEntry, dnsLabels: Record<string, string>, clientIdentities?: Map<string, ClientIdentity>) {
  const addresses = [
    entry.original?.source,
    entry.original?.destination,
    entry.reply?.source,
    entry.reply?.destination,
  ].filter(Boolean) as string[];
  const labels = addresses.map(address => dnsLabels[address] ?? "").filter(Boolean);
  const clientLabels = clientIdentities ? addresses.map(address => clientIdentities.get(normalizeAddressKey(address))?.searchText ?? "").filter(Boolean) : [];
  return [
    entry.family,
    entry.protocol,
    entry.state || "stateless",
    entry.assured ? "assured" : "",
    entry.timeout,
    entry.mark,
    connectionApp(entry, dnsLabels),
    connectionDPIDetail(entry, dnsLabels),
    connectionAppSource(entry),
    entry.localRedirect ? "local redirect" : "",
    entry.localRedirect?.resourceName,
    entry.localRedirect?.ruleName,
    entry.localRedirect?.destinationSetRef,
    entry.localRedirect?.originalAddress,
    entry.localRedirect?.redirectAddress,
    destinationIdentity(entry, dnsLabels, clientIdentities),
    connectionInlineIdentity(entry, dnsLabels, clientIdentities),
    entry.original?.sourceHostname,
    entry.original?.destinationHostname,
    entry.original?.sourceService,
    entry.original?.destinationService,
    entry.reply?.sourceHostname,
    entry.reply?.destinationHostname,
    entry.reply?.sourceService,
    entry.reply?.destinationService,
    entry.appCategory,
    entry.appConfidence,
    endpoint(entry.original),
    endpoint(entry.reply),
    ...labels,
    ...clientLabels,
  ].join(" ").toLowerCase();
}

function compareConnectionSortValue(a: ConnectionEntry, b: ConnectionEntry, sort: string, dnsLabels: Record<string, string>) {
  if (sort === "stable") return compareConnectionStable(a, b);
  if (sort === "timeout") return Number(a.timeout ?? 0) - Number(b.timeout ?? 0);
  if (sort === "traffic") return connectionTrafficBytes(a) - connectionTrafficBytes(b);
  return stringSort(connectionSortValue(a, sort, dnsLabels), connectionSortValue(b, sort, dnsLabels));
}

function connectionSortValue(entry: ConnectionEntry, sort: string, dnsLabels: Record<string, string>) {
  if (sort === "state") return `${normalizeFacet(entry.state, "stateless")} ${entry.assured ? "assured" : ""}`;
  if (sort === "source") return hostPort(entry.original?.source, entry.original?.sourcePort);
  if (sort === "destination") return hostPort(entry.original?.destination, entry.original?.destinationPort);
  if (sort === "label") return dnsLabels[entry.original?.destination ?? ""] ?? entry.original?.destination ?? "";
  if (sort === "app") return `${connectionApp(entry, dnsLabels)} ${connectionDPIDetail(entry, dnsLabels)}`;
  return "";
}

function compareConnectionStable(a: ConnectionEntry, b: ConnectionEntry) {
  return stringSort(connectionStableKey(a), connectionStableKey(b));
}

function connectionTrafficBytes(entry: ConnectionEntry) {
  return Math.max(0, Number(entry.original?.bytes ?? 0)) + Math.max(0, Number(entry.reply?.bytes ?? 0));
}

function hasConnectionAccounting(entry: ConnectionEntry) {
  return Boolean(entry.original?.accounting || entry.reply?.accounting || entry.original?.bytes || entry.reply?.bytes);
}

function normalizeFacet(value: unknown, fallback: string) {
  const text = String(value ?? "").trim().toLowerCase();
  return text || fallback;
}

function formatFacet(value: string) {
  if (value === "ipv4") return "IPv4";
  if (value === "ipv6") return "IPv6";
  return value.toUpperCase();
}

function connectionApp(entry: ConnectionEntry, dnsLabels?: Record<string, string>) {
  const app = canonicalConnectionApp(entry.appName);
  const fallback = connectionPortFallback(entry, dnsLabels);
  if (fallback && (!app || app === "unknown" || app === "unidentified" || preferPortFallbackOverApp(app, fallback.app))) return fallback.app;
  if (app && app !== "unknown" && app !== "unidentified") return app;
  return fallback?.app ?? "unidentified";
}

function canonicalConnectionApp(value: unknown) {
  const app = normalizeFacet(value, "");
  return providerFromApp(app) ? "tls" : app;
}

function connectionDPIDetail(entry: ConnectionEntry, dnsLabels?: Record<string, string>) {
  if (entry.tlsSNI) return `tls-sni:${entry.tlsSNI}`;
  if (entry.httpHost) return `http-host:${entry.httpHost}`;
  if (entry.dnsQuery) {
    const app = connectionApp(entry, dnsLabels);
    const prefix = app === "netbios" ? "nbns-query" : app === "tailscale" ? "tailscale-dns" : "dns-query";
    return `${prefix}:${entry.dnsQuery}`;
  }
  const fallback = connectionPortFallback(entry, dnsLabels);
  if (fallback) return `port-guess:${fallback.label}`;
  return "";
}

function formatConnectionApp(value: string) {
  if (value === "unidentified") return "Unidentified";
  if (value === "tls") return "TLS";
  if (value === "http") return "HTTP";
  if (value === "dns") return "DNS";
  if (value === "netbios") return "NetBIOS";
  if (value === "ssh") return "SSH";
  if (value === "smb") return "SMB";
  if (value === "ntp") return "NTP";
  if (value === "dhcp") return "DHCP";
  if (value === "mdns") return "mDNS";
  if (value === "llmnr") return "LLMNR";
  if (value === "ssdp") return "SSDP";
  if (value === "ipsec") return "IPsec";
  if (value === "wireguard") return "WireGuard";
  if (value === "tailscale") return "Tailscale";
  if (value === "stun") return "STUN";
  if (value === "otlp") return "OTLP";
  if (value === "otlp-http") return "OTLP/HTTP";
  if (value === "quic") return "QUIC/HTTP3";
  if (value === "rdp") return "RDP";
  return value.toUpperCase();
}

function formatConnectionService(service: string, app: string) {
  const normalized = normalizeFacet(service, "");
  if (normalized === "https") return "HTTPS";
  if (normalized === "domain-s") return "DNS/TLS";
  if (normalized === "ms-wbt-server") return "RDP";
  if (normalized === "microsoft-ds") return "SMB";
  if (normalized === "isakmp") return "IPsec";
  if (normalized === "ipsec-nat-t") return "IPsec/NAT-T";
  if (normalized === "netbios-ns") return "NetBIOS-NS";
  if (normalized === "netbios-dgm") return "NetBIOS-DGM";
  if (normalized === "netbios-ssn") return "NetBIOS-SSN";
  if (normalized === "otlp") return "OTLP";
  if (normalized === "otlp-http") return "OTLP/HTTP";
  return formatConnectionApp(app || normalized);
}

function connectionAppColor(value: string): "brand" | "danger" | "informative" | "severe" | "subtle" | "success" | "warning" {
  if (value === "tls" || value === "http") return "brand";
  if (value === "dns") return "success";
  if (value === "netbios") return "warning";
  if (value === "ssh" || value === "rdp") return "danger";
  if (value === "smb" || value === "ipsec" || value === "wireguard" || value === "tailscale" || value === "stun" || value === "otlp" || value === "otlp-http" || value === "quic") return "informative";
  if (value === "unidentified") return "subtle";
  return "informative";
}

function connectionServiceApp(value: string) {
  switch (normalizeFacet(value, "")) {
    case "https":
    case "imaps":
    case "pop3s":
    case "submissions":
      return "tls";
    case "http":
      return "http";
    case "domain-s":
      return "dns";
    case "ssh":
      return "ssh";
    case "dns":
      return "dns";
    case "ntp":
      return "ntp";
    case "dhcp-server":
    case "dhcp-client":
      return "dhcp";
    case "netbios-ns":
    case "netbios-dgm":
    case "netbios-ssn":
      return "netbios";
    case "microsoft-ds":
      return "smb";
    case "isakmp":
    case "ipsec-nat-t":
      return "ipsec";
    case "ssdp":
      return "ssdp";
    case "ms-wbt-server":
      return "rdp";
    case "stun":
      return "stun";
    default:
      return normalizeFacet(value, "");
  }
}

function connectionClass(entry: ConnectionEntry) {
  const source = connectionAppSource(entry);
  if (connectionApp(entry) !== "unidentified") return source === "port-fallback" ? "port guess" : "dpi identified";
  if (source === "identifying") return "identifying";
  const state = normalizeFacet(entry.state, "stateless");
  if (entry.assured || state.includes("established")) return "established";
  if (state.includes("syn") || state.includes("unreplied")) return "probe";
  return "unidentified";
}

function connectionClassColor(entry: ConnectionEntry): "brand" | "danger" | "informative" | "severe" | "subtle" | "success" | "warning" {
  const cls = connectionClass(entry);
  if (cls === "dpi identified") return "success";
  if (cls === "port guess") return "informative";
  if (cls === "identifying") return "subtle";
  if (cls === "probe") return "warning";
  if (cls === "established") return "brand";
  return "subtle";
}

type ConnectionPortFallback = {
  app: string;
  port: string;
  label: string;
};

type ConnectionClassificationStats = {
  total: number;
  classified: number;
  dpi: number;
  guessed: number;
  identifying: number;
  unclassified: number;
  classifiedRatio: number;
};

type ConnectionClassification = {
  app: string;
  source: "dpi" | "port-fallback" | "identifying" | "none";
  detail: string;
  category?: string;
  confidence?: number;
  cacheHit?: boolean;
};

function connectionClassification(entry: ConnectionEntry, dnsLabels?: Record<string, string>): ConnectionClassification {
  const category = normalizeFacet(entry.appCategory, "");
  if (category === "port-fallback") {
    const fallback = connectionPortFallback(entry, dnsLabels);
    const app = fallback?.app || canonicalConnectionApp(entry.appName) || "unidentified";
    return {
      app,
      source: "port-fallback",
      detail: fallback ? `port-guess:${fallback.label}` : `port-guess:${formatConnectionApp(app)}`,
      category: "port-fallback",
      confidence: entry.appConfidence || 30,
    };
  }
  const raw = canonicalConnectionApp(entry.appName);
  if (raw && raw !== "unknown" && raw !== "unidentified") {
    return {
      app: raw,
      source: "dpi",
      detail: connectionDPIDetail(entry, dnsLabels),
      category: entry.appCategory,
      confidence: entry.appConfidence,
      cacheHit: normalizeFacet(entry.appCategory, "").includes("cache"),
    };
  }
  const fallback = connectionPortFallback(entry, dnsLabels);
  if (fallback) {
    return {
      app: fallback.app,
      source: "port-fallback",
      detail: `port-guess:${fallback.label}`,
      category: "port-fallback",
      confidence: 30,
    };
  }
  return {
    app: "unidentified",
    source: connectionNeedsIdentification(entry) ? "identifying" : "none",
    detail: "",
  };
}

function connectionAppSource(entry: ConnectionEntry) {
  if (normalizeFacet(entry.appCategory, "") === "port-fallback") return "port-fallback";
  const raw = canonicalConnectionApp(entry.appName);
  if (raw && raw !== "unknown" && raw !== "unidentified") return "dpi";
  if (connectionPortFallback(entry)) return "port-fallback";
  return connectionNeedsIdentification(entry) ? "identifying" : "none";
}

function connectionPortFallback(entry: ConnectionEntry, dnsLabels?: Record<string, string>): ConnectionPortFallback | undefined {
  const raw = canonicalConnectionApp(entry.appName);
  const category = normalizeFacet(entry.appCategory, "");
  const protocol = normalizeFacet(entry.protocol, "");
  const labels = [
    remoteHostname(entry.original, "destination", dnsLabels),
    remoteHostname(entry.reply, "source", dnsLabels),
  ].filter(Boolean) as string[];
  const ports = [
    { port: entry.original?.destinationPort, peerLabel: labels[0] ?? "", service: entry.original?.destinationService },
    { port: entry.original?.sourcePort, peerLabel: labels[1] ?? "", service: entry.original?.sourceService },
  ].filter(item => item.port) as { port: string; peerLabel: string; service?: string }[];
  for (const port of ports) {
    const app = portProtocolFallback(protocol, port.port, port.peerLabel) || (category === "port-fallback" ? raw : "");
    if (app && raw && raw !== "unknown" && raw !== "unidentified" && category !== "port-fallback" && !preferPortFallbackOverApp(raw, app)) return undefined;
    if (app) return { app, port: port.port, label: formatPortGuessLabel(app, port.port, port.peerLabel, port.service) };
  }
  return undefined;
}

function portProtocolFallback(protocol: string, port: string, peerLabel = "") {
  const numeric = Number(port);
  if (!Number.isFinite(numeric) || numeric <= 0) return "";
  switch (numeric) {
    case 20:
    case 21:
      return "ftp";
    case 22:
      return "ssh";
    case 25:
    case 465:
    case 587:
      return "smtp";
    case 53:
    case 853:
      return "dns";
    case 67:
    case 68:
      return protocol === "udp" ? "dhcp" : "";
    case 80:
    case 8000:
    case 8080:
    case 8888:
      return "http";
    case 110:
    case 995:
      return "pop3";
    case 123:
      return protocol === "udp" ? "ntp" : "";
    case 137:
    case 138:
      return protocol === "udp" ? "netbios" : "";
    case 139:
    case 445:
      return "smb";
    case 143:
    case 993:
      return "imap";
    case 443:
    case 8443:
      if (protocol === "udp") return "quic";
      return "tls";
    case 500:
    case 4500:
      return protocol === "udp" ? "ipsec" : "";
    case 1900:
      return protocol === "udp" ? "ssdp" : "";
    case 3306:
      return "mysql";
    case 3389:
      return "rdp";
    case 4317:
      return protocol === "tcp" ? "otlp" : "";
    case 3478:
    case 5349:
      return protocol === "udp" ? "stun" : "";
    case 4318:
      return protocol === "tcp" ? "otlp-http" : "";
    case 5353:
      return protocol === "udp" ? "mdns" : "";
    case 5355:
      return protocol === "udp" ? "llmnr" : "";
    case 5432:
      return "postgresql";
    case 51820:
      return protocol === "udp" ? "wireguard" : "";
    case 41641:
      return protocol === "udp" ? "tailscale" : "";
    default:
      return "";
  }
}

function preferPortFallbackOverApp(current: string, fallback: string) {
  if (normalizeFacet(current, "") !== "dns") return false;
  return ["tailscale", "stun", "wireguard", "quic"].includes(normalizeFacet(fallback, ""));
}

function connectionNeedsIdentification(entry: ConnectionEntry) {
  const protocol = normalizeFacet(entry.protocol, "");
  const state = normalizeFacet(entry.state, "");
  if (protocol === "tcp") return !state || state.includes("established") || state.includes("unreplied") || state.includes("syn");
  return protocol === "udp";
}

function providerFromHost(label = "") {
  const value = label.toLowerCase();
  if (!value) return "";
  if (/(^|\.)(amazonaws\.com|awsdns|cloudfront\.net)$/.test(value) || value.includes(".amazonaws.com") || value.includes(".cloudfront.net")) return "aws";
  if (value.includes(".googleusercontent.com") || value.includes(".googleapis.com") || value.includes(".gvt1.com") || value.includes(".google.com")) return "google";
  if (value.includes(".microsoft.com") || value.includes(".windowsupdate.com") || value.includes(".office.com") || value.includes(".azure.com")) return "microsoft";
  if (value.includes(".icloud.com") || value.includes(".apple.com") || value.includes(".cdn-apple.com")) return "apple";
  if (value.includes(".cloudflare.com") || value.includes(".cloudflare.net")) return "cloudflare";
  return "";
}

function providerFromApp(app = "") {
  switch (normalizeFacet(app, "")) {
    case "amazonaws":
    case "aws-https":
      return "aws";
    case "google":
    case "googleservices":
    case "google-https":
      return "google";
    case "microsoft":
    case "microsoft365":
    case "azure":
    case "microsoft-https":
      return "microsoft";
    case "apple":
    case "appleicloud":
    case "applepush":
    case "apple-https":
      return "apple";
    case "cloudflare":
    case "cloudflare-https":
      return "cloudflare";
    case "nintendo":
      return "nintendo";
    default:
      return "";
  }
}

function formatProviderLabel(provider: string) {
  switch (provider) {
    case "aws":
      return "AWS";
    case "google":
      return "Google";
    case "microsoft":
      return "Microsoft";
    case "apple":
      return "Apple";
    case "cloudflare":
      return "Cloudflare";
    case "nintendo":
      return "Nintendo";
    default:
      return provider.toUpperCase();
  }
}

function connectionDestinationProvider(entry: ConnectionEntry, dnsLabels?: Record<string, string>) {
  const labels = [
    remoteHostname(entry.original, "destination", dnsLabels),
    remoteHostname(entry.reply, "source", dnsLabels),
  ];
  for (const label of labels) {
    const provider = providerFromHost(label);
    if (provider) return provider;
  }
  return providerFromApp(entry.appName);
}

function formatPortGuessLabel(app: string, port: string, peerLabel = "", service = "") {
  const formatted = service ? service.toUpperCase() : formatConnectionApp(app);
  const suffix = peerLabel ? ` via ${peerLabel}` : "";
  return `${formatted}:${port}${suffix}`;
}

function connectionInlineIdentity(entry: ConnectionEntry, dnsLabels?: Record<string, string>, clientIdentities?: Map<string, ClientIdentity>) {
  const source = peerIdentity(entry.original, "source", dnsLabels, clientIdentities);
  const destination = destinationIdentity(entry, dnsLabels, clientIdentities);
  const parts = [];
  if (source) parts.push(`src ${source}`);
  if (destination) parts.push(`dst ${destination}`);
  return parts.join(" / ");
}

function destinationIdentity(entry: ConnectionEntry, dnsLabels?: Record<string, string>, clientIdentities?: Map<string, ClientIdentity>) {
  return peerIdentity(entry.original, "destination", dnsLabels, clientIdentities);
}

function peerIdentity(tuple: ConnTuple | undefined, side: "source" | "destination", dnsLabels?: Record<string, string>, clientIdentities?: Map<string, ClientIdentity>) {
  if (!tuple) return "";
  const address = side === "source" ? tuple.source : tuple.destination;
  const client = address ? clientIdentities?.get(normalizeAddressKey(address)) : undefined;
  if (address && client) return `${address} [${client.compactLabel}]`;
  if (side === "source") return "";
  return remoteIdentity(tuple, dnsLabels);
}

function remoteIdentity(tuple: ConnTuple | undefined, dnsLabels?: Record<string, string>) {
  const host = remoteHostname(tuple, "destination", dnsLabels);
  const service = tuple?.destinationService || serviceNameForPort(tuple?.destinationPort);
  const parts = [];
  if (host) parts.push(`${tuple?.destination ?? ""} (${host})`);
  if (service) parts.push(`service ${service}`);
  return parts.join(" / ");
}

function remoteHostname(tuple: ConnTuple | undefined, side: "source" | "destination", dnsLabels?: Record<string, string>) {
  if (!tuple) return "";
  if (side === "source") return tuple.sourceHostname || (tuple.source ? dnsLabels?.[tuple.source] : "") || "";
  return tuple.destinationHostname || (tuple.destination ? dnsLabels?.[tuple.destination] : "") || "";
}

function serviceNameForPort(port?: string) {
  const numeric = Number(port);
  if (!Number.isFinite(numeric)) return "";
  const names: Record<number, string> = {
    20: "ftp-data",
    21: "ftp",
    22: "ssh",
    25: "smtp",
    53: "dns",
    67: "dhcp-server",
    68: "dhcp-client",
    80: "http",
    110: "pop3",
    123: "ntp",
    137: "netbios-ns",
    138: "netbios-dgm",
    139: "netbios-ssn",
    143: "imap",
    443: "https",
    445: "microsoft-ds",
    465: "submissions",
    500: "isakmp",
    587: "submission",
    853: "domain-s",
    993: "imaps",
    995: "pop3s",
    1900: "ssdp",
    3306: "mysql",
    3389: "ms-wbt-server",
    3478: "stun",
    4317: "otlp",
    4318: "otlp-http",
    4500: "ipsec-nat-t",
    5353: "mdns",
    5355: "llmnr",
    5432: "postgresql",
    51820: "wireguard",
    41641: "tailscale",
  };
  return names[numeric] ?? "";
}

function connectionClassificationStats(entries: ConnectionEntry[]): ConnectionClassificationStats {
  const stats: ConnectionClassificationStats = {
    total: entries.length,
    classified: 0,
    dpi: 0,
    guessed: 0,
    identifying: 0,
    unclassified: 0,
    classifiedRatio: 0,
  };
  for (const entry of entries) {
    const app = connectionApp(entry);
    if (app === "unidentified") {
      if (connectionAppSource(entry) === "identifying") stats.identifying++;
      else stats.unclassified++;
      continue;
    }
    stats.classified++;
    if (connectionAppSource(entry) === "port-fallback") stats.guessed++;
    else stats.dpi++;
  }
  stats.classifiedRatio = stats.total ? Math.round((stats.classified / stats.total) * 100) : 0;
  return stats;
}

function topTrafficProtocols(flows: TrafficFlow[]) {
  const totals = new Map<string, number>();
  for (const flow of flows) {
    const classification = trafficFlowClassification(flow);
    totals.set(classification.rankLabel, (totals.get(classification.rankLabel) ?? 0) + trafficFlowBytes(flow));
  }
  return topRows(totals, 5);
}

function topTrafficSources(flows: TrafficFlow[]) {
  const totals = new Map<string, number>();
  for (const flow of flows) {
    const classification = trafficFlowClassification(flow);
    totals.set(classification.source, (totals.get(classification.source) ?? 0) + trafficFlowBytes(flow));
  }
  return topRows(totals, 5);
}

function topTalkers(clients: ClientEntry[], flows: TrafficFlow[]) {
  const labels = new Map<string, string>();
  for (const client of clients) {
    const label = client.hostname || client.mac || client.id || "";
    for (const address of client.addresses ?? []) {
      if (address && label) labels.set(address, label);
    }
    if (client.id && label) labels.set(client.id, label);
  }
  const totals = new Map<string, number>();
  for (const flow of flows) {
    const bytes = trafficFlowBytes(flow);
    if (bytes <= 0) continue;
    const key = labels.get(flow.clientAddress ?? "") || flow.clientAddress || "-";
    totals.set(key, (totals.get(key) ?? 0) + bytes);
  }
  return topRows(totals, 5);
}

function topDomains(flows: TrafficFlow[]) {
  const totals = new Map<string, number>();
  for (const flow of flows) {
    const label = trafficFlowDomain(flow);
    if (!label) continue;
    totals.set(label, (totals.get(label) ?? 0) + Math.max(1, trafficFlowBytes(flow)));
  }
  return topRows(totals, 5);
}

function connectionClassSummary(entries: ConnectionEntry[]) {
  const totals = new Map<string, number>();
  for (const entry of entries) {
    const cls = connectionClass(entry);
    totals.set(cls, (totals.get(cls) ?? 0) + 1);
  }
  return topRows(totals, 5);
}

function trafficFlowApp(flow: TrafficFlow) {
  return trafficFlowClassification(flow).app;
}

function trafficFlowClassification(flow: TrafficFlow) {
  const app = canonicalConnectionApp(flow.appName);
  const typedApp = canonicalConnectionApp(flow.applicationProtocol || flow.detectedProtocol || flow.masterProtocol || "");
  const source = trafficFlowSource(flow);
  if (app && app !== "unknown") return { app, source, rankLabel: `${source}:${app}` };
  if (typedApp && typedApp !== "unknown") return { app: typedApp, source, rankLabel: `${source}:${typedApp}` };
  if (flow.tlsSNI) return { app: "tls", source, rankLabel: `${source}:tls` };
  if (flow.httpHost) return { app: "http", source, rankLabel: `${source}:http` };
  if (flow.dnsQuery) return { app: "dns", source, rankLabel: `${source}:dns` };
  const port = flow.peerPort ? String(flow.peerPort) : "";
  const fallback = port ? portProtocolFallback(normalizeFacet(flow.protocol, ""), port, flow.resolvedHostname) : "";
  if (fallback) return { app: fallback, source: "port-fallback", rankLabel: `port-guess:${fallback}` };
  const protocol = normalizeFacet(flow.protocol, "unidentified");
  return { app: protocol, source: "none", rankLabel: `unidentified:${protocol}` };
}

function trafficFlowSource(flow: TrafficFlow) {
  const source = normalizeFacet(flow.source, "");
  if (source === "ndpi-agent" || source === "builtin" || source === "port-fallback") return source;
  const engine = normalizeFacet(flow.engine, "");
  if (engine === "ndpi-agent" || engine === "builtin") return engine;
  if (normalizeFacet(flow.appCategory, "") === "port-fallback") return "port-fallback";
  return "dpi";
}

function formatProtocolRankLabel(value: string) {
  const [source, app = value] = value.split(":", 2);
  if (source === "dpi") return `DPI ${formatConnectionApp(app)}`;
  if (source === "ndpi-agent") return `nDPI ${formatConnectionApp(app)}`;
  if (source === "builtin") return `Built-in ${formatConnectionApp(app)}`;
  if (source === "port-guess") return `port-guess:${formatConnectionApp(app)}`;
  if (source === "port-fallback") return `port-guess:${formatConnectionApp(app)}`;
  if (source === "unidentified") return `Identifying ${formatConnectionApp(app)}`;
  return formatConnectionApp(value);
}

function formatTrafficSourceLabel(value: string) {
  if (value === "ndpi-agent") return "nDPI agent";
  if (value === "builtin") return "Built-in parser";
  if (value === "port-fallback") return "Port fallback";
  if (value === "dpi") return "DPI";
  if (value === "none") return "Unidentified";
  return formatFacet(value);
}

function trafficFlowDomain(flow: TrafficFlow) {
  return String(flow.tlsSNI || flow.httpHost || flow.dnsQuery || flow.resolvedHostname || "").trim();
}

function trafficFlowBytes(flow: TrafficFlow) {
  return Math.max(0, Number(flow.bytesOut ?? 0)) + Math.max(0, Number(flow.bytesIn ?? 0));
}

function topRows(values: Map<string, number>, limit: number) {
  return Array.from(values.entries())
    .filter(([, value]) => value > 0)
    .map(([label, value]) => ({ label, value }))
    .sort((a, b) => b.value - a.value || stringSort(a.label, b.label))
    .slice(0, limit);
}

function facetSort(a: string, b: string) {
  const order: Record<string, number> = { ipv4: 0, ipv6: 1, tcp: 0, udp: 1, icmp: 2, icmpv6: 3, ipv6_icmp: 3, established: 0, tls: 0, http: 1, dns: 2, netbios: 3, unidentified: 99 };
  return (order[a] ?? 9) - (order[b] ?? 9) || a.localeCompare(b);
}

function stringSort(a: string, b: string) {
  return a.localeCompare(b, undefined, { numeric: true, sensitivity: "base" });
}

function connectionGroups(entries: ConnectionEntry[]) {
  const groups = new Map<string, ConnectionEntry[]>();
  for (const entry of entries) {
    const key = connectionGroupKey(entry);
    groups.set(key, [...(groups.get(key) ?? []), entry]);
  }
  const order: Record<string, number> = { ipv4: 0, ipv6: 1, other: 9, tcp: 0, udp: 1, icmp: 2 };
  return Array.from(groups.entries())
    .sort((a, b) => {
      const [af, ap] = a[0].split("/");
      const [bf, bp] = b[0].split("/");
      return (order[af] ?? 9) - (order[bf] ?? 9) || (order[ap] ?? 9) - (order[bp] ?? 9) || a[0].localeCompare(b[0]);
    })
    .map(([key, rows]) => ({ key, rows }));
}

function connectionGroupKey(entry: ConnectionEntry) {
  const family = normalizeFacet(entry.family, "other");
  const protocol = normalizeConnectionGroupProtocol(entry.protocol);
  if ((family === "ipv4" || family === "ipv6") && (protocol === "tcp" || protocol === "udp" || protocol === "icmp")) {
    return `${family}/${protocol}`;
  }
  return "other/other";
}

function normalizeConnectionGroupProtocol(protocol: string | undefined) {
  const normalized = normalizeFacet(protocol, "other");
  if (normalized === "icmp" || normalized === "icmpv6" || normalized === "ipv6-icmp" || normalized === "ipv6_icmp") return "icmp";
  if (normalized === "tcp" || normalized === "udp") return normalized;
  return "other";
}

function connectionStateSummary(entries: ConnectionEntry[]) {
  const counts = new Map<string, number>();
  for (const entry of entries) {
    const state = normalizeFacet(entry.state, "stateless");
    counts.set(state, (counts.get(state) ?? 0) + 1);
  }
  return Array.from(counts.entries())
    .map(([label, count]) => ({ label, count }))
    .sort((a, b) => b.count - a.count || facetSort(a.label, b.label));
}

function connectionAppSummary(entries: ConnectionEntry[]) {
  const counts = new Map<string, number>();
  for (const entry of entries) {
    const app = connectionApp(entry);
    counts.set(app, (counts.get(app) ?? 0) + 1);
  }
  return Array.from(counts.entries())
    .map(([label, count]) => ({ label, count }))
    .sort((a, b) => b.count - a.count || facetSort(a.label, b.label))
    .slice(0, 5);
}

function connectionGroupLabel(key: string) {
  const [family, protocol] = key.split("/");
  return {
    family: family === "ipv4" ? "IPv4" : family === "ipv6" ? "IPv6" : "Other",
    protocol: protocol || "other",
  };
}

function formatConnectionGroupTitle(label: { family: string; protocol: string }) {
  if (label.family === "Other") return "Other";
  return `${label.family}/${label.protocol.toUpperCase()}`;
}

function connectionGroupID(key: string) {
  return `connections-${key.replace(/[^a-zA-Z0-9_-]+/g, "-")}`;
}

function routeKey(route: RouteEntry) {
  return [
    route.source || "",
    route.resource || "",
    route.family || "",
    route.destination || "",
    route.gateway || "",
    route.device || "",
    route.protocol || "",
    route.table || "",
    route.metric || "",
    route.peer || "",
  ].join("|");
}

function routePeerKey(peer: RouteBGPPeer) {
  return [peer.router || "", peer.peer || "", peer.asn || ""].join("|");
}

function routeSearchText(route: RouteEntry) {
  return [
    route.source,
    route.resource,
    route.family,
    route.destination,
    route.gateway,
    route.device,
    route.protocol,
    route.table,
    route.metric,
    route.scope,
    route.type,
    route.peer,
    route.phase,
  ].join(" ").toLowerCase();
}

function routeProtocolBucket(route: RouteEntry) {
  const source = String(route.source || "").toLowerCase();
  const protocol = String(route.protocol || "").toLowerCase();
  if (source === "bgp" || protocol === "bgp") return "bgp";
  if (source === "static" || protocol === "static") return "static";
  if (source === "kernel" && !route.gateway && (protocol === "kernel" || route.scope === "link")) return "connected";
  if (source === "kernel") return "kernel";
  if (source === "dhcpv4" || protocol === "dhcp") return "dhcp";
  if (source === "policy") return "policy";
  return source || protocol || "other";
}

function routeProtocolLabel(value: string) {
  switch (value) {
    case "bgp":
      return "BGP";
    case "dhcp":
      return "DHCP";
    default:
      return formatFacet(value);
  }
}

function routeProtocolOptions(routes: RouteEntry[]) {
  const preferred = ["bgp", "static", "connected", "kernel"];
  const values = new Set(routes.map(routeProtocolBucket));
  const out = preferred.filter(value => values.has(value));
  for (const value of Array.from(values).sort(facetSort)) {
    if (!out.includes(value)) out.push(value);
  }
  return out;
}

function routeFamilyOptions(routes: RouteEntry[]) {
  return Array.from(new Set(routes.map(route => String(route.family || "unknown")))).sort(facetSort);
}

function routeProtocolColor(value: string): "success" | "warning" | "danger" | "informative" | "subtle" {
  switch (value) {
    case "bgp":
      return "success";
    case "static":
      return "informative";
    case "connected":
      return "subtle";
    case "kernel":
      return "warning";
    default:
      return "subtle";
  }
}

function latestRouteObservedAt(status: RoutesStatus | null) {
  const values = (status?.routes ?? []).map(route => route.observedAt).filter(Boolean) as string[];
  let latest = "";
  for (const value of values) {
    const timestamp = Date.parse(value);
    if (Number.isNaN(timestamp)) continue;
    if (!latest || timestamp > Date.parse(latest)) latest = value;
  }
  return latest || status?.generatedAt || "";
}

function navigationSubItems(selected: ViewKey, groups: { key: string; rows: ConnectionEntry[] }[], summary: Summary | null): NavSubItem[] {
  if (selected === "overview") {
    return [
      { key: "metrics", label: "Metrics", view: "overview", targetID: "overview-metrics" },
      { key: "interfaces", label: "Interfaces", count: summary?.interfaces?.length ?? 0, view: "overview", targetID: "overview-interfaces" },
      { key: "activity", label: "Activity", count: (summary?.events ?? []).length, view: "overview", targetID: "overview-activity" },
    ];
  }
  if (selected === "controllers") {
    const controllers = summary?.controllers ?? (summary?.status?.status?.controllers as ControllerStatus[] | undefined) ?? [];
    return [
      { key: "controllers", label: "Controllers", count: controllers.length, view: "controllers", targetID: "controllers-table" },
    ];
  }
  if (selected === "resources") {
    const resources = importantResources(summary?.resources ?? []);
    return [
      { key: "resources", label: "Resources", count: resources.length, view: "resources", targetID: "resources-table" },
    ];
  }
  if (selected === "connections") {
    return groups.map(group => {
      const label = connectionGroupLabel(group.key);
      return {
        key: group.key,
        label: formatConnectionGroupTitle(label),
        count: group.rows.length,
        view: "connections",
        targetID: connectionGroupID(group.key),
      };
    });
  }
  if (selected === "clients") {
    const leases = summary?.dhcpLeases ?? [];
    const flows = summary?.trafficFlows ?? [];
    const clients = summary?.clients ?? [];
    return [
      { key: "inventory", label: "Inventory", count: clients.length, view: "clients", targetID: "clients-inventory" },
      { key: "traffic", label: "Traffic", count: clientTrafficRows(flows).length, view: "clients", targetID: "clients-traffic" },
      { key: "leases", label: "DHCP leases", count: leases.length, view: "clients", targetID: "clients-leases" },
    ];
  }
  if (selected === "firewall") {
    const logs = summary?.firewallLogs ?? [];
    const tuning = summary?.conntrackTuning?.suggestions ?? [];
    return [
      { key: "tuning", label: "Tuning", count: tuning.length, view: "firewall", targetID: "firewall-tuning" },
      { key: "ranking", label: "Deny ranking", count: denyRows(logs).length, view: "firewall", targetID: "firewall-ranking" },
      { key: "timeline", label: "Deny timeline", count: logs.length, view: "firewall", targetID: "firewall-timeline" },
    ];
  }
  if (selected === "vpn") {
    const wireGuard = summary?.vpn?.wireGuard ?? [];
    const tailscalePeers = summary?.vpn?.tailscale?.peers ?? [];
    return [
      { key: "tailscale", label: "Tailscale", count: tailscalePeers.length, view: "vpn", targetID: "vpn-tailscale" },
      { key: "wireguard", label: "WireGuard", count: wireGuard.reduce((total, item) => total + (item.peers?.length ?? 0), 0), view: "vpn", targetID: "vpn-wireguard" },
    ];
  }
  if (selected === "events") {
    const events = summary?.events ?? [];
    return [
      { key: "list", label: "Event list", count: events.length, view: "events", targetID: "events-list" },
      { key: "detail", label: "Detail", view: "events", targetID: "events-detail" },
    ];
  }
  return [];
}

function clientSectionID(targetID?: string) {
  switch (targetID) {
    case "clients-traffic":
    case "clients-leases":
    case "clients-inventory":
      return targetID;
    default:
      return "clients-inventory";
  }
}

function parseLocationHash(): { view: ViewKey; targetID?: string } {
  const raw = window.location.hash.replace(/^#/, "").trim();
  const [viewPart, sectionPart] = raw.split("/", 2);
  const view = viewKeys.has(viewPart) ? viewPart as ViewKey : "overview";
  if (!sectionPart) return { view };
  return { view, targetID: `${view}-${sectionPart}` };
}

function hashForView(view: ViewKey, targetID?: string) {
  if (!targetID) return `#${view}`;
  const prefix = `${view}-`;
  const section = targetID.startsWith(prefix) ? targetID.slice(prefix.length) : targetID;
  return `#${view}/${section}`;
}

function readStoredRecord<T extends number | boolean>(key: string): Record<string, T> {
  try {
    const raw = window.localStorage.getItem(key);
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {};
    return parsed as Record<string, T>;
  } catch {
    return {};
  }
}

function writeStoredRecord<T extends number | boolean>(key: string, value: Record<string, T>) {
  try {
    window.localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Ignore storage failures; the URL hash still preserves the selected section.
  }
}

function scrollToElement(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" });
}

function scrollToGenerationResult() {
  window.requestAnimationFrame(() => {
    window.requestAnimationFrame(() => {
      const target = document.getElementById("generation-result");
      target?.scrollIntoView({ behavior: "smooth", block: "start" });
      target?.focus({ preventScroll: true });
    });
  });
}

let lastUserWindowScrollAt = 0;
let programmaticScrollUntil = 0;
let horizontalTouchState: {
  scroller: HTMLElement;
  startX: number;
  startY: number;
  lastY: number;
  axis: "horizontal" | "vertical" | "";
} | null = null;

function installHorizontalScrollTouchCoordinator() {
  const onStart = (event: TouchEvent) => {
    if (window.innerWidth > 860 || event.touches.length !== 1) {
      horizontalTouchState = null;
      return;
    }
    const scroller = horizontalScrollerFromTarget(event.target);
    if (!scroller) {
      horizontalTouchState = null;
      return;
    }
    const touch = event.touches[0];
    horizontalTouchState = {
      scroller,
      startX: touch.clientX,
      startY: touch.clientY,
      lastY: touch.clientY,
      axis: "",
    };
  };
  const onMove = (event: TouchEvent) => {
    if (!horizontalTouchState || event.touches.length !== 1) return;
    const touch = event.touches[0];
    const deltaX = touch.clientX - horizontalTouchState.startX;
    const deltaY = touch.clientY - horizontalTouchState.startY;
    const absX = Math.abs(deltaX);
    const absY = Math.abs(deltaY);
    if (!horizontalTouchState.axis && Math.max(absX, absY) > 8) {
      horizontalTouchState.axis = absY > absX * 1.2 ? "vertical" : absX > absY * 1.2 ? "horizontal" : "";
    }
    if (horizontalTouchState.axis !== "vertical") return;
    if (event.cancelable) event.preventDefault();
    const scrollDelta = horizontalTouchState.lastY - touch.clientY;
    if (Math.abs(scrollDelta) > 0.5) {
      markProgrammaticScroll();
      window.scrollBy(0, scrollDelta);
      horizontalTouchState.lastY = touch.clientY;
    }
  };
  const onEnd = () => {
    horizontalTouchState = null;
  };
  document.addEventListener("touchstart", onStart, { passive: true, capture: true });
  document.addEventListener("touchmove", onMove, { passive: false, capture: true });
  document.addEventListener("touchend", onEnd, { passive: true, capture: true });
  document.addEventListener("touchcancel", onEnd, { passive: true, capture: true });
  return () => {
    document.removeEventListener("touchstart", onStart, { capture: true });
    document.removeEventListener("touchmove", onMove, { capture: true });
    document.removeEventListener("touchend", onEnd, { capture: true });
    document.removeEventListener("touchcancel", onEnd, { capture: true });
  };
}

function horizontalScrollerFromTarget(target: EventTarget | null) {
  if (!(target instanceof Element)) return null;
  let element: HTMLElement | null = target.closest("[data-routerd-scroll-key], [role='table']");
  while (element) {
    if (isHorizontalOnlyScroller(element)) return element;
    element = element.parentElement?.closest("[data-routerd-scroll-key], [role='table']") ?? null;
  }
  return null;
}

function isHorizontalOnlyScroller(element: HTMLElement) {
  const style = window.getComputedStyle(element);
  const canScrollX = /(auto|scroll)/.test(style.overflowX) && element.scrollWidth > element.clientWidth + 2;
  if (!canScrollX) return false;
  const canScrollY = /(auto|scroll)/.test(style.overflowY) && element.scrollHeight > element.clientHeight + 2;
  return !canScrollY;
}

function captureScrollSnapshot(): ScrollSnapshot {
  const elements = Array.from(document.querySelectorAll<HTMLElement>("[data-routerd-scroll-key]")).map(element => ({
    key: element.dataset.routerdScrollKey ?? "",
    top: element.scrollTop,
    left: element.scrollLeft,
  })).filter(item => item.key);
  return {
    capturedAt: performance.now(),
    windowX: window.scrollX,
    windowY: window.scrollY,
    anchor: captureScrollAnchor(),
    elements,
  };
}

function restoreScrollSnapshot(snapshot: ScrollSnapshot) {
  if (lastUserWindowScrollAt > snapshot.capturedAt + 50) return;
  if (!restoreScrollAnchor(snapshot)) {
    markProgrammaticScroll();
    window.scrollTo(snapshot.windowX, snapshot.windowY);
  } else if (Math.abs(window.scrollX - snapshot.windowX) > 1) {
    markProgrammaticScroll();
    window.scrollTo(snapshot.windowX, window.scrollY);
  }
  const elements = new Map<string, HTMLElement>();
  document.querySelectorAll<HTMLElement>("[data-routerd-scroll-key]").forEach(element => {
    const key = element.dataset.routerdScrollKey;
    if (key) elements.set(key, element);
  });
  for (const item of snapshot.elements) {
    const element = elements.get(item.key);
    if (!element) continue;
    element.scrollTop = item.top;
    element.scrollLeft = item.left;
  }
}

function restoreScrollAfterRender(snapshot: ScrollSnapshot) {
  window.requestAnimationFrame(() => {
    restoreScrollSnapshot(snapshot);
    window.requestAnimationFrame(() => restoreScrollSnapshot(snapshot));
  });
  window.setTimeout(() => restoreScrollSnapshot(snapshot), 120);
}

function captureScrollAnchor() {
  const anchors = Array.from(document.querySelectorAll<HTMLElement>("main [id]"))
    .filter(element => element.id && isVisibleScrollAnchor(element));
  if (!anchors.length) return undefined;
  const topEdge = 96;
  let selected = anchors[0];
  for (const anchor of anchors) {
    const rect = anchor.getBoundingClientRect();
    if (rect.top <= topEdge) {
      selected = anchor;
      continue;
    }
    break;
  }
  const rect = selected.getBoundingClientRect();
  return { id: selected.id, top: rect.top };
}

function restoreScrollAnchor(snapshot: ScrollSnapshot) {
  if (!snapshot.anchor?.id) return false;
  const target = document.getElementById(snapshot.anchor.id);
  if (!target) return false;
  const rect = target.getBoundingClientRect();
  const delta = rect.top - snapshot.anchor.top;
  if (Math.abs(delta) > 1) {
    markProgrammaticScroll();
    window.scrollBy(0, delta);
  }
  return true;
}

function isVisibleScrollAnchor(element: HTMLElement) {
  const rect = element.getBoundingClientRect();
  return rect.height > 0 && rect.width > 0 && rect.bottom >= 0;
}

function markProgrammaticScroll() {
  programmaticScrollUntil = performance.now() + 250;
}

function endpoint(tuple?: ConnTuple) {
  if (!tuple) return "-";
  return `${hostPort(tuple.source, tuple.sourcePort)} -> ${hostPort(tuple.destination, tuple.destinationPort)}`;
}

function hostPort(host?: string, port?: string) {
  return host ? `${host}${port ? `:${port}` : ""}` : "";
}

function firewallEndpoint(host?: string, port?: number) {
  return host ? `${host}${port ? `:${port}` : ""}` : "-";
}

function flowKey(entry: ConnectionEntry) {
  return connectionStableKey(entry);
}

function connectionStableKey(entry: ConnectionEntry) {
  return [entry.family, entry.protocol, endpoint(entry.original), endpoint(entry.reply), entry.mark].join("|");
}

function eventKey(event?: RouterEvent) {
  if (!event) return "";
  return String(event.id ?? `${event.createdAt}-${event.topic ?? event.type ?? ""}-${resourceName(event)}`);
}

function resourceDetail(status: Record<string, unknown>) {
  return ["selectedCandidate", "selectedDevice", "activeEgressInterface", "target", "address", "currentPrefix", "backendState", "tailnetName", "tailscaleIPs", "peerCount", "changedFields"]
    .map(key => status[key] ? `${key}=${status[key]}` : "")
    .filter(Boolean)
    .join(" ");
}

function dryRunControllerByKind(controllers: ControllerStatus[]) {
  const byKind = new Map<string, ControllerStatus>();
  for (const controller of controllers) {
    if (controller.mode !== "dry-run") continue;
    for (const kind of controller.resourceKinds ?? []) {
      byKind.set(kind, controller);
    }
  }
  return byKind;
}

function resourceName(event: RouterEvent) {
  const kind = event.resourceKind || event.kind || "";
  const name = event.resourceName || event.name || "";
  return kind || name ? `${kind}/${name}` : "-";
}

function eventAttributeEntries(event: RouterEvent): [string, unknown][] {
  const attrs = event.attributes ?? {};
  const preferred = ["mac", "ip", "hostname", "action", "interface", "address", "prefix", "target", "result", "phase", "changedFields"];
  const keys: string[] = [];
  for (const key of preferred) if (attrs[key] !== undefined && attrs[key] !== "") keys.push(key);
  for (const key of Object.keys(attrs).sort()) if (!keys.includes(key) && attrs[key] !== undefined && attrs[key] !== "") keys.push(key);
  return keys.map(key => [key, attrs[key]]);
}

function formatDetailValue(value: unknown) {
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
}

function unifiedLineDiff(fromName: string, toName: string, fromText: string, toText: string) {
  if (fromText === toText) return `--- ${fromName}\n+++ ${toName}\n# no changes\n`;
  const from = fromText.replace(/\n$/, "").split(/\n/);
  const to = toText.replace(/\n$/, "").split(/\n/);
  const table = Array.from({ length: from.length + 1 }, () => Array<number>(to.length + 1).fill(0));
  for (let i = from.length - 1; i >= 0; i--) {
    for (let j = to.length - 1; j >= 0; j--) {
      table[i][j] = from[i] === to[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1]);
    }
  }
  const lines = [`--- ${fromName}`, `+++ ${toName}`];
  let i = 0;
  let j = 0;
  while (i < from.length || j < to.length) {
    if (i < from.length && j < to.length && from[i] === to[j]) {
      lines.push(` ${from[i]}`);
      i++;
      j++;
    } else if (j < to.length && (i === from.length || table[i][j + 1] >= table[i + 1][j])) {
      lines.push(`+${to[j]}`);
      j++;
    } else if (i < from.length) {
      lines.push(`-${from[i]}`);
      i++;
    }
  }
  return `${lines.join("\n")}\n`;
}

function clientTrafficRows(flows: TrafficFlow[]) {
  const totals = new Map<string, { client: string; bytesOut?: number; bytesIn?: number; peers: Set<string>; protocols: Set<string> }>();
  for (const flow of flows) {
    const key = flow.clientAddress || "-";
    const row = totals.get(key) ?? { client: key, peers: new Set<string>(), protocols: new Set<string>() };
    row.bytesOut = addOptionalBytes(row.bytesOut, flow.bytesOut, flow.accounting);
    row.bytesIn = addOptionalBytes(row.bytesIn, flow.bytesIn, flow.accounting);
    const peer = flow.resolvedHostname || flow.tlsSNI || flow.peerAddress;
    if (peer) row.peers.add(peer);
    const protocol = normalizeFacet(flow.appName || flow.protocol, "unidentified");
    if (protocol) row.protocols.add(protocol);
    totals.set(key, row);
  }
  return Array.from(totals.values()).slice(0, 10);
}

function clientEntryToRow(entry: ClientEntry): ClientRow {
  return {
    id: entry.id,
    ip: entry.addresses?.[0] || entry.id || entry.mac || "-",
    addresses: new Set(entry.addresses ?? []),
    hostname: entry.hostname ?? "",
    mac: normalizeMAC(entry.mac),
    vendor: entry.vendor ?? "",
    state: entry.state ?? "",
    sources: new Set(entry.sources ?? []),
    expiresAt: "",
    bytesOut: entry.bytesOut,
    bytesIn: entry.bytesIn,
    peers: new Set(entry.peers ?? []),
    primaryActivity: entry.primaryActivity ?? "",
    lastProtocol: entry.lastProtocol ?? "",
    lastProtocolDetail: entry.lastProtocolDetail ?? "",
    protocolMix: new Set(entry.protocolMix ?? []),
    inferredOSFamily: entry.inferredOSFamily ?? "",
    inferredDeviceClass: entry.inferredDeviceClass ?? "",
    fingerprintConfidence: entry.fingerprintConfidence,
    fingerprintSignals: new Set(entry.fingerprintSignals ?? []),
    stickyUntil: entry.stickyUntil ?? "",
    stickyState: entry.stickyState ?? "",
    clientPolicy: entry.clientPolicy ?? "",
    clientPolicyMode: entry.clientPolicyMode ?? "",
    isolationPolicy: new Set(entry.isolationPolicy ?? []),
  };
}

function clientRowKey(row: ClientRow) {
  return [
    row.id,
    row.mac,
    row.hostname,
    primaryClientAddress(row),
    Array.from(row.addresses).sort().join(","),
  ].find(value => value && value !== "-") ?? "-";
}

function clientSections(rows: ClientRow[]) {
  const sections = new Map<string, { key: string; label: string; rows: ClientRow[]; addressCount: number }>();
  for (const row of rows) {
    const label = clientSectionLabel(clientOSFamily(row));
    const section = sections.get(label) ?? { key: label.toLowerCase(), label, rows: [], addressCount: 0 };
    section.rows.push(row);
    section.addressCount += row.addresses.size;
    sections.set(label, section);
  }
  return Array.from(sections.values());
}

function clientSectionLabel(family: string) {
  const normalized = family.trim().toLowerCase();
  if (!normalized || normalized === "-") return "Other";
  if (normalized === "nintendo") return "Nintendo";
  if (normalized === "playstation") return "PlayStation";
  if (normalized === "xbox") return "Xbox";
  if (normalized === "steam-os" || normalized === "steamos") return "SteamOS";
  if (normalized === "ios" || normalized === "macos" || normalized === "apple") return "Apple";
  if (normalized === "android") return "Android";
  if (normalized === "windows") return "Windows";
  if (normalized === "linux") return "Linux";
  if (normalized === "iot") return "IoT";
  if (normalized === "printer") return "Printer";
  if (normalized === "nas") return "NAS";
  if (normalized === "voip") return "VoIP";
  if (normalized === "embedded" || normalized === "iot") return "Embedded";
  return family;
}

function normalizeMAC(mac?: string) {
  return String(mac ?? "").trim().toLowerCase();
}

function addOptionalBytes(current: number | undefined, next: number | undefined, accounting?: boolean) {
  if (!accounting) return current;
  const value = typeof next === "number" && Number.isFinite(next) ? next : 0;
  return (current ?? 0) + value;
}

function clientActivityFacets(clients: ClientEntry[]) {
  const values = new Set<string>();
  for (const client of clients) {
    const activity = normalizeFacet(client.primaryActivity, "");
    if (activity) values.add(activity);
  }
  return Array.from(values).sort(facetSort);
}

function filterClients(clients: ClientEntry[], query: string, activity: string) {
  const needle = query.trim().toLowerCase();
  return clients.filter(client => {
    if (activity !== "all" && normalizeFacet(client.primaryActivity, "unclassified") !== activity) return false;
    if (!needle) return true;
    return clientSearchText(client).includes(needle);
  });
}

function clientSearchText(client: ClientEntry) {
  return [
    client.id,
    client.hostname,
    client.mac,
    client.vendor,
    client.state,
    client.primaryActivity,
    client.lastProtocol,
    client.lastProtocolDetail,
    client.inferredOSFamily,
    client.inferredDeviceClass,
    client.fingerprintConfidence,
    client.stickyUntil,
    client.stickyState,
    client.clientPolicy,
    client.clientPolicyMode,
    ...(client.addresses ?? []),
    ...(client.sources ?? []),
    ...(client.peers ?? []),
    ...(client.protocolMix ?? []),
    ...(client.fingerprintSignals ?? []),
    ...(client.isolationPolicy ?? []),
  ].filter(Boolean).join(" ").toLowerCase();
}

function formatClientPolicyMode(value?: string) {
  const normalized = String(value ?? "").trim().toLowerCase();
  if (normalized === "trusted") return "trusted";
  if (normalized === "exclude") return "guest by default";
  return "guest";
}

function clientIdentityMap(clients: ClientEntry[]) {
  const identities = new Map<string, ClientIdentity>();
  for (const client of clients) {
    const identity = clientIdentity(client);
    if (!identity) continue;
    for (const address of clientIdentityAddresses(client)) {
      if (!identities.has(address)) identities.set(address, identity);
    }
  }
  return identities;
}

function clientIdentity(client: ClientEntry): ClientIdentity | undefined {
  const hostname = cleanIdentityPart(client.hostname);
  const vendor = cleanIdentityPart(client.vendor);
  const osFamily = cleanIdentityPart(client.inferredOSFamily);
  const deviceClass = cleanIdentityPart(client.inferredDeviceClass);
  const mac = cleanIdentityPart(client.mac);
  const id = cleanIdentityPart(client.id);
  const primaryAddress = cleanIdentityPart((client.addresses ?? []).find(address => isLikelyIPAddress(normalizeAddressKey(address))));
  const primary = hostname || vendor || mac || (id && !isLikelyIPAddress(id) ? id : "") || primaryAddress || (id && isLikelyIPAddress(id) ? id : "");
  const kind = [osFamily, deviceClass].filter(Boolean).join("/");
  if (!primary && !kind) return undefined;
  const details = [kind, vendor && vendor !== primary ? vendor : ""].filter(Boolean);
  const label = details.length ? `${primary} [${details.join(", ")}]` : primary;
  return {
    label,
    compactLabel: details.length ? `${primary} - ${details.join(" - ")}` : primary,
    searchText: [
      label,
      primary,
      hostname,
      vendor,
      osFamily,
      deviceClass,
      client.mac,
      client.id,
      ...(client.addresses ?? []),
    ].filter(Boolean).join(" ").toLowerCase(),
  };
}

function clientIdentityAddresses(client: ClientEntry) {
  const values = new Set<string>();
  for (const address of client.addresses ?? []) {
    const normalized = normalizeAddressKey(address);
    if (normalized) values.add(normalized);
  }
  const id = normalizeAddressKey(client.id);
  if (id && isLikelyIPAddress(id)) values.add(id);
  return Array.from(values);
}

function normalizeAddressKey(address?: string) {
  const value = String(address ?? "").trim();
  if (!value) return "";
  const withoutCIDR = value.split("/")[0] ?? "";
  return withoutCIDR.replace(/^\[/, "").replace(/\]$/, "").toLowerCase();
}

function cleanIdentityPart(value?: string) {
  const trimmed = String(value ?? "").trim();
  if (!trimmed || trimmed === "-" || trimmed.toLowerCase() === "unknown") return "";
  return trimmed;
}

function isLikelyIPAddress(value: string) {
  return /^\d{1,3}(?:\.\d{1,3}){3}$/.test(value) || value.includes(":");
}

function isLikelyLocalClientAddress(value: string) {
  const address = normalizeAddressKey(value);
  if (!address) return false;
  const ipv4 = address.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/);
  if (ipv4) {
    const octets = ipv4.slice(1).map(Number);
    if (octets.some(octet => !Number.isFinite(octet) || octet < 0 || octet > 255)) return false;
    return octets[0] === 10
      || (octets[0] === 172 && octets[1] >= 16 && octets[1] <= 31)
      || (octets[0] === 192 && octets[1] === 168)
      || (octets[0] === 169 && octets[1] === 254);
  }
  const lower = address.toLowerCase();
  return lower.startsWith("fe80:") || lower.startsWith("fc") || lower.startsWith("fd");
}

function formatClientActivity(value: string) {
  const normalized = normalizeFacet(value, "unclassified");
  if (normalized === "iot-telemetry") return "IoT telemetry";
  if (normalized === "resolver-only") return "Resolver only";
  if (normalized === "web-heavy") return "Web heavy";
  if (normalized === "streaming") return "Streaming";
  if (normalized === "gaming") return "Gaming";
  if (normalized === "mixed") return "Mixed";
  if (normalized === "unclassified") return "Unclassified";
  return normalized.split("-").map(part => part.charAt(0).toUpperCase() + part.slice(1)).join(" ");
}

function clientActivityColor(value: string): "brand" | "danger" | "informative" | "severe" | "subtle" | "success" | "warning" {
  const normalized = normalizeFacet(value, "unclassified");
  if (normalized === "web-heavy") return "brand";
  if (normalized === "iot-telemetry") return "warning";
  if (normalized === "resolver-only") return "success";
  if (normalized === "mixed") return "informative";
  return "subtle";
}

function filterGenerations(generations: GenerationRecord[], query: string) {
  const needle = query.trim().toLowerCase();
  if (!needle) return generations;
  return generations.filter(row => generationSearchText(row).includes(needle));
}

function generationSearchText(row: GenerationRecord) {
  return [
    row.generation,
    row.phase,
    row.configHash,
    row.startedAt,
    row.finishedAt,
    row.hasYaml ? "yaml stored" : "yaml unavailable",
  ].filter(value => value !== undefined && value !== "").join(" ").toLowerCase();
}

function clientOnline(row: ClientRow) {
  const state = String(row.state ?? "").toLowerCase();
  if (/failed|stale|expired|offline/.test(state)) return false;
  return row.addresses.size > 0 || !!row.bytesIn || !!row.bytesOut || row.peers.size > 0;
}

function primaryClientAddress(row: ClientRow) {
  const addresses = Array.from(row.addresses);
  return addresses.find(address => address.includes("."))
    ?? addresses.find(address => isStableIPv6(address))
    ?? addresses[0]
    ?? row.ip
    ?? "";
}

function clientConnectionAddresses(row: ClientRow) {
  const values = new Set<string>();
  for (const address of row.addresses) {
    const normalized = normalizeAddressKey(address);
    if (normalized) values.add(normalized);
  }
  const id = normalizeAddressKey(row.id);
  if (id && isLikelyIPAddress(id)) values.add(id);
  const primary = normalizeAddressKey(row.ip);
  if (primary && isLikelyIPAddress(primary)) values.add(primary);
  return Array.from(values);
}

function formatPrimaryClientAddress(address: string) {
  if (!address || address === "-") return "-";
  if (address.includes(".")) return address;
  const [host, suffix] = address.split("/", 2);
  if (host.length <= 24) return address;
  const shortHost = `${host.slice(0, 10)}...${host.slice(-8)}`;
  return suffix ? `${shortHost}/${suffix}` : shortHost;
}

function groupedClientAddresses(addresses: string[]) {
  const groups = { ipv4: [] as string[], ipv6Stable: [] as string[], ipv6Privacy: [] as string[] };
  for (const address of addresses) {
    if (address.includes(".")) groups.ipv4.push(address);
    else if (isStableIPv6(address)) groups.ipv6Stable.push(address);
    else groups.ipv6Privacy.push(address);
  }
  return groups;
}

function isStableIPv6(address: string) {
  const text = address.split("/")[0].toLowerCase();
  return text.includes(":ff:fe") || /(^|:)0*1[123]$/.test(text) || text.endsWith("::1");
}

function clientOSFamily(row: ClientRow) {
  return row.inferredOSFamily || "-";
}

function formatClientOSFamily(family: string) {
  const normalized = family.trim().toLowerCase();
  if (normalized === "nintendo") return "Nintendo";
  if (normalized === "playstation") return "PlayStation";
  if (normalized === "xbox") return "Xbox";
  if (normalized === "steam-os" || normalized === "steamos") return "SteamOS";
  if (normalized === "iot") return "IoT";
  if (normalized === "printer") return "Printer";
  if (normalized === "nas") return "NAS";
  if (normalized === "voip") return "VoIP";
  return family;
}

function clientOSBadgeColor(family: string) {
  switch (family.toLowerCase()) {
    case "nintendo":
    case "playstation":
    case "xbox":
    case "steam-os":
    case "steamos":
      return "important";
    case "apple":
      return "brand";
    case "windows":
      return "informative";
    case "android":
      return "success";
    case "iot":
      return "warning";
    case "printer":
    case "nas":
    case "voip":
      return "informative";
    case "embedded":
      return "warning";
    default:
      return "subtle";
  }
}

function clientLastSeen(row: ClientRow) {
  if (clientOnline(row)) return "now";
  if (row.expiresAt) return relativeTimeText(row.expiresAt) || absoluteTime(row.expiresAt);
  return "-";
}

function formatBytes(value?: number) {
  if (typeof value !== "number" || !Number.isFinite(value)) return "not collected";
  if (value <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let current = value;
  let unit = 0;
  while (current >= 1024 && unit < units.length - 1) {
    current /= 1024;
    unit++;
  }
  const digits = current >= 100 || unit === 0 ? 0 : current >= 10 ? 1 : 2;
  return `${current.toFixed(digits)} ${units[unit]}`;
}

function formatSeconds(value?: number) {
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) return "-";
  if (value < 60) return `${Math.round(value)}s`;
  if (value < 3600) return `${Math.round(value / 60)}m`;
  if (value < 86400) return `${Math.round(value / 3600)}h`;
  return `${Math.round(value / 86400)}d`;
}

function formatMilliseconds(value?: number) {
  if (typeof value !== "number" || !Number.isFinite(value)) return "-";
  if (value < 1) return `${value.toFixed(2)} ms`;
  if (value < 10) return `${value.toFixed(1)} ms`;
  return `${Math.round(value)} ms`;
}

function durationLabel(value?: string, millis?: number) {
  if (typeof millis === "number" && Number.isFinite(millis) && millis > 0) return formatMilliseconds(millis);
  return value || "-";
}

function formatPercent(value?: number) {
  if (typeof value !== "number" || !Number.isFinite(value)) return "-";
  return `${Math.round(value * 100)}%`;
}

function systemCPULabel(usage?: SystemUsage) {
  if (typeof usage?.cpuPercent === "number" && Number.isFinite(usage.cpuPercent)) return formatPercent(usage.cpuPercent);
  if (typeof usage?.load1 === "number" && Number.isFinite(usage.load1)) return `load ${usage.load1.toFixed(2)}`;
  return "-";
}

function systemMemoryLabel(usage?: SystemUsage) {
  if (!usage?.memoryUsedPercent) return "-";
  return `${formatPercent(usage.memoryUsedPercent)} / ${formatBytes(usage.memoryTotalBytes)}`;
}

function systemDiskLabel(usage?: SystemUsage) {
  const disk = usage?.disks?.[0];
  if (!disk?.usedPercent) return "-";
  return `${formatPercent(disk.usedPercent)} / ${formatBytes(disk.totalBytes)}`;
}

function dpiLatencyLabel(dpi?: DPIStatus) {
  const stats = dpi?.classifier?.stats;
  const average = Number(stats?.averageLatencyMs);
  const max = Number(stats?.maxLatencyMs);
  if (!Number.isFinite(average) || average <= 0) return "-";
  return `${formatMilliseconds(average)} avg / ${formatMilliseconds(max)} max`;
}

function slowestControllerLabel(controllers: ControllerStatus[]) {
  let slowest: ControllerStatus | undefined;
  for (const controller of controllers) {
    if ((controller.maxDurationMillis ?? 0) > (slowest?.maxDurationMillis ?? 0)) slowest = controller;
  }
  if (!slowest || !slowest.maxDurationMillis) return "-";
  return `${slowest.name ?? "controller"} ${formatMilliseconds(slowest.maxDurationMillis)}`;
}

function denyRows(logs: FirewallLog[]) {
  const totals = new Map<string, { key: string; src: string; dst: string; proto: string; tcpFlags: string; trafficClass: string; correlation: string; dpi: string; count: number; example?: FirewallLog }>();
  for (const log of logs) {
    const key = firewallLogKey(log);
    const row = totals.get(key) ?? {
      key,
      src: log.srcAddress || "-",
      dst: log.dstAddress || "-",
      proto: log.protocol || "-",
      tcpFlags: firewallTCPFlags(log),
      trafficClass: firewallTrafficClass(log),
      correlation: firewallCorrelation(log),
      dpi: firewallDPIText(log),
      count: 0,
      example: log,
    };
    if (!row.tcpFlags) row.tcpFlags = firewallTCPFlags(log);
    if (!row.trafficClass || row.trafficClass === "unclassified") row.trafficClass = firewallTrafficClass(log);
    if (!row.dpi) {
      row.dpi = firewallDPIText(log);
      row.example = log;
    }
    row.count++;
    totals.set(key, row);
  }
  return Array.from(totals.values()).sort((a, b) => b.count - a.count || a.src.localeCompare(b.src)).slice(0, 10);
}

function firewallLogKey(log: FirewallLog) {
  return `${firewallTupleKey(log.srcAddress, log.srcPort, log.dstAddress, log.dstPort, log.protocol)}>${firewallTCPFlags(log)}>${firewallTrafficClass(log)}`;
}

function firewallCorrelation(log: FirewallLog) {
  return log.correlation || "true_suspicious";
}

function firewallTCPFlags(log: FirewallLog) {
  return String(log.tcpFlags ?? "").trim();
}

function firewallTrafficClass(log: FirewallLog) {
  if (firewallDPIText(log)) return "DPI label";
  const protocol = normalizeFacet(log.protocol || log.l3Proto, "");
  if (protocol === "udp" || protocol === "icmp" || protocol === "icmpv6") return "stateless probe";
  if (protocol === "tcp") {
    const flags = new Set(firewallTCPFlags(log).split(/[, ]+/).map(flag => flag.trim().toUpperCase()).filter(Boolean));
    if (flags.size === 1 && flags.has("SYN")) return "new-connection attempt / scan";
    if (flags.size === 1 && flags.has("ACK")) {
      return firewallCorrelation(log) === "orphan_return" ? "orphan return" : "established follow";
    }
    if (flags.has("RST") || flags.has("FIN")) return "termination";
  }
  if (firewallCorrelation(log) === "orphan_return") return "orphan return";
  return "unclassified";
}

function firewallTrafficClassColor(value: string): "brand" | "danger" | "informative" | "severe" | "subtle" | "success" | "warning" {
  const normalized = value.toLowerCase();
  if (normalized.includes("scan")) return "danger";
  if (normalized.includes("orphan")) return "warning";
  if (normalized.includes("established")) return "informative";
  if (normalized.includes("termination")) return "subtle";
  if (normalized.includes("stateless")) return "warning";
  if (normalized.includes("dpi")) return "brand";
  return "subtle";
}

function firewallDPIText(log: FirewallLog) {
  if (log.dpiTlsSNI) return `tls-sni:${log.dpiTlsSNI}`;
  if (log.dpiHttpHost) return `http-host:${log.dpiHttpHost}`;
  if (log.dpiDnsQuery) return `${firewallQueryLabel(log.dpiApp)}:${log.dpiDnsQuery}`;
  const parts = [log.dpiApp, log.dpiCategory].filter(Boolean);
  if (log.dpiConfidence) parts.push(`${log.dpiConfidence}%`);
  const structured = parts.join(" ");
  if (structured) return structured;
  return firewallDPITextFromHint(log.hint);
}

function firewallDPIClassification(log: FirewallLog, dnsLabels: Record<string, string>) {
  const detail = firewallDPIText(log);
  const fallback = firewallPortFallback(log, dnsLabels, Boolean(detail));
  if (detail && fallback && preferPortFallbackOverApp(log?.dpiApp || "dns", fallback.app)) {
    return {
      source: "port-fallback" as const,
      detail: `port-guess:${fallback.label}`,
      confidence: 30,
      cacheHit: false,
    };
  }
  if (detail) {
    return {
      source: "dpi" as const,
      detail,
      confidence: log.dpiConfidence,
      cacheHit: String(log.hint ?? "").includes("dpi flow cache hit") || String(log.correlationDetail ?? "").includes("expired"),
    };
  }
  if (fallback) {
    return {
      source: "port-fallback" as const,
      detail: `port-guess:${fallback.label}`,
      confidence: 30,
      cacheHit: false,
    };
  }
  return { source: "none" as const, detail: "", confidence: 0, cacheHit: false };
}

function firewallDPITextFromHint(hint?: string) {
  if (!hint) return "";
  const values = new Map<string, string>();
  for (const part of hint.split(/\s+/)) {
    const [key, value] = part.split("=", 2);
    if (key?.startsWith("dpi.") && value) values.set(key, value);
  }
  if (values.has("dpi.tls_sni")) return `tls-sni:${values.get("dpi.tls_sni")}`;
  if (values.has("dpi.http_host")) return `http-host:${values.get("dpi.http_host")}`;
  if (values.has("dpi.dns_query")) return `${firewallQueryLabel(values.get("dpi.app"))}:${values.get("dpi.dns_query")}`;
  return [values.get("dpi.app"), values.get("dpi.category"), values.get("dpi.confidence") ? `${values.get("dpi.confidence")}%` : ""].filter(Boolean).join(" ");
}

function firewallQueryLabel(app?: string) {
  const normalized = String(app ?? "").toLowerCase();
  if (normalized === "netbios") return "nbns-query";
  if (normalized === "tailscale") return "tailscale-dns";
  return "dns-query";
}

function firewallPortFallback(log: FirewallLog, dnsLabels: Record<string, string>, allowKnown = false): ConnectionPortFallback | undefined {
  if (!allowKnown && firewallDPIText(log)) return undefined;
  const protocol = normalizeFacet(log.protocol || log.l3Proto, "");
  const labels = [
    log.dstHostname || (log.dstAddress ? dnsLabels[log.dstAddress] : ""),
    log.srcHostname || (log.srcAddress ? dnsLabels[log.srcAddress] : ""),
  ];
  const ports = [
    { port: log.dstPort ? String(log.dstPort) : "", peerLabel: labels[0] ?? "", service: log.dstService ?? "" },
    { port: log.srcPort ? String(log.srcPort) : "", peerLabel: labels[1] ?? "", service: log.srcService ?? "" },
  ].filter(item => item.port);
  for (const item of ports) {
    const app = portProtocolFallback(protocol, item.port, item.peerLabel);
    if (app) return { app, port: item.port, label: formatPortGuessLabel(app, item.port, item.peerLabel, item.service || serviceNameForPort(item.port)) };
  }
  return undefined;
}

function firewallTupleKey(source?: string, sourcePort?: string | number, destination?: string, destinationPort?: string | number, protocol?: string) {
  return `${source || "-"}:${sourcePort || ""}>${destination || "-"}:${destinationPort || ""}>${protocol || "-"}`;
}

function absoluteTime(value?: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return `${new Intl.DateTimeFormat("en-US", { month: "2-digit", day: "2-digit" }).format(date)} ${new Intl.DateTimeFormat("en-US", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }).format(date)}`;
}

function relativeTimeText(value?: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const diffSeconds = Math.round((date.getTime() - Date.now()) / 1000);
  const abs = Math.abs(diffSeconds);
  const rtf = new Intl.RelativeTimeFormat("en-US", { numeric: "auto" });
  if (abs < 60) return rtf.format(diffSeconds, "second");
  const diffMinutes = Math.round(diffSeconds / 60);
  if (Math.abs(diffMinutes) < 60) return rtf.format(diffMinutes, "minute");
  const diffHours = Math.round(diffMinutes / 60);
  if (Math.abs(diffHours) < 48) return rtf.format(diffHours, "hour");
  const diffDays = Math.round(diffHours / 24);
  return rtf.format(diffDays, "day");
}

createRoot(document.getElementById("root")!).render(<App />);
