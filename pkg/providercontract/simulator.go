// SPDX-License-Identifier: BSD-3-Clause

package providercontract

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

const (
	ActionAssignSecondaryIP       = "assign-secondary-ip"
	ActionUnassignSecondaryIP     = "unassign-secondary-ip"
	ActionAssignRouteTableRoute   = "assign-route-table-route"
	ActionUnassignRouteTableRoute = "unassign-route-table-route"
	ActionEnsureForwardingEnabled = "ensure-forwarding-enabled"
)

type Result struct {
	Status  string
	Reason  string
	Message string
}

type AddressAssignment struct {
	Address       string
	TargetRef     string
	Generation    string
	AssignedAt    time.Time
	LastActionKey string
}

type RouteAssignment struct {
	Prefix        string
	RouteTableRef string
	NextHopRef    string
	Generation    string
	UpdatedAt     time.Time
}

type Snapshot struct {
	Generation        int64
	CompletedAt       time.Time
	Addresses         map[string]AddressAssignment
	Routes            map[string]RouteAssignment
	ForwardingEnabled map[string]bool
}

type Simulator struct {
	mu         sync.Mutex
	now        func() time.Time
	generation int64

	addresses  map[string]AddressAssignment
	routes     map[string]RouteAssignment
	forwarding map[string]bool
}

func New(now func() time.Time) *Simulator {
	if now == nil {
		now = time.Now
	}
	return &Simulator{
		now:        now,
		addresses:  map[string]AddressAssignment{},
		routes:     map[string]RouteAssignment{},
		forwarding: map[string]bool{},
	}
}

func (s *Simulator) Execute(plan dynamicconfig.ActionPlan) Result {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch strings.TrimSpace(plan.Action) {
	case ActionAssignSecondaryIP:
		return s.assignSecondaryIP(plan)
	case ActionUnassignSecondaryIP:
		return s.unassignSecondaryIP(plan)
	case ActionAssignRouteTableRoute:
		return s.assignRouteTableRoute(plan)
	case ActionUnassignRouteTableRoute:
		return s.unassignRouteTableRoute(plan)
	case ActionEnsureForwardingEnabled:
		return s.ensureForwardingEnabled(plan)
	default:
		return failed("UnsupportedAction", fmt.Sprintf("unsupported provider action %q", plan.Action))
	}
}

func (s *Simulator) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *Simulator) snapshotLocked() Snapshot {
	addresses := make(map[string]AddressAssignment, len(s.addresses))
	for key, value := range s.addresses {
		addresses[key] = value
	}
	routes := make(map[string]RouteAssignment, len(s.routes))
	for key, value := range s.routes {
		routes[key] = value
	}
	forwarding := make(map[string]bool, len(s.forwarding))
	for key, value := range s.forwarding {
		forwarding[key] = value
	}
	return Snapshot{
		Generation:        s.generation,
		CompletedAt:       s.now().UTC(),
		Addresses:         addresses,
		Routes:            routes,
		ForwardingEnabled: forwarding,
	}
}

func (s *Simulator) assignSecondaryIP(plan dynamicconfig.ActionPlan) Result {
	address := clean(plan.Target["address"])
	targetRef := firstNonEmpty(plan.Target["targetRef"], plan.Target["nicRef"], plan.Target["vnicRef"], plan.Target["networkInterfaceId"])
	if address == "" || targetRef == "" {
		return failed("InvalidTarget", "assign-secondary-ip requires target.address and targetRef/nicRef/vnicRef")
	}
	current, exists := s.addresses[address]
	if exists && current.TargetRef == targetRef {
		return succeeded("AlreadyAssigned", "address is already assigned to requested target")
	}
	if exists && !truthy(plan.Parameters["allowReassignment"]) {
		return failed("AddressHeldByAnotherTarget", fmt.Sprintf("%s is held by %s", address, current.TargetRef))
	}
	if exists {
		expected := clean(plan.Parameters["expectedHolderRef"])
		if expected != "" && expected != current.TargetRef {
			return failed("ObservedHolderMismatch", fmt.Sprintf("expected holder %s, observed %s", expected, current.TargetRef))
		}
	}
	s.generation++
	s.addresses[address] = AddressAssignment{
		Address:       address,
		TargetRef:     targetRef,
		Generation:    firstNonEmpty(plan.Parameters["assignmentGeneration"], plan.Parameters["claimGeneration"]),
		AssignedAt:    s.now().UTC(),
		LastActionKey: clean(plan.IdempotencyKey),
	}
	return succeeded("Assigned", "address assigned")
}

func (s *Simulator) unassignSecondaryIP(plan dynamicconfig.ActionPlan) Result {
	address := clean(plan.Target["address"])
	targetRef := firstNonEmpty(plan.Target["targetRef"], plan.Target["nicRef"], plan.Target["vnicRef"], plan.Target["networkInterfaceId"])
	if address == "" {
		return failed("InvalidTarget", "unassign-secondary-ip requires target.address")
	}
	current, exists := s.addresses[address]
	if !exists {
		return succeeded("AlreadyAbsent", "address is already absent")
	}
	if targetRef != "" && current.TargetRef != targetRef {
		return failed("AddressHeldByAnotherTarget", fmt.Sprintf("%s is held by %s", address, current.TargetRef))
	}
	delete(s.addresses, address)
	s.generation++
	return succeeded("Unassigned", "address unassigned")
}

func (s *Simulator) assignRouteTableRoute(plan dynamicconfig.ActionPlan) Result {
	prefix := firstNonEmpty(plan.Target["prefix"], plan.Target["destination"], plan.Target["address"])
	routeTable := clean(plan.Target["routeTableRef"])
	nextHop := firstNonEmpty(plan.Target["nextHopRef"], plan.Target["nextHop"], plan.Target["targetRef"])
	if prefix == "" || routeTable == "" || nextHop == "" {
		return failed("InvalidTarget", "assign-route-table-route requires prefix, routeTableRef, and nextHopRef")
	}
	key := routeKey(routeTable, prefix)
	current, exists := s.routes[key]
	if exists && current.NextHopRef != nextHop && !truthy(plan.Parameters["allowReassignment"]) {
		return failed("RouteHeldByAnotherTarget", fmt.Sprintf("%s points to %s", prefix, current.NextHopRef))
	}
	s.generation++
	s.routes[key] = RouteAssignment{
		Prefix:        prefix,
		RouteTableRef: routeTable,
		NextHopRef:    nextHop,
		Generation:    firstNonEmpty(plan.Parameters["assignmentGeneration"], plan.Parameters["claimGeneration"]),
		UpdatedAt:     s.now().UTC(),
	}
	return succeeded("RouteAssigned", "route assigned")
}

func (s *Simulator) unassignRouteTableRoute(plan dynamicconfig.ActionPlan) Result {
	prefix := firstNonEmpty(plan.Target["prefix"], plan.Target["destination"], plan.Target["address"])
	routeTable := clean(plan.Target["routeTableRef"])
	if prefix == "" || routeTable == "" {
		return failed("InvalidTarget", "unassign-route-table-route requires prefix and routeTableRef")
	}
	key := routeKey(routeTable, prefix)
	if _, exists := s.routes[key]; !exists {
		return succeeded("AlreadyAbsent", "route is already absent")
	}
	delete(s.routes, key)
	s.generation++
	return succeeded("RouteUnassigned", "route unassigned")
}

func (s *Simulator) ensureForwardingEnabled(plan dynamicconfig.ActionPlan) Result {
	targetRef := firstNonEmpty(plan.Target["targetRef"], plan.Target["nicRef"], plan.Target["vnicRef"], plan.Target["networkInterfaceId"])
	if targetRef == "" {
		return failed("InvalidTarget", "ensure-forwarding-enabled requires targetRef/nicRef/vnicRef")
	}
	if s.forwarding[targetRef] {
		return succeeded("AlreadyEnabled", "forwarding is already enabled")
	}
	s.forwarding[targetRef] = true
	s.generation++
	return succeeded("ForwardingEnabled", "forwarding enabled")
}

func (s Snapshot) AddressHolder(address string) (AddressAssignment, bool) {
	assignment, ok := s.Addresses[clean(address)]
	return assignment, ok
}

func (s Snapshot) Route(routeTableRef, prefix string) (RouteAssignment, bool) {
	route, ok := s.Routes[routeKey(routeTableRef, prefix)]
	return route, ok
}

func (s Snapshot) AddressKeys() []string {
	keys := make([]string, 0, len(s.Addresses))
	for key := range s.Addresses {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func succeeded(reason, message string) Result {
	return Result{Status: "Succeeded", Reason: reason, Message: message}
}

func failed(reason, message string) Result {
	return Result{Status: "Failed", Reason: reason, Message: message}
}

func routeKey(routeTableRef, prefix string) string {
	return clean(routeTableRef) + "|" + clean(prefix)
}

func truthy(value string) bool {
	switch strings.ToLower(clean(value)) {
	case "1", "t", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if cleaned := clean(value); cleaned != "" {
			return cleaned
		}
	}
	return ""
}

func clean(value string) string {
	return strings.TrimSpace(value)
}
