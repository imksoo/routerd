// SPDX-License-Identifier: BSD-3-Clause

// Package addressclaim is the SHARED, provider-neutral core for the example
// cloud "*-address-claim" routerd plugins (aws/azure/oci). It is an EXAMPLE /
// REFERENCE helper for CloudEdge Event Federation (ADR 0006, Phase 4.1): it
// turns one observed "routerd.client.ipv4.observed" federation event plus the
// allowlisted, secret-redacted plugin context into a single RemoteAddressClaim
// plus two canonical, DRY-RUN provider ActionPlans (assign-secondary-ip and
// ensure-forwarding-enabled, each with an undo).
//
// It is pure data transformation: it NEVER execs a process, NEVER imports a
// provider CLI/SDK, NEVER touches the network, and NEVER mutates anything. The
// actionPlans it builds are display-only proposals — routerd validates and
// persists them but never executes them. Provider actionPlan EXECUTION is out
// of scope (Phase 5).
//
// Like examples/plugins/event-to-remote-claim, this package mirrors the routerd
// plugin wire JSON with local structs and depends only on the Go standard
// library; it does NOT import pkg/plugin or pkg/api so the examples stay
// standalone and copyable.
package addressclaim

import (
	"fmt"
	"strings"
)

// Event types and constant API identifiers, mirrored from the routerd wire
// contract (kept local so this package stays standalone).
const (
	ObservedEventType = "routerd.client.ipv4.observed"

	ClaimAPIVersion  = "hybrid.routerd.net/v1alpha1"
	ClaimKind        = "RemoteAddressClaim"
	ResultAPIVersion = "plugin.routerd.net/v1alpha1"
	ResultKind       = "PluginResult"

	// Context resource kinds the cloud-side plugins read (allowlisted + redacted
	// by routerd before delivery; never carries secrets).
	KindCloudProviderProfile  = "CloudProviderProfile"
	KindAddressMobilityDomain = "AddressMobilityDomain"
	KindOverlayPeer           = "OverlayPeer"

	// Canonical ActionPlan verbs (must match pkg/plugin.ValidateActionPlan).
	ActionAssignSecondaryIP        = "assign-secondary-ip"
	ActionUnassignSecondaryIP      = "unassign-secondary-ip"
	ActionEnsureForwardingEnabled  = "ensure-forwarding-enabled"
	ActionEnsureForwardingDisabled = "ensure-forwarding-disabled"

	// ActionModeDryRun is the only mode these example plugins ever emit. routerd
	// rejects mode=execute outright.
	ActionModeDryRun = "dry-run"

	// RiskLevelMedium is the risk level both emitted plans carry.
	RiskLevelMedium = "medium"

	// DefaultTTL bounds how long the proposed dynamic config part lives.
	DefaultTTL = "30m"

	// DefaultOwnerSide is assumed when the observed event omits ownerSide.
	DefaultOwnerSide = "onprem"
)

// --- wire structs (subset of the routerd plugin protocol) ---

// PluginRequest mirrors the subset of the routerd PluginRequest wire JSON the
// cloud-side address-claim plugins read on stdin: matched federation events and
// the allowlisted, secret-redacted context resources.
type PluginRequest struct {
	Spec PluginRequestSpec `json:"spec"`
}

// PluginRequestSpec carries the matched events and the redacted context.
type PluginRequestSpec struct {
	Events  []MatchedEvent `json:"events"`
	Context PluginContext  `json:"context"`
}

// MatchedEvent mirrors pkg/plugin.PluginMatchedEvent on the wire.
type MatchedEvent struct {
	ID         string            `json:"id"`
	Group      string            `json:"group"`
	SourceNode string            `json:"sourceNode"`
	Type       string            `json:"type"`
	Subject    string            `json:"subject"`
	DedupeKey  string            `json:"dedupeKey"`
	Payload    map[string]string `json:"payload"`
}

// PluginContext mirrors pkg/plugin.PluginContext: the redacted resources the
// plugin is permitted to read.
type PluginContext struct {
	Resources []ContextResource `json:"resources"`
}

// ContextResource mirrors pkg/plugin.PluginContextResource. Spec is a generic
// JSON object (secrets already stripped by routerd); the plugin reads only the
// non-secret fields it needs.
type ContextResource struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Name       string         `json:"name"`
	Spec       map[string]any `json:"spec"`
}

// PluginResult mirrors the subset of the routerd PluginResult wire JSON the
// plugins emit on stdout. routerd accepts YAML or JSON; the plugins emit JSON.
type PluginResult struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   ObjectMeta         `json:"metadata"`
	Status     PluginResultStatus `json:"status"`
}

// PluginResultStatus carries the proposed dynamic resources and the display-only
// action plans.
type PluginResultStatus struct {
	TTL         string       `json:"ttl"`
	Resources   []Resource   `json:"resources"`
	ActionPlans []ActionPlan `json:"actionPlans"`
}

// Resource is one emitted RemoteAddressClaim (the only Kind these plugins emit).
type Resource struct {
	APIVersion string     `json:"apiVersion"`
	Kind       string     `json:"kind"`
	Metadata   ObjectMeta `json:"metadata"`
	Spec       ClaimSpec  `json:"spec"`
}

// ObjectMeta is the minimal resource metadata the plugins set.
type ObjectMeta struct {
	Name string `json:"name"`
}

// ClaimSpec mirrors pkg/api.RemoteAddressClaimSpec on the wire.
type ClaimSpec struct {
	DomainRef string       `json:"domainRef"`
	Address   string       `json:"address"`
	OwnerSide string       `json:"ownerSide"`
	Capture   CaptureSpec  `json:"capture"`
	Delivery  DeliverySpec `json:"delivery"`
}

// CaptureSpec mirrors pkg/api.AddressCapture on the wire.
type CaptureSpec struct {
	Type               string `json:"type"`
	ProviderRef        string `json:"providerRef"`
	ProviderMode       string `json:"providerMode"`
	NICRef             string `json:"nicRef"`
	ConfigureOSAddress bool   `json:"configureOSAddress"`
}

// DeliverySpec mirrors pkg/api.AddressDelivery on the wire.
type DeliverySpec struct {
	PeerRef         string `json:"peerRef"`
	Mode            string `json:"mode"`
	TunnelInterface string `json:"tunnelInterface"`
}

// ActionPlan mirrors pkg/dynamicconfig.ActionPlan (re-exported by pkg/plugin)
// on the wire. The plugins emit only the fields they populate.
type ActionPlan struct {
	Name            string            `json:"name"`
	Provider        string            `json:"provider"`
	Action          string            `json:"action"`
	Target          map[string]string `json:"target"`
	ProviderRef     string            `json:"providerRef,omitempty"`
	Mode            string            `json:"mode,omitempty"`
	Description     string            `json:"description,omitempty"`
	RiskLevel       string            `json:"riskLevel,omitempty"`
	IdempotencyKey  string            `json:"idempotencyKey,omitempty"`
	Parameters      map[string]string `json:"parameters,omitempty"`
	ExpectedEffects []string          `json:"expectedEffects,omitempty"`
	Undo            *ActionUndo       `json:"undo,omitempty"`
}

// ActionUndo mirrors pkg/dynamicconfig.ActionUndo on the wire.
type ActionUndo struct {
	Action     string            `json:"action"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

// --- provider profile (the per-provider knobs the common builder needs) ---

// ProviderProfile captures the provider-specific bits each provider main
// supplies; everything else (event parsing, claim shape, plan structure) is
// shared. It is plain data — no behavior, no execution.
type ProviderProfile struct {
	// Provider is the canonical provider id: "aws" | "azure" | "oci".
	Provider string
	// ForwardingParamKey/ForwardingParamValue is the provider-specific source/dest
	// check knob expressed as one parameter on the ensure-forwarding-* plan, e.g.
	// aws sourceDestCheck=false, azure ipForwarding=true, oci skipSourceDestCheck=true.
	ForwardingParamKey   string
	ForwardingParamValue string
	// TargetKeys are the provider-specific target keys (besides the universal
	// provider/providerRef/nicRef/address) copied from the event payload into the
	// assign/forwarding plan targets when present, in declaration order. Each entry
	// maps the target key to the event payload key it is read from. Absent payload
	// keys are simply omitted (they are not required by ValidateActionPlan).
	TargetKeys []TargetKey
}

// TargetKey maps an ActionPlan target key to the event payload key it is read
// from for one provider.
type TargetKey struct {
	// TargetKey is the key written into ActionPlan.Target / Parameters.
	TargetKey string
	// PayloadKey is the event payload key it is read from.
	PayloadKey string
}

// --- inputs extracted from the request ---

// claimInputs are the universal fields extracted from the observed event and
// context before the provider profile is applied.
type claimInputs struct {
	address     string
	domain      string
	ownerSide   string
	providerRef string
	nicRef      string
	peerRef     string
}

// Build is the single shared entry point: it parses the PluginRequest, applies
// the provider profile, and returns the PluginResult the calling main writes to
// stdout. It returns a clear error when a REQUIRED input is missing (no required
// context resource, or no usable address/domain on the event). It performs NO
// I/O, NO exec, and NO network access.
func Build(resultName string, profile ProviderProfile, req PluginRequest) (PluginResult, error) {
	if strings.TrimSpace(profile.Provider) == "" {
		return PluginResult{}, fmt.Errorf("%s: provider profile has no provider", resultName)
	}

	ev, ok := firstObservedEvent(req.Spec.Events)
	if !ok {
		return PluginResult{}, fmt.Errorf("%s: no %q event in PluginRequest", resultName, ObservedEventType)
	}

	// REQUIRED context resources. The cloud-side plugin is meaningless without a
	// provider profile, a mobility domain, and the overlay peer that carries the
	// delivered route, so a missing one is a hard error (not a silent default).
	profileRes, err := requireResource(req.Spec.Context.Resources, KindCloudProviderProfile)
	if err != nil {
		return PluginResult{}, fmt.Errorf("%s: %w", resultName, err)
	}
	domainRes, err := requireResource(req.Spec.Context.Resources, KindAddressMobilityDomain)
	if err != nil {
		return PluginResult{}, fmt.Errorf("%s: %w", resultName, err)
	}
	peerRes, err := requireResource(req.Spec.Context.Resources, KindOverlayPeer)
	if err != nil {
		return PluginResult{}, fmt.Errorf("%s: %w", resultName, err)
	}

	in, err := extractInputs(resultName, profile, ev, profileRes, domainRes, peerRes)
	if err != nil {
		return PluginResult{}, err
	}

	claim := buildClaim(profile, in)
	plans := buildActionPlans(profile, ev, in)

	return PluginResult{
		APIVersion: ResultAPIVersion,
		Kind:       ResultKind,
		Metadata:   ObjectMeta{Name: resultName},
		Status: PluginResultStatus{
			TTL:         DefaultTTL,
			Resources:   []Resource{claim},
			ActionPlans: plans,
		},
	}, nil
}

// extractInputs derives the universal claim/plan inputs from the observed event
// and the (required) context resources, validating the two facts that cannot be
// defaulted: the observed address and its mobility domain.
func extractInputs(resultName string, profile ProviderProfile, ev MatchedEvent, profileRes, domainRes, peerRes ContextResource) (claimInputs, error) {
	address := firstNonEmpty(ev.Payload["address"], ev.Subject)
	if address == "" {
		return claimInputs{}, fmt.Errorf("%s: event %q has no address (payload.address or subject)", resultName, ev.ID)
	}

	// domain: prefer the event payload (the observing node names the domain), then
	// the AddressMobilityDomain context resource name. Required.
	domain := firstNonEmpty(ev.Payload["domain"], domainRes.Name)
	if domain == "" {
		return claimInputs{}, fmt.Errorf("%s: event %q has no domain (payload.domain) and no AddressMobilityDomain in context", resultName, ev.ID)
	}

	// providerRef: prefer the context CloudProviderProfile name (the operator's
	// chosen reference), else the event payload, else the provider id.
	providerRef := firstNonEmpty(profileRes.Name, ev.Payload["providerRef"], profile.Provider)

	// peerRef: prefer the OverlayPeer context resource name, else the payload.
	peerRef := firstNonEmpty(peerRes.Name, ev.Payload["peerRef"])

	// nicRef is provider-specific and NOT present in the redacted profile, so it
	// is sourced from the observed event payload (like event-to-remote-claim).
	// Required: assign/unassign plans demand target.nicRef, and we never invent a
	// cloud resource id.
	nicRef := strings.TrimSpace(ev.Payload["nicRef"])
	if nicRef == "" {
		return claimInputs{}, fmt.Errorf("%s: event %q has no nicRef (payload.nicRef) for provider %q; refusing to invent a cloud NIC id", resultName, ev.ID, profile.Provider)
	}

	return claimInputs{
		address:     address,
		domain:      domain,
		ownerSide:   firstNonEmpty(ev.Payload["ownerSide"], DefaultOwnerSide),
		providerRef: providerRef,
		nicRef:      nicRef,
		peerRef:     peerRef,
	}, nil
}

// buildClaim turns the universal inputs into one RemoteAddressClaim. The capture
// is provider-secondary-ip with configureOSAddress=false: dry-run intent only.
func buildClaim(profile ProviderProfile, in claimInputs) Resource {
	return Resource{
		APIVersion: ClaimAPIVersion,
		Kind:       ClaimKind,
		Metadata:   ObjectMeta{Name: claimName(in.ownerSide, in.address)},
		Spec: ClaimSpec{
			DomainRef: in.domain,
			Address:   in.address,
			OwnerSide: in.ownerSide,
			Capture: CaptureSpec{
				Type:               "provider-secondary-ip",
				ProviderRef:        in.providerRef,
				ProviderMode:       "secondary-ip",
				NICRef:             in.nicRef,
				ConfigureOSAddress: false,
			},
			Delivery: DeliverySpec{
				PeerRef:         in.peerRef,
				Mode:            "route",
				TunnelInterface: "wg-hybrid",
			},
		},
	}
}

// buildActionPlans builds the two canonical, dry-run, undo-bearing plans:
// assign-secondary-ip (undo unassign-secondary-ip) and ensure-forwarding-enabled
// (undo ensure-forwarding-disabled). Both pass pkg/plugin.ValidateActionPlan.
func buildActionPlans(profile ProviderProfile, ev MatchedEvent, in claimInputs) []ActionPlan {
	// Universal target for the assign/unassign plans (address + nicRef are
	// required by ValidateActionPlan).
	assignTarget := map[string]string{
		"provider":    profile.Provider,
		"providerRef": in.providerRef,
		"nicRef":      in.nicRef,
		"address":     in.address,
	}
	addProviderTargetKeys(assignTarget, profile, ev)

	// The forwarding plan acts on the NIC, not a single address.
	fwdTarget := map[string]string{
		"provider":    profile.Provider,
		"providerRef": in.providerRef,
		"nicRef":      in.nicRef,
	}
	addProviderTargetKeys(fwdTarget, profile, ev)

	unassignTarget := copyTarget(assignTarget)
	fwdDisableTarget := copyTarget(fwdTarget)

	fwdParams := map[string]string{profile.ForwardingParamKey: profile.ForwardingParamValue}

	assign := ActionPlan{
		Name:           fmt.Sprintf("%s-assign-%s", profile.Provider, sanitize(in.address)),
		Provider:       profile.Provider,
		Action:         ActionAssignSecondaryIP,
		Target:         assignTarget,
		ProviderRef:    in.providerRef,
		Mode:           ActionModeDryRun,
		Description:    fmt.Sprintf("Assign %s as a secondary IP on %s NIC %s (dry-run; not executed)", in.address, profile.Provider, in.nicRef),
		RiskLevel:      RiskLevelMedium,
		IdempotencyKey: idempotencyKey(profile.Provider, in.nicRef, ActionAssignSecondaryIP, in.address),
		ExpectedEffects: []string{
			fmt.Sprintf("%s NIC %s would advertise secondary IP %s", profile.Provider, in.nicRef, in.address),
		},
		Undo: &ActionUndo{
			Action:     ActionUnassignSecondaryIP,
			Parameters: unassignTarget,
		},
	}

	forwarding := ActionPlan{
		Name:           fmt.Sprintf("%s-forwarding-%s", profile.Provider, sanitize(in.nicRef)),
		Provider:       profile.Provider,
		Action:         ActionEnsureForwardingEnabled,
		Target:         fwdTarget,
		ProviderRef:    in.providerRef,
		Mode:           ActionModeDryRun,
		Description:    fmt.Sprintf("Ensure IP forwarding / source-dest check disabled on %s NIC %s (dry-run; not executed)", profile.Provider, in.nicRef),
		RiskLevel:      RiskLevelMedium,
		IdempotencyKey: idempotencyKey(profile.Provider, in.nicRef, ActionEnsureForwardingEnabled, ""),
		Parameters:     fwdParams,
		ExpectedEffects: []string{
			fmt.Sprintf("%s NIC %s would forward traffic for the claimed address (%s=%s)", profile.Provider, in.nicRef, profile.ForwardingParamKey, profile.ForwardingParamValue),
		},
		Undo: &ActionUndo{
			Action:     ActionEnsureForwardingDisabled,
			Parameters: mergeParams(fwdDisableTarget, fwdParams),
		},
	}

	return []ActionPlan{assign, forwarding}
}

// addProviderTargetKeys copies the provider-specific target keys from the event
// payload into the target map when present. Absent keys are omitted (they are
// not required by ValidateActionPlan).
func addProviderTargetKeys(target map[string]string, profile ProviderProfile, ev MatchedEvent) {
	for _, k := range profile.TargetKeys {
		if v := strings.TrimSpace(ev.Payload[k.PayloadKey]); v != "" {
			target[k.TargetKey] = v
		}
	}
}

// --- helpers (pure) ---

func firstObservedEvent(events []MatchedEvent) (MatchedEvent, bool) {
	for _, ev := range events {
		if ev.Type == ObservedEventType {
			return ev, true
		}
	}
	return MatchedEvent{}, false
}

func requireResource(resources []ContextResource, kind string) (ContextResource, error) {
	for _, r := range resources {
		if r.Kind == kind {
			return r, nil
		}
	}
	return ContextResource{}, fmt.Errorf("required context resource of kind %q is missing", kind)
}

// claimName builds a deterministic resource name from owner side + address, e.g.
// ("onprem", "10.88.60.9/32") -> "onprem-10-88-60-9".
func claimName(ownerSide, address string) string {
	host := sanitize(stripPrefix(address))
	if host == "" {
		host = "addr"
	}
	if ownerSide == "" {
		ownerSide = DefaultOwnerSide
	}
	return ownerSide + "-" + host
}

// idempotencyKey is deterministic: "<provider>:<nicRef>:<action>:<address>".
// address is omitted (trailing colon dropped) for NIC-scoped plans.
func idempotencyKey(provider, nicRef, action, address string) string {
	key := provider + ":" + nicRef + ":" + action
	if address != "" {
		key += ":" + address
	}
	return key
}

func stripPrefix(address string) string {
	if idx := strings.IndexByte(address, '/'); idx >= 0 {
		return address[:idx]
	}
	return address
}

func sanitize(s string) string {
	s = strings.NewReplacer(".", "-", ":", "-", "/", "-").Replace(s)
	return strings.Trim(s, "-")
}

func copyTarget(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mergeParams(base, extra map[string]string) map[string]string {
	out := copyTarget(base)
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
