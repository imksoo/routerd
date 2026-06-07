// SPDX-License-Identifier: BSD-3-Clause

export const ROUTERD_CONFIG_SCHEMA_URL =
  "https://routerd.net/schemas/routerd-config-v1alpha1.schema.json";
export const ROUTERD_CONFIG_MODELINE = `# yaml-language-server: $schema=${ROUTERD_CONFIG_SCHEMA_URL}`;

const ROUTER_API = "routerd.net/v1alpha1";
const NET_API = "net.routerd.net/v1alpha1";
const FIREWALL_API = "firewall.routerd.net/v1alpha1";

export type HomeWanMode = "dhcpv4" | "pppoe" | "dslite" | "static";

export interface HomeRouterWizardState {
  routerName: string;
  interfaces: {
    wan: string;
    lans: string[];
    guest?: string;
  };
  wan: {
    mode: HomeWanMode;
    staticAddress?: string;
    staticGateway?: string;
    pppoeUsername?: string;
    pppoePasswordFile?: string;
    dsliteAFTRFQDN?: string;
    ipv6PD: boolean;
    healthCheck: boolean;
  };
  lan: {
    address: string;
    domain: string;
    dhcpv4: boolean;
    dns: boolean;
    raDhcpv6: boolean;
    nat44: boolean;
    firewallZones: boolean;
    guest: boolean;
  };
  ha: {
    vrrp: boolean;
    virtualAddress?: string;
    virtualRouterID?: number;
    priority?: number;
    peer?: string;
  };
}

export interface WizardFixtureScenario {
  name: string;
  description: string;
  state: HomeRouterWizardState;
}

type RouterResource = {
  apiVersion: string;
  kind: string;
  metadata: {
    name: string;
  };
  spec: Record<string, unknown>;
};

type RouterConfig = {
  apiVersion: string;
  kind: "Router";
  metadata: {
    name: string;
  };
  spec: {
    resources: RouterResource[];
  };
};

export const DEFAULT_HOME_ROUTER_STATE: HomeRouterWizardState = {
  routerName: "home-router",
  interfaces: {
    wan: "ens18",
    lans: ["ens19"],
  },
  wan: {
    mode: "dhcpv4",
    staticAddress: "203.0.113.2/24",
    staticGateway: "203.0.113.1",
    pppoeUsername: "user@example.jp",
    pppoePasswordFile: "/usr/local/etc/routerd/secrets/pppoe-home.password",
    dsliteAFTRFQDN: "gw.transix.jp",
    ipv6PD: false,
    healthCheck: false,
  },
  lan: {
    address: "192.168.10.1/24",
    domain: "home.example",
    dhcpv4: true,
    dns: true,
    raDhcpv6: false,
    nat44: true,
    firewallZones: true,
    guest: false,
  },
  ha: {
    vrrp: false,
    virtualAddress: "192.168.10.254/32",
    virtualRouterID: 10,
    priority: 150,
    peer: "192.168.10.2",
  },
};

export const HOME_ROUTER_FIXTURE_SCENARIOS: WizardFixtureScenario[] = [
  {
    name: "home-dhcpv4-lan",
    description: "DHCPv4 WAN with LAN DHCP, DNS, NAT44, and firewall zones.",
    state: mergeHomeRouterState({
      routerName: "wizard-home-dhcpv4",
      wan: { mode: "dhcpv4", healthCheck: true },
    }),
  },
  {
    name: "home-pppoe-vrrp",
    description: "PPPoE WAN with NAT44 and a LAN VRRP virtual address.",
    state: mergeHomeRouterState({
      routerName: "wizard-home-pppoe-vrrp",
      wan: { mode: "pppoe", healthCheck: true },
      lan: { address: "192.168.40.1/24" },
      ha: {
        vrrp: true,
        virtualAddress: "192.168.40.254/32",
        virtualRouterID: 40,
        peer: "192.168.40.2",
      },
    }),
  },
  {
    name: "home-dslite-ipv6",
    description: "DS-Lite WAN with delegated IPv6, LAN RA, DHCPv4, DNS, and firewall zones.",
    state: mergeHomeRouterState({
      routerName: "wizard-home-dslite-ipv6",
      wan: { mode: "dslite", ipv6PD: true, healthCheck: true },
      lan: {
        address: "192.168.60.1/24",
        raDhcpv6: true,
        nat44: false,
      },
    }),
  },
  {
    name: "home-static-guest-vrrp",
    description: "Static WAN with guest isolation and a LAN VRRP virtual address.",
    state: mergeHomeRouterState({
      routerName: "wizard-home-static-guest-vrrp",
      interfaces: { wan: "ens18", lans: ["ens19"], guest: "ens20" },
      wan: { mode: "static", healthCheck: true },
      lan: {
        address: "192.168.80.1/24",
        guest: true,
      },
      ha: {
        vrrp: true,
        virtualAddress: "192.168.80.254/32",
        virtualRouterID: 80,
        peer: "192.168.80.2",
      },
    }),
  },
];

export function mergeHomeRouterState(
  overrides: PartialHomeRouterWizardState = {},
): HomeRouterWizardState {
  return {
    routerName: overrides.routerName ?? DEFAULT_HOME_ROUTER_STATE.routerName,
    interfaces: {
      ...DEFAULT_HOME_ROUTER_STATE.interfaces,
      ...(overrides.interfaces ?? {}),
      lans: overrides.interfaces?.lans ?? DEFAULT_HOME_ROUTER_STATE.interfaces.lans,
    },
    wan: {
      ...DEFAULT_HOME_ROUTER_STATE.wan,
      ...(overrides.wan ?? {}),
    },
    lan: {
      ...DEFAULT_HOME_ROUTER_STATE.lan,
      ...(overrides.lan ?? {}),
    },
    ha: {
      ...DEFAULT_HOME_ROUTER_STATE.ha,
      ...(overrides.ha ?? {}),
    },
  };
}

type PartialHomeRouterWizardState = {
  routerName?: string;
  interfaces?: Partial<HomeRouterWizardState["interfaces"]>;
  wan?: Partial<HomeRouterWizardState["wan"]>;
  lan?: Partial<HomeRouterWizardState["lan"]>;
  ha?: Partial<HomeRouterWizardState["ha"]>;
};

export function buildHomeRouterConfig(state: HomeRouterWizardState): RouterConfig {
  const resources: RouterResource[] = [];
  const lanName = "lan";
  const lanAddress = normalizeCIDR(state.lan.address, DEFAULT_HOME_ROUTER_STATE.lan.address);
  const lanGateway = addressHost(lanAddress);
  const lanNetwork = networkCIDR(lanAddress);
  const wanEgress = wanEgressInterface(state.wan.mode);

  resources.push(resource(NET_API, "Interface", "wan", {
    ifname: state.interfaces.wan || DEFAULT_HOME_ROUTER_STATE.interfaces.wan,
    adminUp: true,
    managed: false,
    owner: "external",
  }));

  state.interfaces.lans.forEach((ifname, index) => {
    const name = index === 0 ? lanName : `lan${index + 1}`;
    resources.push(resource(NET_API, "Interface", name, {
      ifname,
      adminUp: true,
      managed: true,
      owner: "routerd",
    }));
  });

  if (state.lan.guest && state.interfaces.guest) {
    resources.push(resource(NET_API, "Interface", "guest", {
      ifname: state.interfaces.guest,
      adminUp: true,
      managed: true,
      owner: "routerd",
    }));
  }

  resources.push(resource(NET_API, "IPv4StaticAddress", "lan-base", {
    interface: lanName,
    address: lanAddress,
    exclusive: false,
  }));

  addWanResources(resources, state, wanEgress);
  addIPv6Resources(resources, state, lanName);
  addDNSResources(resources, state, lanName, lanAddress);
  addDHCPResources(resources, state, lanName, lanAddress, lanGateway);
  addNATResources(resources, state, wanEgress, lanNetwork);
  addFirewallResources(resources, state, wanEgress);
  addGuestResources(resources, state);
  addVRRPResources(resources, state, lanName);

  return {
    apiVersion: ROUTER_API,
    kind: "Router",
    metadata: {
      name: sanitizeName(state.routerName || DEFAULT_HOME_ROUTER_STATE.routerName),
    },
    spec: {
      resources,
    },
  };
}

export function buildHomeRouterYaml(state: HomeRouterWizardState): string {
  return `${ROUTERD_CONFIG_MODELINE}\n${dumpYaml(buildHomeRouterConfig(state))}\n`;
}

export function buildHomeRouterFixtureYamls(): Record<string, string> {
  return Object.fromEntries(
    HOME_ROUTER_FIXTURE_SCENARIOS.map((scenario) => [
      `${scenario.name}.yaml`,
      buildHomeRouterYaml(scenario.state),
    ]),
  );
}

function addWanResources(resources: RouterResource[], state: HomeRouterWizardState, wanEgress: string): void {
  switch (state.wan.mode) {
    case "dhcpv4":
      resources.push(resource(NET_API, "DHCPv4Client", "wan-dhcpv4", {
        interface: "wan",
      }));
      break;
    case "pppoe":
      resources.push(resource(NET_API, "PPPoESession", "pppoe-wan", {
        interface: "wan",
        ifname: "ppp-wan",
        username: state.wan.pppoeUsername || DEFAULT_HOME_ROUTER_STATE.wan.pppoeUsername,
        passwordFile: state.wan.pppoePasswordFile || DEFAULT_HOME_ROUTER_STATE.wan.pppoePasswordFile,
        mtu: 1454,
        mru: 1454,
        defaultRoute: true,
        usePeerDNS: false,
      }));
      break;
    case "dslite":
      resources.push(resource(NET_API, "DSLiteTunnel", "dslite", {
        interface: "wan",
        tunnelName: "ds-home",
        aftrFQDN: state.wan.dsliteAFTRFQDN || DEFAULT_HOME_ROUTER_STATE.wan.dsliteAFTRFQDN,
        aftrDNSServers: ["2404:1a8:7f01:a::3", "2404:1a8:7f01:b::3"],
        localAddressSource: state.wan.ipv6PD ? "delegatedAddress" : "interface",
        localDelegatedAddress: state.wan.ipv6PD ? "lan-v6" : undefined,
        localAddressSuffix: state.wan.ipv6PD ? "::100" : undefined,
        defaultRoute: true,
      }));
      break;
    case "static":
      resources.push(resource(NET_API, "IPv4StaticAddress", "wan-static", {
        interface: "wan",
        address: state.wan.staticAddress || DEFAULT_HOME_ROUTER_STATE.wan.staticAddress,
        exclusive: false,
      }));
      resources.push(resource(NET_API, "IPv4Route", "wan-default", {
        destination: "0.0.0.0/0",
        device: wanEgress,
        gateway: state.wan.staticGateway || DEFAULT_HOME_ROUTER_STATE.wan.staticGateway,
        metric: 100,
      }));
      break;
  }

  if (state.wan.healthCheck) {
    resources.push(resource(NET_API, "HealthCheck", "internet-v4", {
      role: "internet",
      addressFamily: "ipv4",
      target: "1.1.1.1",
      protocol: "tcp",
      port: 443,
      interval: "30s",
    }));
  }
}

function addIPv6Resources(resources: RouterResource[], state: HomeRouterWizardState, lanName: string): void {
  if (!state.wan.ipv6PD) {
    return;
  }
  resources.push(resource(NET_API, "DHCPv6PrefixDelegation", "wan-pd", {
    interface: "wan",
    profile: "ntt-hgw-lan-pd",
  }));
  resources.push(resource(NET_API, "IPv6DelegatedAddress", "lan-v6", {
    prefixDelegation: "wan-pd",
    interface: lanName,
    subnetID: "0",
    addressSuffix: "::1",
    announce: true,
  }));
  if (state.lan.raDhcpv6) {
    resources.push(resource(NET_API, "IPv6RouterAdvertisement", "lan-ra", {
      interface: lanName,
      prefixFrom: ref("IPv6DelegatedAddress/lan-v6", "address"),
      rdnssFrom: [ref("IPv6DelegatedAddress/lan-v6", "address")],
      dnsslFrom: [ref("DNSZone/home", "zone")],
      oFlag: true,
    }));
    resources.push(resource(NET_API, "DHCPv6Server", "lan-dhcpv6", {
      server: "dnsmasq",
      managed: true,
      interface: lanName,
      mode: "stateless",
      delegatedAddress: "lan-v6",
      dnsSource: "self",
      domainSearchFrom: [ref("DNSZone/home", "zone")],
    }));
  }
}

function addDNSResources(resources: RouterResource[], state: HomeRouterWizardState, lanName: string, lanAddress: string): void {
  if (!state.lan.dns) {
    return;
  }
  const listenAddressFrom = [ref("IPv4StaticAddress/lan-base", "address")];
  if (state.wan.ipv6PD) {
    listenAddressFrom.push(ref("IPv6DelegatedAddress/lan-v6", "address"));
  }
  resources.push(resource(NET_API, "DNSZone", "home", {
    zone: state.lan.domain || DEFAULT_HOME_ROUTER_STATE.lan.domain,
    ttl: 300,
    records: [
      {
        hostname: "router",
        ipv4From: ref("IPv4StaticAddress/lan-base", "address"),
      },
    ],
    dhcpDerived: state.lan.dhcpv4 ? {
      sources: ["DHCPv4Server/lan-dhcpv4"],
      ddns: true,
      ttl: 60,
    } : undefined,
  }));
  resources.push(resource(NET_API, "DNSResolver", "lan-resolver", {
    listen: [
      {
        name: lanName,
        addressFrom: listenAddressFrom,
        port: 53,
        sources: ["home", "default"],
      },
    ],
    cache: {
      enabled: true,
      maxEntries: 10000,
    },
  }));
  resources.push(resource(NET_API, "DNSForwarder", "home", {
    resolver: "DNSResolver/lan-resolver",
    match: [state.lan.domain || DEFAULT_HOME_ROUTER_STATE.lan.domain],
    zoneRefs: ["DNSZone/home"],
  }));
  resources.push(resource(NET_API, "DNSForwarder", "default", {
    resolver: "DNSResolver/lan-resolver",
    match: ["."],
    upstreams: ["DNSUpstream/cloudflare-udp", "DNSUpstream/google-udp"],
  }));
  resources.push(resource(NET_API, "DNSUpstream", "cloudflare-udp", {
    protocol: "udp",
    address: "1.1.1.1",
  }));
  resources.push(resource(NET_API, "DNSUpstream", "google-udp", {
    protocol: "udp",
    address: "8.8.8.8",
  }));
}

function addDHCPResources(
  resources: RouterResource[],
  state: HomeRouterWizardState,
  lanName: string,
  lanAddress: string,
  lanGateway: string,
): void {
  if (!state.lan.dhcpv4) {
    return;
  }
  resources.push(resource(NET_API, "DHCPv4Server", "lan-dhcpv4", {
    interface: lanName,
    addressPool: {
      start: poolAddress(lanAddress, 100),
      end: poolAddress(lanAddress, 199),
      leaseTime: "12h",
    },
    gatewayFrom: ref("IPv4StaticAddress/lan-base", "address"),
    dnsServerFrom: state.lan.dns ? [ref("IPv4StaticAddress/lan-base", "address")] : undefined,
    dnsServers: state.lan.dns ? undefined : ["1.1.1.1", "8.8.8.8"],
    domainFrom: state.lan.dns ? ref("DNSZone/home", "zone") : undefined,
    domain: state.lan.dns ? undefined : state.lan.domain,
    ntpServers: [lanGateway],
  }));
}

function addNATResources(resources: RouterResource[], state: HomeRouterWizardState, wanEgress: string, lanNetwork: string): void {
  if (!state.lan.nat44 || state.wan.mode === "dslite") {
    return;
  }
  resources.push(resource(NET_API, "NAT44Rule", "lan-to-wan", {
    type: "masquerade",
    egressInterface: wanEgress,
    sourceRanges: [lanNetwork],
    excludeDestinationCIDRs: ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"],
  }));
}

function addFirewallResources(resources: RouterResource[], state: HomeRouterWizardState, wanEgress: string): void {
  if (!state.lan.firewallZones) {
    return;
  }
  const wanInterfaces = ["wan"];
  if (wanEgress !== "wan") {
    wanInterfaces.push(wanEgress);
  }
  resources.push(resource(FIREWALL_API, "FirewallZone", "wan", {
    role: "untrust",
    interfaces: wanInterfaces,
  }));
  resources.push(resource(FIREWALL_API, "FirewallZone", "lan", {
    role: "trust",
    interfaces: state.lan.guest && state.interfaces.guest ? ["lan"] : ["lan", ...extraLanNames(state.interfaces.lans.length)],
  }));
  if (state.lan.guest && state.interfaces.guest) {
    resources.push(resource(FIREWALL_API, "FirewallZone", "guest", {
      role: "trust",
      interfaces: ["guest"],
    }));
  }
  resources.push(resource(FIREWALL_API, "FirewallPolicy", "home", {
    logDeny: true,
    sameRoleAccept: true,
  }));
}

function addGuestResources(resources: RouterResource[], state: HomeRouterWizardState): void {
  if (!state.lan.guest || !state.interfaces.guest) {
    return;
  }
  resources.push(resource(FIREWALL_API, "ClientPolicy", "guest-devices", {
    mode: "include",
    interfaces: ["guest"],
    isolation: {
      lanInternet: "allow",
      lanLAN: "deny",
      lanMgmt: "deny",
      mDNSBroadcast: "deny",
    },
    guestServices: ["dns", "dhcp"],
  }));
}

function addVRRPResources(resources: RouterResource[], state: HomeRouterWizardState, lanName: string): void {
  if (!state.ha.vrrp) {
    return;
  }
  resources.push(resource(NET_API, "VirtualAddress", "lan-vip", {
    interface: lanName,
    address: state.ha.virtualAddress || DEFAULT_HOME_ROUTER_STATE.ha.virtualAddress,
    family: "ipv4",
    mode: "vrrp",
    vrrp: {
      virtualRouterID: state.ha.virtualRouterID || DEFAULT_HOME_ROUTER_STATE.ha.virtualRouterID,
      priority: state.ha.priority || DEFAULT_HOME_ROUTER_STATE.ha.priority,
      authentication: "change-me",
      peers: state.ha.peer ? [state.ha.peer] : [],
    },
  }));
}

function resource(apiVersion: string, kind: string, name: string, spec: Record<string, unknown>): RouterResource {
  return {
    apiVersion,
    kind,
    metadata: {
      name,
    },
    spec: prune(spec),
  };
}

function ref(resourceName: string, field: string): Record<string, string> {
  return { resource: resourceName, field };
}

function wanEgressInterface(mode: HomeWanMode): string {
  switch (mode) {
    case "pppoe":
      return "pppoe-wan";
    case "dslite":
      return "dslite";
    default:
      return "wan";
  }
}

function extraLanNames(count: number): string[] {
  return Array.from({ length: Math.max(count - 1, 0) }, (_, index) => `lan${index + 2}`);
}

function sanitizeName(value: string): string {
  const out = value.trim().toLowerCase().replace(/[^a-z0-9-]+/g, "-").replace(/^-+|-+$/g, "");
  return out || DEFAULT_HOME_ROUTER_STATE.routerName;
}

function normalizeCIDR(value: string | undefined, fallback: string): string {
  return value && /^\d+\.\d+\.\d+\.\d+\/\d+$/.test(value) ? value : fallback;
}

function addressHost(cidr: string): string {
  return cidr.split("/")[0];
}

function networkCIDR(cidr: string): string {
  const [ip, prefix = "24"] = cidr.split("/");
  const parts = ip.split(".").map((part) => Number(part));
  if (parts.length !== 4 || prefix !== "24" || parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)) {
    return cidr;
  }
  return `${parts[0]}.${parts[1]}.${parts[2]}.0/24`;
}

function poolAddress(cidr: string, host: number): string {
  const parts = addressHost(cidr).split(".").map((part) => Number(part));
  if (parts.length !== 4 || parts.some((part) => !Number.isInteger(part) || part < 0 || part > 255)) {
    return host === 100 ? "192.168.10.100" : "192.168.10.199";
  }
  return `${parts[0]}.${parts[1]}.${parts[2]}.${host}`;
}

function prune<T>(value: T): T {
  if (Array.isArray(value)) {
    return value.map((item) => prune(item)).filter((item) => !emptyValue(item)) as T;
  }
  if (value && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [key, child] of Object.entries(value)) {
      const pruned = prune(child);
      if (!emptyValue(pruned)) {
        out[key] = pruned;
      }
    }
    return out as T;
  }
  return value;
}

function emptyValue(value: unknown): boolean {
  return value === undefined || value === null || (Array.isArray(value) && value.length === 0);
}

function dumpYaml(value: unknown, indent = 0): string {
  if (Array.isArray(value)) {
    return dumpArray(value, indent);
  }
  if (value && typeof value === "object") {
    return dumpObject(value as Record<string, unknown>, indent);
  }
  return `${" ".repeat(indent)}${formatScalar(value)}`;
}

function dumpObject(value: Record<string, unknown>, indent: number): string {
  const pad = " ".repeat(indent);
  return Object.entries(value)
    .map(([key, child]) => {
      if (isScalar(child)) {
        return `${pad}${key}: ${formatScalar(child)}`;
      }
      return `${pad}${key}:\n${dumpYaml(child, indent + 2)}`;
    })
    .join("\n");
}

function dumpArray(value: unknown[], indent: number): string {
  const pad = " ".repeat(indent);
  return value
    .map((item) => {
      if (isScalar(item)) {
        return `${pad}- ${formatScalar(item)}`;
      }
      if (Array.isArray(item)) {
        return `${pad}-\n${dumpArray(item, indent + 2)}`;
      }
      return dumpArrayObject(item as Record<string, unknown>, indent);
    })
    .join("\n");
}

function dumpArrayObject(value: Record<string, unknown>, indent: number): string {
  const entries = Object.entries(value);
  const pad = " ".repeat(indent);
  const childPad = " ".repeat(indent + 2);
  if (entries.length === 0) {
    return `${pad}- {}`;
  }
  return entries
    .map(([key, child], index) => {
      const prefix = index === 0 ? `${pad}- ` : childPad;
      if (isScalar(child)) {
        return `${prefix}${key}: ${formatScalar(child)}`;
      }
      return `${prefix}${key}:\n${dumpYaml(child, indent + 4)}`;
    })
    .join("\n");
}

function isScalar(value: unknown): boolean {
  return value === null || value === undefined || typeof value !== "object";
}

function formatScalar(value: unknown): string {
  if (typeof value === "string") {
    return JSON.stringify(value);
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return "null";
}
