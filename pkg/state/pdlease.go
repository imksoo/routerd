package state

import (
	"encoding/json"
	"strings"
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
	LastReleaseAt  string               `json:"lastReleaseAt,omitempty"`
	DUID           string               `json:"duid,omitempty"`
	DUIDText       string               `json:"duidText,omitempty"`
	IAID           string               `json:"iaid,omitempty"`
	ExpectedDUID   string               `json:"expectedDUID,omitempty"`
	WANObserved    *PDWANObserved       `json:"wanObserved,omitempty"`
	Hung           *PDHungStatus        `json:"hung,omitempty"`
	Acquisition    *PDAcquisitionStatus `json:"acquisition,omitempty"`
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
	SuspectedAt string `json:"suspectedAt,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type PDAcquisitionStatus struct {
	Strategy           string `json:"strategy,omitempty"`
	Phase              string `json:"phase,omitempty"`
	LastAttemptAt      string `json:"lastAttemptAt,omitempty"`
	AttemptsSinceReply int    `json:"attemptsSinceReply,omitempty"`
	NextAction         string `json:"nextAction,omitempty"`
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
