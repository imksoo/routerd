// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"encoding/json"
	"fmt"
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
	peersMaps := findMaps(root, "peers")
	if len(peersMaps) == 0 {
		return nil, nil
	}
	var peers []Peer
	seen := map[string]bool{}
	for _, peersMap := range peersMaps {
		for address, raw := range peersMap {
			if seen[address] {
				continue
			}
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			seen[address] = true
			state := firstString(item, "state", "bgpState", "peerState")
			lastErrorReason := firstString(item, "lastErrorReason", "lastResetDueTo", "lastNotificationReason", "lastErrorCode")
			peers = append(peers, Peer{
				Address:           address,
				ASN:               uint32(firstNumber(item, "remoteAs", "remoteAS", "remote_as", "remoteAsn", "remoteASN", "remote_asn", "peerAs", "peerAS", "peer_as")),
				State:             state,
				Established:       strings.EqualFold(state, "Established"),
				PrefixesReceived:  int(firstNumber(item, "pfxRcd", "prefixReceivedCount", "prefixesReceived")),
				MessagesReceived:  int(firstNumber(item, "msgRcvd", "messagesReceived", "messagesRcvd")),
				MessagesSent:      int(firstNumber(item, "msgSent", "messagesSent")),
				LastEstablishedAt: firstStringOrNumber(item, "lastEstablishedAt", "lastConnectionEstablished", "peerUptimeEstablishedEpoch"),
				LastErrorAt:       firstStringOrNumber(item, "lastErrorAt", "lastResetTime", "lastNotificationTime"),
				LastErrorReason:   lastErrorReason,
			})
		}
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

func ParseFRRBFDPeersJSON(data []byte) (map[string]BFD, error) {
	root, err := decodeMap(data)
	if err != nil {
		return nil, err
	}
	out := map[string]BFD{}
	collectBFDPeers("", root, out)
	return out, nil
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

func AttachBFD(state State, bfd map[string]BFD) State {
	for i, peer := range state.Peers {
		if status, ok := bfd[peer.Address]; ok {
			status.State = strings.TrimSpace(status.State)
			peer.BFD = &status
			state.Peers[i] = peer
		}
	}
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

func findMaps(value any, key string) []map[string]any {
	var out []map[string]any
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if child, ok := typed[key].(map[string]any); ok {
				out = append(out, child)
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return out
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
					prefix := routeFlags(key, routes)
					*out = append(*out, prefix)
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
	return annotatePrefixSelection(Prefix{
		Prefix:          prefix,
		Best:            firstBool(route, "bestpath", "best"),
		Valid:           !firstBool(route, "invalid"),
		Installed:       routeInstalled(route),
		Selected:        firstBool(route, "selected", "bestSelected"),
		Stale:           firstBool(route, "stale", "gracefulRestartStale"),
		SelectDeferred:  routeSelectDeferred(route),
		SelectionReason: routeSelectionReason(route),
		Communities:     communitiesFromRoute(route),
	}), true
}

func routeFlags(prefix string, routes []any) Prefix {
	out := Prefix{Prefix: prefix}
	var reasons []string
	for _, raw := range routes {
		route, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if !firstBool(route, "invalid") {
			out.Valid = true
		}
		if firstBool(route, "bestpath", "best") {
			out.Best = true
		}
		out.Installed = out.Installed || routeInstalled(route)
		out.Selected = out.Selected || firstBool(route, "selected", "bestSelected")
		out.Stale = out.Stale || firstBool(route, "stale", "gracefulRestartStale")
		out.SelectDeferred = out.SelectDeferred || routeSelectDeferred(route)
		if reason := routeSelectionReason(route); reason != "" {
			reasons = append(reasons, reason)
		}
		out.Communities = append(out.Communities, communitiesFromRoute(route)...)
	}
	out.SelectionReason = strings.Join(sortedUnique(reasons), "; ")
	out.Communities = sortedUnique(out.Communities)
	return annotatePrefixSelection(out)
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

func routeInstalled(route map[string]any) bool {
	if firstBool(route, "installed", "fib", "fibInstalled", "zebraInstalled", "inKernel") {
		return true
	}
	for _, key := range []string{"nexthops", "nexthop", "nextHops"} {
		if nestedAnyBool(route[key], "installed", "fib", "fibInstalled", "active") {
			return true
		}
	}
	return false
}

func routeSelectDeferred(route map[string]any) bool {
	if firstBool(route, "selectDeferred", "selectionDeferred", "deferred") {
		return true
	}
	reason := strings.ToLower(routeSelectionReason(route))
	return strings.Contains(reason, "selectdeferred") || (strings.Contains(reason, "select") && strings.Contains(reason, "defer"))
}

func routeSelectionReason(route map[string]any) string {
	return firstString(route, "selectionReason", "selectionReasonStr", "notSelectedReason", "ribFailureReason", "reason")
}

func communitiesFromRoute(route map[string]any) []string {
	var out []string
	for _, key := range []string{"community", "communities"} {
		out = append(out, communitiesFromValue(route[key])...)
	}
	return sortedUnique(out)
}

func collectBFDPeers(key string, value any, out map[string]BFD) {
	switch typed := value.(type) {
	case map[string]any:
		if peer, status, ok := bfdPeerStatus(key, typed); ok {
			out[peer] = status
		}
		for childKey, child := range typed {
			collectBFDPeers(childKey, child, out)
		}
	case []any:
		for _, child := range typed {
			collectBFDPeers("", child, out)
		}
	}
}

func bfdPeerStatus(key string, item map[string]any) (string, BFD, bool) {
	peer := firstString(item, "peer", "peerId", "peerAddress", "remote", "remoteAddress", "neighbor")
	if peer == "" && strings.Contains(key, ".") {
		peer = key
	}
	state := firstString(item, "status", "state", "peerState", "localDiag")
	if peer == "" || state == "" {
		return "", BFD{}, false
	}
	return peer, BFD{
		State:    state,
		LastUp:   firstStringOrNumber(item, "lastUp", "lastUpTime", "lastUpTimestamp"),
		LastDown: firstStringOrNumber(item, "lastDown", "lastDownTime", "lastDownTimestamp"),
	}, true
}

func communitiesFromValue(value any) []string {
	switch typed := value.(type) {
	case string:
		return strings.Fields(strings.TrimSpace(typed))
	case []any:
		var out []string
		for _, item := range typed {
			out = append(out, communitiesFromValue(item)...)
		}
		return out
	case map[string]any:
		var out []string
		for _, key := range []string{"string", "value", "name"} {
			if value, ok := typed[key].(string); ok {
				out = append(out, strings.Fields(strings.TrimSpace(value))...)
			}
		}
		if list, ok := typed["list"]; ok {
			out = append(out, communitiesFromValue(list)...)
		}
		return out
	default:
		return nil
	}
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
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func firstBool(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		switch value := values[key].(type) {
		case bool:
			return value
		case string:
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "true", "yes", "1":
				return true
			}
		case float64:
			if value != 0 {
				return true
			}
		}
	}
	return false
}

func nestedAnyBool(value any, keys ...string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if firstBool(typed, keys...) {
			return true
		}
		for _, child := range typed {
			if nestedAnyBool(child, keys...) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if nestedAnyBool(child, keys...) {
				return true
			}
		}
	}
	return false
}
