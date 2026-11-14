// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
)

type PDLease struct {
	CurrentPrefix    string             `json:"currentPrefix,omitempty"`
	LastPrefix       string             `json:"lastPrefix,omitempty"`
	PreviousPrefixes []PDPreviousPrefix `json:"previousPrefixes,omitempty"`
	Prefix           string             `json:"prefix,omitempty"`
	T1               string             `json:"t1,omitempty"`
	T2               string             `json:"t2,omitempty"`
	PLTime           string             `json:"pltime,omitempty"`
	VLTime           string             `json:"vltime,omitempty"`
	SourceMAC        string             `json:"sourceMAC,omitempty"`
	SourceLL         string             `json:"sourceLL,omitempty"`
	LastObservedAt   string             `json:"lastObservedAt,omitempty"`
	LastReplyAt      string             `json:"lastReplyAt,omitempty"`
	LastSolicitAt    string             `json:"lastSolicitAt,omitempty"`
	LastRequestAt    string             `json:"lastRequestAt,omitempty"`
	LastRenewAt      string             `json:"lastRenewAt,omitempty"`
	LastRebindAt     string             `json:"lastRebindAt,omitempty"`
	LastReleaseAt    string             `json:"lastReleaseAt,omitempty"`
	DUID             string             `json:"duid,omitempty"`
	DUIDText         string             `json:"duidText,omitempty"`
	IAID             string             `json:"iaid,omitempty"`
	ExpectedDUID     string             `json:"expectedDUID,omitempty"`
}

type PDPreviousPrefix struct {
	Prefix    string    `json:"prefix"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (l PDLease) ValidPreviousPrefixes(now time.Time) []PDPreviousPrefix {
	out := make([]PDPreviousPrefix, 0, len(l.PreviousPrefixes))
	for _, previous := range l.PreviousPrefixes {
		if previous.Prefix == "" || previous.Prefix == l.CurrentPrefix || previous.ExpiresAt.IsZero() || !now.Before(previous.ExpiresAt) {
			continue
		}
		out = append(out, previous)
	}
	return out
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

func PDLeaseFromStore(store Store, base string) (PDLease, bool, error) {
	return store.PDLease(base)
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

// PD lease contents live in the existing top-level object status. _lease holds
// only Value metadata, never another copy of the lease. Old binaries ignore it
// and derive their lease from the same top-level fields when _variables.lease
// is absent.
const pdAPIVersion = "net.routerd.net/v1alpha1"

func pdObject(ref objectRef) bool {
	return ref.APIVersion == pdAPIVersion && ref.Kind == "DHCPv6PrefixDelegation"
}

func pdLeaseStrings(lease *PDLease) map[string]*string {
	return map[string]*string{
		"currentPrefix":  &lease.CurrentPrefix,
		"lastPrefix":     &lease.LastPrefix,
		"prefix":         &lease.Prefix,
		"t1":             &lease.T1,
		"t2":             &lease.T2,
		"pltime":         &lease.PLTime,
		"vltime":         &lease.VLTime,
		"sourceMAC":      &lease.SourceMAC,
		"sourceLL":       &lease.SourceLL,
		"lastObservedAt": &lease.LastObservedAt,
		"lastReplyAt":    &lease.LastReplyAt,
		"lastSolicitAt":  &lease.LastSolicitAt,
		"lastRequestAt":  &lease.LastRequestAt,
		"lastRenewAt":    &lease.LastRenewAt,
		"lastRebindAt":   &lease.LastRebindAt,
		"lastReleaseAt":  &lease.LastReleaseAt,
		"duid":           &lease.DUID,
		"duidText":       &lease.DUIDText,
		"iaid":           &lease.IAID,
		"expectedDUID":   &lease.ExpectedDUID,
	}
}

func pdLeaseFields(lease PDLease) objectStatus {
	out := objectStatus{}
	for key, value := range pdLeaseStrings(&lease) {
		if *value != "" {
			out[key] = *value
		}
	}
	if len(lease.PreviousPrefixes) > 0 {
		out["previousPrefixes"] = lease.PreviousPrefixes
	}
	return out
}
func pdLeaseFromStatus(status objectStatus) (PDLease, bool, error) {
	var lease PDLease
	found := false
	// This is the existing untyped SQLite object-status boundary, not an
	// internal JSON round trip. Each stored field is checked once here.
	for key, target := range pdLeaseStrings(&lease) {
		if raw, exists := status[key]; exists {
			value, ok := raw.(string)
			if !ok {
				return lease, false, fmt.Errorf("PD lease %s must be a string", key)
			}
			*target, found = value, true
		}
	}
	if raw, exists := status["previousPrefixes"]; exists {
		found = true
		switch value := raw.(type) {
		case nil:
		case []PDPreviousPrefix:
			lease.PreviousPrefixes = slices.Clone(value)
		case []any:
			for _, item := range value {
				previous, ok := item.(map[string]any)
				if !ok {
					return lease, false, fmt.Errorf("invalid PD previous prefix")
				}
				prefix, ok := previous["prefix"].(string)
				if !ok {
					return lease, false, fmt.Errorf("invalid PD previous prefix")
				}
				expires, ok := previous["expiresAt"].(string)
				if !ok {
					return lease, false, fmt.Errorf("invalid PD previous prefix expiry")
				}
				t, err := time.Parse(time.RFC3339Nano, expires)
				if err != nil {
					return lease, false, err
				}
				lease.PreviousPrefixes = append(lease.PreviousPrefixes, PDPreviousPrefix{Prefix: prefix, ExpiresAt: t})
			}
		default:
			return lease, false, fmt.Errorf("invalid PD previous prefixes")
		}
	}
	return lease, found, nil
}
func samePDLease(a, b PDLease) bool {
	right := pdLeaseStrings(&b)
	for key, value := range pdLeaseStrings(&a) {
		if *value != *right[key] {
			return false
		}
	}
	return slices.EqualFunc(a.PreviousPrefixes, b.PreviousPrefixes, func(a, b PDPreviousPrefix) bool {
		return a.Prefix == b.Prefix && a.ExpiresAt.Equal(b.ExpiresAt)
	})
}

// normalizePDLease validates the two observed old representations before
// removing the redundant one. It never chooses one side of a disagreement.
func normalizePDLease(status objectStatus) error {
	if status == nil {
		return fmt.Errorf("PD object status must be an object")
	}
	top, found, err := pdLeaseFromStatus(status)
	if err != nil {
		return err
	}
	if raw, ok := status["_variables"]; ok {
		if _, valid := raw.(map[string]any); !valid {
			return fmt.Errorf("invalid PD variables")
		}
	}
	if raw, exists := status["_lease"]; exists {
		meta, ok := decodeValue(raw)
		if !ok || meta.Value != "" {
			return fmt.Errorf("invalid canonical PD lease metadata")
		}
	}
	vars := variablesMap(status)
	raw, legacy := vars["lease"]
	if !legacy {
		return nil
	}
	value, ok := decodeValue(raw)
	if !ok {
		return fmt.Errorf("invalid legacy PD lease metadata")
	}
	if _, exists := status["_lease"]; exists {
		return fmt.Errorf("PD lease has both legacy and canonical metadata")
	}
	if value.Value != "" {
		if !strings.HasPrefix(strings.TrimSpace(value.Value), "{") {
			return fmt.Errorf("legacy PD lease must be an object")
		}
		if value.Status != StatusSet {
			return fmt.Errorf("legacy PD lease value has status %q", value.Status)
		}
		var old PDLease
		decoder := json.NewDecoder(strings.NewReader(value.Value))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&old); err != nil {
			return fmt.Errorf("decode legacy PD lease: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("legacy PD lease has trailing data")
		}
		if !samePDLease(top, old) {
			return fmt.Errorf("legacy PD lease disagrees with top-level status")
		}
	} else if found || value.Status == StatusSet {
		return fmt.Errorf("empty legacy PD lease disagrees with top-level status or metadata")
	}
	value.Value = ""
	status["_lease"] = encodePDLeaseMetadata(value)
	// Copy the nested map: callers and transition builders may retain it.
	next := map[string]any{}
	for key, item := range vars {
		if key != "lease" {
			next[key] = item
		}
	}
	if len(next) == 0 {
		delete(status, "_variables")
	} else {
		status["_variables"] = next
	}
	return nil
}

func pdLeaseValue(status objectStatus) (Value, bool, error) {
	if err := normalizePDLease(status); err != nil {
		return Value{}, false, err
	}
	lease, found, err := pdLeaseFromStatus(status)
	if err != nil {
		return Value{}, false, err
	}
	meta, hasMeta := decodeValue(status["_lease"])
	if !found && !hasMeta {
		return Value{}, false, nil
	}
	if !hasMeta {
		meta.Status = StatusSet
	}
	if meta.Status == StatusSet {
		meta.Value = EncodePDLease(lease)
	}
	return meta, true, nil
}

func replacePDLease(status objectStatus, lease PDLease, meta Value) {
	for key := range pdLeaseStrings(&lease) {
		delete(status, key)
	}
	delete(status, "previousPrefixes")
	// Delete even fields omitted by the new value, without deleting phase,
	// conditions, HA state, or other producers' observations.
	for key, value := range pdLeaseFields(lease) {
		status[key] = value
	}
	meta.Value = ""
	status["_lease"] = encodePDLeaseMetadata(meta)
}

func (s *SQLiteStore) PDLease(base string) (PDLease, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return PDLease{}, false, sql.ErrConnDone
	}
	ref := objectRefForKey(base + ".lease")
	if !pdObject(ref) {
		return PDLease{}, false, fmt.Errorf("invalid PD lease key %q", base)
	}
	status, _, err := s.loadStatus(ref.APIVersion, ref.Kind, ref.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return PDLease{}, false, nil
	}
	if err != nil {
		return PDLease{}, false, err
	}
	lease, found, err := pdLeaseFromStatus(status)
	if meta, ok := decodeValue(status["_lease"]); ok {
		if meta.Status != StatusSet {
			return PDLease{}, false, nil
		}
		found = true
	}
	return lease, found, err
}

func (s *SQLiteStore) SavePDLease(base string, lease PDLease, reason string) (Value, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writePDLeaseLocked(objectRefForKey(base+".lease"), lease, StatusSet, reason)
}

func (s *SQLiteStore) writePDLeaseLocked(ref objectRef, lease PDLease, leaseStatus, reason string) (Value, error) {
	if s.closed {
		return Value{}, sql.ErrConnDone
	}
	if !pdObject(ref) || ref.Field != "lease" {
		return Value{}, fmt.Errorf("invalid PD lease key")
	}
	status, _, err := s.loadStatus(ref.APIVersion, ref.Kind, ref.Name)
	if errors.Is(err, sql.ErrNoRows) {
		status = objectStatus{}
	} else if err != nil {
		return Value{}, err
	}
	previous, _, err := pdLeaseFromStatus(status)
	if err != nil {
		return Value{}, err
	}
	meta, _ := decodeValue(status["_lease"])
	meta = nextPDLeaseMetadata(previous, meta, lease, leaseStatus, reason, time.Now().UTC())
	replacePDLease(status, lease, meta)
	if err := s.saveStatus(ref, status); err != nil {
		return Value{}, err
	}
	if meta.Status == StatusSet {
		meta.Value = EncodePDLease(lease)
	} // Output only.
	return meta, nil
}

func nextPDLeaseMetadata(old PDLease, meta Value, next PDLease, status, reason string, now time.Time) Value {
	if meta.Status != status || !samePDLease(old, next) || meta.Since.IsZero() {
		meta.Since = now
	}
	meta.Status, meta.Reason, meta.UpdatedAt = status, reason, now
	meta.Value = ""
	return meta
}

func encodePDLeaseMetadata(value Value) map[string]any {
	return map[string]any{"status": value.Status, "reason": value.Reason, "since": formatStateTime(value.Since), "updatedAt": formatStateTime(value.UpdatedAt)}
}

func (s *JSONStore) PDLease(base string) (PDLease, bool, error) {
	value := s.Get(base + ".lease")
	if value.Value == "" {
		return PDLease{}, false, nil
	}
	var lease PDLease
	if !strings.HasPrefix(strings.TrimSpace(value.Value), "{") {
		return lease, false, fmt.Errorf("PD lease must be an object")
	}
	if err := json.Unmarshal([]byte(value.Value), &lease); err != nil {
		return lease, false, err
	}
	return lease, true, nil
}

func (s *JSONStore) SavePDLease(base string, lease PDLease, reason string) (Value, error) {
	// JSONStore is the legacy file boundary, with one serialized representation.
	return s.Set(base+".lease", EncodePDLease(lease), reason), nil
}

// PreparePDLeaseStatus is shared by ordinary and atomic status/event writes.
// A helper status updates only the lease fields it actually reports; a typed
// SavePDLease supplies _lease and replaces the whole lease deliberately.
func PreparePDLeaseStatus(current, next map[string]any) error {
	if err := normalizePDLease(current); err != nil {
		return err
	}
	if err := normalizePDLease(next); err != nil {
		return err
	}
	old, _, err := pdLeaseFromStatus(current)
	if err != nil {
		return err
	}
	_, explicit := next["_lease"]
	if !explicit {
		for key, value := range pdLeaseFields(old) {
			if _, ok := next[key]; !ok {
				next[key] = value
			}
		}
		if value, ok := current["_variables"]; ok {
			if _, exists := next["_variables"]; !exists {
				next["_variables"] = value
			}
		}
	}
	lease, found, err := pdLeaseFromStatus(next)
	if err != nil {
		return err
	}
	meta, ok := decodeValue(next["_lease"])
	if !ok {
		meta, ok = decodeValue(current["_lease"])
	}
	if !found && !ok {
		return nil
	}
	if !ok {
		meta.Status = StatusSet
	}
	oldMeta, _ := decodeValue(current["_lease"])
	suppliedMeta, supplied := decodeValue(next["_lease"])
	metadataChanged := supplied && suppliedMeta != oldMeta
	if !samePDLease(old, lease) && !metadataChanged {
		meta = nextPDLeaseMetadata(old, meta, lease, StatusSet, meta.Reason, time.Now().UTC())
	}
	if meta.Since.IsZero() {
		meta.Since = time.Now().UTC()
	}
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = meta.Since
	}
	meta.Value = ""
	next["_lease"] = encodePDLeaseMetadata(meta)
	return nil
}

func readPDStatus(status objectStatus, created, modified string) error {
	if err := normalizePDLease(status); err != nil {
		return err
	}
	_, found, err := pdLeaseFromStatus(status)
	if err != nil {
		return err
	}
	if _, ok := status["_lease"]; !ok && found {
		status["_lease"] = encodePDLeaseMetadata(Value{Status: StatusSet, Since: parseStateTime(created), UpdatedAt: parseStateTime(modified)})
	}
	return nil
}
