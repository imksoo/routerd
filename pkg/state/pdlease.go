package state

import (
	"encoding/json"
	"strings"
)

type PDLease struct {
	CurrentPrefix  string `json:"currentPrefix,omitempty"`
	LastPrefix     string `json:"lastPrefix,omitempty"`
	LastObservedAt string `json:"lastObservedAt,omitempty"`
	DUID           string `json:"duid,omitempty"`
	DUIDText       string `json:"duidText,omitempty"`
	IAID           string `json:"iaid,omitempty"`
	ExpectedDUID   string `json:"expectedDUID,omitempty"`
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
