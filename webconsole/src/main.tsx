import React, { useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Badge,
  Button,
  Card,
  CardHeader,
  FluentProvider,
  Spinner,
  Tab,
  TabList,
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
  ChevronDownRegular,
  ChevronRightRegular,
} from "@fluentui/react-icons";
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
  events?: RouterEvent[];
  connections?: ConnectionTable;
  dnsQueries?: DNSQuery[];
  trafficFlows?: TrafficFlow[];
  firewallLogs?: FirewallLog[];
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
  bytesOut?: number;
  bytesIn?: number;
};

type FirewallLog = {
  srcAddress?: string;
  dstAddress?: string;
  protocol?: string;
  ruleName?: string;
};

type ConfigSnapshot = {
  path?: string;
  text?: string;
};

const cfg = window.__ROUTERD_WEB_CONSOLE__ ?? { basePath: "/", title: "routerd" };
const basePath = normalizeBasePath(cfg.basePath);

const useStyles = makeStyles({
  shell: {
    minHeight: "100vh",
    backgroundColor: tokens.colorNeutralBackground1,
    color: tokens.colorNeutralForeground1,
  },
  header: {
    position: "sticky",
    top: 0,
    zIndex: 10,
    display: "flex",
    alignItems: "center",
    justifyContent: "space-between",
    gap: "12px",
    padding: "14px 18px",
    borderBottom: `1px solid ${tokens.colorNeutralStroke2}`,
    backgroundColor: tokens.colorNeutralBackground2,
  },
  title: {
    minWidth: 0,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  main: {
    maxWidth: "1380px",
    margin: "0 auto",
    padding: "16px",
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
  tableWrap: {
    overflowX: "auto",
  },
  code: {
    fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace",
    whiteSpace: "nowrap",
  },
  muted: {
    color: tokens.colorNeutralForeground3,
  },
  chips: {
    display: "flex",
    flexWrap: "wrap",
    gap: "4px",
  },
  toolbar: {
    display: "flex",
    alignItems: "center",
    gap: "8px",
  },
  connectionGroup: {
    display: "grid",
    gap: "8px",
  },
  config: {
    maxHeight: "66vh",
    overflow: "auto",
    border: `1px solid ${tokens.colorNeutralStroke2}`,
    borderRadius: tokens.borderRadiusMedium,
    padding: "10px",
    backgroundColor: tokens.colorNeutralBackground2,
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
  const [selected, setSelected] = useState("overview");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
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
  const resources = useMemo(() => importantResources(summary?.resources ?? []), [summary?.resources]);

  return (
    <FluentProvider theme={webDarkTheme} className={styles.shell}>
      <header className={styles.header}>
        <Text size={600} weight="semibold" className={styles.title}>{cfg.title || "routerd"}</Text>
        <div className={styles.toolbar}>
          {loading ? <Spinner size="tiny" /> : null}
          <Badge appearance="tint" color={phaseColor(summary?.status?.status?.phase)}>{String(summary?.status?.status?.phase ?? "Unknown")}</Badge>
          <Button appearance="subtle" icon={<ArrowClockwiseRegular />} onClick={refresh}>Refresh</Button>
        </div>
      </header>
      <main className={styles.main}>
        {error ? <Card><Text role="alert">Web console error: {error}</Text></Card> : null}
        <TabList selectedValue={selected} onTabSelect={(_, data) => setSelected(String(data.value))}>
          <Tab value="overview">Overview</Tab>
          <Tab value="connections">Connections</Tab>
          <Tab value="events">Events</Tab>
          <Tab value="config">Config</Tab>
        </TabList>
        {selected === "overview" ? (
          <>
            <div className={styles.grid}>
              <Metric label="phase" value={String(summary?.status?.status?.phase ?? "Unknown")} />
              <Metric label="generation" value={String(summary?.status?.status?.generation ?? "-")} />
              <Metric label="resources" value={String(summary?.status?.status?.resourceCount ?? resources.length)} />
              <Metric label="conntrack" value={conntrackLabel(summary?.connections)} />
              <Metric label="families" value={connectionFamilyCounts(summary?.connections)} />
            </div>
            <div className={styles.sectionGrid}>
              <Card>
                <CardHeader header={<Text weight="semibold">Resources</Text>} />
                <ResourceTable resources={resources} />
              </Card>
              <Card>
                <CardHeader header={<Text weight="semibold">Client traffic</Text>} />
                <ClientTraffic flows={summary?.trafficFlows ?? []} />
              </Card>
            </div>
          </>
        ) : null}
        {selected === "connections" ? (
          <Card>
            <CardHeader header={<Text weight="semibold">Connections</Text>} description={<Text className={styles.muted}>{connectionFamilyCounts(summary?.connections)}</Text>} />
            <div className={styles.connectionGroup}>
              {connectionGroups(connections).map(group => (
                <ConnectionGroup
                  key={group.key}
                  group={group}
                  dnsLabels={dnsLabels}
                  collapsed={collapsed[group.key] ?? true}
                  toggle={() => setCollapsed(current => ({ ...current, [group.key]: !(current[group.key] ?? true) }))}
                />
              ))}
            </div>
          </Card>
        ) : null}
        {selected === "events" ? (
          <div className={styles.sectionGrid}>
            <Card>
              <CardHeader header={<Text weight="semibold">Events</Text>} />
              <EventTable events={summary?.events ?? []} />
            </Card>
            <Card>
              <CardHeader header={<Text weight="semibold">Recent deny</Text>} />
              <RecentDeny logs={summary?.firewallLogs ?? []} />
            </Card>
          </div>
        ) : null}
        {selected === "config" ? (
          <Card>
            <CardHeader header={<Text weight="semibold">Config</Text>} description={<Text className={styles.muted}>{config?.path ?? ""}</Text>} />
            <div className={styles.config}>
              <pre className={styles.pre}>{config?.text ?? "Config is unavailable"}</pre>
            </div>
          </Card>
        ) : null}
      </main>
    </FluentProvider>
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

function ResourceTable({ resources }: { resources: ResourceStatus[] }) {
  const styles = useStyles();
  return (
    <div className={styles.tableWrap}>
      <Table size="small">
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
                <TableCell><code className={styles.code}>{resourceDetail(status)}</code></TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

function EventTable({ events }: { events: RouterEvent[] }) {
  const styles = useStyles();
  return (
    <div className={styles.tableWrap}>
      <Table size="small">
        <TableHeader>
          <TableRow>
            <TableHeaderCell>Time</TableHeaderCell>
            <TableHeaderCell>Severity</TableHeaderCell>
            <TableHeaderCell>Topic</TableHeaderCell>
            <TableHeaderCell>Resource</TableHeaderCell>
            <TableHeaderCell>Details</TableHeaderCell>
          </TableRow>
        </TableHeader>
        <TableBody>
          {events.slice(0, 15).map(event => (
            <TableRow key={event.id ?? `${event.createdAt}-${event.topic}`}>
              <TableCell>{formatTime(event.createdAt)}</TableCell>
              <TableCell>{event.severity ?? ""}</TableCell>
              <TableCell><code className={styles.code}>{event.topic ?? event.type}</code></TableCell>
              <TableCell>{resourceName(event)}</TableCell>
              <TableCell><EventAttributes event={event} /></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function EventAttributes({ event }: { event: RouterEvent }) {
  const styles = useStyles();
  const attrs = eventAttributeEntries(event);
  if (attrs.length === 0) return <Text className={styles.muted}>{event.reason || event.message || "-"}</Text>;
  return (
    <div className={styles.chips}>
      {attrs.slice(0, 10).map(([key, value]) => (
        <Badge key={key} appearance="tint">
          {key}: {String(value)}
        </Badge>
      ))}
    </div>
  );
}

function ConnectionGroup({ group, dnsLabels, collapsed, toggle }: { group: { key: string; rows: ConnectionEntry[] }; dnsLabels: Record<string, string>; collapsed: boolean; toggle: () => void }) {
  const styles = useStyles();
  const label = connectionGroupLabel(group.key);
  return (
    <Card>
      <CardHeader
        header={<Text weight="semibold">{label.family}/{label.protocol.toUpperCase()} {group.rows.length}</Text>}
        action={<Button appearance="subtle" icon={collapsed ? <ChevronRightRegular /> : <ChevronDownRegular />} onClick={toggle}>{collapsed ? "Open" : "Close"}</Button>}
      />
      {!collapsed ? (
        <div className={styles.tableWrap}>
          <Table size="small">
            <TableHeader>
              <TableRow>
                <TableHeaderCell>State</TableHeaderCell>
                <TableHeaderCell>Flow</TableHeaderCell>
                <TableHeaderCell>Destination label</TableHeaderCell>
                <TableHeaderCell>Timeout</TableHeaderCell>
              </TableRow>
            </TableHeader>
            <TableBody>
              {group.rows.map(entry => (
                <TableRow key={flowKey(entry)}>
                  <TableCell><Badge appearance="tint" color={stateColor(entry.state)}>{entry.state || (entry.assured ? "ASSURED" : "stateless")}</Badge></TableCell>
                  <TableCell><code className={styles.code}>{endpoint(entry.original)}</code></TableCell>
                  <TableCell>{dnsLabels[entry.original?.destination ?? ""] ?? "-"}</TableCell>
                  <TableCell>{entry.timeout ?? 0}s</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : null}
    </Card>
  );
}

function ClientTraffic({ flows }: { flows: TrafficFlow[] }) {
  const styles = useStyles();
  return (
    <div className={styles.tableWrap}>
      <Table size="small">
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
              <TableCell>{row.bytesOut}</TableCell>
              <TableCell>{row.bytesIn}</TableCell>
              <TableCell><code className={styles.code}>{Array.from(row.peers).slice(0, 3).join(", ") || "-"}</code></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

function RecentDeny({ logs }: { logs: FirewallLog[] }) {
  const styles = useStyles();
  return (
    <div className={styles.tableWrap}>
      <Table size="small">
        <TableHeader>
          <TableRow>
            <TableHeaderCell>Count</TableHeaderCell>
            <TableHeaderCell>Source</TableHeaderCell>
            <TableHeaderCell>Destination</TableHeaderCell>
            <TableHeaderCell>Proto</TableHeaderCell>
          </TableRow>
        </TableHeader>
        <TableBody>
          {denyRows(logs).map(row => (
            <TableRow key={`${row.src}-${row.dst}-${row.proto}`}>
              <TableCell>{row.count}</TableCell>
              <TableCell><code className={styles.code}>{row.src}</code></TableCell>
              <TableCell><code className={styles.code}>{row.dst}</code></TableCell>
              <TableCell>{row.proto}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
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

function endpoint(tuple?: ConnTuple) {
  if (!tuple) return "-";
  return `${hostPort(tuple.source, tuple.sourcePort)} -> ${hostPort(tuple.destination, tuple.destinationPort)}`;
}

function hostPort(host?: string, port?: string) {
  return host ? `${host}${port ? `:${port}` : ""}` : "";
}

function flowKey(entry: ConnectionEntry) {
  return [entry.family, entry.protocol, entry.state, endpoint(entry.original), endpoint(entry.reply), entry.mark].join("|");
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

function clientTrafficRows(flows: TrafficFlow[]) {
  const totals = new Map<string, { client: string; bytesOut: number; bytesIn: number; peers: Set<string> }>();
  for (const flow of flows) {
    const key = flow.clientAddress || "-";
    const row = totals.get(key) ?? { client: key, bytesOut: 0, bytesIn: 0, peers: new Set<string>() };
    row.bytesOut += Number(flow.bytesOut || 0);
    row.bytesIn += Number(flow.bytesIn || 0);
    const peer = flow.resolvedHostname || flow.tlsSNI || flow.peerAddress;
    if (peer) row.peers.add(peer);
    totals.set(key, row);
  }
  return Array.from(totals.values()).sort((a, b) => a.client.localeCompare(b.client)).slice(0, 10);
}

function denyRows(logs: FirewallLog[]) {
  const totals = new Map<string, { src: string; dst: string; proto: string; count: number }>();
  for (const log of logs) {
    const key = `${log.srcAddress || "-"}>${log.dstAddress || "-"}>${log.protocol || "-"}`;
    const row = totals.get(key) ?? { src: log.srcAddress || "-", dst: log.dstAddress || "-", proto: log.protocol || "-", count: 0 };
    row.count++;
    totals.set(key, row);
  }
  return Array.from(totals.values()).sort((a, b) => b.count - a.count || a.src.localeCompare(b.src)).slice(0, 10);
}

function formatTime(value?: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return `${new Intl.DateTimeFormat(undefined, { month: "2-digit", day: "2-digit" }).format(date)} ${new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }).format(date)}`;
}

createRoot(document.getElementById("root")!).render(<App />);
