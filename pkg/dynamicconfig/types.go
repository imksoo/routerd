// SPDX-License-Identifier: BSD-3-Clause

// Package dynamicconfig defines the runtime configuration fragments produced by
// trusted local sources and merged with startup configuration by routerd.
package dynamicconfig

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

const (
	// ConfigAPIVersion is the API group for dynamic configuration objects.
	ConfigAPIVersion = api.ConfigAPIVersion
	// HybridAPIVersion is the API group for hybrid cloud/on-prem resources.
	HybridAPIVersion = api.HybridAPIVersion

	// DirectiveOpMask suppresses a matching startup-config resource while the
	// directive is active.
	DirectiveOpMask = "mask"
)

// DynamicConfigPart is one generated runtime configuration fragment.
//
// DynamicConfigPart objects are produced by trusted local plugins or other
// dynamic sources and are stored separately from the human-managed startup
// configuration.
type DynamicConfigPart struct {
	api.TypeMeta `yaml:",inline" json:",inline"`
	Metadata     api.ObjectMeta        `yaml:"metadata" json:"metadata"`
	Spec         DynamicConfigPartSpec `yaml:"spec" json:"spec"`
}

// NewPart creates the common persistence envelope for a controller-produced
// DynamicConfigPart. Producers fill only the typed payloads they own; keeping
// the envelope here prevents otherwise-identical source, lease and empty
// directive setup from drifting between controllers.
func NewPart(name, source string, owners []api.OwnerRef, generation int64, observedAt, expiresAt time.Time) DynamicConfigPart {
	return DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{Name: name, OwnerRefs: append([]api.OwnerRef(nil), owners...)},
		Spec: DynamicConfigPartSpec{
			Source:     source,
			Generation: generation,
			ObservedAt: observedAt,
			ExpiresAt:  expiresAt,
			Directives: []DynamicConfigDirective{},
		},
	}
}

// DynamicConfigPartSpec describes the resources and directives observed from a
// dynamic source at one generation.
//
// ActionPlans are provider operations proposed by a trusted dynamic source.
// They are not resources and are not merged into effective config. The core
// reconciler never invokes provider CLIs/SDKs from a DynamicConfigPart; the
// separate provider-action engine may import these plans into its journal and
// hand them to an executor plugin only after ProviderActionPolicy and approval
// gates allow it.
type DynamicConfigPartSpec struct {
	Source      string                   `yaml:"source" json:"source"`
	Generation  int64                    `yaml:"generation" json:"generation"`
	ObservedAt  time.Time                `yaml:"observedAt" json:"observedAt"`
	ExpiresAt   time.Time                `yaml:"expiresAt" json:"expiresAt"`
	Digest      string                   `yaml:"digest" json:"digest"`
	Resources   []api.Resource           `yaml:"resources" json:"resources"`
	Directives  []DynamicConfigDirective `yaml:"directives" json:"directives"`
	ActionPlans []ActionPlan             `yaml:"actionPlans,omitempty" json:"actionPlans,omitempty"`
	// MobilityDataplane is the complete typed desired local-dataplane plan
	// produced by MobilityPool planning. It is deliberately separate from
	// Resources: its effects are not authorable API objects and must not be
	// reconstructed from controller status by a downstream controller.
	MobilityDataplane MobilityDataplanePlan `yaml:"mobilityDataplane,omitempty" json:"mobilityDataplane,omitempty"`
	// ARPObserverIntents are typed local observation bootstrap operations
	// produced from the normalized on-prem MobilityPool input. They keep the
	// daemon supervisor from reopening MobilityPool configuration just to start
	// an ownership observation source.
	ARPObserverIntents []ARPObserverIntent `yaml:"arpObserverIntents,omitempty" json:"arpObserverIntents,omitempty"`
	// FIBVerdicts are typed ownership results for the route effector. They keep
	// the FIB controller from reparsing MobilityPool status to recover a plan.
	FIBVerdicts []FIBVerdict `yaml:"fibVerdicts,omitempty" json:"fibVerdicts,omitempty"`
}

// MobilityPoolPlanSource identifies one reserved, typed MobilityPool
// DynamicConfigPart channel. No other dynamic source may carry MobilityPool
// dataplane, FIB, or ARP-observer intents.
type MobilityPoolPlanSource struct {
	PoolRef     string
	NodeRef     string
	ARPObserver bool
}

// ParseMobilityPoolPlanSource accepts only the two source shapes produced by
// mobility: MobilityPool/<pool>/node/<node> and its /arp-observer sibling.
// It deliberately rejects arbitrary suffixes so a similarly named source
// cannot enter the reserved typed channel.
func ParseMobilityPoolPlanSource(source string) (MobilityPoolPlanSource, bool) {
	if source != strings.TrimSpace(source) {
		return MobilityPoolPlanSource{}, false
	}
	segments := strings.Split(source, "/")
	if len(segments) != 4 && len(segments) != 5 {
		return MobilityPoolPlanSource{}, false
	}
	for _, segment := range segments {
		if segment == "" || strings.TrimSpace(segment) != segment {
			return MobilityPoolPlanSource{}, false
		}
	}
	if segments[0] != "MobilityPool" || segments[2] != "node" {
		return MobilityPoolPlanSource{}, false
	}
	result := MobilityPoolPlanSource{PoolRef: segments[1], NodeRef: segments[3]}
	if len(segments) == 5 {
		if segments[4] != "arp-observer" {
			return MobilityPoolPlanSource{}, false
		}
		result.ARPObserver = true
	}
	return result, true
}

// IsMobilityPoolPlanSource reports whether a DynamicConfigPart belongs to a
// reserved typed MobilityPool channel. Such parts must never contribute
// generic Resources or Directives, including an upgrade-stale payload written
// by the removed synthetic-resource bridge.
func IsMobilityPoolPlanSource(source string) bool {
	_, ok := ParseMobilityPoolPlanSource(source)
	return ok
}

// IsMobilityPoolReservedSource reports whether source occupies the namespace
// reserved for the strict MobilityPool plan protocol. A malformed entry in
// this namespace is inert; it must never fall back into generic DynamicConfig
// resource/directive merging.
func IsMobilityPoolReservedSource(source string) bool {
	return strings.HasPrefix(strings.TrimSpace(source), "MobilityPool/")
}

// IsMobilityPoolMainPlanSource reports whether source is the main typed plan
// channel, which alone may carry local dataplane and FIB effects.
func IsMobilityPoolMainPlanSource(source string) bool {
	parsed, ok := ParseMobilityPoolPlanSource(source)
	return ok && !parsed.ARPObserver
}

// IsMobilityPoolARPObserverPlanSource reports whether source is the typed
// ARP-observer channel, which alone may carry ARP observer intents.
func IsMobilityPoolARPObserverPlanSource(source string) bool {
	parsed, ok := ParseMobilityPoolPlanSource(source)
	return ok && parsed.ARPObserver
}

// CaptureDisposition is the one ownership decision a local dataplane
// effector consumes.  It prevents downstream layers from re-evaluating BGP,
// provider observations, or placement.
type CaptureDisposition string

const (
	CaptureProhibited      CaptureDisposition = "prohibited"
	CaptureDesired         CaptureDisposition = "desired"
	CaptureProtectExisting CaptureDisposition = "protect-existing"
	CaptureRelease         CaptureDisposition = "release"
	CaptureHold            CaptureDisposition = "hold"
)

// MobilityDataplanePlan is the complete typed local effect plan emitted by a
// MobilityPool reconcile. Captures are lowered by the SAM dataplane, while
// Routes and StaticAddresses are applied directly by the corresponding host
// effectors. None of these values are generic Router resources.
type MobilityDataplanePlan struct {
	// PoolPrefix is the canonical IPv4 scope of every address, route, and
	// static-address effect in this one Pool plan. The consumer verifies it
	// before giving any effect to a host controller; PoolRef alone is only an
	// identity label and must not authorize an arbitrary host address.
	PoolPrefix      string                      `yaml:"poolPrefix,omitempty" json:"poolPrefix,omitempty"`
	Captures        []LocalCaptureIntent        `yaml:"captures,omitempty" json:"captures,omitempty"`
	Routes          []MobilityIPv4RouteIntent   `yaml:"routes,omitempty" json:"routes,omitempty"`
	StaticAddresses []MobilityIPv4AddressIntent `yaml:"staticAddresses,omitempty" json:"staticAddresses,omitempty"`
}

// IsEmpty reports whether the plan carries no desired local dataplane effects.
func (p MobilityDataplanePlan) IsEmpty() bool {
	return len(p.Captures) == 0 && len(p.Routes) == 0 && len(p.StaticAddresses) == 0
}

// MobilityIPv4RoutePurpose identifies why a typed mobility route is needed.
// It remains distinct from generic IPv4Route resources so the route effector
// never has to reconstruct mobility ownership from annotations or status.
type MobilityIPv4RoutePurpose string

const (
	MobilityIPv4RoutePurposeLocalInventory MobilityIPv4RoutePurpose = "local-inventory"
	MobilityIPv4RoutePurposeCapturePrefix  MobilityIPv4RoutePurpose = "capture-prefix"
)

// MobilityIPv4RouteIntent is one exact IPv4 route that a mobility plan owns.
// Destination and PreferredSource use canonical CIDR/address strings. Metric
// is part of the identity of the effect the route adapter applies.
type MobilityIPv4RouteIntent struct {
	ID      string `yaml:"id" json:"id"`
	PoolRef string `yaml:"poolRef" json:"poolRef"`
	// PoolPrefix is injected by the typed record reader after it verifies the
	// enclosing plan scope. It is runtime-only so an aggregate spanning Pools
	// retains the teardown boundary without duplicating wire payload fields.
	PoolPrefix      string                   `yaml:"-" json:"-"`
	Purpose         MobilityIPv4RoutePurpose `yaml:"purpose" json:"purpose"`
	Destination     string                   `yaml:"destination" json:"destination"`
	Device          string                   `yaml:"device" json:"device"`
	PreferredSource string                   `yaml:"preferredSource,omitempty" json:"preferredSource,omitempty"`
	Metric          int                      `yaml:"metric,omitempty" json:"metric,omitempty"`
}

// MobilityIPv4AddressPurpose identifies why a typed mobility IPv4 address is
// needed. It is intentionally not a generic IPv4StaticAddress resource.
type MobilityIPv4AddressPurpose string

const (
	MobilityIPv4AddressPurposeCaptureSource MobilityIPv4AddressPurpose = "capture-source"
)

// MobilityIPv4AddressIntent is one exact IPv4 address that a mobility plan
// owns. Address is a canonical IPv4 host prefix.
type MobilityIPv4AddressIntent struct {
	ID         string                     `yaml:"id" json:"id"`
	PoolRef    string                     `yaml:"poolRef" json:"poolRef"`
	PoolPrefix string                     `yaml:"-" json:"-"`
	Purpose    MobilityIPv4AddressPurpose `yaml:"purpose" json:"purpose"`
	Interface  string                     `yaml:"interface" json:"interface"`
	Address    string                     `yaml:"address" json:"address"`
}

// LocalCaptureIntent carries the already-decided capture effect of a mobility
// plan across the DynamicConfigPart persistence boundary. Address is a
// canonical IPv4 host prefix. String fields keep this lower-level package
// independent of controller-only netip types while retaining a typed wire
// contract. Route and static-address projection details intentionally live in
// MobilityDataplanePlan rather than on this capture intent.
type LocalCaptureIntent struct {
	ID               string             `yaml:"id" json:"id"`
	PoolRef          string             `yaml:"poolRef" json:"poolRef"`
	PoolPrefix       string             `yaml:"-" json:"-"`
	Address          string             `yaml:"address" json:"address"`
	Disposition      CaptureDisposition `yaml:"disposition" json:"disposition"`
	CaptureType      string             `yaml:"captureType" json:"captureType"`
	CaptureInterface string             `yaml:"captureInterface,omitempty" json:"captureInterface,omitempty"`
	TunnelInterfaces []string           `yaml:"tunnelInterfaces,omitempty" json:"tunnelInterfaces,omitempty"`
	GratuitousARP    bool               `yaml:"gratuitousARP,omitempty" json:"gratuitousARP,omitempty"`
	Reason           string             `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// ARPObserverIntent carries the complete local daemon configuration for one
// on-prem ownership observation source. It is a DynamicConfigPart-only
// internal contract: the consumer adds runtime paths and applies it, but never
// reopens MobilityPool, EventGroup, or SAMNodeSet configuration.
type ARPObserverIntent struct {
	// ResourceName is a controller-generated, globally unique daemon identity.
	// It keys the supervised process, socket, and state path, so it must never
	// be copied directly from an ownershipDiscovery source.resource value.
	ResourceName      string   `yaml:"resourceName" json:"resourceName"`
	PoolRef           string   `yaml:"poolRef" json:"poolRef"`
	Prefix            string   `yaml:"prefix" json:"prefix"`
	SourceType        string   `yaml:"sourceType" json:"sourceType"`
	IfName            string   `yaml:"ifName" json:"ifName"`
	EventInterface    string   `yaml:"eventInterface" json:"eventInterface"`
	Network           string   `yaml:"network,omitempty" json:"network,omitempty"`
	Bridge            string   `yaml:"bridge,omitempty" json:"bridge,omitempty"`
	SourceAddress     string   `yaml:"sourceAddress,omitempty" json:"sourceAddress,omitempty"`
	Observe           bool     `yaml:"observe,omitempty" json:"observe,omitempty"`
	OnDemand          bool     `yaml:"onDemand,omitempty" json:"onDemand,omitempty"`
	ProbeTimeout      string   `yaml:"probeTimeout,omitempty" json:"probeTimeout,omitempty"`
	ProbeRetries      int      `yaml:"probeRetries,omitempty" json:"probeRetries,omitempty"`
	ScanInterval      string   `yaml:"scanInterval,omitempty" json:"scanInterval,omitempty"`
	IgnoredSenderMACs []string `yaml:"ignoredSenderMACs,omitempty" json:"ignoredSenderMACs,omitempty"`
}

var (
	mobilityARPObserverResourceName = regexp.MustCompile(`^mobility-arp-[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)
	mobilityInterfaceToken          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,62}$`)
)

const maxMobilityARPObserverDuration = 24 * time.Hour

// ValidateARPObserverIntent verifies the complete controller-to-daemon
// contract before a consumer can derive a socket path or start an observer.
// It is deliberately strict: a malformed DynamicConfigPart must result in no
// daemon spec, rather than a best-effort observer that could emit traffic.
func ValidateARPObserverIntent(intent ARPObserverIntent, sourcePoolRef string) error {
	sourcePoolRef = strings.TrimSpace(sourcePoolRef)
	if sourcePoolRef == "" || intent.PoolRef != sourcePoolRef {
		return fmt.Errorf("poolRef %q does not match source pool %q", intent.PoolRef, sourcePoolRef)
	}
	if !mobilityARPObserverResourceName.MatchString(intent.ResourceName) || len(intent.ResourceName) > 128 {
		return fmt.Errorf("resourceName %q is not a canonical mobility ARP observer name", intent.ResourceName)
	}
	if !validMobilityInterfaceToken(intent.IfName) || !validMobilityInterfaceToken(intent.EventInterface) ||
		(intent.Network != "" && !validMobilityInterfaceToken(intent.Network)) ||
		(intent.Bridge != "" && !validMobilityInterfaceToken(intent.Bridge)) {
		return fmt.Errorf("interface, eventInterface, network, and bridge must be safe interface tokens")
	}
	prefix, err := strictIPv4Prefix(intent.Prefix)
	if err != nil {
		return fmt.Errorf("prefix: %w", err)
	}
	if prefix.Bits() == 32 {
		return fmt.Errorf("prefix %q must describe a network, not a host", intent.Prefix)
	}
	if intent.SourceAddress != "" {
		address, err := netip.ParseAddr(intent.SourceAddress)
		if err != nil || !address.Is4() || address.String() != intent.SourceAddress {
			return fmt.Errorf("sourceAddress %q must be a canonical IPv4 address", intent.SourceAddress)
		}
	}
	if err := validateMobilityARPObserverTiming(intent.ProbeTimeout, intent.ProbeRetries, intent.ScanInterval); err != nil {
		return err
	}
	seenMACs := map[string]bool{}
	for _, rawMAC := range intent.IgnoredSenderMACs {
		mac, err := net.ParseMAC(rawMAC)
		if err != nil || strings.ToLower(mac.String()) != rawMAC || seenMACs[rawMAC] {
			return fmt.Errorf("ignoredSenderMACs contains noncanonical or duplicate MAC %q", rawMAC)
		}
		seenMACs[rawMAC] = true
	}
	switch intent.SourceType {
	case "arp-observer", "pve-svnet":
		if !intent.Observe || intent.OnDemand || intent.SourceAddress != "" {
			return fmt.Errorf("sourceType %q requires observe=true, onDemand=false, and no sourceAddress", intent.SourceType)
		}
	case "on-demand-arp":
		if intent.Observe || !intent.OnDemand || intent.SourceAddress == "" {
			return fmt.Errorf("sourceType on-demand-arp requires observe=false, onDemand=true, and a sourceAddress")
		}
	default:
		return fmt.Errorf("unsupported sourceType %q", intent.SourceType)
	}
	return nil
}

func validMobilityInterfaceToken(value string) bool {
	return mobilityInterfaceToken.MatchString(value)
}

func validateMobilityARPObserverTiming(probeTimeout string, probeRetries int, scanInterval string) error {
	if probeRetries < 0 || probeRetries > 20 {
		return fmt.Errorf("probeRetries %d must be between 0 and 20", probeRetries)
	}
	for _, field := range []struct {
		name  string
		value string
		min   time.Duration
	}{
		{name: "probeTimeout", value: probeTimeout, min: time.Nanosecond},
		{name: "scanInterval", value: scanInterval, min: time.Second},
	} {
		if field.value == "" {
			continue
		}
		duration, err := time.ParseDuration(field.value)
		if err != nil || duration < field.min || duration > maxMobilityARPObserverDuration {
			return fmt.Errorf("%s %q must be between %s and %s", field.name, field.value, field.min, maxMobilityARPObserverDuration)
		}
	}
	return nil
}

// ValidateMobilityDataplanePlanScope proves that all effect addresses are
// inside the canonical prefix emitted by the same Pool planner. Consumers call
// this before lower-level validation so an untrusted persisted record cannot
// direct a typed effect at an unrelated host route, address, or neighbor.
func ValidateMobilityDataplanePlanScope(plan MobilityDataplanePlan, sourcePoolRef string) error {
	sourcePoolRef = strings.TrimSpace(sourcePoolRef)
	if sourcePoolRef == "" {
		return fmt.Errorf("source pool is required")
	}
	pool, err := strictIPv4Prefix(plan.PoolPrefix)
	if err != nil {
		return fmt.Errorf("poolPrefix: %w", err)
	}
	for _, capture := range plan.Captures {
		if capture.PoolRef != sourcePoolRef {
			return fmt.Errorf("capture %q belongs to pool %q, want %q", capture.ID, capture.PoolRef, sourcePoolRef)
		}
		if _, err := scopedIPv4HostPrefix(capture.Address, pool); err != nil {
			return fmt.Errorf("capture %q address: %w", capture.ID, err)
		}
	}
	for _, route := range plan.Routes {
		if route.PoolRef != sourcePoolRef {
			return fmt.Errorf("route %q belongs to pool %q, want %q", route.ID, route.PoolRef, sourcePoolRef)
		}
		if _, err := scopedIPv4Prefix(route.Destination, pool); err != nil {
			return fmt.Errorf("route %q destination: %w", route.ID, err)
		}
		if route.PreferredSource != "" {
			address, err := netip.ParseAddr(route.PreferredSource)
			if err != nil || !address.Is4() || address.String() != route.PreferredSource || !pool.Contains(address) {
				return fmt.Errorf("route %q preferredSource %q is outside poolPrefix %q", route.ID, route.PreferredSource, plan.PoolPrefix)
			}
		}
	}
	for _, address := range plan.StaticAddresses {
		if address.PoolRef != sourcePoolRef {
			return fmt.Errorf("static address %q belongs to pool %q, want %q", address.ID, address.PoolRef, sourcePoolRef)
		}
		if _, err := scopedIPv4HostPrefix(address.Address, pool); err != nil {
			return fmt.Errorf("static address %q: %w", address.ID, err)
		}
	}
	return nil
}

func strictIPv4Prefix(value string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is4() || prefix.Masked().String() != value {
		return netip.Prefix{}, fmt.Errorf("%q must be a canonical IPv4 prefix", value)
	}
	return prefix, nil
}

// ParseCanonicalIPv4Prefix parses only a canonical masked IPv4 CIDR. It is
// shared by local effectors that need to retain a proven Pool scope in their
// applied-effect ledger.
func ParseCanonicalIPv4Prefix(value string) (netip.Prefix, error) {
	return strictIPv4Prefix(value)
}

func scopedIPv4HostPrefix(value string, pool netip.Prefix) (netip.Prefix, error) {
	prefix, err := strictIPv4Prefix(value)
	if err != nil || prefix.Bits() != 32 || !pool.Contains(prefix.Addr()) {
		return netip.Prefix{}, fmt.Errorf("%q must be a canonical IPv4 host prefix inside %s", value, pool)
	}
	return prefix, nil
}

func scopedIPv4Prefix(value string, pool netip.Prefix) (netip.Prefix, error) {
	prefix, err := strictIPv4Prefix(value)
	if err != nil || prefix.Bits() < pool.Bits() || !pool.Contains(prefix.Addr()) {
		return netip.Prefix{}, fmt.Errorf("%q must be a canonical IPv4 prefix inside %s", value, pool)
	}
	return prefix, nil
}

// FIBPoolScope is the small, normalized Pool-level safety boundary consumed by
// the BGP FIB admission controller.  It deliberately contains no ownership,
// placement, or provider facts: those remain per-address FIBVerdicts.  A
// scope is persisted in the existing FIBVerdicts stream so the consumer never
// needs to normalize MobilityPool configuration a second time.
type FIBPoolScope struct {
	Prefix                  string   `yaml:"prefix" json:"prefix"`
	RemoteReturnCommunities []string `yaml:"remoteReturnCommunities,omitempty" json:"remoteReturnCommunities,omitempty"`
	PreferredSource         string   `yaml:"preferredSource,omitempty" json:"preferredSource,omitempty"`
}

// FIBVerdict is the persisted, typed route-admission decision for one address.
// A row with an empty Address and non-nil Scope is the mandatory Pool-level
// scope header for a MobilityPool plan.  Consumers must fail closed for
// mobility-marked paths when no valid scope is available.
type FIBVerdict struct {
	PoolRef   string        `yaml:"poolRef" json:"poolRef"`
	Scope     *FIBPoolScope `yaml:"scope,omitempty" json:"scope,omitempty"`
	Address   string        `yaml:"address" json:"address"`
	Action    string        `yaml:"action" json:"action"`
	Class     string        `yaml:"class" json:"class"`
	OwnerNode string        `yaml:"ownerNode,omitempty" json:"ownerNode,omitempty"`
	Reason    string        `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// ValidateMobilityFIBVerdicts proves that one persisted MobilityPool FIB
// stream is a complete, canonical decision set for the exact source Pool.
// It intentionally rejects malformed rows as a whole rather than normalizing
// or dropping them: the BGP FIB consumer must never derive a partial policy
// from a corrupted typed plan.
func ValidateMobilityFIBVerdicts(verdicts []FIBVerdict, sourcePoolRef string) error {
	if sourcePoolRef == "" || sourcePoolRef != strings.TrimSpace(sourcePoolRef) {
		return fmt.Errorf("source pool %q must be nonempty and canonical", sourcePoolRef)
	}
	if len(verdicts) == 0 {
		return fmt.Errorf("MobilityPool/%s FIB verdicts must not be empty", sourcePoolRef)
	}
	var scope *FIBPoolScope
	for index, verdict := range verdicts {
		if verdict.PoolRef != sourcePoolRef {
			return fmt.Errorf("FIB verdict %d belongs to pool %q, want %q", index, verdict.PoolRef, sourcePoolRef)
		}
		if verdict.Scope == nil {
			continue
		}
		if scope != nil {
			return fmt.Errorf("MobilityPool/%s has more than one FIB scope header", sourcePoolRef)
		}
		if verdict.Address != "" || verdict.Action != "" || verdict.Class != "" || verdict.OwnerNode != "" || verdict.Reason != "" {
			return fmt.Errorf("MobilityPool/%s FIB scope header must not carry an address decision", sourcePoolRef)
		}
		if err := validateMobilityFIBPoolScope(*verdict.Scope); err != nil {
			return fmt.Errorf("MobilityPool/%s FIB scope: %w", sourcePoolRef, err)
		}
		scope = verdict.Scope
	}
	if scope == nil {
		return fmt.Errorf("MobilityPool/%s FIB verdicts require one scope header", sourcePoolRef)
	}
	pool, err := strictIPv4Prefix(scope.Prefix)
	if err != nil {
		return fmt.Errorf("MobilityPool/%s FIB scope prefix: %w", sourcePoolRef, err)
	}
	seenAddresses := map[string]bool{}
	for index, verdict := range verdicts {
		if verdict.Scope != nil {
			continue
		}
		if verdict.Address == "" {
			return fmt.Errorf("FIB verdict %d for MobilityPool/%s has no address or scope", index, sourcePoolRef)
		}
		if _, err := scopedIPv4HostPrefix(verdict.Address, pool); err != nil {
			return fmt.Errorf("FIB verdict %d address: %w", index, err)
		}
		if !validMobilityFIBAction(verdict.Action) {
			return fmt.Errorf("FIB verdict %d has unsupported action %q", index, verdict.Action)
		}
		for _, field := range []struct {
			name  string
			value string
		}{
			{name: "class", value: verdict.Class},
			{name: "ownerNode", value: verdict.OwnerNode},
			{name: "reason", value: verdict.Reason},
		} {
			if field.value != "" && field.value != strings.TrimSpace(field.value) {
				return fmt.Errorf("FIB verdict %d %s must not have surrounding whitespace", index, field.name)
			}
		}
		if seenAddresses[verdict.Address] {
			return fmt.Errorf("MobilityPool/%s has duplicate FIB verdict address %q", sourcePoolRef, verdict.Address)
		}
		seenAddresses[verdict.Address] = true
	}
	return nil
}

func validateMobilityFIBPoolScope(scope FIBPoolScope) error {
	pool, err := strictIPv4Prefix(scope.Prefix)
	if err != nil {
		return fmt.Errorf("prefix: %w", err)
	}
	seenCommunities := map[string]bool{}
	for _, community := range scope.RemoteReturnCommunities {
		if !validMobilityNodeIdentityCommunity(community) {
			return fmt.Errorf("remoteReturnCommunities contains invalid node identity %q", community)
		}
		if seenCommunities[community] {
			return fmt.Errorf("remoteReturnCommunities contains duplicate node identity %q", community)
		}
		seenCommunities[community] = true
	}
	if scope.PreferredSource == "" {
		return nil
	}
	if _, err := scopedIPv4HostPrefix(scope.PreferredSource, pool); err != nil {
		return fmt.Errorf("preferredSource: %w", err)
	}
	return nil
}

func validMobilityNodeIdentityCommunity(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || !strings.HasPrefix(value, "64512:") {
		return false
	}
	local, err := strconv.Atoi(strings.TrimPrefix(value, "64512:"))
	return err == nil && local >= 20000 && local < 60000
}

func validMobilityFIBAction(value string) bool {
	switch value {
	case "deliver-remote", "local-route", "withhold":
		return true
	default:
		return false
	}
}

// ActionPlan is a provider operation proposed by a dynamic source. It remains
// inert while attached to DynamicConfigPart; execution is possible only through
// the separate provider-action journal/approval/policy path.
//
// It is defined here (the lower-level package) rather than in pkg/plugin so
// DynamicConfigPartSpec can carry it without an import cycle (pkg/plugin imports
// pkg/dynamicconfig, not the reverse). pkg/plugin aliases these types.
type ActionPlan struct {
	Name            string            `yaml:"name" json:"name"`
	Provider        string            `yaml:"provider" json:"provider"`
	Action          string            `yaml:"action" json:"action"`
	Target          map[string]string `yaml:"target" json:"target"`
	ProviderRef     string            `yaml:"providerRef,omitempty" json:"providerRef,omitempty"`
	Mode            string            `yaml:"mode,omitempty" json:"mode,omitempty"`
	Description     string            `yaml:"description,omitempty" json:"description,omitempty"`
	RiskLevel       string            `yaml:"riskLevel,omitempty" json:"riskLevel,omitempty"`
	IdempotencyKey  string            `yaml:"idempotencyKey,omitempty" json:"idempotencyKey,omitempty"`
	Parameters      map[string]string `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Preconditions   []ActionCheck     `yaml:"preconditions,omitempty" json:"preconditions,omitempty"`
	ExpectedEffects []string          `yaml:"expectedEffects,omitempty" json:"expectedEffects,omitempty"`
	Undo            *ActionUndo       `yaml:"undo,omitempty" json:"undo,omitempty"`
}

// ActionCheck is a display-only precondition a plugin attached to an ActionPlan.
// routerd does not evaluate it.
type ActionCheck struct {
	Name   string            `yaml:"name" json:"name"`
	Expect string            `yaml:"expect,omitempty" json:"expect,omitempty"`
	Detail string            `yaml:"detail,omitempty" json:"detail,omitempty"`
	Target map[string]string `yaml:"target,omitempty" json:"target,omitempty"`
}

// ActionUndo describes the inverse provider operation for display only.
type ActionUndo struct {
	Action     string            `yaml:"action" json:"action"`
	Parameters map[string]string `yaml:"parameters,omitempty" json:"parameters,omitempty"`
}

// IsExpired reports whether the part's expiresAt timestamp is at or before now.
func (p *DynamicConfigPart) IsExpired(now time.Time) bool {
	if p == nil || p.Spec.ExpiresAt.IsZero() {
		return false
	}
	return !now.Before(p.Spec.ExpiresAt)
}

// DynamicConfigDirective changes how effective-config is derived without
// mutating startup-config.
type DynamicConfigDirective struct {
	Op     string          `yaml:"op" json:"op"`
	Target DirectiveTarget `yaml:"target" json:"target"`
	Reason string          `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// DirectiveTarget identifies one resource by API version, kind, and name.
type DirectiveTarget struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind" json:"kind"`
	Name       string `yaml:"name" json:"name"`
}

// DynamicOverridePolicy grants dynamic sources permission to use directives
// against selected startup resources.
type DynamicOverridePolicy struct {
	api.TypeMeta `yaml:",inline" json:",inline"`
	Metadata     api.ObjectMeta            `yaml:"metadata" json:"metadata"`
	Spec         DynamicOverridePolicySpec `yaml:"spec" json:"spec"`
}

// DynamicOverridePolicySpec lists allowed dynamic override rules.
type DynamicOverridePolicySpec struct {
	Allow []OverrideAllowRule `yaml:"allow" json:"allow"`
}

// OverrideAllowRule allows a source to perform operations on selected targets.
type OverrideAllowRule struct {
	Source     string            `yaml:"source" json:"source"`
	Operations []string          `yaml:"operations" json:"operations"`
	Targets    []DirectiveTarget `yaml:"targets" json:"targets"`
}
