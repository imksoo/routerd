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
  errors?: string[];
};

type ResourceStatus = {
  apiVersion?: string;
  kind?: string;
  name?: string;
  status?: Record<string, unknown>;
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

type ConfigSnapshot = {
  path?: string;
  text?: string;
};

type ConnectionFilters = {
  query: string;
  family: string;
  protocol: string;
  state: string;
  sort: string;
  direction: string;
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

type ViewKey = "overview" | "clients" | "connections" | "events" | "firewall" | "config";
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
  { key: "clients", label: "Clients", description: "Leases and endpoint traffic", icon: <PeopleRegular /> },
  { key: "connections", label: "Connections", description: "conntrack and live flows", icon: <PlugConnectedRegular /> },
  { key: "events", label: "Events", description: "Bus events and resource changes", icon: <ServerRegular /> },
  { key: "firewall", label: "Firewall", description: "Deny ranking and timeline", icon: <ShieldRegular /> },
  { key: "config", label: "Config", description: "Read-only YAML tree", icon: <DocumentTextRegular /> },
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
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  sectionGrid: {
    display: "grid",
    gridTemplateColumns: "minmax(0, 1.4fr) minmax(320px, 0.8fr)",
    gap: "16px",
    "@media (max-width: 900px)": {
      gridTemplateColumns: "1fr",
    },
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
  firewallStack: {
    display: "grid",
    gap: "16px",
  },
  clientsGrid: {
    display: "grid",
    gap: "16px",
    gridTemplateColumns: "1fr",
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
  eventTable: {
    minWidth: "760px",
    tableLayout: "fixed",
  },
  connectionTable: {
    minWidth: "820px",
    tableLayout: "fixed",
  },
  clientInventoryTable: {
    minWidth: "1040px",
    tableLayout: "fixed",
  },
  clientTrafficTable: {
    minWidth: "760px",
    tableLayout: "fixed",
  },
  dhcpLeaseTable: {
    minWidth: "840px",
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
  firewallTable: {
    display: "grid",
    gap: "6px",
  },
  firewallRankHeader: {
    display: "grid",
    gridTemplateColumns: "56px minmax(220px, 1.25fr) minmax(220px, 1.25fr) minmax(210px, 1fr) 64px",
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
    gridTemplateColumns: "96px 68px minmax(220px, 1.35fr) minmax(220px, 1.35fr) minmax(210px, 1fr) 72px minmax(96px, 0.55fr)",
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
    gridTemplateColumns: "56px minmax(220px, 1.25fr) minmax(220px, 1.25fr) minmax(210px, 1fr) 64px",
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
    gridTemplateColumns: "96px 68px minmax(220px, 1.35fr) minmax(220px, 1.35fr) minmax(210px, 1fr) 72px minmax(96px, 0.55fr)",
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
  const [summary, setSummary] = useState<Summary | null>(null);
  const [config, setConfig] = useState<ConfigSnapshot | null>(null);
  const [error, setError] = useState<string>("");
  const [selected, setSelected] = useState<ViewKey>(() => parseLocationHash().view);
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
  const [selectedEventKey, setSelectedEventKey] = useState<string>("");
  const [loading, setLoading] = useState(true);

  async function refresh() {
    try {
      const [summaryResponse, configResponse] = await Promise.all([
        fetchJSON<Summary>("api/v1/summary?events=15&connections=240"),
        config ? Promise.resolve(config) : fetchJSON<ConfigSnapshot>("api/v1/config"),
      ]);
      setSummary(summaryResponse);
      if (!config) setConfig(configResponse as ConfigSnapshot);
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

  const connections = summary?.connections?.entries ?? [];
  const dnsLabels = useMemo(() => dnsLabelMap(summary?.dnsQueries ?? []), [summary?.dnsQueries]);
  const leaseMap = useMemo(() => dhcpLeaseMap(summary?.dhcpLeases ?? []), [summary?.dhcpLeases]);
  const relatedClients = useMemo(() => firewallRelatedClientMap(connections), [connections]);
  const filteredConnections = useMemo(
    () => filterAndSortConnections(connections, dnsLabels, connectionFilters),
    [connections, dnsLabels, connectionFilters],
  );
  const connectionGroupsList = useMemo(() => connectionGroups(filteredConnections), [filteredConnections]);
  const connectionFacets = useMemo(() => connectionFilterFacets(connections), [connections]);
  const navSubItems = useMemo(() => navigationSubItems(selected, connectionGroupsList, summary), [selected, connectionGroupsList, summary]);
  const resources = useMemo(() => importantResources(summary?.resources ?? []), [summary?.resources]);
  const events = summary?.events ?? [];
  const selectedEvent = useMemo(() => {
    if (events.length === 0) return undefined;
    return events.find(event => eventKey(event) === selectedEventKey) ?? events[0];
  }, [events, selectedEventKey]);

  useEffect(() => {
    if (events.length > 0 && !events.some(event => eventKey(event) === selectedEventKey)) {
      setSelectedEventKey(eventKey(events[0]));
    }
  }, [events, selectedEventKey]);

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
      const targetID = next.targetID;
      if (targetID) {
        window.setTimeout(() => scrollToElement(targetID), 80);
      }
    };
    window.addEventListener("hashchange", onHashChange);
    return () => window.removeEventListener("hashchange", onHashChange);
  }, []);

  useEffect(() => {
    const next = parseLocationHash();
    const targetID = next.targetID;
    if (next.view === selected && targetID) {
      window.setTimeout(() => scrollToElement(targetID), 80);
    }
  }, [selected, connectionGroupsList.map(group => group.key).join("|"), summary?.generatedAt]);

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

  function navigateTo(view: ViewKey, targetID?: string) {
    setSelected(view);
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

  const selectedNav = navItems.find(item => item.key === selected) ?? navItems[0];

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
                        className={styles.navSubButton}
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
              <Text size={200} className={styles.muted}>{summary?.generatedAt ? `Updated ${formatTime(summary.generatedAt)}` : ""}</Text>
              <Button appearance="primary" icon={<ArrowClockwiseRegular />} onClick={refresh}>Refresh</Button>
            </div>
          </div>
          <main className={styles.main}>
            {error ? <Card><Text role="alert">Web console error: {error}</Text></Card> : null}
            {selected === "overview" ? (
              <>
                <div id="overview-metrics" className={styles.connectionAnchor}>
                  <div className={styles.grid}>
                    <Metric label="phase" value={String(summary?.status?.status?.phase ?? "Unknown")} />
                    <Metric label="generation" value={String(summary?.status?.status?.generation ?? "-")} />
                    <Metric label="resources" value={String(summary?.status?.status?.resourceCount ?? resources.length)} />
                    <Metric label="conntrack" value={conntrackLabel(summary?.connections)} />
                    <Metric label="families" value={connectionFamilyCounts(summary?.connections)} />
                  </div>
                </div>
                <Card>
                  <CardHeader header={<Text weight="semibold">Interfaces</Text>} description={<Text className={styles.muted}>Role, link state, MTU, and assigned addresses</Text>} />
                  <InterfaceOverview interfaces={summary?.interfaces ?? []} />
                </Card>
                <Card>
                  <CardHeader header={<Text weight="semibold">Resources</Text>} />
                  <ResourceTable resources={resources} />
                </Card>
              </>
            ) : null}
            {selected === "clients" ? (
              <div className={styles.clientsGrid}>
                <Card id="clients-inventory" className={styles.connectionAnchor}>
                  <CardHeader
                    header={<Text weight="semibold">Client inventory</Text>}
                    description={<Text className={styles.muted}>DHCP leases combined with observed traffic</Text>}
                  />
                  <ClientInventory clients={summary?.clients ?? []} />
                </Card>
                <Card id="clients-traffic" className={styles.connectionAnchor}>
                  <CardHeader header={<Text weight="semibold">Client traffic</Text>} />
                  <ClientTraffic flows={summary?.trafficFlows ?? []} />
                </Card>
                <Card id="clients-leases" className={styles.connectionAnchor}>
                  <CardHeader header={<Text weight="semibold">DHCP leases</Text>} />
                  <DHCPLeaseTable leases={summary?.dhcpLeases ?? []} />
                </Card>
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
              <div className={styles.filterControl}>
                <Text size={200} className={styles.muted}>Filter</Text>
                <Input
                  className={styles.filterInput}
                  size="small"
                  value={connectionFilters.query}
                  placeholder="address, port, state, label"
                  onChange={(_, data) => updateConnectionFilter("query", data.value)}
                />
              </div>
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
            {selected === "events" ? (
              <div className={styles.eventsGrid}>
                <Card>
                  <CardHeader header={<Text weight="semibold">Events</Text>} />
                  <EventTable events={events} selectedKey={eventKey(selectedEvent)} onSelect={event => setSelectedEventKey(eventKey(event))} />
                </Card>
                <EventDetail event={selectedEvent} />
              </div>
            ) : null}
            {selected === "firewall" ? (
              <div className={styles.firewallStack}>
                <Card id="firewall-ranking" className={styles.connectionAnchor}>
                  <CardHeader header={<Text weight="semibold">Deny ranking</Text>} description={<Text className={styles.muted}>Grouped by source, destination, and protocol</Text>} />
                  <RecentDeny logs={summary?.firewallLogs ?? []} dnsLabels={dnsLabels} leases={leaseMap} relatedClients={relatedClients} />
                </Card>
                <Card id="firewall-timeline" className={styles.connectionAnchor}>
                  <CardHeader header={<Text weight="semibold">Deny timeline</Text>} description={<Text className={styles.muted}>Newest firewall log rows</Text>} />
                  <FirewallTimeline logs={summary?.firewallLogs ?? []} dnsLabels={dnsLabels} leases={leaseMap} relatedClients={relatedClients} />
                </Card>
              </div>
            ) : null}
            {selected === "config" ? (
              <Card>
                <CardHeader header={<Text weight="semibold">Config</Text>} description={<Text className={styles.muted}>{config?.path ?? ""}</Text>} />
                <ConfigView config={config} />
              </Card>
            ) : null}
          </main>
        </section>
      </div>
    </FluentProvider>
  );
}

function ConfigView({ config }: { config: ConfigSnapshot | null }) {
  const styles = useStyles();
  const [mode, setMode] = useState<"tree" | "raw">("tree");
  const parsed = useMemo(() => parseConfig(config?.text), [config?.text]);
  return (
    <>
      <div className={styles.configToolbar}>
        <Text className={styles.muted}>Read-only view of the active routerd YAML</Text>
        <div className={styles.configModeButtons}>
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
    </>
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

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value) && !(value instanceof Date);
}

function ResourceTable({ resources }: { resources: ResourceStatus[] }) {
  const styles = useStyles();
  return (
    <div className={styles.tableWrap}>
      <Table size="small" className={styles.resourceTable}>
        <colgroup>
          <col style={{ width: "170px" }} />
          <col style={{ width: "220px" }} />
          <col style={{ width: "120px" }} />
          <col />
        </colgroup>
        <TableHeader>
          <TableRow>
            <TableHeaderCell>Kind</TableHeaderCell>
            <TableHeaderCell>Name</TableHeaderCell>
            <TableHeaderCell>Phase</TableHeaderCell>
            <TableHeaderCell>Detail</TableHeaderCell>
          </TableRow>
        </TableHeader>
        <TableBody>
          {resources.slice(0, 80).map(resource => {
            const status = resource.status ?? {};
            return (
              <TableRow key={`${resource.apiVersion}/${resource.kind}/${resource.name}`}>
                <TableCell>{resource.kind}</TableCell>
                <TableCell><code className={styles.code}>{resource.name}</code></TableCell>
                <TableCell><Badge appearance="tint" color={phaseColor(status.phase)}>{String(status.phase ?? "Unknown")}</Badge></TableCell>
                <TableCell><code className={styles.wrapCode}>{resourceDetail(status)}</code></TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

function EventTable({ events, selectedKey, onSelect }: { events: RouterEvent[]; selectedKey: string; onSelect: (event: RouterEvent) => void }) {
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
          {events.slice(0, 15).map(event => {
            const key = eventKey(event);
            return (
              <TableRow key={key} className={key === selectedKey ? styles.eventRowSelected : undefined} onClick={() => onSelect(event)}>
                <TableCell>{formatTime(event.createdAt)}</TableCell>
                <TableCell>{event.severity ?? ""}</TableCell>
                <TableCell><code className={styles.wrapCode}>{event.topic ?? event.type}</code></TableCell>
                <TableCell>{resourceName(event)}</TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

function EventDetail({ event }: { event?: RouterEvent }) {
  const styles = useStyles();
  if (!event) {
    return (
      <Card className={styles.detailPanel}>
        <CardHeader header={<Text weight="semibold">Detail</Text>} />
        <Text className={styles.muted}>No event selected</Text>
      </Card>
    );
  }
  const baseRows: [string, unknown][] = [
    ["time", formatTime(event.createdAt)],
    ["severity", event.severity ?? ""],
    ["topic", event.topic ?? event.type ?? ""],
    ["resource", resourceName(event)],
    ["reason", event.reason ?? ""],
    ["message", event.message ?? ""],
  ];
  const rows = [...baseRows, ...eventAttributeEntries(event)].filter(([, value]) => value !== undefined && value !== "");
  return (
    <Card className={styles.detailPanel}>
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

function ClientInventory({ clients }: { clients: ClientEntry[] }) {
  const styles = useStyles();
  const rows = clients.map(clientEntryToRow);
  return (
    <div className={styles.tableWrap}>
      <Table size="small" className={styles.clientInventoryTable}>
        <colgroup>
          <col style={{ width: "190px" }} />
          <col style={{ width: "320px" }} />
          <col style={{ width: "150px" }} />
          <col style={{ width: "96px" }} />
          <col />
        </colgroup>
        <TableHeader>
          <TableRow>
            <TableHeaderCell>Client</TableHeaderCell>
            <TableHeaderCell>Address</TableHeaderCell>
            <TableHeaderCell>MAC</TableHeaderCell>
            <TableHeaderCell>Traffic</TableHeaderCell>
            <TableHeaderCell>Peers</TableHeaderCell>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map(row => (
            <TableRow key={row.id || row.mac || row.ip || row.hostname}>
              <TableCell>
                <div className={styles.connectionFlow}>
                  <Text>{row.hostname || "-"}</Text>
                  <Text size={200} className={styles.muted}>{row.vendor || "-"}</Text>
                </div>
              </TableCell>
              <TableCell>
                <div className={styles.connectionFlow}>
                  {Array.from(row.addresses ?? []).slice(0, 6).map(address => (
                    <code className={styles.code} key={address}>{address}</code>
                  ))}
                  {row.state ? <Text size={200} className={styles.muted}>{row.state}</Text> : null}
                </div>
              </TableCell>
              <TableCell><code className={styles.wrapCode}>{row.mac || "-"}</code></TableCell>
              <TableCell>
                <div className={styles.connectionFlow}>
                  <Text size={200}>out {formatBytes(row.bytesOut)}</Text>
                  <Text size={200}>in {formatBytes(row.bytesIn)}</Text>
                </div>
              </TableCell>
              <TableCell><code className={styles.wrapCode}>{Array.from(row.peers ?? []).slice(0, 4).join(", ") || "-"}</code></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
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
          <col style={{ width: "170px" }} />
          <col />
          <col style={{ width: "150px" }} />
          <col style={{ width: "170px" }} />
          <col style={{ width: "112px" }} />
        </colgroup>
        <TableHeader>
          <TableRow>
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
              <TableCell><code className={styles.wrapCode}>{lease.ip || "-"}</code></TableCell>
              <TableCell>{lease.hostname || "-"}</TableCell>
              <TableCell><code className={styles.wrapCode}>{lease.mac || "-"}</code></TableCell>
              <TableCell>{lease.vendor || "-"}</TableCell>
              <TableCell>{formatTime(lease.expiresAt)}</TableCell>
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
  relatedClients,
}: {
  logs: FirewallLog[];
  dnsLabels: Record<string, string>;
  leases: Record<string, DHCPLease>;
  relatedClients: Record<string, ConnTuple | undefined>;
}) {
  const styles = useStyles();
  return (
    <div className={styles.firewallTable} role="table" aria-label="Deny ranking">
      <div className={styles.firewallRankHeader} role="row">
        <span>Count</span>
        <span>Source</span>
        <span>Destination</span>
        <span>Related client</span>
        <span>Proto</span>
      </div>
      {denyRows(logs).map(row => (
        <div className={styles.firewallRankRow} role="row" key={`${row.src}-${row.dst}-${row.proto}`}>
          <FirewallCell label="Count">{row.count}</FirewallCell>
          <FirewallCell label="Source"><EndpointDetail address={row.src} dnsLabels={dnsLabels} leases={leases} /></FirewallCell>
          <FirewallCell label="Destination"><EndpointDetail address={row.dst} dnsLabels={dnsLabels} leases={leases} /></FirewallCell>
          <FirewallCell label="Related"><RelatedClient tuple={relatedClients[row.key]} leases={leases} /></FirewallCell>
          <FirewallCell label="Proto">{row.proto}</FirewallCell>
        </div>
      ))}
    </div>
  );
}

function FirewallTimeline({
  logs,
  dnsLabels,
  leases,
  relatedClients,
}: {
  logs: FirewallLog[];
  dnsLabels: Record<string, string>;
  leases: Record<string, DHCPLease>;
  relatedClients: Record<string, ConnTuple | undefined>;
}) {
  const styles = useStyles();
  return (
    <div className={styles.firewallTable} role="table" aria-label="Deny timeline">
      <div className={styles.firewallTimelineHeader} role="row">
        <span>Time</span>
        <span>Action</span>
        <span>Source</span>
        <span>Destination</span>
        <span>Related client</span>
        <span>Proto</span>
        <span>Rule</span>
      </div>
      {logs.slice(0, 50).map((log, index) => (
        <div className={styles.firewallTimelineRow} role="row" key={log.id ?? `${log.ts}-${log.srcAddress}-${log.dstAddress}-${index}`}>
          <FirewallCell label="Time">{formatTime(log.ts)}</FirewallCell>
          <FirewallCell label="Action"><Badge appearance="tint" color={firewallActionColor(log.action)}>{log.action || "-"}</Badge></FirewallCell>
          <FirewallCell label="Source"><EndpointDetail address={log.srcAddress} port={log.srcPort} dnsLabels={dnsLabels} leases={leases} /></FirewallCell>
          <FirewallCell label="Destination"><EndpointDetail address={log.dstAddress} port={log.dstPort} dnsLabels={dnsLabels} leases={leases} /></FirewallCell>
          <FirewallCell label="Related"><RelatedClient tuple={relatedClients[firewallLogKey(log)]} leases={leases} /></FirewallCell>
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

function RelatedClient({ tuple, leases }: { tuple?: ConnTuple; leases: Record<string, DHCPLease> }) {
  const styles = useStyles();
  if (!tuple?.source) return <Text className={styles.muted}>-</Text>;
  const lease = leases[tuple.source];
  return (
    <div className={styles.connectionFlow}>
      <code className={styles.wrapCode}>{firewallEndpoint(tuple.source, Number(tuple.sourcePort) || undefined)}</code>
      {lease?.hostname || lease?.mac || lease?.vendor ? (
        <Text size={200} className={styles.muted}>
          {[lease.hostname, lease.mac, lease.vendor].filter(Boolean).join(" / ")}
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

function firewallRelatedClientMap(entries: ConnectionEntry[]) {
  const related: Record<string, ConnTuple | undefined> = {};
  for (const entry of entries) {
    const reply = entry.reply;
    if (!reply?.source || !reply?.destination) continue;
    related[firewallTupleKey(reply.source, reply.sourcePort, reply.destination, reply.destinationPort, entry.protocol)] = entry.original;
  }
  return related;
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
  return [];
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
  return ["selectedCandidate", "selectedDevice", "activeEgressInterface", "target", "address", "currentPrefix", "changedFields"]
    .map(key => status[key] ? `${key}=${status[key]}` : "")
    .filter(Boolean)
    .join(" ");
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

function formatTime(value?: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return `${new Intl.DateTimeFormat(undefined, { month: "2-digit", day: "2-digit" }).format(date)} ${new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }).format(date)}`;
}

createRoot(document.getElementById("root")!).render(<App />);
