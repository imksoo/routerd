package dhcp6control

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
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
	Resource ResourceRef
	Spec     api.IPv6PrefixDelegationSpec
	Lease    routerstate.PDLease
	Identity Identity
	Prefix   netip.Prefix
	IAID     uint32
}

func (c Controller) SendRequest(ctx context.Context, store routerstate.Store, in SendInput) error {
	return c.send(ctx, store, in, MessageRequest, "RequestSent", "lastRequestAt")
}

func (c Controller) SendRenew(ctx context.Context, store routerstate.Store, in SendInput) error {
	return c.send(ctx, store, in, MessageRenew, "RenewSent", "lastRenewAt")
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
		if err != nil {
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
	reconfigureAccept := messageType == MessageRequest || messageType == MessageRenew
	if messageType == MessageRelease {
		t1 = 0
		t2 = 0
		preferredLifetime = 0
		validLifetime = 0
	}
	packet := PacketSpec{
		MessageType:       messageType,
		TransactionID:     0,
		SourceMAC:         in.Identity.SourceMAC,
		DestinationMAC:    in.Identity.DestinationMAC,
		SourceIP:          in.Identity.SourceIP,
		DestinationIP:     in.Identity.DestinationIP,
		ClientDUID:        in.Identity.ClientDUID,
		ServerDUID:        in.Identity.ServerDUID,
		IAID:              iaid,
		T1:                t1,
		T2:                t2,
		Prefix:            prefix,
		PreferredLifetime: preferredLifetime,
		ValidLifetime:     validLifetime,
		ORO:               []uint16{23},
		ReconfigureAccept: reconfigureAccept,
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
	if err := c.Sender.SendFrame(ctx, in.Identity.InterfaceName, frame); err != nil {
		return err
	}
	if store != nil {
		lease := in.Lease
		lease.PriorPrefix = prefix.String()
		lease.Prefix = prefix.String()
		lease.ServerID = firstNonEmpty(in.Spec.ServerID, lease.ServerID)
		switch leaseTimeField {
		case "lastRequestAt":
			lease.LastRequestAt = now.Format(time.RFC3339)
		case "lastRenewAt":
			lease.LastRenewAt = now.Format(time.RFC3339)
		case "lastReleaseAt":
			lease.LastReleaseAt = now.Format(time.RFC3339)
		}
		base := "ipv6PrefixDelegation." + in.Resource.Name
		store.Set(base+".lease", routerstate.EncodePDLease(lease), reason)
		if recorder, ok := store.(routerstate.EventRecorder); ok {
			_ = recorder.RecordEvent(in.Resource.APIVersion, in.Resource.Kind, in.Resource.Name, "Normal", reason, fmt.Sprintf("sent DHCPv6 %s for %s iaid=%d", messageName(messageType), prefix, iaid))
		}
	}
	return nil
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
	case MessageRequest:
		return "Request"
	case MessageRenew:
		return "Renew"
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
