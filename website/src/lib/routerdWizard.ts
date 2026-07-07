// SPDX-License-Identifier: BSD-3-Clause

export const ROUTERD_CONFIG_SCHEMA_URL =
  "https://routerd.net/schemas/routerd-config-v1alpha1.schema.json";
export const ROUTERD_CONFIG_MODELINE = `# yaml-language-server: $schema=${ROUTERD_CONFIG_SCHEMA_URL}`;

const ROUTER_API = "routerd.net/v1alpha1";
const NET_API = "net.routerd.net/v1alpha1";
const FIREWALL_API = "firewall.routerd.net/v1alpha1";
const FEDERATION_API = "federation.routerd.net/v1alpha1";
const HYBRID_API = "hybrid.routerd.net/v1alpha1";
const MOBILITY_API = "mobility.routerd.net/v1alpha1";

export type HomeWanMode = "dhcpv4" | "pppoe" | "dslite" | "static";
export type WizardProfile = "home" | "sam" | "k8s";
export type SAMNodeRole = "onprem" | "cloud";
export type SAMProvider = "aws" | "azure" | "oci";
export type K8SBGPSessionType = "ebgp" | "ibgp";
export type K8SBGPTimerProfile = "default" | "fast" | "slow";
export type K8SBGPConvergenceProfile = "default" | "fast" | "stable";

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

export interface SAMNode {
  nodeRef: string;
  site: string;
  role: SAMNodeRole;
  underlayIPv4: string;
  wgEndpoint: string;
  wgPublicKey: string;
  routerID: string;
  provider?: SAMProvider;
  providerRef?: string;
  captureInterface?: string;
  staticOwnedAddresses?: string[];
  vrrpGateRef?: string;
  placementGroup?: string;
  placementPriority?: number;
}

export interface SAMWizardState {
  name: string;
  mobilityPrefix: string;
  innerCIDR: string;
  bgpASN: number;
  routeReflectorNodeRef: string;
  nodes: SAMNode[];
}

export interface K8SWizardState {
  routerName: string;
  bgpRouterName: string;
  bgpPeerName: string;
  sessionType: K8SBGPSessionType;
  localASN: number;
  peerASN: number;
  routerID: string;
  listenAddress?: string;
  peerAddresses: string[];
  importPrefixes: string[];
  exportPrefixes: string[];
  redistributeConnected: boolean;
  redistributeStatic: boolean;
  ebgpMultihop: number;
  timersProfile: K8SBGPTimerProfile;
  convergenceProfile: K8SBGPConvergenceProfile;
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

export const DEFAULT_SAM_WIZARD_STATE: SAMWizardState = {
  name: "cloudedge",
  mobilityPrefix: "10.77.60.0/24",
  innerCIDR: "10.255.0.0/24",
  bgpASN: 64577,
  routeReflectorNodeRef: "onprem-router",
  nodes: [
    {
      nodeRef: "onprem-router",
      site: "onprem",
      role: "onprem",
      underlayIPv4: "10.99.0.1",
      wgEndpoint: "onprem.example.net:51820",
      wgPublicKey: "${ONPREM_WG_PUBLIC_KEY}",
      routerID: "10.99.0.1",
      captureInterface: "ens21",
      staticOwnedAddresses: ["10.77.60.10/32"],
    },
    {
      nodeRef: "aws-router-a",
      site: "aws",
      role: "cloud",
      underlayIPv4: "10.99.0.2",
      wgEndpoint: "aws-a.example.net:51820",
      wgPublicKey: "${AWS_ROUTER_A_WG_PUBLIC_KEY}",
      routerID: "10.99.0.2",
      provider: "aws",
      providerRef: "aws-lab",
      captureInterface: "ens5",
      placementGroup: "aws-edge",
      placementPriority: 10,
    },
    {
      nodeRef: "azure-router",
      site: "azure",
      role: "cloud",
      underlayIPv4: "10.99.0.3",
      wgEndpoint: "azure.example.net:51820",
      wgPublicKey: "${AZURE_WG_PUBLIC_KEY}",
      routerID: "10.99.0.3",
      provider: "azure",
      providerRef: "azure-lab",
      captureInterface: "eth0",
      placementGroup: "azure-edge",
      placementPriority: 10,
    },
  ],
};

export const DEFAULT_K8S_WIZARD_STATE: K8SWizardState = {
  routerName: "k8s-edge",
  bgpRouterName: "k8s-edge",
  bgpPeerName: "k8s-rt",
  sessionType: "ebgp",
  localASN: 64500,
  peerASN: 64512,
  routerID: "192.168.1.249",
  listenAddress: "192.168.1.249",
  peerAddresses: ["192.168.1.38", "192.168.1.53"],
  importPrefixes: ["10.250.0.0/24"],
  exportPrefixes: ["192.168.1.0/24"],
  redistributeConnected: true,
  redistributeStatic: false,
  ebgpMultihop: 0,
  timersProfile: "fast",
  convergenceProfile: "fast",
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

export function mergeSAMWizardState(overrides: PartialSAMWizardState = {}): SAMWizardState {
  return {
    name: overrides.name ?? DEFAULT_SAM_WIZARD_STATE.name,
    mobilityPrefix: overrides.mobilityPrefix ?? DEFAULT_SAM_WIZARD_STATE.mobilityPrefix,
    innerCIDR: overrides.innerCIDR ?? DEFAULT_SAM_WIZARD_STATE.innerCIDR,
    bgpASN: overrides.bgpASN ?? DEFAULT_SAM_WIZARD_STATE.bgpASN,
    routeReflectorNodeRef: overrides.routeReflectorNodeRef ?? DEFAULT_SAM_WIZARD_STATE.routeReflectorNodeRef,
    nodes: overrides.nodes ?? DEFAULT_SAM_WIZARD_STATE.nodes.map((node) => ({...node})),
  };
}

type PartialSAMWizardState = {
  name?: string;
  mobilityPrefix?: string;
  innerCIDR?: string;
  bgpASN?: number;
  routeReflectorNodeRef?: string;
  nodes?: SAMNode[];
};

export function buildSAMRouterConfig(state: SAMWizardState, selfNodeRef: string): RouterConfig {
  const nodes = stableSAMNodes(state.nodes);
  const self = nodes.find((node) => node.nodeRef === selfNodeRef) ?? nodes[0];
  const topologyNodeRefs = nodes.map((node) => node.nodeRef).sort();
  const routeReflector = nodes.find((node) => node.nodeRef === state.routeReflectorNodeRef) ?? nodes[0];
  const resources: RouterResource[] = [];

  resources.push(resource(FEDERATION_API, "EventGroup", "cloudedge", {
    nodeName: self.nodeRef,
    auth: {
      secretFile: "/usr/local/etc/routerd/secrets/eventd.key",
    },
    retention: {
      maxEvents: 1000,
      maxAge: "24h",
    },
    listen: {
      address: self.underlayIPv4,
      port: 9443,
    },
    replayWindow: "5m",
  }));

  for (const peer of nodes.filter((node) => node.nodeRef !== self.nodeRef)) {
    resources.push(resource(FEDERATION_API, "EventPeer", peer.nodeRef, {
      groupRef: "cloudedge",
      nodeName: peer.nodeRef,
      endpoint: `http://${peer.underlayIPv4}:9443`,
      direction: "push",
      types: ["routerd.client.ipv4.observed", "routerd.client.ipv4.expired"],
      subjectPrefixes: [mobilitySubjectPrefix(state.mobilityPrefix)],
    }));
  }

  resources.push(resource(NET_API, "WireGuardInterface", "wg-hybrid", {
    privateKeyFile: "/usr/local/etc/routerd/secrets/wg-hybrid.key",
    listenPort: 51820,
    mtu: 1420,
  }));
  resources.push(resource(NET_API, "Interface", "wg-hybrid", {
    ifname: "wg-hybrid",
    managed: false,
    mtu: 1420,
  }));
  resources.push(resource(NET_API, "IPv4StaticAddress", "wg-hybrid-ipv4", {
    interface: "wg-hybrid",
    address: `${self.underlayIPv4}/32`,
  }));

  for (const peer of nodes.filter((node) => node.nodeRef !== self.nodeRef)) {
    resources.push(resource(NET_API, "WireGuardPeer", `wg-${peer.nodeRef}`, {
      interface: "wg-hybrid",
      publicKey: peer.wgPublicKey,
      endpoint: peer.wgEndpoint,
      allowedIPs: [`${peer.underlayIPv4}/32`],
      persistentKeepalive: 25,
    }));
  }

  resources.push(resource(NET_API, "BGPRouter", "mobility-bgp", {
    asn: state.bgpASN,
    routerID: self.routerID || self.underlayIPv4,
    listen: {port: 179},
    importPolicy: {
      allowedPrefixes: [state.mobilityPrefix],
      nextHopRewrite: self.nodeRef === routeReflector.nodeRef ? "unchanged" : "peer-address",
    },
    exportPolicy: {
      allowedPrefixes: [state.mobilityPrefix],
    },
    timers: {profile: "fast"},
    convergenceProfile: "fast",
  }));

  resources.push(resource(MOBILITY_API, "SAMTransportProfile", "cloudedge-transport", {
    selfNodeRef: self.nodeRef,
    mode: "ipip",
    encryption: "wireguard",
    innerPrefix: state.innerCIDR,
    topologyNodeRefs,
    underlayInterface: "wg-hybrid",
    localEndpointFrom: ref("IPv4StaticAddress/wg-hybrid-ipv4", "address"),
    bgp: {
      routerRef: "BGPRouter/mobility-bgp",
      peerASN: state.bgpASN,
      timersPreset: "fast",
      routeReflectorClient: self.nodeRef !== routeReflector.nodeRef,
      routeReflectorClusterID: self.nodeRef !== routeReflector.nodeRef ? routeReflector.routerID : undefined,
      importPolicy: {
        allowedPrefixes: [state.mobilityPrefix],
        nextHopRewrite: self.nodeRef === routeReflector.nodeRef ? "unchanged" : "peer-address",
      },
      exportPolicy: {
        allowedPrefixes: [state.mobilityPrefix],
      },
    },
    peers: nodes
      .filter((node) => node.nodeRef !== self.nodeRef)
      .map((node) => ({
        nodeRef: node.nodeRef,
        remoteEndpoint: node.underlayIPv4,
      })),
  }));

  addSAMProviderProfiles(resources, nodes);
  resources.push(resource(MOBILITY_API, "MobilityPool", state.name || "cloudedge", {
    prefix: state.mobilityPrefix,
    groupRef: "cloudedge",
    deliveryPolicy: {mode: "bgp"},
    members: nodes.map((node) => mobilityMember(node)),
  }));

  return {
    apiVersion: ROUTER_API,
    kind: "Router",
    metadata: {
      name: `${sanitizeName(state.name || "cloudedge")}-${sanitizeName(self.nodeRef)}`,
    },
    spec: {
      resources,
    },
  };
}

export function buildSAMRouterYamls(state: SAMWizardState): Record<string, string> {
  const out: Record<string, string> = {};
  for (const node of stableSAMNodes(state.nodes)) {
    out[`${node.nodeRef}.yaml`] = `${ROUTERD_CONFIG_MODELINE}\n${dumpYaml(buildSAMRouterConfig(state, node.nodeRef))}\n`;
  }
  return out;
}

export function mergeK8SWizardState(overrides: PartialK8SWizardState = {}): K8SWizardState {
  return {
    routerName: overrides.routerName ?? DEFAULT_K8S_WIZARD_STATE.routerName,
    bgpRouterName: overrides.bgpRouterName ?? DEFAULT_K8S_WIZARD_STATE.bgpRouterName,
    bgpPeerName: overrides.bgpPeerName ?? DEFAULT_K8S_WIZARD_STATE.bgpPeerName,
    sessionType: overrides.sessionType ?? DEFAULT_K8S_WIZARD_STATE.sessionType,
    localASN: overrides.localASN ?? DEFAULT_K8S_WIZARD_STATE.localASN,
    peerASN: overrides.peerASN ?? DEFAULT_K8S_WIZARD_STATE.peerASN,
    routerID: overrides.routerID ?? DEFAULT_K8S_WIZARD_STATE.routerID,
    listenAddress: overrides.listenAddress ?? DEFAULT_K8S_WIZARD_STATE.listenAddress,
    peerAddresses: overrides.peerAddresses ?? [...DEFAULT_K8S_WIZARD_STATE.peerAddresses],
    importPrefixes: overrides.importPrefixes ?? [...DEFAULT_K8S_WIZARD_STATE.importPrefixes],
    exportPrefixes: overrides.exportPrefixes ?? [...DEFAULT_K8S_WIZARD_STATE.exportPrefixes],
    redistributeConnected: overrides.redistributeConnected ?? DEFAULT_K8S_WIZARD_STATE.redistributeConnected,
    redistributeStatic: overrides.redistributeStatic ?? DEFAULT_K8S_WIZARD_STATE.redistributeStatic,
    ebgpMultihop: overrides.ebgpMultihop ?? DEFAULT_K8S_WIZARD_STATE.ebgpMultihop,
    timersProfile: overrides.timersProfile ?? DEFAULT_K8S_WIZARD_STATE.timersProfile,
    convergenceProfile: overrides.convergenceProfile ?? DEFAULT_K8S_WIZARD_STATE.convergenceProfile,
  };
}

type PartialK8SWizardState = {
  routerName?: string;
  bgpRouterName?: string;
  bgpPeerName?: string;
  sessionType?: K8SBGPSessionType;
  localASN?: number;
  peerASN?: number;
  routerID?: string;
  listenAddress?: string;
  peerAddresses?: string[];
  importPrefixes?: string[];
  exportPrefixes?: string[];
  redistributeConnected?: boolean;
  redistributeStatic?: boolean;
  ebgpMultihop?: number;
  timersProfile?: K8SBGPTimerProfile;
  convergenceProfile?: K8SBGPConvergenceProfile;
};

export function buildK8SRouterConfig(state: K8SWizardState): RouterConfig {
  const routerName = sanitizeName(state.bgpRouterName || DEFAULT_K8S_WIZARD_STATE.bgpRouterName);
  const peerName = sanitizeName(state.bgpPeerName || DEFAULT_K8S_WIZARD_STATE.bgpPeerName);
  const importPrefixes = nonEmptyList(state.importPrefixes);
  const exportPrefixes = nonEmptyList(state.exportPrefixes);
  const resources: RouterResource[] = [
    resource(NET_API, "BGPRouter", routerName, {
      asn: state.localASN,
      routerID: state.routerID || DEFAULT_K8S_WIZARD_STATE.routerID,
      listen: state.listenAddress ? {address: state.listenAddress, port: 179} : {port: 179},
      importPolicy: importPrefixes.length > 0 ? {allowedPrefixes: importPrefixes} : undefined,
      exportPolicy: exportPrefixes.length > 0 ? {allowedPrefixes: exportPrefixes} : undefined,
      redistribute: {
        connected: state.redistributeConnected && exportPrefixes.length > 0 ? {allowedPrefixes: exportPrefixes} : undefined,
        static: state.redistributeStatic && exportPrefixes.length > 0 ? {allowedPrefixes: exportPrefixes} : undefined,
      },
      timers: {profile: state.timersProfile},
      convergenceProfile: state.convergenceProfile,
    }),
    resource(NET_API, "BGPPeer", peerName, {
      routerRef: `BGPRouter/${routerName}`,
      peerASN: state.sessionType === "ibgp" ? state.localASN : state.peerASN,
      peers: nonEmptyList(state.peerAddresses),
      ebgpMultihop: state.sessionType === "ebgp" && state.ebgpMultihop > 0 ? state.ebgpMultihop : undefined,
      importPolicy: importPrefixes.length > 0 ? {allowedPrefixes: importPrefixes} : undefined,
      exportPolicy: exportPrefixes.length > 0 ? {allowedPrefixes: exportPrefixes} : undefined,
      timers: {profile: state.timersProfile},
    }),
  ];

  return {
    apiVersion: ROUTER_API,
    kind: "Router",
    metadata: {
      name: sanitizeName(state.routerName || DEFAULT_K8S_WIZARD_STATE.routerName),
    },
    spec: {
      resources,
    },
  };
}

export function buildK8SRouterYaml(state: K8SWizardState): string {
  return `${ROUTERD_CONFIG_MODELINE}\n${dumpYaml(buildK8SRouterConfig(state))}\n`;
}

export function buildK8SRouterFixtureYamls(): Record<string, string> {
  return {
    "k8s-edge.yaml": buildK8SRouterYaml(DEFAULT_K8S_WIZARD_STATE),
  };
}

export function buildWizardFixtureYamls(): Record<string, string> {
  const home = Object.fromEntries(
    Object.entries(buildHomeRouterFixtureYamls()).map(([name, yaml]) => [`home/${name}`, yaml]),
  );
  const sam = Object.fromEntries(
    Object.entries(buildSAMRouterYamls(DEFAULT_SAM_WIZARD_STATE)).map(([name, yaml]) => [`sam/cloudedge-3node/${name}`, yaml]),
  );
  const k8s = Object.fromEntries(
    Object.entries(buildK8SRouterFixtureYamls()).map(([name, yaml]) => [`k8s/${name}`, yaml]),
  );
  return {...home, ...sam, ...k8s};
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

function stableSAMNodes(nodes: SAMNode[]): SAMNode[] {
  const candidates = nodes.length > 0 ? nodes : DEFAULT_SAM_WIZARD_STATE.nodes;
  const seen = new Set<string>();
  const out: SAMNode[] = [];
  for (const node of candidates) {
    const nodeRef = sanitizeSAMName(node.nodeRef || node.site || "node");
    if (seen.has(nodeRef)) {
      continue;
    }
    seen.add(nodeRef);
    const role: SAMNodeRole = node.role === "cloud" ? "cloud" : "onprem";
    const provider = role === "cloud" ? (node.provider ?? "aws") : undefined;
    out.push({
      ...node,
      nodeRef,
      site: sanitizeSAMName(node.site || nodeRef),
      role,
      underlayIPv4: node.underlayIPv4 || "10.99.0.1",
      wgEndpoint: node.wgEndpoint || `${nodeRef}.example.net:51820`,
      wgPublicKey: node.wgPublicKey || `\${${nodeRef.toUpperCase().replace(/-/g, "_")}_WG_PUBLIC_KEY}`,
      routerID: node.routerID || node.underlayIPv4 || "10.99.0.1",
      provider,
      providerRef: role === "cloud" ? (node.providerRef || `${provider}-lab`) : undefined,
      captureInterface: node.captureInterface || (role === "cloud" ? defaultCloudInterface(provider) : "ens21"),
      staticOwnedAddresses: role === "onprem" ? node.staticOwnedAddresses : undefined,
      placementGroup: role === "cloud" ? (node.placementGroup || `${node.site || provider}-edge`) : undefined,
      placementPriority: role === "cloud" ? (node.placementPriority || 10) : undefined,
    });
  }
  return out.sort((a, b) => a.nodeRef.localeCompare(b.nodeRef));
}

function sanitizeSAMName(value: string): string {
  return sanitizeName(value).replace(/^home-router$/, "node");
}

function mobilitySubjectPrefix(prefix: string): string {
  const address = addressHost(prefix);
  const parts = address.split(".");
  if (parts.length === 4 && parts.every((part) => /^\d+$/.test(part))) {
    return `${parts[0]}.${parts[1]}.${parts[2]}.`;
  }
  return address;
}

function addSAMProviderProfiles(resources: RouterResource[], nodes: SAMNode[]): void {
  const emitted = new Set<string>();
  for (const node of nodes) {
    if (node.role !== "cloud" || !node.provider || !node.providerRef || emitted.has(node.providerRef)) {
      continue;
    }
    emitted.add(node.providerRef);
    resources.push(resource(HYBRID_API, "CloudProviderProfile", node.providerRef, {
      provider: node.provider,
      capabilities: providerCapabilities(node.provider),
      auth: {
        mode: "external-command",
        command: `/usr/local/libexec/routerd/plugins/${node.provider}-auth`,
      },
    }));
  }
}

function providerCapabilities(provider: SAMProvider): string[] {
  switch (provider) {
    case "azure":
      return ["nic-secondary-ip", "ip-forwarding"];
    case "oci":
      return ["vnic-secondary-ip", "skip-source-dest-check"];
    case "aws":
    default:
      return ["eni-secondary-ip", "source-dest-check-disable"];
  }
}

function providerMode(provider: SAMProvider): string {
  switch (provider) {
    case "azure":
      return "nic-secondary-ip";
    case "oci":
      return "vnic-secondary-ip";
    case "aws":
    default:
      return "eni-secondary-ip";
  }
}

function defaultCloudInterface(provider?: SAMProvider): string {
  return provider === "azure" ? "eth0" : "ens5";
}

function mobilityMember(node: SAMNode): Record<string, unknown> {
  if (node.role === "cloud") {
    return {
      nodeRef: node.nodeRef,
      site: node.site,
      role: "cloud",
      capture: {
        type: "provider-secondary-ip",
        interface: node.captureInterface || defaultCloudInterface(node.provider),
        providerRef: node.providerRef,
        providerMode: providerMode(node.provider ?? "aws"),
        configureOSAddress: false,
      },
      ownershipDiscovery: {
        mode: "provider-private-ip",
        providerRef: node.providerRef,
        scanInterval: "60s",
        leaseTTL: "10m",
      },
      placement: {
        group: node.placementGroup,
        priority: node.placementPriority,
      },
    };
  }
  return {
    nodeRef: node.nodeRef,
    site: node.site,
    role: "onprem",
    staticOwnedAddresses: node.staticOwnedAddresses,
    capture: {
      type: "proxy-arp",
      interface: node.captureInterface || "ens21",
      gratuitousARP: true,
      activeWhen: node.vrrpGateRef
        ? {type: "vrrp-master", virtualAddressRef: node.vrrpGateRef}
        : {type: "single-router"},
    },
  };
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

function nonEmptyList(values: string[]): string[] {
  return values.map((value) => value.trim()).filter(Boolean);
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
