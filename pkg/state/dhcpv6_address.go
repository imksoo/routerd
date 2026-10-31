// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/pdclient"
)

// DHCPv6AddressSource identifies one association, independently of the helper's
// current supervisor token. Restarting the helper does not extend lease time.
type DHCPv6AddressSource struct {
	Worker     string               `json:"worker"`
	IfName     string               `json:"ifname"`
	ClientDUID string               `json:"clientDUID"`
	IAID       uint32               `json:"iaid"`
	Mode       pdclient.RequestMode `json:"mode"`
}

type DHCPv6AddressIntent struct {
	Operation      string    `json:"operation"`
	PreferredUntil time.Time `json:"preferredUntil"`
	ValidUntil     time.Time `json:"validUntil"`
	AcquiredAt     time.Time `json:"acquiredAt"`
}

type DHCPv6OwnedAddress struct {
	Address        string               `json:"address"`
	Held           bool                 `json:"held"`
	PreferredUntil time.Time            `json:"preferredUntil"`
	ValidUntil     time.Time            `json:"validUntil"`
	AcquiredAt     time.Time            `json:"acquiredAt"`
	Pending        *DHCPv6AddressIntent `json:"pending,omitempty"`
}

type DHCPv6AddressReceipt struct {
	Source      DHCPv6AddressSource  `json:"source"`
	IfName      string               `json:"ifname"`
	IfIndex     int                  `json:"ifIndex"`
	Scope       string               `json:"scope"`
	ParentToken string               `json:"parentToken"`
	Addresses   []DHCPv6OwnedAddress `json:"addresses"`
}

// Leases are upstream authority. Native is the exact applied/pending cleanup
// journal; lease withdrawal must never erase it before native cleanup succeeds.
type DHCPv6AddressDecline struct {
	Address    string    `json:"address"`
	AcquiredAt time.Time `json:"acquiredAt"`
	ServerDUID string    `json:"serverDUID"`
}

type DHCPv6AddressState struct {
	PendingDeclines []DHCPv6AddressDecline     `json:"pendingDeclines,omitempty"`
	Phase           string                     `json:"phase"`
	Reason          string                     `json:"reason"`
	Message         string                     `json:"message"`
	Source          DHCPv6AddressSource        `json:"source"`
	SnapshotAt      time.Time                  `json:"snapshotAt"`
	ServerDUID      string                     `json:"serverDUID"`
	Leases          []pdclient.AddressSnapshot `json:"leases"`
	Native          *DHCPv6AddressReceipt      `json:"native,omitempty"`
}

// Revision fences concurrent writes. Owner also fences deletion/recreation,
// where a new row can have the same numeric revision as a retired one.
type DHCPv6AddressRecord struct {
	Revision int64
	Owner    string
	State    DHCPv6AddressState
}

var ErrDHCPv6AddressRecordChanged = errors.New("DHCPv6 address ownership record changed")

type dhcpv6AddressDocument struct {
	Owner     string             `json:"nativeOwner"`
	State     DHCPv6AddressState `json:"nativeState"`
	Phase     string             `json:"phase"`
	Reason    string             `json:"reason"`
	Message   string             `json:"message"`
	IfName    string             `json:"ifname"`
	Address   string             `json:"address"`
	Addresses []string           `json:"addresses"`
}

func (s *SQLiteStore) ReadDHCPv6AddressRecord(name string) (DHCPv6AddressRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, _, err := s.readDHCPv6AddressRecordLocked(name)
	return record, err
}

func (s *SQLiteStore) readDHCPv6AddressRecordLocked(name string) (DHCPv6AddressRecord, string, error) {
	var record DHCPv6AddressRecord
	if s.closed {
		return record, "", sql.ErrConnDone
	}
	var raw string
	err := s.db.QueryRow(`SELECT resource_version,status FROM objects WHERE api_version=? AND kind='DHCPv6Address' AND name=?`, api.NetAPIVersion, name).Scan(&record.Revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return record, "", nil
	}
	if err != nil {
		return record, "", err
	}
	var document struct {
		Owner string          `json:"nativeOwner"`
		State json.RawMessage `json:"nativeState"`
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return record, raw, err
	}
	record.Owner = document.Owner
	if len(document.State) != 0 {
		if record.Owner == "" || bytes.Equal(bytes.TrimSpace(document.State), []byte("null")) {
			return record, raw, fmt.Errorf("invalid DHCPv6 address native state")
		}
		decoder := json.NewDecoder(bytes.NewReader(document.State))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record.State); err != nil {
			return record, raw, err
		}
		if err := validateDHCPv6AddressState(record.State); err != nil {
			return record, raw, err
		}
	}
	return record, raw, nil
}

func (s *SQLiteStore) SaveDHCPv6AddressRecord(name string, expected DHCPv6AddressRecord, next DHCPv6AddressState) (DHCPv6AddressRecord, error) {
	if err := validateDHCPv6AddressState(next); err != nil {
		return DHCPv6AddressRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, raw, err := s.readDHCPv6AddressRecordLocked(name)
	if err != nil {
		return DHCPv6AddressRecord{}, err
	}
	if current.Revision != expected.Revision || current.Owner != expected.Owner {
		return DHCPv6AddressRecord{}, ErrDHCPv6AddressRecordChanged
	}
	owner := current.Owner
	if owner == "" {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return DHCPv6AddressRecord{}, err
		}
		owner = hex.EncodeToString(token[:])
	}
	document := dhcpv6AddressDocument{Owner: owner, State: next, Phase: next.Phase, Reason: next.Reason, Message: next.Message, IfName: next.Source.IfName, Addresses: []string{}}
	if next.Phase == "Applied" && next.Native != nil {
		for _, address := range next.Native.Addresses {
			if address.Held && address.Pending == nil {
				document.Addresses = append(document.Addresses, address.Address)
			}
		}
		if len(document.Addresses) > 0 {
			document.Address = document.Addresses[0]
		}
	}
	data, err := json.Marshal(document)
	if err != nil {
		return DHCPv6AddressRecord{}, err
	}
	if string(data) == raw {
		return current, nil
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.Exec(`INSERT INTO objects(api_version,kind,name,uid,resource_version,observed_generation,status,created_at,modified_at)
VALUES(?,'DHCPv6Address',?,?,1,?,?,?,?)
ON CONFLICT(api_version,kind,name) DO UPDATE SET resource_version=resource_version+1,observed_generation=excluded.observed_generation,status=excluded.status,modified_at=excluded.modified_at
WHERE objects.resource_version=? AND coalesce(json_extract(objects.status,'$.nativeOwner'),'')=? AND ?>0`, api.NetAPIVersion, name, api.NetAPIVersion+"/DHCPv6Address/"+name, nullGeneration(s.generation), string(data), now, now, expected.Revision, expected.Owner, expected.Revision)
	if err != nil {
		return DHCPv6AddressRecord{}, s.noteStorageWriteErrorLocked(err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return DHCPv6AddressRecord{}, err
	}
	if changed != 1 {
		return DHCPv6AddressRecord{}, ErrDHCPv6AddressRecordChanged
	}
	s.statusWriteCount++
	s.incrementStatusKindWriteLocked("DHCPv6Address")
	return DHCPv6AddressRecord{Revision: expected.Revision + 1, Owner: owner, State: next}, nil
}

func (s *SQLiteStore) DeleteDHCPv6AddressRecord(name string, expected DHCPv6AddressRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sql.ErrConnDone
	}
	current, _, err := s.readDHCPv6AddressRecordLocked(name)
	if err != nil {
		return err
	}
	if current.Revision != expected.Revision || current.Owner != expected.Owner {
		return ErrDHCPv6AddressRecordChanged
	}
	if current.State.Native != nil && len(current.State.Native.Addresses) != 0 {
		return fmt.Errorf("DHCPv6 native ownership must be withdrawn before deletion")
	}
	result, err := s.db.Exec(`DELETE FROM objects WHERE api_version=? AND kind='DHCPv6Address' AND name=? AND resource_version=? AND coalesce(json_extract(status,'$.nativeOwner'),'')=?`, api.NetAPIVersion, name, expected.Revision, expected.Owner)
	if err != nil {
		return s.noteStorageWriteErrorLocked(err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrDHCPv6AddressRecordChanged
	}
	return nil
}

func validateDHCPv6AddressState(state DHCPv6AddressState) error {
	if len(state.Leases) > 2048 {
		return fmt.Errorf("DHCPv6 address lease bound exceeded")
	}
	seen := map[string]bool{}
	for _, lease := range state.Leases {
		address, err := netip.ParseAddr(lease.Address)
		if err != nil || !address.Is6() || address.Is4In6() || address.IsMulticast() || address.IsUnspecified() || address.IsLinkLocalUnicast() || address.Zone() != "" || seen[lease.Address] || lease.AcquiredAt.IsZero() || lease.Preferred < 0 || lease.Preferred > lease.Valid || lease.Valid <= 0 || lease.Valid > 0xffffffff {
			return fmt.Errorf("invalid DHCPv6 address lease")
		}
		seen[lease.Address] = true
	}
	if len(state.Leases) > 0 && (state.Source.Worker == "" || state.Source.IfName == "" || state.Source.ClientDUID == "" || state.ServerDUID == "" || state.Source.Mode == pdclient.RequestPrefixDelegation || state.Source.Mode > pdclient.RequestPrefixAndAddress) {
		return fmt.Errorf("DHCPv6 address lease source is incomplete")
	}
	if state.Native == nil {
		return nil
	}
	if state.Native.IfName == "" || state.Native.IfIndex < 1 || state.Native.Scope == "" || len(state.Native.Addresses) > 2048 {
		return fmt.Errorf("invalid DHCPv6 native identity")
	}
	seen = map[string]bool{}
	for _, address := range state.Native.Addresses {
		prefix, err := netip.ParsePrefix(address.Address)
		if err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() || prefix.Bits() != 128 || seen[address.Address] || (!address.Held && address.Pending == nil) {
			return fmt.Errorf("invalid DHCPv6 native address")
		}
		seen[address.Address] = true
		if address.Held && (address.ValidUntil.IsZero() || address.PreferredUntil.IsZero() || address.PreferredUntil.After(address.ValidUntil) || address.AcquiredAt.IsZero()) {
			return fmt.Errorf("invalid DHCPv6 held address lifetime")
		}
		if p := address.Pending; p != nil {
			if p.Operation != "apply" && p.Operation != "withdraw" {
				return fmt.Errorf("invalid DHCPv6 pending operation")
			}
			if p.Operation == "apply" && (p.ValidUntil.IsZero() || p.PreferredUntil.IsZero() || p.PreferredUntil.After(p.ValidUntil) || p.AcquiredAt.IsZero()) {
				return fmt.Errorf("invalid DHCPv6 pending lifetime")
			}
		}
	}
	return nil
}
