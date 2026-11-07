// SPDX-License-Identifier: BSD-3-Clause

package pdclient

import (
	"bytes"
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

type PreviousPrefixSnapshot struct {
	Prefix    string    `json:"prefix"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// PrefixSnapshot is the recovery record for one independently timed lease.
type PrefixSnapshot struct {
	Prefix     string    `json:"prefix"`
	Preferred  int64     `json:"preferredSeconds"`
	Valid      int64     `json:"validSeconds"`
	AcquiredAt time.Time `json:"acquiredAt"`
}

type AddressSnapshot struct {
	Address    string    `json:"address"`
	Preferred  int64     `json:"preferredSeconds"`
	Valid      int64     `json:"validSeconds"`
	AcquiredAt time.Time `json:"acquiredAt"`
}

// Snapshot retains the complete association for recovery. CurrentPrefix and
// PreviousPrefixes are the LAN controller's primary and renumbering projection;
// they are derived output, never an alternate source for lease restoration.
type Snapshot struct {
	Resource         string                   `json:"resource"`
	Interface        string                   `json:"interface"`
	State            State                    `json:"state"`
	Mode             RequestMode              `json:"requestMode"`
	Addresses        []AddressSnapshot        `json:"addresses,omitempty"`
	AddressUpdatedAt time.Time                `json:"addressUpdatedAt,omitempty"`
	AddressT1Seconds int64                    `json:"addressT1Seconds,omitempty"`
	AddressT2Seconds int64                    `json:"addressT2Seconds,omitempty"`
	Prefixes         []PrefixSnapshot         `json:"prefixes,omitempty"`
	LeaseUpdatedAt   time.Time                `json:"leaseUpdatedAt,omitempty"`
	CurrentPrefix    string                   `json:"currentPrefix,omitempty"`
	PreviousPrefixes []PreviousPrefixSnapshot `json:"previousPrefixes,omitempty"`
	ServerDUID       string                   `json:"serverDUID,omitempty"`
	IAID             uint32                   `json:"iaid,omitempty"`
	ClientDUID       string                   `json:"clientDuid,omitempty"`
	T1Seconds        int64                    `json:"t1Seconds,omitempty"`
	T2Seconds        int64                    `json:"t2Seconds,omitempty"`
	Preferred        int64                    `json:"preferredSeconds,omitempty"`
	Valid            int64                    `json:"validSeconds,omitempty"`
	AcquiredAt       time.Time                `json:"acquiredAt,omitempty"`
	RenewAt          time.Time                `json:"renewAt,omitempty"`
	RebindAt         time.Time                `json:"rebindAt,omitempty"`
	ExpiresAt        time.Time                `json:"expiresAt,omitempty"`
	AFTRName         string                   `json:"aftrName,omitempty"`
	DNSServers       []string                 `json:"dnsServers,omitempty"`
	SNTPServers      []string                 `json:"sntpServers,omitempty"`
	DomainSearch     []string                 `json:"domainSearch,omitempty"`
	InfoUpdatedAt    time.Time                `json:"infoUpdatedAt,omitempty"`
	InfoRefreshAt    time.Time                `json:"infoRefreshAt,omitempty"`
	InfoLastSentAt   time.Time                `json:"infoLastSentAt,omitempty"`
	InfoPending      bool                     `json:"infoPending,omitempty"`
	InfoStatusCode   uint16                   `json:"infoStatusCode,omitempty"`
	InfoLastError    string                   `json:"infoLastError,omitempty"`
	SOLMaxRTSeconds  int64                    `json:"solMaxRTSeconds,omitempty"`
	INFMaxRTSeconds  int64                    `json:"infMaxRTSeconds,omitempty"`
	UpdatedAt        time.Time                `json:"updatedAt"`
	LastError        string                   `json:"lastError,omitempty"`
	LastSentAt       time.Time                `json:"lastSentAt,omitempty"`
	LastMessageType  uint8                    `json:"lastMessageType,omitempty"`
}

func (c *Client) Snapshot() Snapshot {
	now := c.now()
	s := Snapshot{Resource: c.Config.Resource, Interface: c.Config.Interface, State: c.State, Mode: c.Config.Mode, IAID: c.Config.IAID,
		ClientDUID: hex.EncodeToString(c.Config.ClientDUID), UpdatedAt: now, LastError: c.lastError, LastSentAt: c.lastSentAt, LastMessageType: c.lastMessageType,
		InfoPending: c.infoExchange.active(), InfoStatusCode: c.Info.StatusCode, InfoLastError: c.Info.LastError,
		SOLMaxRTSeconds: int64(c.solicitMaxRT / time.Second), INFMaxRTSeconds: int64(c.infoMaxRT / time.Second)}
	current := currentPrefixAt(c.Lease.Prefixes, c.Lease.CurrentPrefix, now)
	for _, a := range c.Lease.Addresses {
		if now.Before(a.ExpiresAt()) {
			s.Addresses = append(s.Addresses, AddressSnapshot{Address: a.Address.String(), Preferred: int64(a.Preferred / time.Second), Valid: int64(a.Valid / time.Second), AcquiredAt: a.AcquiredAt})
		}
	}
	if len(s.Addresses) > 0 {
		s.AddressUpdatedAt = c.Lease.AddressAcquiredAt
		s.AddressT1Seconds = int64(c.Lease.AddressT1 / time.Second)
		s.AddressT2Seconds = int64(c.Lease.AddressT2 / time.Second)
	}
	if !current.IsValid() && len(s.Addresses) == 0 && snapshotCarriesUsableLease(s.State) {
		s.State = StateExpired
	}
	if (current.IsValid() || len(s.Addresses) > 0) && s.State == StateRequesting {
		// NoBinding on one association does not withdraw a valid sibling.
		s.State = StateBound
	}
	for _, p := range c.Lease.Prefixes {
		if !now.Before(p.ExpiresAt()) {
			continue
		}
		s.Prefixes = append(s.Prefixes, PrefixSnapshot{Prefix: p.Prefix.String(), Preferred: int64(p.Preferred / time.Second), Valid: int64(p.Valid / time.Second), AcquiredAt: p.AcquiredAt})
		if p.Prefix == current {
			s.CurrentPrefix = p.Prefix.String()
			s.Preferred = int64(p.Preferred / time.Second)
			s.Valid = int64(p.Valid / time.Second)
			s.AcquiredAt = p.AcquiredAt
			s.ExpiresAt = p.ExpiresAt()
		} else {
			s.PreviousPrefixes = append(s.PreviousPrefixes, PreviousPrefixSnapshot{Prefix: p.Prefix.String(), ExpiresAt: p.ExpiresAt()})
		}
	}
	if len(s.Prefixes) > 0 {
		s.LeaseUpdatedAt = c.Lease.AcquiredAt
		s.T1Seconds = int64(c.Lease.T1 / time.Second)
		s.T2Seconds = int64(c.Lease.T2 / time.Second)
	}
	if len(s.Prefixes) > 0 || len(s.Addresses) > 0 {
		s.ServerDUID = hex.EncodeToString(c.Lease.ServerDUID)
		s.RenewAt = c.Lease.RenewAt()
		s.RebindAt = c.Lease.RebindAt()
	}
	s.AFTRName = c.Info.AFTRName
	s.DNSServers = addrStrings(c.Info.DNSServers)
	s.SNTPServers = addrStrings(c.Info.SNTPServers)
	s.DomainSearch = append([]string(nil), c.Info.DomainSearch...)
	s.InfoLastSentAt = c.Info.LastSentAt
	s.InfoUpdatedAt = c.Info.UpdatedAt
	s.InfoRefreshAt = c.Info.RefreshAt
	return s
}

func (c *Client) Restore(snapshot Snapshot) {
	c.State = StateIdle
	c.Lease = Lease{}
	c.leaseExchange = exchange{}
	c.infoExchange = exchange{}
	if !snapshotMatchesConfig(snapshot, c.Config) {
		return
	}
	c.restoreInformation(snapshot)
	c.lastError = snapshot.LastError
	c.lastSentAt, c.lastMessageType = snapshot.LastSentAt, snapshot.LastMessageType
	now := c.now()
	if !snapshotCarriesUsableLease(snapshot.State) {
		return
	}
	serverDUID, err := hex.DecodeString(snapshot.ServerDUID)
	if err != nil || len(serverDUID) == 0 || len(serverDUID) > 130 {
		return
	}
	lease := Lease{ServerDUID: serverDUID, IAID: snapshot.IAID}
	if c.wantsAddresses() && validSnapshotTimers(snapshot.AddressUpdatedAt, snapshot.AddressT1Seconds, snapshot.AddressT2Seconds, now) {
		lease.AddressAcquiredAt = snapshot.AddressUpdatedAt
		lease.AddressT1, lease.AddressT2 = seconds(uint32(snapshot.AddressT1Seconds)), seconds(uint32(snapshot.AddressT2Seconds))
		seen := make(map[netip.Addr]bool, len(snapshot.Addresses))
		for _, entry := range snapshot.Addresses {
			addr, err := netip.ParseAddr(entry.Address)
			if err != nil || !usableAddress(IAAddress{Address: addr}) || entry.Preferred < 0 || entry.Preferred > entry.Valid || entry.Valid > 0xffffffff || entry.AcquiredAt.IsZero() || entry.AcquiredAt.After(now) || seen[addr] || len(lease.Addresses) >= maxAssignedAddresses {
				continue
			}
			a := AddressLease{Address: addr, Preferred: seconds(uint32(entry.Preferred)), Valid: seconds(uint32(entry.Valid)), AcquiredAt: entry.AcquiredAt}
			if now.Before(a.ExpiresAt()) {
				seen[addr] = true
				lease.Addresses = append(lease.Addresses, a)
			}
		}
	}
	if c.wantsPrefixes() && validSnapshotTimers(snapshot.LeaseUpdatedAt, snapshot.T1Seconds, snapshot.T2Seconds, now) {
		lease.T1, lease.T2, lease.AcquiredAt = seconds(uint32(snapshot.T1Seconds)), seconds(uint32(snapshot.T2Seconds)), snapshot.LeaseUpdatedAt
		seen := make(map[netip.Prefix]bool, len(snapshot.Prefixes))
		for _, entry := range snapshot.Prefixes {
			prefix, err := netip.ParsePrefix(entry.Prefix)
			if err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() || entry.Preferred < 0 || entry.Preferred > entry.Valid || entry.Valid > 0xffffffff || entry.AcquiredAt.IsZero() || entry.AcquiredAt.After(now) {
				continue
			}
			prefix = prefix.Masked()
			if seen[prefix] || len(lease.Prefixes) >= maxDelegatedPrefixes {
				continue
			}
			p := PrefixLease{Prefix: prefix, Preferred: seconds(uint32(entry.Preferred)), Valid: seconds(uint32(entry.Valid)), AcquiredAt: entry.AcquiredAt}
			if !now.Before(p.ExpiresAt()) {
				continue
			}
			seen[prefix] = true
			lease.Prefixes = append(lease.Prefixes, p)
		}
	}
	if len(lease.Prefixes) == 0 && len(lease.Addresses) == 0 {
		return
	}
	lease.CurrentPrefix, _ = netip.ParsePrefix(snapshot.CurrentPrefix)
	c.Lease = lease
	c.selectCurrent(now)
	c.State = snapshot.State
}

func validSnapshotTimers(updated time.Time, t1, t2 int64, now time.Time) bool {
	return !updated.IsZero() && !updated.After(now) && t1 > 0 && t2 > 0 && t1 <= t2 && t2 <= 0xffffffff
}

func snapshotCarriesUsableLease(state State) bool {
	return state == StateBound || state == StateRenewing || state == StateRebinding
}

func snapshotMatchesConfig(snapshot Snapshot, config Config) bool {
	if snapshot.Resource != config.Resource || snapshot.Interface != config.Interface || snapshot.IAID != config.IAID || snapshot.Mode != config.Mode {
		return false
	}
	duid, err := hex.DecodeString(snapshot.ClientDUID)
	return err == nil && len(duid) > 0 && bytes.Equal(duid, config.ClientDUID)
}

func (c *Client) restoreInformation(snapshot Snapshot) {
	c.Info = Information{AFTRName: snapshot.AFTRName, DNSServers: parseAddrStrings(snapshot.DNSServers), SNTPServers: parseAddrStrings(snapshot.SNTPServers), DomainSearch: append([]string(nil), snapshot.DomainSearch...), UpdatedAt: snapshot.InfoUpdatedAt, LastSentAt: snapshot.InfoLastSentAt, RefreshAt: snapshot.InfoRefreshAt, StatusCode: snapshot.InfoStatusCode, LastError: snapshot.InfoLastError}
	// Recover an interrupted command as a fresh exchange, without persisting or
	// replaying the old transaction identifier after a process restart.
	if snapshot.InfoPending {
		c.Info.RefreshAt = c.now()
	}
	if snapshot.SOLMaxRTSeconds >= 60 && snapshot.SOLMaxRTSeconds <= 86400 {
		c.solicitMaxRT = time.Duration(snapshot.SOLMaxRTSeconds) * time.Second
	}
	if snapshot.INFMaxRTSeconds >= 60 && snapshot.INFMaxRTSeconds <= 86400 {
		c.infoMaxRT = time.Duration(snapshot.INFMaxRTSeconds) * time.Second
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
