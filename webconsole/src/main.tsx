// SPDX-License-Identifier: BSD-3-Clause

import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Badge,
  Button,
  Card,
  CardHeader,
  FluentProvider,
  Input,
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
  ChevronDownRegular,
  ChevronRightRegular,
  DocumentTextRegular,
  HomeRegular,
  NavigationRegular,
  PeopleRegular,
  PlugConnectedRegular,
  ServerRegular,
  ShieldRegular,
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
  dhcpLeases?: DHCPLease[];
  neighbors?: NeighborEntry[];
  clients?: ClientEntry[];
  vpn?: VPNStatus;
  errors?: string[];
};

type ResourceStatus = {
  apiVersion?: string;
  kind?: string;
  name?: string;
  status?: Record<string, unknown>;
};

type ControllerStatus = {
  name?: string;
  mode?: string;
  reason?: string;
  message?: string;
  resourceKinds?: string[];
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
};

type DNSQuery = {
  questionName?: string;
  answers?: string[];
};

type TrafficFlow = {
  clientAddress?: string;
  peerAddress?: string;
  resolvedHostname?: string;
  tlsSNI?: string;
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
  protocol?: string;
  l3Proto?: string;
  ruleName?: string;
  inIface?: string;
  outIface?: string;
  packetBytes?: number;
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
  family: string;
  protocol: string;
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
};

type ViewKey = "overview" | "controllers" | "clients" | "connections" | "vpn" | "events" | "firewall" | "config" | "generations";
type NavSubItem = { key: string; label: string; count?: number; view: ViewKey; targetID: string };

const cfg = window.__ROUTERD_WEB_CONSOLE__ ?? { basePath: "/", title: "routerd" };
const basePath = normalizeBasePath(cfg.basePath);
const defaultConnectionPageSize = 25;
const connectionPageSizeOptions = [25, 50, 100];
const collapsedStorageKey = "routerd.webconsole.collapsed";
const connectionPagesStorageKey = "routerd.webconsole.connectionPages";
const connectionPageSizesStorageKey = "routerd.webconsole.connectionPageSizes";
const navItems: { key: ViewKey; label: string; description: string; icon: React.ReactNode }[] = [
  { key: "overview", label: "Overview", description: "Status and interfaces", icon: <HomeRegular /> },
  { key: "controllers", label: "Controllers", description: "Live and dry-run controller modes", icon: <ServerRegular /> },
  { key: "clients", label: "Clients", description: "Leases and endpoint traffic", icon: <PeopleRegular /> },
  { key: "connections", label: "Connections", description: "conntrack and live flows", icon: <PlugConnectedRegular /> },
  { key: "vpn", label: "VPN", description: "WireGuard and Tailscale peers", icon: <PlugConnectedRegular /> },
  { key: "events", label: "Events", description: "Bus events and resource changes", icon: <ServerRegular /> },
  { key: "firewall", label: "Firewall", description: "Deny ranking and timeline", icon: <ShieldRegular /> },
  { key: "config", label: "Config", description: "Read-only YAML tree", icon: <DocumentTextRegular /> },
  { key: "generations", label: "Generations", description: "Applied YAML history and diffs", icon: <DocumentTextRegular /> },
];
const viewKeys = new Set<string>(navItems.map(item => item.key));

const useStyles = makeStyles({
  shell: {
    minHeight: "100vh",
    backgroundColor: "#0b1118",
    color: tokens.colorNeutralForeground1,
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
    gridTemplateColumns: "248px minmax(0, 1fr)",
    minHeight: "calc(100vh - 49px)",
    "@media (max-width: 860px)": {
      gridTemplateColumns: "1fr",
    },
  },
  layoutCollapsed: {
    gridTemplateColumns: "56px minmax(0, 1fr)",
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
    "@media (max-width: 860px)": {
      position: "static",
      height: "auto",
      display: "flex",
      overflowX: "auto",
      overflowY: "hidden",
      borderRight: 0,
      borderBottom: "1px solid #243041",
      padding: "8px",
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
      display: "flex",
      gap: "6px",
      minWidth: "max-content",
    },
  },
  navButton: {
    width: "100%",
    justifyContent: "flex-start",
    borderRadius: "4px",
    padding: "9px 10px",
    color: tokens.colorNeutralForeground2,
    backgroundColor: "transparent",
    border: "1px solid transparent",
    ":hover": {
      backgroundColor: "#172235",
      color: tokens.colorNeutralForeground1,
    },
    "@media (max-width: 860px)": {
      width: "auto",
      minWidth: "132px",
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
      borderLeft: "1px solid #2f4664",
      borderBottom: "3px solid #60cdff",
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
    borderRadius: "4px",
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
  },
  sectionButton: {
    borderRadius: "4px",
  },
  jumpBar: {
    display: "flex",
    flexWrap: "wrap",
    gap: "6px",
    marginBottom: "12px",
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
    borderRadius: "4px",
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
  main: {
    padding: "16px 20px 24px",
    display: "grid",
    gap: "16px",
  },
  dryRunBanner: {
    display: "flex",
    flexWrap: "wrap",
    alignItems: "center",
    justifyContent: "space-between",
    gap: "10px",
    padding: "12px",
    border: "1px solid #8a5a00",
    borderRadius: "4px",
    backgroundColor: "#332300",
  },
  grid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(170px, 1fr))",
    gap: "12px",
  },
  metric: {
    minWidth: 0,
    borderRadius: "4px",
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
    gridTemplateColumns: "minmax(0, 1.4fr) minmax(320px, 0.8fr)",
    gap: "16px",
    "@media (max-width: 900px)": {
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
    padding: "8px 0",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    "@media (max-width: 640px)": {
      gridTemplateColumns: "1fr",
    },
  },
  chartGrid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))",
    gap: "12px",
  },
  chartCard: {
    minWidth: 0,
    display: "grid",
    gap: "8px",
    borderRadius: "4px",
    border: "1px solid #243041",
    backgroundColor: "#101a28",
    padding: "10px",
  },
  chartSvg: {
    width: "100%",
    height: "86px",
    display: "block",
  },
  resourceFilters: {
    display: "grid",
    gridTemplateColumns: "minmax(220px, 1fr) 180px",
    gap: "8px",
    alignItems: "end",
    marginBottom: "12px",
    "@media (max-width: 640px)": {
      gridTemplateColumns: "1fr",
    },
  },
  singleSearchRow: {
    display: "grid",
    gridTemplateColumns: "minmax(220px, 420px)",
    gap: "8px",
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
    gridTemplateColumns: "minmax(0, 1.25fr) minmax(320px, 0.75fr)",
    gap: "16px",
    alignItems: "start",
    "@media (max-width: 900px)": {
      gridTemplateColumns: "1fr",
    },
  },
  eventFilters: {
    display: "grid",
    gridTemplateColumns: "minmax(220px, 1.4fr) repeat(4, minmax(120px, 1fr))",
    gap: "8px",
    alignItems: "end",
    marginBottom: "12px",
    "@media (max-width: 900px)": {
      gridTemplateColumns: "repeat(auto-fit, minmax(150px, 1fr))",
    },
  },
  firewallStack: {
    display: "grid",
    gap: "16px",
  },
  clientsGrid: {
    display: "grid",
    gap: "16px",
    gridTemplateColumns: "1fr",
  },
  vpnGrid: {
    display: "grid",
    gap: "16px",
    gridTemplateColumns: "1fr",
  },
  vpnSummaryGrid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))",
    gap: "10px",
    marginBottom: "12px",
  },
  interfaceGrid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit, minmax(230px, 1fr))",
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
    maxWidth: "100%",
  },
  dataTable: {
    minWidth: "720px",
    tableLayout: "fixed",
  },
  resourceTable: {
    minWidth: "900px",
    tableLayout: "fixed",
  },
  controllerTable: {
    minWidth: "820px",
    tableLayout: "fixed",
  },
  eventTable: {
    minWidth: "760px",
    tableLayout: "fixed",
  },
  connectionTable: {
    minWidth: "820px",
    tableLayout: "fixed",
  },
  clientInventoryTable: {
    minWidth: "1120px",
    tableLayout: "fixed",
  },
  clientTrafficTable: {
    minWidth: "760px",
    tableLayout: "fixed",
  },
  dhcpLeaseTable: {
    minWidth: "900px",
    tableLayout: "fixed",
  },
  vpnPeerTable: {
    minWidth: "980px",
    tableLayout: "fixed",
  },
  code: {
    fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace",
    whiteSpace: "nowrap",
    wordBreak: "normal",
    overflowWrap: "normal",
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
    gridTemplateColumns: "minmax(220px, 1.4fr) repeat(5, minmax(120px, 1fr))",
    gap: "8px",
    alignItems: "end",
    marginBottom: "12px",
    "@media (max-width: 900px)": {
      gridTemplateColumns: "repeat(auto-fit, minmax(150px, 1fr))",
    },
  },
  firewallFilters: {
    display: "grid",
    gridTemplateColumns: "minmax(220px, 1.4fr) repeat(4, minmax(130px, 1fr))",
    gap: "8px",
    alignItems: "end",
    marginTop: "12px",
    marginBottom: "8px",
    "@media (max-width: 900px)": {
      gridTemplateColumns: "repeat(auto-fit, minmax(150px, 1fr))",
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
  connectionFlow: {
    display: "grid",
    gap: "2px",
    minWidth: 0,
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
    gridTemplateColumns: "64px minmax(0, 1fr) 80px",
    gap: "10px",
    alignItems: "center",
    padding: "8px 10px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    "@media (max-width: 640px)": {
      gridTemplateColumns: "48px minmax(0, 1fr)",
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
    gridTemplateColumns: "56px minmax(240px, 1.35fr) minmax(240px, 1.35fr) 72px",
    gap: "10px",
    padding: "0 10px 6px",
    color: tokens.colorNeutralForeground3,
    fontSize: "12px",
    fontWeight: 600,
    "@media (max-width: 760px)": {
      display: "none",
    },
  },
  firewallTimelineHeader: {
    display: "grid",
    gridTemplateColumns: "96px 68px minmax(240px, 1.4fr) minmax(240px, 1.4fr) 72px minmax(120px, 0.7fr)",
    gap: "10px",
    padding: "0 10px 6px",
    color: tokens.colorNeutralForeground3,
    fontSize: "12px",
    fontWeight: 600,
    "@media (max-width: 760px)": {
      display: "none",
    },
  },
  firewallRankRow: {
    display: "grid",
    gridTemplateColumns: "56px minmax(240px, 1.35fr) minmax(240px, 1.35fr) 72px",
    gap: "10px",
    alignItems: "start",
    padding: "8px 10px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    "@media (max-width: 760px)": {
      gridTemplateColumns: "1fr",
      gap: "8px",
      padding: "10px",
      border: `1px solid ${tokens.colorNeutralStroke2}`,
      borderRadius: tokens.borderRadiusMedium,
      backgroundColor: tokens.colorNeutralBackground2,
    },
  },
  firewallTimelineRow: {
    display: "grid",
    gridTemplateColumns: "96px 68px minmax(240px, 1.4fr) minmax(240px, 1.4fr) 72px minmax(120px, 0.7fr)",
    gap: "10px",
    alignItems: "start",
    padding: "8px 10px",
    borderTop: `1px solid ${tokens.colorNeutralStroke2}`,
    "@media (max-width: 760px)": {
      gridTemplateColumns: "1fr",
      gap: "8px",
      padding: "10px",
      border: `1px solid ${tokens.colorNeutralStroke2}`,
      borderRadius: tokens.borderRadiusMedium,
      backgroundColor: tokens.colorNeutralBackground2,
    },
  },
  firewallCell: {
    minWidth: 0,
    overflow: "hidden",
    "@media (max-width: 760px)": {
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
    "@media (max-width: 760px)": {
      display: "block",
    },
  },
  firewallCellValue: {
    minWidth: 0,
  },
  pager: {
    display: "flex",
    flexWrap: "wrap",
    gap: "8px",
    alignItems: "center",
    justifyContent: "flex-end",
  },
  pageSize: {
    width: "86px",
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
    "@media (max-width: 900px)": {
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
  config: {
    maxHeight: "66vh",
    overflow: "auto",
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
    minWidth: "960px",
    tableLayout: "fixed",
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
    width: "180px",
  },
  diffPanel: {
    maxHeight: "62vh",
    overflow: "auto",
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: tokens.borderRadiusMedium,
    backgroundColor: tokens.colorNeutralBackground2,
    padding: "10px",
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
    gridTemplateColumns: "minmax(130px, 0.42fr) minmax(0, 1fr)",
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
  const [generations, setGenerations] = useState<GenerationRecord[]>([]);
  const [generationDiff, setGenerationDiff] = useState<string>("");
  const [configPlanDiff, setConfigPlanDiff] = useState<string>("");
  const [generationConfig, setGenerationConfig] = useState<{ generation: number; text: string } | null>(null);
  const [generationFrom, setGenerationFrom] = useState<string>("");
  const [generationTo, setGenerationTo] = useState<string>("");
  const [error, setError] = useState<string>("");
  const [clientQuery, setClientQuery] = useState("");
  const [generationQuery, setGenerationQuery] = useState("");
  const [selected, setSelected] = useState<ViewKey>(initialLocation.view);
  const [selectedTargetID, setSelectedTargetID] = useState<string | undefined>(initialLocation.targetID);
  const [navCollapsed, setNavCollapsed] = useState(false);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(() => readStoredRecord(collapsedStorageKey));
  const [connectionPages, setConnectionPages] = useState<Record<string, number>>(() => readStoredRecord(connectionPagesStorageKey));
  const [connectionPageSizes, setConnectionPageSizes] = useState<Record<string, number>>(() => readStoredRecord(connectionPageSizesStorageKey));
  const [connectionFilters, setConnectionFilters] = useState<ConnectionFilters>({
    query: "",
    family: "all",
    protocol: "all",
    state: "all",
    sort: "observed",
    direction: "asc",
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

  async function refresh() {
    try {
      const [summaryResponse, configResponse, generationResponse] = await Promise.all([
        fetchJSON<Summary>("api/v1/summary?events=200&connections=240"),
        config ? Promise.resolve(config) : fetchJSON<ConfigSnapshot>("api/v1/config"),
        fetchJSON<GenerationRecord[]>("api/v1/generations?limit=200"),
      ]);
      setSummary(summaryResponse);
      setMetricSamples(current => appendMetricSample(current, summaryResponse));
      if (!config) setConfig(configResponse as ConfigSnapshot);
      setGenerations(generationResponse);
      setError("");
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    refresh();
    const id = window.setInterval(refresh, 5000);
    return () => window.clearInterval(id);
  }, []);

  useEffect(() => {
    const withYaml = generations.filter(row => row.hasYaml);
    if (!generationTo && withYaml[0]) setGenerationTo(String(withYaml[0].generation));
    if (!generationFrom && withYaml[1]) setGenerationFrom(String(withYaml[1].generation));
    if (!generationFrom && withYaml.length === 1) setGenerationFrom(String(withYaml[0].generation));
  }, [generations, generationFrom, generationTo]);

  const connections = summary?.connections?.entries ?? [];
  const dnsLabels = useMemo(() => dnsLabelMap(summary?.dnsQueries ?? []), [summary?.dnsQueries]);
  const leaseMap = useMemo(() => dhcpLeaseMap(summary?.dhcpLeases ?? []), [summary?.dhcpLeases]);
  const filteredConnections = useMemo(
    () => filterAndSortConnections(connections, dnsLabels, connectionFilters),
    [connections, dnsLabels, connectionFilters],
  );
  const connectionGroupsList = useMemo(() => connectionGroups(filteredConnections), [filteredConnections]);
  const connectionFacets = useMemo(() => connectionFilterFacets(connections), [connections]);
  const navSubItems = useMemo(() => navigationSubItems(selected, connectionGroupsList, summary), [selected, connectionGroupsList, summary]);
  const resources = useMemo(() => importantResources(summary?.resources ?? []), [summary?.resources]);
  const controllers = summary?.controllers ?? (summary?.status?.status?.controllers as ControllerStatus[] | undefined) ?? [];
  const dryRunControllers = useMemo(() => controllers.filter(controller => controller.mode === "dry-run"), [controllers]);
  const events = summary?.events ?? [];
  const filteredEvents = useMemo(() => filterEvents(events, eventFilters), [events, eventFilters]);
  const eventFacets = useMemo(() => eventFilterFacets(events), [events]);
  const firewallLogs = summary?.firewallLogs ?? [];
  const filteredFirewallLogs = useMemo(() => filterFirewallLogs(firewallLogs, firewallFilters), [firewallLogs, firewallFilters]);
  const firewallProtocols = useMemo(() => firewallProtocolFacets(firewallLogs), [firewallLogs]);
  const selectedEvent = useMemo(() => {
    if (filteredEvents.length === 0) return undefined;
    return filteredEvents.find(event => eventKey(event) === selectedEventKey) ?? filteredEvents[0];
  }, [filteredEvents, selectedEventKey]);
  const filteredClients = useMemo(() => filterClients(summary?.clients ?? [], clientQuery), [summary?.clients, clientQuery]);
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

  function showSection(item: NavSubItem) {
    navigateTo(item.view, item.targetID);
  }

  function sectionActive(item: NavSubItem) {
    return item.view === selected && selectedTargetID === item.targetID;
  }

  function navigateTo(view: ViewKey, targetID?: string) {
    setSelected(view);
    setSelectedTargetID(targetID);
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
      <div className={`${styles.layout} ${navCollapsed ? styles.layoutCollapsed : ""}`}>
        <aside className={`${styles.sidebar} ${navCollapsed ? styles.sidebarCollapsed : ""}`} aria-label="Web console navigation">
          <div className={styles.navSection}>
            {navItems.map(item => (
              <React.Fragment key={item.key}>
                <Button
                  appearance="subtle"
                  className={`${styles.navButton} ${navCollapsed ? styles.navButtonCollapsed : ""} ${selected === item.key ? styles.navButtonActive : ""}`}
                  onClick={() => navigateTo(item.key)}
                  aria-label={item.label}
                >
                  <span className={styles.navButtonInner}>
                    <span className={styles.navIcon}>{item.icon}</span>
                    <span className={`${styles.navText} ${navCollapsed ? styles.navTextCollapsed : ""}`}>
                      <Text weight={selected === item.key ? "semibold" : "regular"}>{item.label}</Text>
                      <Text size={200} className={styles.navDescription}>{item.description}</Text>
                    </span>
                  </span>
                </Button>
                {!navCollapsed && item.key === selected && navSubItems.length > 0 ? (
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
              <Text size={200} className={styles.muted}>{summary?.generatedAt ? <>Updated <RelativeTime value={summary.generatedAt} /></> : ""}</Text>
              <Button appearance="primary" icon={<ArrowClockwiseRegular />} onClick={refresh}>Refresh</Button>
            </div>
          </div>
          {navSubItems.length > 0 ? (
            <div className={styles.sectionBar} aria-label={`${selectedNav.label} sections`}>
              {navSubItems.map(sub => (
                <Button
                  key={sub.key}
                  size="small"
                  appearance={sectionActive(sub) ? "primary" : "secondary"}
                  className={styles.sectionButton}
                  onClick={() => showSection(sub)}
                >
                  {sub.label}{sub.count !== undefined ? ` ${sub.count}` : ""}
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
                  </div>
                </div>
                <MetricCharts samples={metricSamples} />
                <Card id="overview-interfaces" className={styles.connectionAnchor}>
                  <CardHeader header={<Text weight="semibold">Interfaces</Text>} description={<Text className={styles.muted}>Role, link state, MTU, and assigned addresses</Text>} />
                  <InterfaceOverview interfaces={summary?.interfaces ?? []} />
                </Card>
                <OverviewActivity resources={summary?.resources ?? []} events={events} navigateTo={navigateTo} />
              </>
            ) : null}
            {selected === "controllers" ? (
              <Card id="controllers-table" className={styles.connectionAnchor}>
                <CardHeader
                  header={<Text weight="semibold">Controllers</Text>}
                  description={<Text className={styles.muted}>Controller mode, reason, and resource ownership surface</Text>}
                />
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
                    <div className={styles.singleSearchRow}>
                      <SearchControl label="Search clients" value={clientQuery} placeholder="name, MAC, address, vendor, peer" onChange={setClientQuery} />
                    </div>
                    <ClientInventory clients={filteredClients} />
                  </Card>
                ) : null}
                {activeClientTargetID === "clients-traffic" ? (
                  <Card id="clients-traffic" className={styles.connectionAnchor}>
                    <CardHeader header={<Text weight="semibold">Client traffic</Text>} description={<Text className={styles.muted}>Traffic grouped by client address</Text>} />
                    <ClientTraffic flows={summary?.trafficFlows ?? []} />
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
              description={<Text className={styles.muted}>{connectionFamilyCounts(summary?.connections)} / Showing {filteredConnections.length}</Text>}
            />
            <div className={styles.jumpBar}>
              <Button size="small" appearance="secondary" icon={<ArrowUpRegular />} onClick={scrollToTop}>Top</Button>
              {connectionGroupsList.map(group => {
                const label = connectionGroupLabel(group.key);
                return (
                  <Button key={group.key} size="small" appearance="secondary" onClick={() => showConnectionsGroup(group.key)}>
                    {label.family}/{label.protocol.toUpperCase()} {group.rows.length}
                  </Button>
                );
              })}
            </div>
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
                <Text size={200} className={styles.muted}>State</Text>
                <Select size="small" value={connectionFilters.state} onChange={event => updateConnectionFilter("state", event.target.value)}>
                  <option value="all">All</option>
                  {connectionFacets.states.map(value => <option key={value} value={value}>{formatFacet(value)}</option>)}
                </Select>
              </div>
              <div className={styles.filterControl}>
                <Text size={200} className={styles.muted}>Sort</Text>
                <Select size="small" value={connectionFilters.sort} onChange={event => updateConnectionFilter("sort", event.target.value)}>
                  <option value="observed">Observed</option>
                  <option value="state">State</option>
                  <option value="source">Source</option>
                  <option value="destination">Destination</option>
                  <option value="label">Label</option>
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
            </div>
            <div className={styles.connectionGroup}>
              {connectionGroupsList.map(group => (
                <ConnectionGroup
                  key={group.key}
                  group={group}
                  dnsLabels={dnsLabels}
                  collapsed={collapsed[group.key] ?? false}
                  toggle={() => setCollapsed(current => ({ ...current, [group.key]: !(current[group.key] ?? false) }))}
                  page={connectionPages[group.key] ?? 0}
                  pageSize={connectionPageSizes[group.key] ?? defaultConnectionPageSize}
                  setPage={page => setConnectionPages(current => ({ ...current, [group.key]: page }))}
                  setPageSize={size => {
                    setConnectionPageSizes(current => ({ ...current, [group.key]: size }));
                    setConnectionPages(current => ({ ...current, [group.key]: 0 }));
                  }}
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
                  <CardHeader header={<Text weight="semibold">Events</Text>} description={<Text className={styles.muted}>Showing {filteredEvents.length} of {events.length} bus events</Text>} />
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
                  <DenyRateChart logs={firewallLogs} />
                  <div className={styles.firewallFilters}>
                    <SearchControl label="Search" value={firewallFilters.query} placeholder="rule, interface, address, protocol" onChange={value => setFirewallFilters(current => ({ ...current, query: value }))} />
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
                  <FirewallSourceTopN logs={filteredFirewallLogs} dnsLabels={dnsLabels} leases={leaseMap} />
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
              <Card>
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
              BSD 3-Clause License - Copyright (c) 2026 Kirino Minato (imksoo) and routerd contributors
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
  const parsed = useMemo(() => parseConfig(config?.text), [config?.text]);
  return (
    <>
      <div className={styles.configToolbar}>
        <Text className={styles.muted}>Read-only view of the active routerd YAML</Text>
        <div className={styles.configModeButtons}>
          <Button size="small" appearance="secondary" disabled={!latestGeneration || !config?.text} onClick={loadPlanDiff}>
            Diff before apply
          </Button>
          <Button size="small" appearance={mode === "tree" ? "primary" : "secondary"} onClick={() => setMode("tree")}>Tree</Button>
          <Button size="small" appearance={mode === "raw" ? "primary" : "secondary"} onClick={() => setMode("raw")}>Raw YAML</Button>
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
            <ConfigTreeNode label="config" value={parsed.value} depth={0} />
          </div>
        ) : (
          <pre className={styles.pre}>{config?.text ?? "Config is unavailable"}</pre>
        )}
      </div>
      {planDiff ? (
        <div style={{ marginTop: "12px" }}>
          <Text weight="semibold">Current file vs latest applied generation</Text>
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
      <Card>
        <CardHeader
          header={<Text weight="semibold">Generations</Text>}
          description={<Text className={styles.muted}>Applied router YAML snapshots. Showing {generations.length} of {totalGenerations}. Older rows without YAML cannot be diffed.</Text>}
        />
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
        <div className={styles.tableWrap}>
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
                <TableRow key={row.generation}>
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
                      <Badge appearance="outline">rollback CLI only</Badge>
                    </div>
                  </TableCell>
                </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      </Card>
      {diff ? (
        <Card>
          <CardHeader header={<Text weight="semibold">Diff</Text>} description={<Text className={styles.muted}>Unified diff between selected generations</Text>} />
          <DiffView diff={diff} />
        </Card>
      ) : null}
      {config ? (
        <Card>
          <CardHeader header={<Text weight="semibold">Generation #{config.generation}</Text>} description={<Text className={styles.muted}>Stored YAML snapshot</Text>} />
          <div className={styles.diffPanel}><pre className={styles.pre}>{config.text}</pre></div>
        </Card>
      ) : null}
    </>
  );
}

function DiffView({ diff }: { diff: string }) {
  const styles = useStyles();
  const lines = diff.split(/\n/);
  return (
    <div className={styles.diffPanel}>
      {lines.map((line, index) => (
        <span key={index} className={`${styles.diffLine} ${line.startsWith("+") && !line.startsWith("+++") ? styles.diffAdded : ""} ${line.startsWith("-") && !line.startsWith("---") ? styles.diffRemoved : ""}`}>
          {line}
        </span>
      ))}
    </div>
  );
}

function ConfigTreeNode({ label, value, depth }: { label: string; value: unknown; depth: number }) {
  const styles = useStyles();
  if (Array.isArray(value)) {
    return (
      <details className={styles.treeNode} open={depth < 2}>
        <summary className={styles.treeSummary}>
          <span className={styles.treeRow}>
            <span className={styles.treeKey}>{label}</span>
            <span className={styles.treeMeta}>[{value.length} items]</span>
          </span>
        </summary>
        <div className={styles.treeChildren}>
          {value.map((item, index) => (
            <ConfigTreeNode key={`${index}-${configNodeLabel(index, item)}`} label={configNodeLabel(index, item)} value={item} depth={depth + 1} />
          ))}
        </div>
      </details>
    );
  }
  if (isRecord(value)) {
    const entries = Object.entries(value);
    return (
      <details className={styles.treeNode} open={depth < 2}>
        <summary className={styles.treeSummary}>
          <span className={styles.treeRow}>
            <span className={styles.treeKey}>{label}</span>
            <span className={styles.treeMeta}>{entries.length} keys</span>
          </span>
        </summary>
        <div className={styles.treeChildren}>
          {entries.map(([key, item]) => (
            <ConfigTreeNode key={key} label={key} value={item} depth={depth + 1} />
          ))}
        </div>
      </details>
    );
  }
  return (
    <div className={styles.treeLeaf}>
      <span className={styles.treeKey}>{label}</span>
      <code className={styles.treeValue}>{formatConfigScalar(value)}</code>
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

function ResourceTable({ resources, controllers }: { resources: ResourceStatus[]; controllers: ControllerStatus[] }) {
  const styles = useStyles();
  const dryRunByKind = useMemo(() => dryRunControllerByKind(controllers), [controllers]);
  const [query, setQuery] = useState("");
  const [phase, setPhase] = useState("all");
  const phases = useMemo(() => {
    const values = new Set<string>();
    for (const resource of resources) values.add(String(resource.status?.phase ?? "Unknown"));
    return Array.from(values).sort(facetSort);
  }, [resources]);
  const filtered = resources.filter(resource => {
    const resourcePhase = String(resource.status?.phase ?? "Unknown");
    if (phase !== "all" && resourcePhase !== phase) return false;
    if (!query.trim()) return true;
    return resourceSearchText(resource).includes(query.trim().toLowerCase());
  });
  return (
    <>
      <div className={styles.resourceFilters}>
        <SearchControl label="Search resources" value={query} placeholder="kind, name, phase, status detail" onChange={setQuery} />
        <div className={styles.filterControl}>
          <Text size={200} className={styles.muted}>Phase</Text>
          <Select size="small" value={phase} onChange={event => setPhase(event.target.value)}>
            <option value="all">All phases</option>
            {phases.map(value => <option key={value} value={value}>{value}</option>)}
          </Select>
        </div>
      </div>
      <Text size={200} className={styles.muted}>Showing {Math.min(filtered.length, 120)} of {filtered.length} matched resources / {resources.length} total</Text>
      <div className={styles.tableWrap}>
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
                <TableRow key={`${resource.apiVersion}/${resource.kind}/${resource.name}`}>
                  <TableCell><Highlighted text={resource.kind ?? ""} query={query} /></TableCell>
                  <TableCell><code className={styles.code}><Highlighted text={resource.name ?? ""} query={query} /></code></TableCell>
                  <TableCell><Badge appearance="tint" color={phaseColor(status.phase)}><Highlighted text={String(status.phase ?? "Unknown")} query={query} /></Badge></TableCell>
                  <TableCell>{dryRunController ? <Badge appearance="tint" color="warning">dry-run</Badge> : <Text size={200} className={styles.muted}>live</Text>}</TableCell>
                  <TableCell><code className={styles.wrapCode}><Highlighted text={resourceDetail(status)} query={query} /></code></TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
    </>
  );
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
      <Input className={styles.filterInput} size="small" value={value} placeholder={placeholder} onChange={(_, data) => onChange(data.value)} />
    </div>
  );
}

function ControllerTable({ controllers }: { controllers: ControllerStatus[] }) {
  const styles = useStyles();
  const rows = [...controllers].sort((a, b) => String(a.name ?? "").localeCompare(String(b.name ?? "")));
  return (
    <div className={styles.tableWrap}>
      <Table size="small" className={styles.controllerTable}>
        <colgroup>
          <col style={{ width: "170px" }} />
          <col style={{ width: "110px" }} />
          <col style={{ width: "260px" }} />
          <col />
        </colgroup>
        <TableHeader>
          <TableRow>
            <TableHeaderCell>Controller</TableHeaderCell>
            <TableHeaderCell>Mode</TableHeaderCell>
            <TableHeaderCell>Resource kinds</TableHeaderCell>
            <TableHeaderCell>Reason</TableHeaderCell>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map(controller => (
            <TableRow key={controller.name}>
              <TableCell><code className={styles.code}>{controller.name}</code></TableCell>
              <TableCell><Badge appearance="tint" color={controller.mode === "dry-run" ? "warning" : "success"}>{controller.mode ?? "unknown"}</Badge></TableCell>
              <TableCell>
                <div className={styles.badges}>
                  {(controller.resourceKinds ?? []).map(kind => <Badge key={kind} appearance="outline">{kind}</Badge>)}
                </div>
              </TableCell>
              <TableCell>
                <div className={styles.connectionFlow}>
                  <Text>{controller.reason || "-"}</Text>
                  {controller.message ? <Text size={200} className={styles.muted}>{controller.message}</Text> : null}
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
    <div className={styles.tableWrap}>
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
              <TableRow key={key} className={key === selectedKey ? styles.eventRowSelected : undefined} onClick={() => onSelect(event)}>
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
  collapsed,
  toggle,
  page,
  pageSize,
  setPage,
  setPageSize,
}: {
  group: { key: string; rows: ConnectionEntry[] };
  dnsLabels: Record<string, string>;
  collapsed: boolean;
  toggle: () => void;
  page: number;
  pageSize: number;
  setPage: (page: number) => void;
  setPageSize: (size: number) => void;
}) {
  const styles = useStyles();
  const label = connectionGroupLabel(group.key);
  const totalPages = Math.max(1, Math.ceil(group.rows.length / pageSize));
  const currentPage = Math.min(Math.max(page, 0), totalPages - 1);
  const start = currentPage * pageSize;
  const visibleRows = group.rows.slice(start, start + pageSize);
  return (
    <Card id={connectionGroupID(group.key)} className={styles.connectionAnchor}>
      <CardHeader
        header={<Text weight="semibold">{label.family}/{label.protocol.toUpperCase()} {group.rows.length}</Text>}
        description={!collapsed ? <Text className={styles.muted}>Showing {visibleRows.length ? start + 1 : 0}-{start + visibleRows.length} of {group.rows.length}</Text> : undefined}
        action={<Button appearance="subtle" icon={collapsed ? <ChevronRightRegular /> : <ChevronDownRegular />} onClick={toggle}>{collapsed ? "Open" : "Close"}</Button>}
      />
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
          <div className={styles.tableWrap}>
            <Table size="small" className={styles.connectionTable}>
              <colgroup>
                <col style={{ width: "132px" }} />
                <col />
                <col style={{ width: "28%" }} />
                <col style={{ width: "80px" }} />
              </colgroup>
              <TableHeader>
                <TableRow>
                  <TableHeaderCell>State</TableHeaderCell>
                  <TableHeaderCell>Flow</TableHeaderCell>
                  <TableHeaderCell>Destination label</TableHeaderCell>
                  <TableHeaderCell>Timeout</TableHeaderCell>
                </TableRow>
              </TableHeader>
              <TableBody>
                {visibleRows.map(entry => (
                  <TableRow key={flowKey(entry)}>
                    <TableCell>
                      <div className={styles.badges}>
                        <Badge appearance="tint" color={stateColor(entry.state)}>{entry.state || "stateless"}</Badge>
                        {entry.assured ? <Badge appearance="outline" color="success">assured</Badge> : null}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className={styles.connectionFlow}>
                        <code className={styles.wrapCode}>{endpoint(entry.original)}</code>
                      </div>
                    </TableCell>
                    <TableCell><code className={styles.wrapCode}>{dnsLabels[entry.original?.destination ?? ""] ?? "-"}</code></TableCell>
                    <TableCell>{entry.timeout ?? 0}s</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </>
      ) : null}
    </Card>
  );
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
            {item.managed ? <Badge appearance="outline" color="success">managed</Badge> : <Badge appearance="outline">adopted</Badge>}
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
          action={<Button size="small" appearance="secondary" onClick={() => navigateTo("clients")}>Clients</Button>}
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
      <PeerStatusStrip
        peers={peers.map(peer => ({
          key: peer.id || peer.dnsName || peer.hostName || "-",
          label: peer.hostName || peer.dnsName || "-",
          active: !!peer.online,
          detail: peer.relay || relativeTimeText(peer.lastSeen) || "direct",
        }))}
      />
      <div className={styles.tableWrap}>
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
              <TableRow key={peer.id || peer.dnsName || peer.hostName}>
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
          <PeerStatusStrip
            peers={(item.peers ?? []).map(peer => ({
              key: peer.publicKey || peer.endpoint || "-",
              label: shortHash(peer.publicKey),
              active: handshakeFresh(peer.latestHandshake),
              detail: peer.endpoint || "no endpoint",
            }))}
          />
          <div className={styles.tableWrap}>
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
                  <TableRow key={peer.publicKey}>
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

function PeerStatusStrip({ peers }: { peers: { key: string; label: string; active: boolean; detail: string }[] }) {
  const styles = useStyles();
  if (peers.length === 0) return <Text className={styles.muted}>No peers reported</Text>;
  return (
    <div className={styles.badges}>
      {peers.slice(0, 24).map(peer => (
        <Badge key={peer.key} appearance="tint" color={peer.active ? "success" : "subtle"}>
          {peer.label} · {peer.detail}
        </Badge>
      ))}
      {peers.length > 24 ? <Badge appearance="outline">+{peers.length - 24} more</Badge> : null}
    </div>
  );
}

function ClientInventory({ clients }: { clients: ClientEntry[] }) {
  const styles = useStyles();
  const rows = clients.map(clientEntryToRow);
  const online = rows.filter(row => clientOnline(row)).length;
  const addressCount = rows.reduce((sum, row) => sum + row.addresses.size, 0);
  return (
    <>
      <div className={styles.vpnSummaryGrid}>
        <Metric label="devices" value={String(rows.length)} />
        <Metric label="online" value={`${online}/${rows.length}`} />
        <Metric label="addresses" value={String(addressCount)} />
      </div>
      <PeerStatusStrip
        peers={rows.slice(0, 24).map(row => ({
          key: row.id || row.mac || row.ip || row.hostname || "-",
          label: row.hostname || row.mac || row.ip || "-",
          active: clientOnline(row),
          detail: primaryClientAddress(row) || `${row.addresses.size} addresses`,
        }))}
      />
      <div className={styles.tableWrap}>
        <Table size="small" className={styles.clientInventoryTable}>
          <colgroup>
            <col style={{ width: "210px" }} />
            <col style={{ width: "104px" }} />
            <col style={{ width: "210px" }} />
            <col style={{ width: "104px" }} />
            <col style={{ width: "92px" }} />
            <col style={{ width: "118px" }} />
            <col />
          </colgroup>
          <TableHeader>
            <TableRow>
              <TableHeaderCell>Name</TableHeaderCell>
              <TableHeaderCell>Status</TableHeaderCell>
              <TableHeaderCell>Primary IP</TableHeaderCell>
              <TableHeaderCell>OS family</TableHeaderCell>
              <TableHeaderCell>Addresses</TableHeaderCell>
              <TableHeaderCell>Last seen</TableHeaderCell>
              <TableHeaderCell>Details</TableHeaderCell>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map(row => {
              const groups = groupedClientAddresses(Array.from(row.addresses));
              return (
                <TableRow key={row.id || row.mac || row.ip || row.hostname}>
                  <TableCell>
                    <div className={styles.connectionFlow}>
                      <Text weight="semibold">{row.hostname || "unknown client"}</Text>
                      <Text size={200} className={styles.muted}>{row.vendor || row.mac || "-"}</Text>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className={styles.badges}>
                      <Badge appearance="tint" color={clientOnline(row) ? "success" : "subtle"}>{clientOnline(row) ? "online" : "offline"}</Badge>
                      {row.state ? <Badge appearance="outline">{row.state}</Badge> : null}
                    </div>
                  </TableCell>
                  <TableCell><code className={styles.code}>{primaryClientAddress(row) || "-"}</code></TableCell>
                  <TableCell><Text className={styles.muted}>{clientOSFamily(row)}</Text></TableCell>
                  <TableCell>{row.addresses.size}</TableCell>
                  <TableCell>{clientLastSeen(row)}</TableCell>
                  <TableCell>
                    <div className={styles.clientDetailStack}>
                      <ClientAddressGroup label="IPv4" addresses={groups.ipv4} />
                      <ClientAddressGroup label="IPv6 stable" addresses={groups.ipv6Stable} />
                      <ClientAddressGroup label="IPv6 privacy" addresses={groups.ipv6Privacy} />
                      <div className={styles.clientMetaLine}>
                        <code className={styles.wrapCode}>{row.mac || "-"}</code>
                        <Text size={200} className={styles.muted}>out {formatBytes(row.bytesOut)} / in {formatBytes(row.bytesIn)}</Text>
                        {row.peers.size > 0 ? <Text size={200} className={styles.muted}>peers {Array.from(row.peers).slice(0, 3).join(", ")}</Text> : null}
                      </div>
                    </div>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
    </>
  );
}

function ClientAddressGroup({ label, addresses }: { label: string; addresses: string[] }) {
  const styles = useStyles();
  if (addresses.length === 0) return null;
  return (
    <div className={styles.clientAddressGroup}>
      <Text size={200} className={styles.muted}>{label}</Text>
      <div className={styles.clientAddressList}>
        {addresses.slice(0, 6).map(address => <code className={styles.code} key={address}>{address}</code>)}
        {addresses.length > 6 ? <Badge appearance="outline">+{addresses.length - 6}</Badge> : null}
      </div>
    </div>
  );
}

function ClientTraffic({ flows }: { flows: TrafficFlow[] }) {
  const styles = useStyles();
  return (
    <div className={styles.tableWrap}>
      <Table size="small" className={styles.clientTrafficTable}>
        <colgroup>
          <col style={{ width: "170px" }} />
          <col style={{ width: "96px" }} />
          <col style={{ width: "96px" }} />
          <col />
        </colgroup>
        <TableHeader>
          <TableRow>
            <TableHeaderCell>Client</TableHeaderCell>
            <TableHeaderCell>Bytes out</TableHeaderCell>
            <TableHeaderCell>Bytes in</TableHeaderCell>
            <TableHeaderCell>Peers</TableHeaderCell>
          </TableRow>
        </TableHeader>
        <TableBody>
          {clientTrafficRows(flows).map(row => (
            <TableRow key={row.client}>
              <TableCell><code className={styles.code}>{row.client}</code></TableCell>
              <TableCell>{formatBytes(row.bytesOut)}</TableCell>
              <TableCell>{formatBytes(row.bytesIn)}</TableCell>
              <TableCell><code className={styles.wrapCode}>{Array.from(row.peers).slice(0, 3).join(", ") || "-"}</code></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function DHCPLeaseTable({ leases }: { leases: DHCPLease[] }) {
  const styles = useStyles();
  const rows = [...leases].sort((a, b) => stringSort(a.ip ?? "", b.ip ?? ""));
  return (
    <div className={styles.tableWrap}>
      <Table size="small" className={styles.dhcpLeaseTable}>
        <colgroup>
          <col style={{ width: "82px" }} />
          <col style={{ width: "250px" }} />
          <col />
          <col style={{ width: "150px" }} />
          <col style={{ width: "170px" }} />
          <col style={{ width: "112px" }} />
        </colgroup>
        <TableHeader>
          <TableRow>
            <TableHeaderCell>Family</TableHeaderCell>
            <TableHeaderCell>IP</TableHeaderCell>
            <TableHeaderCell>Hostname</TableHeaderCell>
            <TableHeaderCell>MAC</TableHeaderCell>
            <TableHeaderCell>Vendor</TableHeaderCell>
            <TableHeaderCell>Expires</TableHeaderCell>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map(lease => (
            <TableRow key={`${lease.ip}-${lease.mac}`}>
              <TableCell><Badge appearance="tint" color={lease.family === "ipv6" ? "brand" : "success"}>{lease.family || "-"}</Badge></TableCell>
              <TableCell><code className={styles.wrapCode}>{lease.ip || "-"}</code></TableCell>
              <TableCell>{lease.hostname || "-"}</TableCell>
              <TableCell><code className={styles.wrapCode}>{lease.mac || "-"}</code></TableCell>
              <TableCell>{lease.vendor || "-"}</TableCell>
              <TableCell><RelativeTime value={lease.expiresAt} /></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
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
  return (
    <div className={styles.firewallTable} role="table" aria-label="Deny ranking">
      <div className={styles.firewallRankHeader} role="row">
        <span>Count</span>
        <span>Source</span>
        <span>Destination</span>
        <span>Proto</span>
      </div>
      {denyRows(logs).map(row => (
        <div className={styles.firewallRankRow} role="row" key={`${row.src}-${row.dst}-${row.proto}`}>
          <FirewallCell label="Count">{row.count}</FirewallCell>
          <FirewallCell label="Source"><EndpointDetail address={row.src} dnsLabels={dnsLabels} leases={leases} /></FirewallCell>
          <FirewallCell label="Destination"><EndpointDetail address={row.dst} dnsLabels={dnsLabels} leases={leases} /></FirewallCell>
          <FirewallCell label="Proto">{row.proto}</FirewallCell>
        </div>
      ))}
    </div>
  );
}

function DenyRateChart({ logs }: { logs: FirewallLog[] }) {
  const styles = useStyles();
  const samples = denyRateSamples(logs);
  const max = Math.max(0, ...samples);
  return (
    <div className={styles.firewallChartWrap}>
      <Sparkline samples={samples} color="#ff8a80" />
      <Text size={200} className={styles.muted}>{logs.length} rows / peak {max} denies per bucket</Text>
    </div>
  );
}

function FirewallSourceTopN({
  logs,
  dnsLabels,
  leases,
}: {
  logs: FirewallLog[];
  dnsLabels: Record<string, string>;
  leases: Record<string, DHCPLease>;
}) {
  const styles = useStyles();
  const rows = sourceTopRows(logs);
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
    <div className={styles.firewallTable} role="table" aria-label="Deny timeline">
      <div className={styles.firewallTimelineHeader} role="row">
        <span>Time</span>
        <span>Action</span>
        <span>Source</span>
        <span>Destination</span>
        <span>Proto</span>
        <span>Rule</span>
      </div>
      {logs.slice(0, 50).map((log, index) => (
        <div className={styles.firewallTimelineRow} role="row" key={log.id ?? `${log.ts}-${log.srcAddress}-${log.dstAddress}-${index}`}>
          <FirewallCell label="Time"><RelativeTime value={log.ts} /></FirewallCell>
          <FirewallCell label="Action"><Badge appearance="tint" color={firewallActionColor(log.action)}>{log.action || "-"}</Badge></FirewallCell>
          <FirewallCell label="Source"><EndpointDetail address={log.srcAddress} port={log.srcPort} dnsLabels={dnsLabels} leases={leases} /></FirewallCell>
          <FirewallCell label="Destination"><EndpointDetail address={log.dstAddress} port={log.dstPort} dnsLabels={dnsLabels} leases={leases} /></FirewallCell>
          <FirewallCell label="Proto">{[log.l3Proto, log.protocol].filter(Boolean).join("/") || "-"}</FirewallCell>
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

function EndpointDetail({
  address,
  port,
  dnsLabels,
  leases,
}: {
  address?: string;
  port?: number;
  dnsLabels: Record<string, string>;
  leases: Record<string, DHCPLease>;
}) {
  const styles = useStyles();
  const lease = address ? leases[address] : undefined;
  const label = lease?.hostname || (address ? dnsLabels[address] : "");
  const vendor = lease?.vendor || "";
  return (
    <div className={styles.connectionFlow}>
      <code className={styles.wrapCode}>{firewallEndpoint(address, port)}</code>
      {label || lease?.mac || vendor ? (
        <Text size={200} className={styles.muted}>
          {[label, lease?.mac, vendor].filter(Boolean).join(" / ")}
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

function normalizeBasePath(value: string) {
  let base = value || "/";
  if (!base.startsWith("/")) base = `/${base}`;
  if (!base.endsWith("/")) base = `${base}/`;
  return base;
}

function phaseColor(phase: unknown): "success" | "warning" | "danger" | "informative" {
  const text = String(phase ?? "");
  if (/Healthy|Applied|Active|Bound|Installed|Ready|Running|Up|Observed/.test(text)) return "success";
  if (/Pending|Drifted|Unknown/.test(text)) return "warning";
  if (/Error|Failed|Down|Unhealthy/.test(text)) return "danger";
  return "informative";
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
  return resources
    .filter(resource => /EgressRoutePolicy|HealthCheck|DNSResolver|DHCP|DSLiteTunnel|NAT44Rule|IPv4Route|Firewall|WireGuard|VXLAN/.test(resource.kind ?? ""))
    .sort((a, b) => `${a.kind}/${a.name}`.localeCompare(`${b.kind}/${b.name}`));
}

function conntrackLabel(table?: ConnectionTable) {
  if (!table) return "-";
  if (table.max) return `${table.count ?? 0}/${table.max}`;
  return String(table.count ?? "-");
}

function connectionFamilyCounts(table?: ConnectionTable) {
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

function filterFirewallLogs(logs: FirewallLog[], filters: FirewallFilters) {
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
    return firewallSearchText(log).includes(query);
  });
}

function firewallSearchText(log: FirewallLog) {
  return [
    log.action,
    log.srcAddress,
    log.srcPort,
    log.dstAddress,
    log.dstPort,
    log.protocol,
    log.l3Proto,
    log.ruleName,
    log.inIface,
    log.outIface,
  ].filter(value => value !== undefined && value !== "").join(" ").toLowerCase();
}

function denyRateSamples(logs: FirewallLog[]) {
  const now = Date.now();
  const bucketCount = 24;
  const bucketMs = 60 * 60 * 1000;
  const buckets = Array(bucketCount).fill(0);
  for (const log of logs) {
    const ts = Date.parse(log.ts ?? "");
    if (Number.isNaN(ts)) continue;
    const age = now - ts;
    if (age < 0 || age >= bucketCount * bucketMs) continue;
    const bucket = bucketCount - 1 - Math.floor(age / bucketMs);
    buckets[bucket]++;
  }
  return buckets;
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
  const states = new Set<string>();
  for (const entry of entries) {
    families.add(normalizeFacet(entry.family, "other"));
    protocols.add(normalizeFacet(entry.protocol, "other"));
    states.add(normalizeFacet(entry.state, "stateless"));
  }
  return {
    families: Array.from(families).sort(facetSort),
    protocols: Array.from(protocols).sort(facetSort),
    states: Array.from(states).sort(facetSort),
  };
}

function resourceSearchText(resource: ResourceStatus) {
  return [
    resource.apiVersion,
    resource.kind,
    resource.name,
    resource.status?.phase,
    resourceDetail(resource.status ?? {}),
    JSON.stringify(resource.status ?? {}),
  ].filter(Boolean).join(" ").toLowerCase();
}

function filterAndSortConnections(entries: ConnectionEntry[], dnsLabels: Record<string, string>, filters: ConnectionFilters) {
  const query = filters.query.trim().toLowerCase();
  const indexed = entries.map((entry, index) => ({ entry, index }));
  const filtered = indexed.filter(({ entry }) => {
    if (filters.family !== "all" && normalizeFacet(entry.family, "other") !== filters.family) return false;
    if (filters.protocol !== "all" && normalizeFacet(entry.protocol, "other") !== filters.protocol) return false;
    if (filters.state !== "all" && normalizeFacet(entry.state, "stateless") !== filters.state) return false;
    if (!query) return true;
    return connectionSearchText(entry, dnsLabels).includes(query);
  });
  const multiplier = filters.direction === "desc" ? -1 : 1;
  return filtered
    .sort((a, b) => {
      if (filters.sort === "observed") return (a.index - b.index) * multiplier;
      const primary = compareConnectionSortValue(a.entry, b.entry, filters.sort, dnsLabels) * multiplier;
      return primary || a.index - b.index;
    })
    .map(row => row.entry);
}

function connectionSearchText(entry: ConnectionEntry, dnsLabels: Record<string, string>) {
  const addresses = [
    entry.original?.source,
    entry.original?.destination,
    entry.reply?.source,
    entry.reply?.destination,
  ].filter(Boolean) as string[];
  const labels = addresses.map(address => dnsLabels[address] ?? "").filter(Boolean);
  return [
    entry.family,
    entry.protocol,
    entry.state || "stateless",
    entry.assured ? "assured" : "",
    entry.timeout,
    entry.mark,
    endpoint(entry.original),
    endpoint(entry.reply),
    ...labels,
  ].join(" ").toLowerCase();
}

function compareConnectionSortValue(a: ConnectionEntry, b: ConnectionEntry, sort: string, dnsLabels: Record<string, string>) {
  if (sort === "timeout") return Number(a.timeout ?? 0) - Number(b.timeout ?? 0);
  return stringSort(connectionSortValue(a, sort, dnsLabels), connectionSortValue(b, sort, dnsLabels));
}

function connectionSortValue(entry: ConnectionEntry, sort: string, dnsLabels: Record<string, string>) {
  if (sort === "state") return `${normalizeFacet(entry.state, "stateless")} ${entry.assured ? "assured" : ""}`;
  if (sort === "source") return hostPort(entry.original?.source, entry.original?.sourcePort);
  if (sort === "destination") return hostPort(entry.original?.destination, entry.original?.destinationPort);
  if (sort === "label") return dnsLabels[entry.original?.destination ?? ""] ?? entry.original?.destination ?? "";
  return "";
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

function facetSort(a: string, b: string) {
  const order: Record<string, number> = { ipv4: 0, ipv6: 1, tcp: 0, udp: 1, icmp: 2, icmpv6: 3, ipv6_icmp: 3, established: 0 };
  return (order[a] ?? 9) - (order[b] ?? 9) || a.localeCompare(b);
}

function stringSort(a: string, b: string) {
  return a.localeCompare(b, undefined, { numeric: true, sensitivity: "base" });
}

function connectionGroups(entries: ConnectionEntry[]) {
  const groups = new Map<string, ConnectionEntry[]>();
  for (const entry of entries) {
    const key = `${String(entry.family || "other").toLowerCase()}/${String(entry.protocol || "other").toLowerCase()}`;
    groups.set(key, [...(groups.get(key) ?? []), entry]);
  }
  const order: Record<string, number> = { ipv4: 0, ipv6: 1, other: 9, tcp: 0, udp: 1, icmp: 2, icmpv6: 3, ipv6_icmp: 3 };
  return Array.from(groups.entries())
    .sort((a, b) => {
      const [af, ap] = a[0].split("/");
      const [bf, bp] = b[0].split("/");
      return (order[af] ?? 9) - (order[bf] ?? 9) || (order[ap] ?? 9) - (order[bp] ?? 9) || a[0].localeCompare(b[0]);
    })
    .map(([key, rows]) => ({ key, rows }));
}

function connectionGroupLabel(key: string) {
  const [family, protocol] = key.split("/");
  return {
    family: family === "ipv4" ? "IPv4" : family === "ipv6" ? "IPv6" : "Other",
    protocol: protocol || "other",
  };
}

function connectionGroupID(key: string) {
  return `connections-${key.replace(/[^a-zA-Z0-9_-]+/g, "-")}`;
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
  if (selected === "connections") {
    return groups.map(group => {
      const label = connectionGroupLabel(group.key);
      return {
        key: group.key,
        label: `${label.family}/${label.protocol.toUpperCase()}`,
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
    return [
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
  return [entry.family, entry.protocol, entry.state, endpoint(entry.original), endpoint(entry.reply), entry.mark].join("|");
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
  const totals = new Map<string, { client: string; bytesOut?: number; bytesIn?: number; peers: Set<string> }>();
  for (const flow of flows) {
    const key = flow.clientAddress || "-";
    const row = totals.get(key) ?? { client: key, peers: new Set<string>() };
    row.bytesOut = addOptionalBytes(row.bytesOut, flow.bytesOut, flow.accounting);
    row.bytesIn = addOptionalBytes(row.bytesIn, flow.bytesIn, flow.accounting);
    const peer = flow.resolvedHostname || flow.tlsSNI || flow.peerAddress;
    if (peer) row.peers.add(peer);
    totals.set(key, row);
  }
  return Array.from(totals.values()).sort((a, b) => a.client.localeCompare(b.client)).slice(0, 10);
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
  };
}

function normalizeMAC(mac?: string) {
  return String(mac ?? "").trim().toLowerCase();
}

function addOptionalBytes(current: number | undefined, next: number | undefined, accounting?: boolean) {
  if (!accounting) return current;
  const value = typeof next === "number" && Number.isFinite(next) ? next : 0;
  return (current ?? 0) + value;
}

function filterClients(clients: ClientEntry[], query: string) {
  const needle = query.trim().toLowerCase();
  if (!needle) return clients;
  return clients.filter(client => clientSearchText(client).includes(needle));
}

function clientSearchText(client: ClientEntry) {
  return [
    client.id,
    client.hostname,
    client.mac,
    client.vendor,
    client.state,
    ...(client.addresses ?? []),
    ...(client.sources ?? []),
    ...(client.peers ?? []),
  ].filter(Boolean).join(" ").toLowerCase();
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

function clientOSFamily(_row: ClientRow) {
  return "-";
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

function denyRows(logs: FirewallLog[]) {
  const totals = new Map<string, { key: string; src: string; dst: string; proto: string; count: number }>();
  for (const log of logs) {
    const key = firewallLogKey(log);
    const row = totals.get(key) ?? { key, src: log.srcAddress || "-", dst: log.dstAddress || "-", proto: log.protocol || "-", count: 0 };
    row.count++;
    totals.set(key, row);
  }
  return Array.from(totals.values()).sort((a, b) => b.count - a.count || a.src.localeCompare(b.src)).slice(0, 10);
}

function firewallLogKey(log: FirewallLog) {
  return firewallTupleKey(log.srcAddress, log.srcPort, log.dstAddress, log.dstPort, log.protocol);
}

function firewallTupleKey(source?: string, sourcePort?: string | number, destination?: string, destinationPort?: string | number, protocol?: string) {
  return `${source || "-"}:${sourcePort || ""}>${destination || "-"}:${destinationPort || ""}>${protocol || "-"}`;
}

function absoluteTime(value?: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return `${new Intl.DateTimeFormat(undefined, { month: "2-digit", day: "2-digit" }).format(date)} ${new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }).format(date)}`;
}

function relativeTimeText(value?: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const diffSeconds = Math.round((date.getTime() - Date.now()) / 1000);
  const abs = Math.abs(diffSeconds);
  const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  if (abs < 60) return rtf.format(diffSeconds, "second");
  const diffMinutes = Math.round(diffSeconds / 60);
  if (Math.abs(diffMinutes) < 60) return rtf.format(diffMinutes, "minute");
  const diffHours = Math.round(diffMinutes / 60);
  if (Math.abs(diffHours) < 48) return rtf.format(diffHours, "hour");
  const diffDays = Math.round(diffHours / 24);
  return rtf.format(diffDays, "day");
}

createRoot(document.getElementById("root")!).render(<App />);
