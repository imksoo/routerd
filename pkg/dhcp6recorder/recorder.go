package dhcp6recorder

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/dhcp6control"
	"routerd/pkg/pdstrategy"
	routerstate "routerd/pkg/state"
)

const (
	etherTypeIPv6   uint16 = 0x86dd
	ipProtocolUDP   uint8  = 17
	dhcp6ClientPort uint16 = 546
	dhcp6ServerPort uint16 = 547
)

type FrameSource interface {
	ReadFrame(ctx context.Context) ([]byte, error)
	Close() error
}

type Observation struct {
	ObservedAt      time.Time
	Interface       string
	Direction       string
	SourceMAC       string
	DestinationMAC  string
	SourceIP        string
	DestinationIP   string
	SourcePort      uint16
	DestinationPort uint16
	Summary         dhcp6control.MessageSummary
}

func Run(ctx context.Context, source FrameSource, handle func(Observation)) error {
	defer source.Close()
	for {
		frame, err := source.ReadFrame(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		obs, ok, err := ParseFrame(frame)
		if err != nil || !ok {
			continue
		}
		obs.ObservedAt = time.Now().UTC()
		handle(obs)
	}
}

func ParseFrame(frame []byte) (Observation, bool, error) {
	if len(frame) < 14+40+8 {
		return Observation{}, false, nil
	}
	if binary.BigEndian.Uint16(frame[12:14]) != etherTypeIPv6 {
		return Observation{}, false, nil
	}
	ip := frame[14 : 14+40]
	if ip[0]>>4 != 6 || ip[6] != ipProtocolUDP {
		return Observation{}, false, nil
	}
	payloadLen := int(binary.BigEndian.Uint16(ip[4:6]))
	udpStart := 14 + 40
	if payloadLen < 8 || len(frame) < udpStart+payloadLen {
		return Observation{}, false, nil
	}
	udp := frame[udpStart : udpStart+8]
	srcPort := binary.BigEndian.Uint16(udp[0:2])
	dstPort := binary.BigEndian.Uint16(udp[2:4])
	if !isDHCP6Port(srcPort) && !isDHCP6Port(dstPort) {
		return Observation{}, false, nil
	}
	payload := frame[udpStart+8 : udpStart+payloadLen]
	summary, err := dhcp6control.ParseDHCPv6(payload)
	if err != nil {
		return Observation{}, false, err
	}
	srcIP, ok := netip.AddrFromSlice(ip[8:24])
	if !ok {
		return Observation{}, false, nil
	}
	dstIP, ok := netip.AddrFromSlice(ip[24:40])
	if !ok {
		return Observation{}, false, nil
	}
	direction := "observed"
	if dstPort == dhcp6ClientPort {
		direction = "received"
	} else if dstPort == dhcp6ServerPort {
		direction = "sent"
	}
	return Observation{
		Direction:       direction,
		SourceMAC:       net.HardwareAddr(frame[6:12]).String(),
		DestinationMAC:  net.HardwareAddr(frame[0:6]).String(),
		SourceIP:        srcIP.String(),
		DestinationIP:   dstIP.String(),
		SourcePort:      srcPort,
		DestinationPort: dstPort,
		Summary:         summary,
	}, true, nil
}

func ApplyObservation(store routerstate.Store, resourceName string, obs Observation) error {
	if store == nil {
		return fmt.Errorf("state store is required")
	}
	if resourceName == "" {
		return fmt.Errorf("resource name is required")
	}
	now := obs.ObservedAt.UTC()
	if now.IsZero() {
		now = store.Now().UTC()
	}
	lease, _ := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation."+resourceName)
	lease = applySummaryToLease(lease, now, obs)
	store.Set("ipv6PrefixDelegation."+resourceName+".lease", routerstate.EncodePDLease(lease), "DHCP6TransactionObserved")
	if recorder, ok := store.(routerstate.EventRecorder); ok {
		message := fmt.Sprintf("observed DHCPv6 %s %s xid=%06x", obs.Direction, messageName(obs.Summary.MessageType), obs.Summary.TransactionID)
		if prefix := firstPrefix(obs.Summary); prefix != "" {
			message += " prefix=" + prefix
		}
		_ = recorder.RecordEvent(api.NetAPIVersion, "IPv6PrefixDelegation", resourceName, "Normal", "DHCP6TransactionObserved", message)
	}
	return nil
}

func applySummaryToLease(lease routerstate.PDLease, now time.Time, obs Observation) routerstate.PDLease {
	tx := transactionFromObservation(now, obs)
	transactions := append([]routerstate.PDDHCP6Transaction{tx}, lease.Transactions...)
	if len(transactions) > 20 {
		transactions = transactions[:20]
	}
	lease.Transactions = transactions
	if len(obs.Summary.ClientDUID) > 0 {
		lease.DUID = hex.EncodeToString(obs.Summary.ClientDUID)
	}
	if len(obs.Summary.ServerDUID) > 0 {
		lease.ServerID = hex.EncodeToString(obs.Summary.ServerDUID)
	}
	if obs.Direction == "received" && obs.Summary.MessageType == dhcp6control.MessageReply {
		lease.LastReplyAt = now.Format(time.RFC3339Nano)
		lease.LastObservedAt = now.Format(time.RFC3339Nano)
		lease.SourceLL = obs.SourceIP
		lease.SourceMAC = obs.SourceMAC
		if len(obs.Summary.IAPD) > 0 {
			iapd := obs.Summary.IAPD[0]
			lease.IAID = strconv.FormatUint(uint64(iapd.IAID), 10)
			lease.T1 = strconv.FormatUint(uint64(iapd.T1), 10)
			lease.T2 = strconv.FormatUint(uint64(iapd.T2), 10)
			if len(iapd.Prefixes) > 0 {
				prefix := iapd.Prefixes[0]
				lease.CurrentPrefix = prefix.Prefix.String()
				lease.LastPrefix = prefix.Prefix.String()
				lease.Prefix = prefix.Prefix.String()
				lease.PriorPrefix = prefix.Prefix.String()
				lease.PLTime = strconv.FormatUint(uint64(prefix.PreferredLifetime), 10)
				lease.VLTime = strconv.FormatUint(uint64(prefix.ValidLifetime), 10)
			}
		}
		strategy := ""
		if lease.Acquisition != nil {
			strategy = lease.Acquisition.Strategy
		}
		lease = pdstrategy.RecordReply(lease, strategy)
	}
	return lease
}

func transactionFromObservation(now time.Time, obs Observation) routerstate.PDDHCP6Transaction {
	tx := routerstate.PDDHCP6Transaction{
		ObservedAt:        now.UTC().Format(time.RFC3339Nano),
		Direction:         obs.Direction,
		Interface:         obs.Interface,
		MessageType:       messageName(obs.Summary.MessageType),
		TransactionID:     fmt.Sprintf("%06x", obs.Summary.TransactionID),
		ClientDUID:        hex.EncodeToString(obs.Summary.ClientDUID),
		ServerDUID:        hex.EncodeToString(obs.Summary.ServerDUID),
		ReconfigureAccept: strconv.FormatBool(obs.Summary.ReconfigureAccept),
	}
	if len(obs.Summary.IAPD) > 0 {
		iapd := obs.Summary.IAPD[0]
		tx.IAID = strconv.FormatUint(uint64(iapd.IAID), 10)
		tx.T1 = strconv.FormatUint(uint64(iapd.T1), 10)
		tx.T2 = strconv.FormatUint(uint64(iapd.T2), 10)
		if iapd.T1 == 0 && iapd.T2 == 0 && obs.Summary.MessageType != dhcp6control.MessageRelease {
			tx.Warning = "zero IA_PD T1/T2"
		}
		if len(iapd.Prefixes) > 0 {
			prefix := iapd.Prefixes[0]
			tx.Prefix = prefix.Prefix.String()
			tx.PreferredLifetime = strconv.FormatUint(uint64(prefix.PreferredLifetime), 10)
			tx.ValidLifetime = strconv.FormatUint(uint64(prefix.ValidLifetime), 10)
			if prefix.PreferredLifetime == 0 && prefix.ValidLifetime == 0 && obs.Summary.MessageType != dhcp6control.MessageRelease {
				tx.Warning = firstNonEmpty(tx.Warning, "zero IA Prefix lifetimes")
			}
		}
	}
	return tx
}

func isDHCP6Port(port uint16) bool {
	return port == dhcp6ClientPort || port == dhcp6ServerPort
}

func firstPrefix(summary dhcp6control.MessageSummary) string {
	if len(summary.IAPD) == 0 || len(summary.IAPD[0].Prefixes) == 0 {
		return ""
	}
	return summary.IAPD[0].Prefixes[0].Prefix.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func messageName(messageType uint8) string {
	switch messageType {
	case dhcp6control.MessageSolicit:
		return "Solicit"
	case dhcp6control.MessageAdvertise:
		return "Advertise"
	case dhcp6control.MessageRequest:
		return "Request"
	case dhcp6control.MessageConfirm:
		return "Confirm"
	case dhcp6control.MessageRenew:
		return "Renew"
	case dhcp6control.MessageRebind:
		return "Rebind"
	case dhcp6control.MessageReply:
		return "Reply"
	case dhcp6control.MessageRelease:
		return "Release"
	case dhcp6control.MessageInformationRequest:
		return "InformationRequest"
	default:
		return fmt.Sprintf("type-%d", messageType)
	}
}
