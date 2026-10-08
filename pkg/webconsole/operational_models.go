// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

type OperationalStatus struct {
	GeneratedAt time.Time                  `json:"generatedAt"`
	Kind        string                     `json:"kind"`
	Resources   []routerstate.ObjectStatus `json:"resources"`
}

type SAMStatus struct {
	GeneratedAt time.Time       `json:"generatedAt"`
	Nodes       []SAMNode       `json:"nodes"`
	Pools       []SAMPool       `json:"pools"`
	Tunnels     []SAMTunnel     `json:"tunnels,omitempty"`
	Federation  []SAMFederation `json:"federation,omitempty"`
	Errors      []string        `json:"errors,omitempty"`
}

type SAMNode struct {
	NodeRef        string `json:"nodeRef"`
	Site           string `json:"site,omitempty"`
	Role           string `json:"role,omitempty"`
	RouteReflector bool   `json:"routeReflector,omitempty"`
	Phase          string `json:"phase,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type SAMPool struct {
	Name                string           `json:"name"`
	Prefix              string           `json:"prefix,omitempty"`
	Phase               string           `json:"phase,omitempty"`
	Reason              string           `json:"reason,omitempty"`
	DiscoveryMode       string           `json:"discoveryMode,omitempty"`
	DiscoveryPhase      string           `json:"discoveryPhase,omitempty"`
	GeneratedBGPPaths   int              `json:"generatedBGPPaths,omitempty"`
	ResolvedMemberCount int              `json:"resolvedMemberCount,omitempty"`
	PlacementActive     bool             `json:"placementActive,omitempty"`
	PlacementActiveNode string           `json:"placementActiveNode,omitempty"`
	Addresses           []SAMPoolAddress `json:"addresses,omitempty"`
}

type SAMPoolAddress struct {
	Address   string `json:"address"`
	OwnerNode string `json:"ownerNode,omitempty"`
	Source    string `json:"source,omitempty"`
	State     string `json:"state,omitempty"`
}

type SAMTunnel struct {
	Name      string `json:"name"`
	PeerRef   string `json:"peerRef,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	Interface string `json:"interface,omitempty"`
}

type SAMFederation struct {
	GroupName  string `json:"groupName"`
	Phase      string `json:"phase,omitempty"`
	Reason     string `json:"reason,omitempty"`
	PeerCount  int    `json:"peerCount,omitempty"`
	ListenAddr string `json:"listenAddr,omitempty"`
}

type RoutesStatus struct {
	GeneratedAt time.Time      `json:"generatedAt"`
	Routes      []RouteEntry   `json:"routes"`
	BGPPeers    []RouteBGPPeer `json:"bgpPeers,omitempty"`
	Errors      []string       `json:"errors,omitempty"`
}

type RouteEntry struct {
	Source      string `json:"source"`
	Resource    string `json:"resource,omitempty"`
	Family      string `json:"family,omitempty"`
	Destination string `json:"destination"`
	Gateway     string `json:"gateway,omitempty"`
	Device      string `json:"device,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Table       string `json:"table,omitempty"`
	Metric      string `json:"metric,omitempty"`
	Scope       string `json:"scope,omitempty"`
	Type        string `json:"type,omitempty"`
	Peer        string `json:"peer,omitempty"`
	Phase       string `json:"phase,omitempty"`
	ObservedAt  string `json:"observedAt,omitempty"`
}

type RouteBGPPeer struct {
	Router           string `json:"router"`
	Peer             string `json:"peer"`
	ASN              string `json:"asn,omitempty"`
	State            string `json:"state,omitempty"`
	Established      bool   `json:"established,omitempty"`
	PrefixesReceived string `json:"prefixesReceived,omitempty"`
	Messages         string `json:"messages,omitempty"`
	LastEstablished  string `json:"lastEstablishedAt,omitempty"`
	LastError        string `json:"lastErrorReason,omitempty"`
}
