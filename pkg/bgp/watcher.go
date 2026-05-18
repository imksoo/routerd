// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	EventPeerUp          = "peer up"
	EventPeerDown        = "peer down"
	EventPrefixAccepted  = "prefix accepted"
	EventPrefixWithdrawn = "prefix withdrawn"
	DefaultMaxPrefixes   = 4096
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
	PrefixesReceived  int    `json:"prefixesReceived,omitempty"`
	LastEstablishedAt string `json:"lastEstablishedAt,omitempty"`
	LastErrorAt       string `json:"lastErrorAt,omitempty"`
	LastErrorReason   string `json:"lastErrorReason,omitempty"`
}

type Prefix struct {
	Prefix string `json:"prefix"`
	Best   bool   `json:"best,omitempty"`
	Valid  bool   `json:"valid,omitempty"`
}

type Event struct {
	Type     string `json:"type"`
	Peer     string `json:"peer,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Previous string `json:"previous,omitempty"`
	Current  string `json:"current,omitempty"`
}

func ParseFRRState(summaryJSON, routesJSON []byte) (State, error) {
	peers, err := ParseFRRSummaryJSON(summaryJSON)
	if err != nil {
		return State{}, err
	}
	prefixes, err := ParseFRRRoutesJSON(routesJSON)
	if err != nil {
		return State{}, err
	}
	return Normalize(State{Peers: peers, Prefixes: prefixes}), nil
}

func ParseFRRSummaryJSON(data []byte) ([]Peer, error) {
	root, err := decodeMap(data)
	if err != nil {
		return nil, err
	}
	peersMap := findMap(root, "peers")
	if peersMap == nil {
		return nil, nil
	}
	var peers []Peer
	for address, raw := range peersMap {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		state := firstString(item, "state", "bgpState", "peerState")
		lastErrorReason := firstString(item, "lastErrorReason", "lastResetDueTo", "lastNotificationReason", "lastErrorCode")
		peers = append(peers, Peer{
			Address:           address,
			ASN:               uint32(firstNumber(item, "remoteAs", "remoteAS", "remote_as")),
			State:             state,
			Established:       strings.EqualFold(state, "Established"),
			PrefixesReceived:  int(firstNumber(item, "pfxRcd", "prefixReceivedCount", "prefixesReceived")),
			LastEstablishedAt: firstStringOrNumber(item, "lastEstablishedAt", "lastConnectionEstablished", "peerUptimeEstablishedEpoch"),
			LastErrorAt:       firstStringOrNumber(item, "lastErrorAt", "lastResetTime", "lastNotificationTime"),
			LastErrorReason:   lastErrorReason,
		})
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Address < peers[j].Address })
	return peers, nil
}

func ParseFRRRoutesJSON(data []byte) ([]Prefix, error) {
	root, err := decodeMap(data)
	if err != nil {
		return nil, err
	}
	var prefixes []Prefix
	collectPrefixes(root, &prefixes)
	prefixes = uniquePrefixes(prefixes)
	sort.Slice(prefixes, func(i, j int) bool { return prefixes[i].Prefix < prefixes[j].Prefix })
	return prefixes, nil
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

func decodeMap(data []byte) (map[string]any, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse FRR JSON: %w", err)
	}
	return root, nil
}

func findMap(value any, key string) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		if child, ok := typed[key].(map[string]any); ok {
			return child
		}
		for _, child := range typed {
			if found := findMap(child, key); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range typed {
			if found := findMap(child, key); found != nil {
				return found
			}
		}
	}
	return nil
}

func collectPrefixes(value any, out *[]Prefix) {
	switch typed := value.(type) {
	case map[string]any:
		if prefix, ok := routePrefix(typed); ok {
			*out = append(*out, prefix)
		}
		for key, child := range typed {
			if strings.Contains(key, "/") {
				if routes, ok := child.([]any); ok {
					best, valid := routeFlags(routes)
					*out = append(*out, Prefix{Prefix: key, Best: best, Valid: valid})
					continue
				}
			}
			collectPrefixes(child, out)
		}
	case []any:
		for _, child := range typed {
			collectPrefixes(child, out)
		}
	}
}

func routePrefix(route map[string]any) (Prefix, bool) {
	prefix := firstString(route, "prefix", "network")
	if prefix == "" || !strings.Contains(prefix, "/") {
		return Prefix{}, false
	}
	return Prefix{Prefix: prefix, Best: firstBool(route, "bestpath", "best"), Valid: !firstBool(route, "invalid")}, true
}

func routeFlags(routes []any) (bool, bool) {
	valid := false
	best := false
	for _, raw := range routes {
		route, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if !firstBool(route, "invalid") {
			valid = true
		}
		if firstBool(route, "bestpath", "best") {
			best = true
		}
	}
	return best, valid
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
		}
		seen[value.Prefix] = value
	}
	out := make([]Prefix, 0, len(seen))
	for _, value := range seen {
		out = append(out, value)
	}
	return out
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstStringOrNumber(values map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := values[key].(type) {
		case string:
			return strings.TrimSpace(value)
		case float64:
			if value != 0 {
				return fmt.Sprintf("%.0f", value)
			}
		case int:
			if value != 0 {
				return fmt.Sprint(value)
			}
		}
	}
	return ""
}

func firstNumber(values map[string]any, keys ...string) float64 {
	for _, key := range keys {
		switch value := values[key].(type) {
		case float64:
			return value
		case int:
			return float64(value)
		}
	}
	return 0
}

func firstBool(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := values[key].(bool); ok {
			return value
		}
	}
	return false
}
