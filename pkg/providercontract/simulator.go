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

type pendingMutation struct {
	Due   time.Time
	Apply func()
}

type pendingRouteObservation struct {
	Key    string
	Route  RouteAssignment
	Delete bool
	Due    time.Time
}

type Simulator struct {
	mu         sync.Mutex
	now        func() time.Time
	generation int64

	actionDelay                time.Duration
	routeTableObservationDelay time.Duration

	addresses      map[string]AddressAssignment
	routes         map[string]RouteAssignment
	observedRoutes map[string]RouteAssignment
	forwarding     map[string]bool

	pendingMutations         []pendingMutation
	pendingRouteObservations []pendingRouteObservation
}

func New(now func() time.Time) *Simulator {
	if now == nil {
		now = time.Now
	}
	return &Simulator{
		now:            now,
		addresses:      map[string]AddressAssignment{},
		routes:         map[string]RouteAssignment{},
		observedRoutes: map[string]RouteAssignment{},
		forwarding:     map[string]bool{},
	}
}

func (s *Simulator) SetActionDelay(delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actionDelay = maxDuration(0, delay)
}

func (s *Simulator) SetRouteTableObservationDelay(delay time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routeTableObservationDelay = maxDuration(0, delay)
}

func (s *Simulator) Execute(plan dynamicconfig.ActionPlan) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyDueLocked()

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
	s.applyDueLocked()
	return s.snapshotLocked()
}

func (s *Simulator) ObserveRouteTableRoute(routeTableRef, prefix string) (RouteAssignment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyDueLocked()
	route, ok := s.observedRoutes[routeKey(routeTableRef, prefix)]
	return route, ok
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
	assignment := AddressAssignment{
		Address:       address,
		TargetRef:     targetRef,
		Generation:    firstNonEmpty(plan.Parameters["assignmentGeneration"], plan.Parameters["claimGeneration"]),
		AssignedAt:    time.Time{},
		LastActionKey: clean(plan.IdempotencyKey),
	}
	s.scheduleMutationLocked(func() {
		assignment.AssignedAt = s.now().UTC()
		s.generation++
		s.addresses[address] = assignment
	})
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
	s.scheduleMutationLocked(func() {
		delete(s.addresses, address)
		s.generation++
	})
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
	route := RouteAssignment{
		Prefix:        prefix,
		RouteTableRef: routeTable,
		NextHopRef:    nextHop,
		Generation:    firstNonEmpty(plan.Parameters["assignmentGeneration"], plan.Parameters["claimGeneration"]),
		UpdatedAt:     time.Time{},
	}
	s.scheduleMutationLocked(func() {
		route.UpdatedAt = s.now().UTC()
		s.generation++
		s.routes[key] = route
		s.scheduleRouteObservationLocked(key, route, false)
	})
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
	s.scheduleMutationLocked(func() {
		delete(s.routes, key)
		s.generation++
		s.scheduleRouteObservationLocked(key, RouteAssignment{}, true)
	})
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
	s.scheduleMutationLocked(func() {
		s.forwarding[targetRef] = true
		s.generation++
	})
	return succeeded("ForwardingEnabled", "forwarding enabled")
}

func (s *Simulator) scheduleMutationLocked(apply func()) {
	if s.actionDelay == 0 {
		apply()
		return
	}
	s.pendingMutations = append(s.pendingMutations, pendingMutation{
		Due:   s.now().UTC().Add(s.actionDelay),
		Apply: apply,
	})
}

func (s *Simulator) scheduleRouteObservationLocked(key string, route RouteAssignment, deleteRoute bool) {
	if s.routeTableObservationDelay == 0 {
		if deleteRoute {
			delete(s.observedRoutes, key)
		} else {
			s.observedRoutes[key] = route
		}
		return
	}
	s.pendingRouteObservations = append(s.pendingRouteObservations, pendingRouteObservation{
		Key:    key,
		Route:  route,
		Delete: deleteRoute,
		Due:    s.now().UTC().Add(s.routeTableObservationDelay),
	})
}

func (s *Simulator) applyDueLocked() {
	now := s.now().UTC()
	if len(s.pendingMutations) > 0 {
		sort.SliceStable(s.pendingMutations, func(i, j int) bool {
			return s.pendingMutations[i].Due.Before(s.pendingMutations[j].Due)
		})
		pending := s.pendingMutations[:0]
		for _, mutation := range s.pendingMutations {
			if mutation.Due.After(now) {
				pending = append(pending, mutation)
				continue
			}
			mutation.Apply()
		}
		s.pendingMutations = pending
	}
	if len(s.pendingRouteObservations) > 0 {
		sort.SliceStable(s.pendingRouteObservations, func(i, j int) bool {
			return s.pendingRouteObservations[i].Due.Before(s.pendingRouteObservations[j].Due)
		})
		pending := s.pendingRouteObservations[:0]
		for _, observation := range s.pendingRouteObservations {
			if observation.Due.After(now) {
				pending = append(pending, observation)
				continue
			}
			if observation.Delete {
				delete(s.observedRoutes, observation.Key)
			} else {
				s.observedRoutes[observation.Key] = observation.Route
			}
		}
		s.pendingRouteObservations = pending
	}
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

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
