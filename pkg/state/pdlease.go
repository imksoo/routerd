package state

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

type PDLease struct {
	CurrentPrefix  string               `json:"currentPrefix,omitempty"`
	LastPrefix     string               `json:"lastPrefix,omitempty"`
	Prefix         string               `json:"prefix,omitempty"`
	PriorPrefix    string               `json:"priorPrefix,omitempty"`
	ServerID       string               `json:"serverID,omitempty"`
	T1             string               `json:"t1,omitempty"`
	T2             string               `json:"t2,omitempty"`
	PLTime         string               `json:"pltime,omitempty"`
	VLTime         string               `json:"vltime,omitempty"`
	SourceMAC      string               `json:"sourceMAC,omitempty"`
	SourceLL       string               `json:"sourceLL,omitempty"`
	LastObservedAt string               `json:"lastObservedAt,omitempty"`
	LastReplyAt    string               `json:"lastReplyAt,omitempty"`
	LastSolicitAt  string               `json:"lastSolicitAt,omitempty"`
	LastRequestAt  string               `json:"lastRequestAt,omitempty"`
	LastRenewAt    string               `json:"lastRenewAt,omitempty"`
	LastRebindAt   string               `json:"lastRebindAt,omitempty"`
	LastReleaseAt  string               `json:"lastReleaseAt,omitempty"`
	DUID           string               `json:"duid,omitempty"`
	DUIDText       string               `json:"duidText,omitempty"`
	IAID           string               `json:"iaid,omitempty"`
	ExpectedDUID   string               `json:"expectedDUID,omitempty"`
	WANObserved    *PDWANObserved       `json:"wanObserved,omitempty"`
	Hung           *PDHungStatus        `json:"hung,omitempty"`
	Acquisition    *PDAcquisitionStatus `json:"acquisition,omitempty"`
	Transactions   []PDDHCP6Transaction `json:"transactions,omitempty"`
}

type PDWANObserved struct {
	HGWLinkLocal  string `json:"hgwLinkLocal,omitempty"`
	HGWMACDerived string `json:"hgwMACDerived,omitempty"`
	RAMFlag       string `json:"raMFlag,omitempty"`
	RAOFlag       string `json:"raOFlag,omitempty"`
	RAPrefix      string `json:"raPrefix,omitempty"`
	RAObservedAt  string `json:"raObservedAt,omitempty"`
}

type PDHungStatus struct {
	SuspectedAt             string `json:"suspectedAt,omitempty"`
	Reason                  string `json:"reason,omitempty"`
	RecoveryMode            string `json:"recoveryMode,omitempty"`
	RecoveryAttempts        int    `json:"recoveryAttempts,omitempty"`
	RecoveryLastAttemptAt   string `json:"recoveryLastAttemptAt,omitempty"`
	RecoveryNextAttemptAt   string `json:"recoveryNextAttemptAt,omitempty"`
	RecoveryExhaustedAt     string `json:"recoveryExhaustedAt,omitempty"`
	RecoveryLastError       string `json:"recoveryLastError,omitempty"`
	RecoveryLastSucceededAt string `json:"recoveryLastSucceededAt,omitempty"`
}

type PDAcquisitionStatus struct {
	Strategy           string `json:"strategy,omitempty"`
	Phase              string `json:"phase,omitempty"`
	LastAttemptAt      string `json:"lastAttemptAt,omitempty"`
	AttemptsSinceReply int    `json:"attemptsSinceReply,omitempty"`
	NextAction         string `json:"nextAction,omitempty"`
}

type PDDHCP6Transaction struct {
	ObservedAt        string `json:"observedAt,omitempty"`
	Direction         string `json:"direction,omitempty"`
	Interface         string `json:"interface,omitempty"`
	MessageType       string `json:"messageType,omitempty"`
	TransactionID     string `json:"transactionID,omitempty"`
	ClientDUID        string `json:"clientDUID,omitempty"`
	ServerDUID        string `json:"serverDUID,omitempty"`
	IAID              string `json:"iaid,omitempty"`
	Prefix            string `json:"prefix,omitempty"`
	T1                string `json:"t1,omitempty"`
	T2                string `json:"t2,omitempty"`
	PreferredLifetime string `json:"preferredLifetime,omitempty"`
	ValidLifetime     string `json:"validLifetime,omitempty"`
	ReconfigureAccept string `json:"reconfigureAccept,omitempty"`
	Warning           string `json:"warning,omitempty"`
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

func PDLeaseFromStore(store Store, base string) (PDLease, bool) {
	return DecodePDLease(store.Get(base + ".lease").Value)
}

// HasFreshTransactionEvidence reports whether the lease has a recent
// DHCPv6 Reply with a positive valid lifetime that has not yet expired
// at the supplied instant. A lease with a CurrentPrefix but no transaction
// evidence is "drifted" — typically the local LAN address has outlived
// its upstream PD binding — and must NOT be treated as an authoritative
// IPv6 service signal for downstream clients.
func (l PDLease) HasFreshTransactionEvidence(now time.Time) bool {
	if l.LastReplyAt == "" {
		return false
	}
	if l.VLTime == "" {
		return false
	}
	vl, err := strconv.Atoi(l.VLTime)
	if err != nil || vl <= 0 {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, l.LastReplyAt)
	if err != nil {
		return false
	}
	expires := parsed.Add(time.Duration(vl) * time.Second)
	return now.Before(expires)
}

func ClearPDLeaseObservedIdentity(lease PDLease) (PDLease, bool) {
	changed := lease.DUID != "" || lease.DUIDText != "" || lease.IAID != ""
	lease.DUID = ""
	lease.DUIDText = ""
	lease.IAID = ""
	return lease, changed
}
