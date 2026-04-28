package state

import (
	"encoding/json"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

type PDLease struct {
	CurrentPrefix      string `json:"currentPrefix,omitempty"`
	LastPrefix         string `json:"lastPrefix,omitempty"`
	LastObservedServer string `json:"lastObservedServer,omitempty"`
	PreferredLifetime  string `json:"preferredLifetime,omitempty"`
	ValidLifetime      string `json:"validLifetime,omitempty"`
	LastObservedAt     string `json:"lastObservedAt,omitempty"`
	LastMissingAt      string `json:"lastMissingAt,omitempty"`
	LastRenewAttemptAt string `json:"lastRenewAttemptAt,omitempty"`
	DUID               string `json:"duid,omitempty"`
	DUIDText           string `json:"duidText,omitempty"`
	IAID               string `json:"iaid,omitempty"`
	ExpectedDUID       string `json:"expectedDUID,omitempty"`
	IdentitySource     string `json:"identitySource,omitempty"`
}

func EncodePDLease(lease PDLease) string {
	data, err := json.Marshal(lease)
	if err != nil {
		return ""
	}
	return string(data)
}

func DecodePDLease(value string) (PDLease, bool) {
	var lease PDLease
	if strings.TrimSpace(value) == "" {
		return lease, false
	}
	if err := json.Unmarshal([]byte(value), &lease); err != nil {
		return lease, false
	}
	return lease, true
}

func PDLeaseFromStore(store *Store, base string) (PDLease, bool) {
	lease, ok := DecodePDLease(store.Get(base + ".lease").Value)
	merged := ok
	mergeString := func(field *string, key string) {
		if *field != "" {
			return
		}
		value := store.Get(base + "." + key)
		if value.Status == StatusSet && value.Value != "" {
			*field = value.Value
			merged = true
		}
	}
	mergeString(&lease.CurrentPrefix, "currentPrefix")
	mergeString(&lease.LastPrefix, "lastPrefix")
	mergeString(&lease.LastObservedServer, "lastObservedServer")
	mergeString(&lease.PreferredLifetime, "preferredLifetime")
	mergeString(&lease.ValidLifetime, "validLifetime")
	mergeString(&lease.LastObservedAt, "lastObservedAt")
	mergeString(&lease.LastMissingAt, "lastMissingAt")
	mergeString(&lease.LastRenewAttemptAt, "lastRenewAttemptAt")
	mergeString(&lease.DUID, "duid")
	mergeString(&lease.DUIDText, "duidText")
	mergeString(&lease.IAID, "iaid")
	mergeString(&lease.ExpectedDUID, "expectedDUID")
	mergeString(&lease.IdentitySource, "identitySource")
	return lease, merged
}

var legacyPDLeaseFields = []string{
	"currentPrefix",
	"lastPrefix",
	"lastObservedServer",
	"preferredLifetime",
	"validLifetime",
	"lastObservedAt",
	"lastMissingAt",
	"lastRenewAttemptAt",
	"duid",
	"duidText",
	"iaid",
	"expectedDUID",
	"identitySource",
}

func MigratePDLeases(store *Store) bool {
	names := map[string]bool{}
	for name := range store.Variables {
		rest, ok := strings.CutPrefix(name, "ipv6PrefixDelegation.")
		if !ok {
			continue
		}
		pdName, field, ok := strings.Cut(rest, ".")
		if ok && pdName != "" && isLegacyPDLeaseField(field) {
			names[pdName] = true
		}
	}
	changed := false
	for name := range names {
		base := "ipv6PrefixDelegation." + name
		lease, ok := PDLeaseFromStore(store, base)
		if ok {
			store.Set(base+".lease", EncodePDLease(lease), "migrated DHCPv6-PD lease state")
			changed = true
		}
		for _, field := range legacyPDLeaseFields {
			key := base + "." + field
			if _, exists := store.Variables[key]; exists {
				store.Delete(key)
				changed = true
			}
		}
	}
	return changed
}

func isLegacyPDLeaseField(field string) bool {
	for _, candidate := range legacyPDLeaseFields {
		if field == candidate {
			return true
		}
	}
	return false
}

func PDLeaseHintPrefix(lease PDLease, now time.Time) (string, bool) {
	prefix := strings.TrimSpace(lease.LastPrefix)
	if prefix == "" {
		return "", false
	}
	if _, err := netip.ParsePrefix(prefix); err != nil {
		return "", false
	}
	validLifetime := strings.TrimSpace(lease.ValidLifetime)
	if validLifetime == "" {
		return prefix, true
	}
	observedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(lease.LastObservedAt))
	if err != nil {
		return "", false
	}
	lifetime, ok := ParseLeaseLifetime(validLifetime)
	if !ok {
		return "", false
	}
	if lifetime >= 0 && !now.UTC().Before(observedAt.UTC().Add(lifetime)) {
		return "", false
	}
	return prefix, true
}

func ParseLeaseLifetime(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if strings.EqualFold(value, "infinity") || strings.EqualFold(value, "infinite") {
		return -1, true
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, false
	}
	return duration, true
}
