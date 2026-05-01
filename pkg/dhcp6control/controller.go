package dhcp6control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/pdstrategy"
	routerstate "routerd/pkg/state"
)

type Sender interface {
	SendFrame(ctx context.Context, ifname string, frame []byte) error
}

type Controller struct {
	Sender        Sender
	Now           func() time.Time
	TransactionID func() (uint32, error)
}

type Identity struct {
	InterfaceName  string
	SourceMAC      net.HardwareAddr
	SourceIP       netip.Addr
	DestinationIP  netip.Addr
	DestinationMAC net.HardwareAddr
	ClientDUID     []byte
	ServerDUID     []byte
}

type ResourceRef struct {
	APIVersion string
	Kind       string
	Name       string
}

type SendInput struct {
	Resource          ResourceRef
	Spec              api.IPv6PrefixDelegationSpec
	Lease             routerstate.PDLease
	Identity          Identity
	Prefix            netip.Prefix
	IAID              uint32
	T1                uint32
	T2                uint32
	PreferredLifetime uint32
	ValidLifetime     uint32
	HopLimit          uint8
}

func (c Controller) SendRequest(ctx context.Context, store routerstate.Store, in SendInput) error {
	return c.send(ctx, store, in, MessageRequest, "RequestSent", "lastRequestAt")
}

func (c Controller) SendSolicit(ctx context.Context, store routerstate.Store, in SendInput) error {
	return c.send(ctx, store, in, MessageSolicit, "SolicitSent", "lastSolicitAt")
}

func (c Controller) SendRenew(ctx context.Context, store routerstate.Store, in SendInput) error {
	return c.send(ctx, store, in, MessageRenew, "RenewSent", "lastRenewAt")
}

func (c Controller) SendRebind(ctx context.Context, store routerstate.Store, in SendInput) error {
	return c.send(ctx, store, in, MessageRebind, "RebindSent", "lastRebindAt")
}

func (c Controller) SendRelease(ctx context.Context, store routerstate.Store, in SendInput) error {
	return c.send(ctx, store, in, MessageRelease, "ReleaseSent", "lastReleaseAt")
}

func (c Controller) send(ctx context.Context, store routerstate.Store, in SendInput, messageType uint8, reason, leaseTimeField string) error {
	if c.Sender == nil {
		return fmt.Errorf("sender is required")
	}
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now().UTC()
	}
	prefix := in.Prefix
	if !prefix.IsValid() {
		var err error
		prefix, err = activePrefixFromInput(in)
		if err != nil && activeMessageRequiresPrefix(messageType) {
			return err
		}
	}
	iaid := in.IAID
	if iaid == 0 {
		iaid = activeIAIDFromInput(in)
	}
	t1 := activeUint32(firstNonEmpty(in.Lease.T1, "7200"), 7200)
	t2 := activeUint32(firstNonEmpty(in.Lease.T2, "12600"), 12600)
	preferredLifetime := activeUint32(firstNonEmpty(in.Lease.PLTime, "14400"), 14400)
	validLifetime := activeUint32(firstNonEmpty(in.Lease.VLTime, "14400"), 14400)
	if in.T1 != 0 {
		t1 = in.T1
	}
	if in.T2 != 0 {
		t2 = in.T2
	}
	if in.PreferredLifetime != 0 {
		preferredLifetime = in.PreferredLifetime
	}
	if in.ValidLifetime != 0 {
		validLifetime = in.ValidLifetime
	}
	reconfigureAccept := messageType == MessageSolicit || messageType == MessageRequest || messageType == MessageRenew || messageType == MessageRebind
	if messageType == MessageRelease {
		t1 = 0
		t2 = 0
		preferredLifetime = 0
		validLifetime = 0
	}
	if messageType == MessageSolicit {
		// Solicit must carry IA_PD with IAID only: T1/T2 zero, no IA Prefix
		// suboption. The reference IX2215 working Solicit observed against
		// this PR-400NE HGW has exactly this shape; routerd previously
		// leaked Renew-style T1/T2 and a stale prefix into the Solicit and
		// got no Advertise.
		prefix = netip.Prefix{}
		t1 = 0
		t2 = 0
		preferredLifetime = 0
		validLifetime = 0
	}
	serverDUID := in.Identity.ServerDUID
	if !activeMessageUsesServerID(messageType) {
		serverDUID = nil
	}
	packet := PacketSpec{
		MessageType:       messageType,
		TransactionID:     0,
		SourceMAC:         in.Identity.SourceMAC,
		DestinationMAC:    in.Identity.DestinationMAC,
		SourceIP:          in.Identity.SourceIP,
		DestinationIP:     in.Identity.DestinationIP,
		ClientDUID:        in.Identity.ClientDUID,
		ServerDUID:        serverDUID,
		IAID:              iaid,
		T1:                t1,
		T2:                t2,
		Prefix:            prefix,
		PreferredLifetime: preferredLifetime,
		ValidLifetime:     validLifetime,
		// ORO is omitted to match the working IX2215 reference packet.
		// LAN DNS is served by routerd's dnsmasq stack, not by relayed
		// HGW DHCPv6 options, so we don't need to ask for option 23.
		ReconfigureAccept: reconfigureAccept,
		HopLimit:          in.HopLimit,
	}
	trid, err := c.transactionID()
	if err != nil {
		return err
	}
	packet.TransactionID = trid
	frame, err := BuildEthernetIPv6UDP(packet)
	if err != nil {
		return err
	}
	payload, err := BuildDHCPv6(packet)
	if err != nil {
		return err
	}
	summary, err := ParseDHCPv6(payload)
	if err != nil {
		return err
	}
	if err := c.Sender.SendFrame(ctx, in.Identity.InterfaceName, frame); err != nil {
		return err
	}
	if store != nil {
		lease := in.Lease
		if prefix.IsValid() {
			lease.PriorPrefix = prefix.String()
			lease.Prefix = prefix.String()
		}
		lease.ServerID = firstNonEmpty(in.Spec.ServerID, lease.ServerID)
		strategy := pdstrategy.EffectiveStrategy(firstNonEmpty(in.Spec.Profile, api.IPv6PDProfileDefault), in.Spec.AcquisitionStrategy)
		action := pdstrategy.ActionRequestClaim
		switch leaseTimeField {
		case "lastSolicitAt":
			lease.LastSolicitAt = now.Format(time.RFC3339)
			action = pdstrategy.ActionSolicit
		case "lastRequestAt":
			lease.LastRequestAt = now.Format(time.RFC3339)
		case "lastRenewAt":
			lease.LastRenewAt = now.Format(time.RFC3339)
			action = pdstrategy.ActionRenew
		case "lastRebindAt":
			lease.LastRebindAt = now.Format(time.RFC3339)
			action = pdstrategy.ActionRebind
		case "lastReleaseAt":
			lease.LastReleaseAt = now.Format(time.RFC3339)
			action = pdstrategy.ActionRelease
		}
		lease = appendTransaction(lease, now, "sent", in.Identity.InterfaceName, summary)
		lease = pdstrategy.RecordAttempt(lease, strategy, action, now)
		base := "ipv6PrefixDelegation." + in.Resource.Name
		store.Set(base+".lease", routerstate.EncodePDLease(lease), reason)
		if recorder, ok := store.(routerstate.EventRecorder); ok {
			_ = recorder.RecordEvent(in.Resource.APIVersion, in.Resource.Kind, in.Resource.Name, "Normal", reason, fmt.Sprintf("sent DHCPv6 %s for %s iaid=%d", messageName(messageType), prefix, iaid))
		}
	}
	return nil
}

func appendTransaction(lease routerstate.PDLease, now time.Time, direction, ifname string, summary MessageSummary) routerstate.PDLease {
	tx := routerstate.PDDHCP6Transaction{
		ObservedAt:        now.UTC().Format(time.RFC3339Nano),
		Direction:         direction,
		Interface:         ifname,
		MessageType:       messageName(summary.MessageType),
		TransactionID:     fmt.Sprintf("%06x", summary.TransactionID),
		ClientDUID:        hex.EncodeToString(summary.ClientDUID),
		ServerDUID:        hex.EncodeToString(summary.ServerDUID),
		ReconfigureAccept: strconv.FormatBool(summary.ReconfigureAccept),
	}
	if len(summary.IAPD) > 0 {
		iapd := summary.IAPD[0]
		tx.IAID = strconv.FormatUint(uint64(iapd.IAID), 10)
		tx.T1 = strconv.FormatUint(uint64(iapd.T1), 10)
		tx.T2 = strconv.FormatUint(uint64(iapd.T2), 10)
		if len(iapd.Prefixes) > 0 {
			prefix := iapd.Prefixes[0]
			tx.Prefix = prefix.Prefix.String()
			tx.PreferredLifetime = strconv.FormatUint(uint64(prefix.PreferredLifetime), 10)
			tx.ValidLifetime = strconv.FormatUint(uint64(prefix.ValidLifetime), 10)
			if prefix.PreferredLifetime == 0 && prefix.ValidLifetime == 0 && summary.MessageType != MessageRelease {
				tx.Warning = "zero IA Prefix lifetimes"
			}
		}
		if iapd.T1 == 0 && iapd.T2 == 0 && summary.MessageType != MessageRelease {
			tx.Warning = firstNonEmpty(tx.Warning, "zero IA_PD T1/T2")
		}
	}
	transactions := append([]routerstate.PDDHCP6Transaction{tx}, lease.Transactions...)
	if len(transactions) > 20 {
		transactions = transactions[:20]
	}
	lease.Transactions = transactions
	return lease
}

func activeMessageUsesServerID(messageType uint8) bool {
	switch messageType {
	case MessageRequest, MessageRenew, MessageRelease:
		return true
	default:
		return false
	}
}

func activeMessageRequiresPrefix(messageType uint8) bool {
	switch messageType {
	case MessageRequest, MessageRenew, MessageRebind, MessageRelease:
		return true
	default:
		return false
	}
}

func activePrefixFromInput(in SendInput) (netip.Prefix, error) {
	for _, value := range []string{in.Spec.PriorPrefix, in.Lease.PriorPrefix, in.Lease.CurrentPrefix, in.Lease.LastPrefix, in.Lease.Prefix} {
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err == nil {
			return prefix, nil
		}
	}
	return netip.Prefix{}, fmt.Errorf("prefix is required for active DHCPv6-PD control")
}

func activeIAIDFromInput(in SendInput) uint32 {
	for _, value := range []string{in.Spec.IAID, in.Lease.IAID} {
		if parsed, ok := parseIAID(value); ok {
			return parsed
		}
	}
	return 0
}

func parseIAID(value string) (uint32, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		parsed, err := strconv.ParseUint(value[2:], 16, 32)
		return uint32(parsed), err == nil
	}
	if len(value) == 8 && isHex(value) {
		parsed, err := strconv.ParseUint(value, 16, 32)
		return uint32(parsed), err == nil
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	return uint32(parsed), err == nil
}

func activeUint32(value string, fallback uint32) uint32 {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return fallback
	}
	return uint32(parsed)
}

func isHex(value string) bool {
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return value != ""
}

func (c Controller) transactionID() (uint32, error) {
	if c.TransactionID != nil {
		return c.TransactionID()
	}
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, fmt.Errorf("generate DHCPv6 transaction ID: %w", err)
	}
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2]), nil
}

func messageName(messageType uint8) string {
	switch messageType {
	case MessageSolicit:
		return "Solicit"
	case MessageRequest:
		return "Request"
	case MessageRenew:
		return "Renew"
	case MessageRebind:
		return "Rebind"
	case MessageRelease:
		return "Release"
	default:
		return fmt.Sprintf("type-%d", messageType)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
