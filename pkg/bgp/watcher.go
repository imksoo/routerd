// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
)

const (
	EventPeerUp          = "peer up"
	EventPeerDown        = "peer down"
	EventPrefixAccepted  = "prefix accepted"
	EventPrefixWithdrawn = "prefix withdrawn"
	DefaultMaxPrefixes   = 4096

	MobilityCommunityOwner        = "64512:100"
	MobilityCommunityNodeLiveness = "64512:130"
)

type State struct {
	Peers    []Peer   `json:"peers,omitempty"`
	Prefixes []Prefix `json:"prefixes,omitempty"`
}

type Peer struct {
	Address           string `json:"address"`
	ASN               uint32 `json:"asn,omitempty"`
	State             string `json:"state,omitempty"`
	Established       bool   `json:"established"`
	BFD               *BFD   `json:"bfd,omitempty"`
	PrefixesReceived  int    `json:"prefixesReceived,omitempty"`
	MessagesReceived  int    `json:"messagesReceived,omitempty"`
	MessagesSent      int    `json:"messagesSent,omitempty"`
	LastEstablishedAt string `json:"lastEstablishedAt,omitempty"`
	LastErrorAt       string `json:"lastErrorAt,omitempty"`
	LastErrorReason   string `json:"lastErrorReason,omitempty"`
}

type BFD struct {
	State    string `json:"state,omitempty"`
	LastUp   string `json:"lastUp,omitempty"`
	LastDown string `json:"lastDown,omitempty"`
}

type Prefix struct {
	Prefix          string   `json:"prefix"`
	NextHop         string   `json:"nextHop,omitempty"`
	Best            bool     `json:"best"`
	Valid           bool     `json:"valid"`
	Installed       bool     `json:"installed"`
	Selected        bool     `json:"selected"`
	Stale           bool     `json:"stale,omitempty"`
	SelectDeferred  bool     `json:"selectDeferred,omitempty"`
	SelectionState  string   `json:"selectionState,omitempty"`
	SelectionReason string   `json:"selectionReason,omitempty"`
	Communities     []string `json:"communities,omitempty"`
}

type Event struct {
	Type     string `json:"type"`
	Peer     string `json:"peer,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Previous string `json:"previous,omitempty"`
	Current  string `json:"current,omitempty"`
}

func MobilityNodeIdentityCommunity(nodeRef string) string {
	nodeRef = strings.TrimSpace(nodeRef)
	if nodeRef == "" {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(nodeRef))
	return "64512:" + strconv.Itoa(20000+int(h.Sum32()%40000))
}

func IsMobilityNodeIdentityCommunity(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "64512:") {
		return false
	}
	local, err := strconv.Atoi(strings.TrimPrefix(value, "64512:"))
	if err != nil {
		return false
	}
	return local >= 20000 && local < 60000
}

func HasCommunity(values []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func Diff(previous, current State) []Event {
	previous = Normalize(previous)
	current = Normalize(current)
	prevPeers := map[string]Peer{}
	for _, peer := range previous.Peers {
		prevPeers[peer.Address] = peer
	}
	currentPeers := map[string]Peer{}
	for _, peer := range current.Peers {
		currentPeers[peer.Address] = peer
	}
	var events []Event
	for address, peer := range currentPeers {
		prev := prevPeers[address]
		if !prev.Established && peer.Established {
			events = append(events, Event{Type: EventPeerUp, Peer: address, Previous: prev.State, Current: peer.State})
		}
		if prev.Established && !peer.Established {
			events = append(events, Event{Type: EventPeerDown, Peer: address, Previous: prev.State, Current: peer.State})
		}
	}
	for address, prev := range prevPeers {
		if _, ok := currentPeers[address]; !ok && prev.Established {
			events = append(events, Event{Type: EventPeerDown, Peer: address, Previous: prev.State, Current: "missing"})
		}
	}
	prevPrefixes := map[string]bool{}
	for _, prefix := range previous.Prefixes {
		prevPrefixes[prefix.Prefix] = true
	}
	currentPrefixes := map[string]bool{}
	for _, prefix := range current.Prefixes {
		currentPrefixes[prefix.Prefix] = true
		if !prevPrefixes[prefix.Prefix] {
			events = append(events, Event{Type: EventPrefixAccepted, Prefix: prefix.Prefix})
		}
	}
	for prefix := range prevPrefixes {
		if !currentPrefixes[prefix] {
			events = append(events, Event{Type: EventPrefixWithdrawn, Prefix: prefix})
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].Type != events[j].Type {
			return events[i].Type < events[j].Type
		}
		if events[i].Peer != events[j].Peer {
			return events[i].Peer < events[j].Peer
		}
		return events[i].Prefix < events[j].Prefix
	})
	return events
}

func Normalize(state State) State {
	peerSeen := map[string]Peer{}
	for _, peer := range state.Peers {
		peer.Address = strings.TrimSpace(peer.Address)
		if peer.Address == "" {
			continue
		}
		peer.Established = peer.Established || strings.EqualFold(peer.State, "Established")
		peerSeen[peer.Address] = peer
	}
	state.Peers = state.Peers[:0]
	for _, peer := range peerSeen {
		state.Peers = append(state.Peers, peer)
	}
	sort.Slice(state.Peers, func(i, j int) bool { return state.Peers[i].Address < state.Peers[j].Address })
	state.Prefixes = uniquePrefixes(state.Prefixes)
	sort.Slice(state.Prefixes, func(i, j int) bool { return state.Prefixes[i].Prefix < state.Prefixes[j].Prefix })
	return state
}

func LimitPrefixes(state State, max int) (State, bool) {
	if max <= 0 || len(state.Prefixes) <= max {
		return state, false
	}
	state.Prefixes = append([]Prefix(nil), state.Prefixes[:max]...)
	return state, true
}

func uniquePrefixes(values []Prefix) []Prefix {
	seen := map[string]Prefix{}
	for _, value := range values {
		value.Prefix = strings.TrimSpace(value.Prefix)
		if value.Prefix == "" {
			continue
		}
		if existing, ok := seen[value.Prefix]; ok {
			value.Best = value.Best || existing.Best
			value.Valid = value.Valid || existing.Valid
			value.Installed = value.Installed || existing.Installed
			value.Selected = value.Selected || existing.Selected
			value.Stale = value.Stale || existing.Stale
			value.SelectDeferred = value.SelectDeferred || existing.SelectDeferred
			value.SelectionReason = strings.Join(sortedUnique([]string{value.SelectionReason, existing.SelectionReason}), "; ")
			value.Communities = sortedUnique(append(value.Communities, existing.Communities...))
		}
		value.Communities = sortedUnique(value.Communities)
		seen[value.Prefix] = annotatePrefixSelection(value)
	}
	out := make([]Prefix, 0, len(seen))
	for _, value := range seen {
		out = append(out, value)
	}
	return out
}

func annotatePrefixSelection(prefix Prefix) Prefix {
	if prefix.SelectionState == "" {
		switch {
		case prefix.SelectDeferred:
			prefix.SelectionState = "selectDeferred"
		case !prefix.Valid:
			prefix.SelectionState = "invalid"
		case !prefix.Best:
			prefix.SelectionState = "noBestPath"
		case !prefix.Installed:
			prefix.SelectionState = "notInstalledToZebra"
		default:
			prefix.SelectionState = "installed"
		}
	}
	return prefix
}

func sortedUnique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
