// SPDX-License-Identifier: BSD-3-Clause

package daemonapi

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

// DHCPLeaseUpdate is a named server's lease observation at the DNS IPC boundary.
// A zero deadline is meaningful only with an explicit Infinite lease.
type DHCPLeaseUpdate struct {
	Action     string    `json:"action"`
	Source     string    `json:"source"`
	MAC        string    `json:"mac,omitempty"`
	IP         string    `json:"ip"`
	Hostname   string    `json:"hostname,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
	ExpiresAt  time.Time `json:"expiresAt,omitempty"`
	Infinite   bool      `json:"infinite,omitempty"`
}

func (lease DHCPLeaseUpdate) Validate(now time.Time) error {
	ip, err := netip.ParseAddr(lease.IP)
	kind, name, ok := strings.Cut(lease.Source, "/")
	if err != nil || !ok || name == "" || strings.Contains(name, "/") ||
		(ip.Is4() && kind != "DHCPv4Server") || (ip.Is6() && kind != "DHCPv6Server") {
		return fmt.Errorf("DHCP lease requires a matching named server and IP address")
	}
	if _, valid := DHCPLeaseEventType(lease.Action); !valid || lease.ObservedAt.IsZero() || lease.ObservedAt.After(now) || lease.MAC == "" {
		return fmt.Errorf("DHCP lease requires a current observation and client identity")
	}
	if lease.Infinite && !lease.ExpiresAt.IsZero() {
		return fmt.Errorf("DHCP infinite lease cannot carry a finite deadline")
	}
	if lease.Action != DHCPLeaseActionRemoved && !lease.Infinite && lease.ExpiresAt.IsZero() {
		return fmt.Errorf("DHCP lease has no authoritative deadline")
	}
	return nil
}

// DHCPLeaseFromEvent decodes the existing journal's resource/attribute boundary.
// The event journal already persists these fields, so replay retains provenance.
func DHCPLeaseFromEvent(event DaemonEvent) (DHCPLeaseUpdate, error) {
	lease := DHCPLeaseUpdate{MAC: event.Attributes["mac"], IP: event.Attributes["ip"], Hostname: event.Attributes["hostname"]}
	var ok bool
	lease.Action, ok = DHCPLeaseActionFromEventType(event.Type)
	if !ok || event.Resource == nil || event.Resource.APIVersion != api.NetAPIVersion {
		return lease, fmt.Errorf("DHCP event has no named lease source")
	}
	lease.Source = event.Resource.Kind + "/" + event.Resource.Name
	var err error
	lease.ObservedAt, err = time.Parse(time.RFC3339Nano, event.Attributes["observedAt"])
	if err != nil {
		return lease, fmt.Errorf("DHCP event observation time: %w", err)
	}
	if value := event.Attributes["expiresAt"]; value != "" {
		lease.ExpiresAt, err = time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return lease, fmt.Errorf("DHCP event expiry: %w", err)
		}
	}
	if value := event.Attributes["infinite"]; value != "" {
		lease.Infinite, err = strconv.ParseBool(value)
		if err != nil {
			return lease, fmt.Errorf("DHCP event infinite flag: %w", err)
		}
	}
	return lease, nil
}
