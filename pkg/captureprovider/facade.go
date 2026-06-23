// SPDX-License-Identifier: BSD-3-Clause

// Package captureprovider defines the CloudEdge SAM capture-provider domain
// facade. The facade is intentionally layered on top of dynamic provider-action
// plans; it does not execute cloud APIs or hold credentials.
package captureprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

const (
	StrategySecondaryIP = "secondary-ip"
	StrategyRouteTable  = "route-table"

	ActionAssignSecondaryIP        = "assign-secondary-ip"
	ActionUnassignSecondaryIP      = "unassign-secondary-ip"
	ActionAssignRouteTableRoute    = "assign-route-table-route"
	ActionUnassignRouteTableRoute  = "unassign-route-table-route"
	ActionEnsureForwardingEnabled  = "ensure-forwarding-enabled"
	ActionEnsureForwardingDisabled = "ensure-forwarding-disabled"
)

// CaptureOutcome is the normalized result vocabulary for provider capture
// facets. Concrete executors may expose provider-specific errors, but the SAM
// controller should reason over this domain vocabulary.
type CaptureOutcome int

const (
	Converged CaptureOutcome = iota
	Pending
	AlreadyOwnedBySelf
	OwnedByOther
	PrimaryAddressImmutable
	QuotaExceeded
	PermissionDenied
	RateLimited
	TransientFailure
	Unsupported
)

func (o CaptureOutcome) String() string {
	switch o {
	case Converged:
		return "Converged"
	case Pending:
		return "Pending"
	case AlreadyOwnedBySelf:
		return "AlreadyOwnedBySelf"
	case OwnedByOther:
		return "OwnedByOther"
	case PrimaryAddressImmutable:
		return "PrimaryAddressImmutable"
	case QuotaExceeded:
		return "QuotaExceeded"
	case PermissionDenied:
		return "PermissionDenied"
	case RateLimited:
		return "RateLimited"
	case TransientFailure:
		return "TransientFailure"
	case Unsupported:
		return "Unsupported"
	default:
		return fmt.Sprintf("CaptureOutcome(%d)", int(o))
	}
}

// AddressClaimProvider is the address-capture facet. Implementations must sit
// behind the provider-action approval/journal/executor path in production.
type AddressClaimProvider interface {
	ClaimAddress(context.Context, AddressClaimRequest) (CaptureOutcome, error)
	ReleaseAddress(context.Context, AddressReleaseRequest) (CaptureOutcome, error)
}

// RouteSteeringProvider is the route-table capture facet.
type RouteSteeringProvider interface {
	SetNextHop(context.Context, RouteSteeringRequest) (CaptureOutcome, error)
	ClearNextHop(context.Context, RouteClearRequest) (CaptureOutcome, error)
}

// CaptureInventoryProvider observes provider-side capture state.
type CaptureInventoryProvider interface {
	ObserveClaims(context.Context) ([]ObservedClaim, error)
}

// ForwardingCapabilityProvider manages provider forwarding capability.
type ForwardingCapabilityProvider interface {
	EnsureForwarding(context.Context, ForwardingRequest) error
}

type FencingToken struct {
	Generation      string
	MobilityPathSig string
	Holder          string
	LastSeenAt      time.Time
	TransitionFence string
	ForwardingDrift string
}

type AddressClaimRequest struct {
	Provider          string
	ProviderRef       string
	Pool              string
	Address           string
	NodeRef           string
	NICRef            string
	Strategy          string
	Target            map[string]string
	AllowReassignment bool
	Fence             FencingToken
}

type AddressReleaseRequest struct {
	Provider    string
	ProviderRef string
	Pool        string
	Address     string
	NodeRef     string
	NICRef      string
	Strategy    string
	Target      map[string]string
	StaleSince  time.Time
	Fence       FencingToken
}

type RouteSteeringRequest struct {
	Provider          string
	ProviderRef       string
	Pool              string
	Address           string
	NodeRef           string
	NICRef            string
	RouteTableRef     string
	NextHopIPAddress  string
	Target            map[string]string
	AllowReassignment bool
	Fence             FencingToken
}

type RouteClearRequest struct {
	Provider      string
	ProviderRef   string
	Pool          string
	Address       string
	NodeRef       string
	NICRef        string
	RouteTableRef string
	Target        map[string]string
	StaleSince    time.Time
	Fence         FencingToken
}

type ForwardingRequest struct {
	Provider    string
	ProviderRef string
	Pool        string
	NICRef      string
	Target      map[string]string
	Parameters  map[string]string
	Fence       FencingToken
}

type ObservedClaim struct {
	Provider      string
	ProviderRef   string
	Address       string
	OwnerNode     string
	NICRef        string
	RouteTableRef string
	Primary       bool
	ObservedAt    time.Time
}

// ActionPlanFacade lowers capture-provider domain operations into inert
// dynamicconfig.ActionPlans. The provider-action engine is still responsible
// for policy, approval, idempotency, journaling, and executor isolation.
type ActionPlanFacade struct{}

func NewActionPlanFacade() ActionPlanFacade {
	return ActionPlanFacade{}
}

func (ActionPlanFacade) ClaimAddress(req AddressClaimRequest) (dynamicconfig.ActionPlan, error) {
	req.Provider = strings.TrimSpace(req.Provider)
	req.ProviderRef = strings.TrimSpace(req.ProviderRef)
	req.Pool = strings.TrimSpace(req.Pool)
	req.Address = strings.TrimSpace(req.Address)
	req.NICRef = strings.TrimSpace(req.NICRef)
	strategy := normalizeStrategy(req.Strategy)
	target := normalizedTarget(req.Provider, req.ProviderRef, req.NICRef, req.Address, strategy, req.Target)
	if err := validateClaimTarget(req.Provider, strategy, target); err != nil {
		return dynamicconfig.ActionPlan{}, err
	}
	assignAction, unassignAction := CaptureActions(strategy)
	description, effects, err := claimDetails(req.Pool, req.Provider, req.NICRef, req.Address, strategy, target)
	if err != nil {
		return dynamicconfig.ActionPlan{}, err
	}
	risk := "medium"
	var params map[string]string
	if req.AllowReassignment {
		description = fmt.Sprintf("Seize/reassign %s capture on %s for MobilityPool/%s after capture failover", req.Address, req.Provider, req.Pool)
		risk = "high"
		params = map[string]string{"allowReassignment": "true"}
		effects = []string{fmt.Sprintf("%s would seize %s from any previous holder", req.Provider, req.Address)}
	}
	return dynamicconfig.ActionPlan{
		Name:            SafeName("mobility-" + req.Pool + "-assign-" + req.Address),
		Provider:        req.Provider,
		Action:          assignAction,
		Target:          target,
		ProviderRef:     req.ProviderRef,
		Mode:            "dry-run",
		Description:     description,
		RiskLevel:       risk,
		IdempotencyKey:  captureKey(req.Pool, req.Provider, providerCaptureTargetRef(strategy, target), assignAction, req.Address),
		Parameters:      params,
		ExpectedEffects: effects,
		Undo: &dynamicconfig.ActionUndo{
			Action:     unassignAction,
			Parameters: copyStringMap(target),
		},
	}, nil
}

func (ActionPlanFacade) ReleaseAddress(req AddressReleaseRequest) (dynamicconfig.ActionPlan, error) {
	req.Provider = strings.TrimSpace(req.Provider)
	req.ProviderRef = strings.TrimSpace(req.ProviderRef)
	req.Pool = strings.TrimSpace(req.Pool)
	req.Address = strings.TrimSpace(req.Address)
	req.NICRef = strings.TrimSpace(req.NICRef)
	strategy := normalizeStrategy(req.Strategy)
	target := normalizedTarget(req.Provider, req.ProviderRef, req.NICRef, req.Address, strategy, req.Target)
	if err := validateReleaseTarget(req.Provider, strategy, target); err != nil {
		return dynamicconfig.ActionPlan{}, err
	}
	assignAction, unassignAction := CaptureActions(strategy)
	description, effects := releaseDetails(req.Pool, req.Provider, req.NICRef, req.Address, strategy, target)
	return dynamicconfig.ActionPlan{
		Name:           SafeName("mobility-" + req.Pool + "-unassign-" + req.Address),
		Provider:       req.Provider,
		Action:         unassignAction,
		Target:         target,
		ProviderRef:    req.ProviderRef,
		Mode:           "dry-run",
		Description:    description,
		RiskLevel:      "medium",
		IdempotencyKey: captureKey(req.Pool, req.Provider, providerCaptureTargetRef(strategy, target), unassignAction, req.Address),
		Parameters: map[string]string{
			"deprovisionSince": req.StaleSince.UTC().Format(time.RFC3339Nano),
		},
		ExpectedEffects: effects,
		Undo: &dynamicconfig.ActionUndo{
			Action:     assignAction,
			Parameters: copyStringMap(target),
		},
	}, nil
}

func (ActionPlanFacade) SetNextHop(req RouteSteeringRequest) (dynamicconfig.ActionPlan, error) {
	target := copyStringMap(req.Target)
	target["routeTableRef"] = strings.TrimSpace(firstNonEmpty(req.RouteTableRef, target["routeTableRef"]))
	target["nextHopIPAddress"] = strings.TrimSpace(firstNonEmpty(req.NextHopIPAddress, target["nextHopIPAddress"]))
	return ActionPlanFacade{}.ClaimAddress(AddressClaimRequest{
		Provider:          req.Provider,
		ProviderRef:       req.ProviderRef,
		Pool:              req.Pool,
		Address:           req.Address,
		NodeRef:           req.NodeRef,
		NICRef:            req.NICRef,
		Strategy:          StrategyRouteTable,
		Target:            target,
		AllowReassignment: req.AllowReassignment,
		Fence:             req.Fence,
	})
}

func (ActionPlanFacade) ClearNextHop(req RouteClearRequest) (dynamicconfig.ActionPlan, error) {
	target := copyStringMap(req.Target)
	target["routeTableRef"] = strings.TrimSpace(firstNonEmpty(req.RouteTableRef, target["routeTableRef"]))
	return ActionPlanFacade{}.ReleaseAddress(AddressReleaseRequest{
		Provider:    req.Provider,
		ProviderRef: req.ProviderRef,
		Pool:        req.Pool,
		Address:     req.Address,
		NodeRef:     req.NodeRef,
		NICRef:      req.NICRef,
		Strategy:    StrategyRouteTable,
		Target:      target,
		StaleSince:  req.StaleSince,
		Fence:       req.Fence,
	})
}

func (ActionPlanFacade) EnsureForwarding(req ForwardingRequest) (dynamicconfig.ActionPlan, error) {
	provider := strings.TrimSpace(req.Provider)
	providerRef := strings.TrimSpace(req.ProviderRef)
	pool := strings.TrimSpace(req.Pool)
	nicRef := strings.TrimSpace(req.NICRef)
	target := copyStringMap(req.Target)
	target["provider"] = provider
	target["providerRef"] = providerRef
	target["nicRef"] = nicRef
	params := copyStringMap(req.Parameters)
	return dynamicconfig.ActionPlan{
		Name:           SafeName("mobility-" + pool + "-forwarding-" + nicRef),
		Provider:       provider,
		Action:         ActionEnsureForwardingEnabled,
		Target:         target,
		ProviderRef:    providerRef,
		Mode:           "dry-run",
		Description:    fmt.Sprintf("Ensure forwarding is enabled on %s NIC %s for MobilityPool/%s", provider, nicRef, pool),
		RiskLevel:      "medium",
		IdempotencyKey: "mobility:" + pool + ":" + provider + ":" + nicRef + ":" + ActionEnsureForwardingEnabled,
		Parameters:     params,
		ExpectedEffects: []string{
			fmt.Sprintf("%s NIC %s would forward traffic for mobility captures", provider, nicRef),
		},
		Undo: &dynamicconfig.ActionUndo{
			Action:     ActionEnsureForwardingDisabled,
			Parameters: mergeStringMaps(target, params),
		},
	}, nil
}

func CaptureActions(strategy string) (assign, unassign string) {
	if strings.TrimSpace(strategy) == StrategyRouteTable {
		return ActionAssignSecondaryIP, ActionUnassignSecondaryIP
	}
	return ActionAssignSecondaryIP, ActionUnassignSecondaryIP
}

func IsCaptureAssignAction(action string) bool {
	action = strings.TrimSpace(action)
	return action == ActionAssignSecondaryIP || action == ActionAssignRouteTableRoute
}

func IsCaptureReleaseAction(action string) bool {
	action = strings.TrimSpace(action)
	return action == ActionUnassignSecondaryIP || action == ActionUnassignRouteTableRoute
}

func SafeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "mobility"
	}
	return out
}

func normalizeStrategy(strategy string) string {
	if strings.TrimSpace(strategy) == "" {
		return StrategySecondaryIP
	}
	return strings.TrimSpace(strategy)
}

func normalizedTarget(provider, providerRef, nicRef, address, strategy string, target map[string]string) map[string]string {
	out := copyStringMap(target)
	out["provider"] = provider
	out["providerRef"] = providerRef
	out["nicRef"] = strings.TrimSpace(firstNonEmpty(nicRef, out["nicRef"]))
	out["address"] = address
	out["captureStrategy"] = strategy
	return out
}

func validateClaimTarget(provider, strategy string, target map[string]string) error {
	switch strings.TrimSpace(strategy) {
	case StrategySecondaryIP:
		return nil
	case StrategyRouteTable:
		routeTableRef := strings.TrimSpace(target["routeTableRef"])
		if routeTableRef == "" {
			return fmt.Errorf("capture.captureStrategy route-table requires capture.target.routeTableRef")
		}
		if (provider == "azure" || provider == "oci") && strings.TrimSpace(target["nextHopIPAddress"]) == "" {
			return fmt.Errorf("provider %s capture.captureStrategy route-table requires capture.target.nextHopIPAddress", provider)
		}
		return nil
	default:
		return fmt.Errorf("capture.captureStrategy %q is not supported", strategy)
	}
}

func validateReleaseTarget(provider, strategy string, target map[string]string) error {
	switch strings.TrimSpace(strategy) {
	case StrategySecondaryIP:
		return nil
	case StrategyRouteTable:
		if strings.TrimSpace(target["routeTableRef"]) == "" {
			return fmt.Errorf("capture.captureStrategy route-table requires capture.target.routeTableRef")
		}
		return nil
	default:
		return fmt.Errorf("capture.captureStrategy %q is not supported", strategy)
	}
}

func claimDetails(pool, provider, nicRef, address, strategy string, target map[string]string) (string, []string, error) {
	if strings.TrimSpace(strategy) == StrategyRouteTable {
		routeTableRef := strings.TrimSpace(target["routeTableRef"])
		return fmt.Sprintf("Route %s in %s route table %s to NIC %s for MobilityPool/%s", address, provider, routeTableRef, nicRef, pool), []string{
			fmt.Sprintf("%s route table %s would send %s to NIC %s", provider, routeTableRef, address, nicRef),
		}, nil
	}
	return fmt.Sprintf("Assign %s as a secondary IP on %s NIC %s for MobilityPool/%s", address, provider, nicRef, pool), []string{
		fmt.Sprintf("%s NIC %s would advertise secondary IP %s", provider, nicRef, address),
	}, nil
}

func releaseDetails(pool, provider, nicRef, address, strategy string, target map[string]string) (string, []string) {
	if strings.TrimSpace(strategy) == StrategyRouteTable {
		routeTableRef := strings.TrimSpace(target["routeTableRef"])
		return fmt.Sprintf("Remove stale route for %s from %s route table %s for MobilityPool/%s", address, provider, routeTableRef, pool), []string{
			fmt.Sprintf("%s route table %s would stop sending stale %s to NIC %s", provider, routeTableRef, address, nicRef),
		}
	}
	return fmt.Sprintf("Unassign stale secondary IP %s from %s NIC %s for MobilityPool/%s", address, provider, nicRef, pool), []string{
		fmt.Sprintf("%s NIC %s would stop advertising stale secondary IP %s", provider, nicRef, address),
	}
}

func providerCaptureTargetRef(strategy string, target map[string]string) string {
	if strings.TrimSpace(strategy) == StrategyRouteTable {
		if value := strings.TrimSpace(target["routeTableRef"]); value != "" {
			return value
		}
	}
	return strings.TrimSpace(target["nicRef"])
}

func captureKey(pool, provider, targetRef, action, address string) string {
	return "mobility:" + pool + ":" + provider + ":" + targetRef + ":" + action + ":" + address
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeStringMaps(a, b map[string]string) map[string]string {
	out := copyStringMap(a)
	for k, v := range b {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
