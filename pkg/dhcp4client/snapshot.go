package dhcp4client

import (
	"net/netip"
	"time"
)

type Snapshot struct {
	Resource         string    `json:"resource"`
	Interface        string    `json:"interface"`
	State            State     `json:"state"`
	CurrentAddress   string    `json:"currentAddress,omitempty"`
	DefaultGateway   string    `json:"defaultGateway,omitempty"`
	ServerID         string    `json:"serverID,omitempty"`
	DNSServers       []string  `json:"dnsServers,omitempty"`
	NTPServers       []string  `json:"ntpServers,omitempty"`
	Domain           string    `json:"domain,omitempty"`
	BroadcastAddress string    `json:"broadcastAddress,omitempty"`
	TFTPServer       string    `json:"tftpServer,omitempty"`
	Bootfile         string    `json:"bootfile,omitempty"`
	MTU              int       `json:"mtu,omitempty"`
	LeaseTimeSeconds int64     `json:"leaseTimeSeconds,omitempty"`
	AcquiredAt       time.Time `json:"acquiredAt,omitempty"`
	RenewAt          time.Time `json:"renewAt,omitempty"`
	RebindAt         time.Time `json:"rebindAt,omitempty"`
	ExpiresAt        time.Time `json:"expiresAt,omitempty"`
	UpdatedAt        time.Time `json:"updatedAt"`
	LastError        string    `json:"lastError,omitempty"`
}

func SnapshotFromLease(resource, ifname string, state State, lease Lease, now time.Time) Snapshot {
	s := Snapshot{Resource: resource, Interface: ifname, State: state, UpdatedAt: now}
	if lease.Address.IsValid() {
		s.CurrentAddress = lease.Address.String()
		if lease.ServerID.IsValid() {
			s.ServerID = lease.ServerID.String()
		}
		if lease.DefaultGateway.IsValid() {
			s.DefaultGateway = lease.DefaultGateway.String()
		}
		if lease.BroadcastAddress.IsValid() {
			s.BroadcastAddress = lease.BroadcastAddress.String()
		}
		s.DNSServers = addrStrings(lease.DNSServers)
		s.NTPServers = addrStrings(lease.NTPServers)
		s.Domain = lease.Domain
		s.TFTPServer = lease.TFTPServer
		s.Bootfile = lease.Bootfile
		s.MTU = lease.MTU
		s.LeaseTimeSeconds = int64(lease.LeaseTime / time.Second)
		s.AcquiredAt = lease.AcquiredAt
		s.RenewAt = lease.RenewAt()
		s.RebindAt = lease.RebindAt()
		s.ExpiresAt = lease.ExpiresAt()
	}
	return s
}

func LeaseFromSnapshot(s Snapshot) Lease {
	lease := Lease{
		Address:          parseAddr(s.CurrentAddress),
		ServerID:         parseAddr(s.ServerID),
		DefaultGateway:   parseAddr(s.DefaultGateway),
		BroadcastAddress: parseAddr(s.BroadcastAddress),
		DNSServers:       parseAddrs(s.DNSServers),
		NTPServers:       parseAddrs(s.NTPServers),
		Domain:           s.Domain,
		TFTPServer:       s.TFTPServer,
		Bootfile:         s.Bootfile,
		MTU:              s.MTU,
		LeaseTime:        time.Duration(s.LeaseTimeSeconds) * time.Second,
		AcquiredAt:       s.AcquiredAt,
		RenewedAt:        s.UpdatedAt,
	}
	if !s.RenewAt.IsZero() && !s.AcquiredAt.IsZero() {
		lease.T1 = s.RenewAt.Sub(s.AcquiredAt)
	}
	if !s.RebindAt.IsZero() && !s.AcquiredAt.IsZero() {
		lease.T2 = s.RebindAt.Sub(s.AcquiredAt)
	}
	return lease
}

func addrStrings(addrs []netip.Addr) []string {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if addr.IsValid() {
			out = append(out, addr.String())
		}
	}
	return out
}

func parseAddrs(values []string) []netip.Addr {
	var out []netip.Addr
	for _, value := range values {
		if addr := parseAddr(value); addr.IsValid() {
			out = append(out, addr)
		}
	}
	return out
}

func parseAddr(value string) netip.Addr {
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}
	}
	return addr
}
