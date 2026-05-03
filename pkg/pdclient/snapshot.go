package pdclient

import (
	"context"
	"encoding/hex"
	"net/netip"
	"time"
)

// Store is the process boundary between a standalone PD client service and
// routerd. A SQLite-backed implementation can share routerd's state database;
// tests and one-shot tools can use an in-memory implementation.
type Store interface {
	Load(ctx context.Context, resource string) (Snapshot, bool, error)
	Save(ctx context.Context, snapshot Snapshot) error
}

// Snapshot is intentionally plain and database-friendly. LAN-side controllers
// should consume CurrentPrefix from here instead of knowing anything about
// DHCPv6 transactions, OS clients, or service lifecycles.
type Snapshot struct {
	Resource      string    `json:"resource"`
	Interface     string    `json:"interface"`
	State         State     `json:"state"`
	CurrentPrefix string    `json:"currentPrefix,omitempty"`
	ServerDUID    string    `json:"serverDUID,omitempty"`
	IAID          uint32    `json:"iaid,omitempty"`
	T1Seconds     int64     `json:"t1Seconds,omitempty"`
	T2Seconds     int64     `json:"t2Seconds,omitempty"`
	Preferred     int64     `json:"preferredSeconds,omitempty"`
	Valid         int64     `json:"validSeconds,omitempty"`
	AcquiredAt    time.Time `json:"acquiredAt,omitempty"`
	RenewAt       time.Time `json:"renewAt,omitempty"`
	RebindAt      time.Time `json:"rebindAt,omitempty"`
	ExpiresAt     time.Time `json:"expiresAt,omitempty"`
	AFTRName      string    `json:"aftrName,omitempty"`
	DNSServers    []string  `json:"dnsServers,omitempty"`
	SNTPServers   []string  `json:"sntpServers,omitempty"`
	DomainSearch  []string  `json:"domainSearch,omitempty"`
	InfoUpdatedAt time.Time `json:"infoUpdatedAt,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt"`
	LastError     string    `json:"lastError,omitempty"`
}

func (c *Client) Snapshot() Snapshot {
	s := Snapshot{
		Resource:  c.Config.Resource,
		Interface: c.Config.Interface,
		State:     c.State,
		IAID:      c.Config.IAID,
		UpdatedAt: c.now(),
	}
	if c.Lease.Prefix.IsValid() {
		s.CurrentPrefix = c.Lease.Prefix.String()
		s.ServerDUID = hex.EncodeToString(c.Lease.ServerDUID)
		s.IAID = c.Lease.IAID
		s.T1Seconds = int64(c.Lease.T1 / time.Second)
		s.T2Seconds = int64(c.Lease.T2 / time.Second)
		s.Preferred = int64(c.Lease.Preferred / time.Second)
		s.Valid = int64(c.Lease.Valid / time.Second)
		s.AcquiredAt = c.Lease.AcquiredAt
		s.RenewAt = c.Lease.RenewAt()
		s.RebindAt = c.Lease.RebindAt()
		s.ExpiresAt = c.Lease.ExpiresAt()
	}
	if c.Info.UpdatedAt.IsZero() {
		return s
	}
	s.AFTRName = c.Info.AFTRName
	s.DNSServers = addrStrings(c.Info.DNSServers)
	s.SNTPServers = addrStrings(c.Info.SNTPServers)
	s.DomainSearch = append([]string(nil), c.Info.DomainSearch...)
	s.InfoUpdatedAt = c.Info.UpdatedAt
	return s
}

func (c *Client) Restore(snapshot Snapshot) {
	c.State = snapshot.State
	if c.State == "" {
		c.State = StateIdle
	}
	if snapshot.CurrentPrefix == "" {
		c.Lease = Lease{}
		c.restoreInformation(snapshot)
		return
	}
	prefix, err := netip.ParsePrefix(snapshot.CurrentPrefix)
	if err != nil {
		c.State = StateExpired
		c.Lease = Lease{}
		c.restoreInformation(snapshot)
		return
	}
	serverDUID, _ := hex.DecodeString(snapshot.ServerDUID)
	c.Lease = Lease{
		Prefix:     prefix,
		ServerDUID: serverDUID,
		IAID:       snapshot.IAID,
		T1:         time.Duration(snapshot.T1Seconds) * time.Second,
		T2:         time.Duration(snapshot.T2Seconds) * time.Second,
		Preferred:  time.Duration(snapshot.Preferred) * time.Second,
		Valid:      time.Duration(snapshot.Valid) * time.Second,
		AcquiredAt: snapshot.AcquiredAt,
		RenewedAt:  snapshot.UpdatedAt,
	}
	c.restoreInformation(snapshot)
}

func (c *Client) restoreInformation(snapshot Snapshot) {
	c.Info = Information{
		AFTRName:     snapshot.AFTRName,
		DNSServers:   parseAddrStrings(snapshot.DNSServers),
		SNTPServers:  parseAddrStrings(snapshot.SNTPServers),
		DomainSearch: append([]string(nil), snapshot.DomainSearch...),
		UpdatedAt:    snapshot.InfoUpdatedAt,
	}
}

func addrStrings(addrs []netip.Addr) []string {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.String())
	}
	return out
}

func parseAddrStrings(values []string) []netip.Addr {
	if len(values) == 0 {
		return nil
	}
	out := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		addr, err := netip.ParseAddr(value)
		if err == nil {
			out = append(out, addr)
		}
	}
	return out
}
