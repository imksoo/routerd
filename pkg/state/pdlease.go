// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pdclient"
)

// PDLease is the single persisted prefix-delegation observation. Lifetimes are
// protocol seconds; timestamps are parsed once at the OS/daemon boundary.
// Applied/configuration status belongs to other fields in the object row.
type PDLease struct {
	CurrentPrefix        string                `json:"currentPrefix,omitempty"`
	LastPrefix           string                `json:"lastPrefix,omitempty"`
	PreviousPrefixes     []PDPreviousPrefix    `json:"previousPrefixes,omitempty"`
	T1Seconds            uint32                `json:"t1Seconds,omitempty"`
	T2Seconds            uint32                `json:"t2Seconds,omitempty"`
	PreferredSeconds     uint32                `json:"preferredSeconds,omitempty"`
	ValidSeconds         uint32                `json:"validSeconds,omitempty"`
	LastObservedAt       time.Time             `json:"lastObservedAt,omitempty"`
	LeaseUpdatedAt       time.Time             `json:"leaseUpdatedAt,omitempty"`
	SnapshotUpdatedAt    time.Time             `json:"snapshotUpdatedAt,omitempty"`
	LastReplyAt          time.Time             `json:"lastReplyAt,omitempty"`
	DUID                 string                `json:"duid,omitempty"`
	DUIDText             string                `json:"duidText,omitempty"`
	ServerDUID           string                `json:"serverDUID,omitempty"`
	IAID                 string                `json:"iaid,omitempty"`
	ExpectedDUID         string                `json:"expectedDUID,omitempty"`
	Client               string                `json:"client,omitempty"`
	Profile              string                `json:"profile,omitempty"`
	UplinkIfname         string                `json:"uplinkIfname,omitempty"`
	DownstreamIfname     string                `json:"downstreamIfname,omitempty"`
	ConfiguredClientDUID string                `json:"configuredClientDUID,omitempty"`
	ConfiguredIAID       uint32                `json:"configuredIAID,omitempty"`
	PrefixLength         int                   `json:"prefixLength,omitempty"`
	Phase                string                `json:"phase,omitempty"`
	Health               string                `json:"health,omitempty"`
	Conditions           []daemonapi.Condition `json:"conditions,omitempty"`
}

type PDPreviousPrefix struct {
	Prefix    string    `json:"prefix"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type PDLeaseConfig struct {
	Client       string
	Profile      string
	UplinkIfname string
	PrefixLength int
	ExpectedDUID string
	ClientDUID   string
	IAID         uint32
}

type PDLeaseReader interface {
	ReadPDLease(name string) (PDLease, bool, error)
}
type PDLeaseStore interface {
	PDLeaseReader
	SavePDLease(name string, lease PDLease) error
	ConfigurePDLease(name string, config PDLeaseConfig) (PDLease, bool, error)
	SavePDLeaseSnapshot(name string, snapshot pdclient.Snapshot) error
}

func (l PDLease) Config() PDLeaseConfig {
	return PDLeaseConfig{Client: l.Client, Profile: l.Profile, UplinkIfname: l.UplinkIfname, PrefixLength: l.PrefixLength, ExpectedDUID: l.ExpectedDUID, ClientDUID: l.ConfiguredClientDUID, IAID: l.ConfiguredIAID}
}

func samePDConfiguredSource(a, b PDLeaseConfig) bool {
	return a.Client == b.Client && a.UplinkIfname == b.UplinkIfname && a.ClientDUID == b.ClientDUID && a.IAID == b.IAID
}

func configurePDLease(l PDLease, config PDLeaseConfig) (PDLease, bool) {
	previous := l.Config()
	changed := previous.Client != "" && !samePDConfiguredSource(previous, config)
	if changed {
		l, _ = ClearPDLeaseObservedIdentity(l)
		l.Phase = daemonapi.ResourcePhasePending
	}
	l.Client, l.Profile, l.UplinkIfname, l.PrefixLength, l.ExpectedDUID, l.ConfiguredClientDUID, l.ConfiguredIAID = config.Client, config.Profile, config.UplinkIfname, config.PrefixLength, config.ExpectedDUID, config.ClientDUID, config.IAID
	return l, changed
}

// Configured returns a new source-bound observation without writing storage.
// The captured previous Config is supplied to the atomic observation commit.
func (l PDLease) Configured(config PDLeaseConfig) (PDLease, bool) {
	return configurePDLease(l, config)
}

var ErrStalePDLeaseSource = errors.New("PD observation source changed while observing")

func ValidatePDLeaseSnapshot(l PDLease, name string, snapshot pdclient.Snapshot) error {
	if !samePDLeaseSource(l, name, snapshot) {
		return fmt.Errorf("PD snapshot does not match configured source %s", name)
	}
	if snapshot.T1Seconds < 0 || snapshot.T1Seconds > 0xffffffff || snapshot.T2Seconds < 0 || snapshot.T2Seconds > 0xffffffff {
		return fmt.Errorf("PD snapshot timers exceed protocol bounds")
	}
	return nil
}

func samePDLeaseSource(l PDLease, name string, snapshot pdclient.Snapshot) bool {
	return l.UplinkIfname != "" && l.ConfiguredClientDUID != "" && snapshot.Resource == name && snapshot.Interface == l.UplinkIfname && strings.EqualFold(snapshot.ClientDUID, l.ConfiguredClientDUID) && snapshot.IAID == l.ConfiguredIAID
}

func (l PDLease) ValidPreviousPrefixes(now time.Time) []PDPreviousPrefix {
	var out []PDPreviousPrefix
	for _, p := range l.PreviousPrefixes {
		if p.Prefix != "" && p.Prefix != l.CurrentPrefix && !p.ExpiresAt.IsZero() && now.Before(p.ExpiresAt) {
			out = append(out, p)
		}
	}
	return out
}

func (l PDLease) HasFreshTransactionEvidence(now time.Time) bool {
	if l.CurrentPrefix == "" || l.LastReplyAt.IsZero() || now.Before(l.LastReplyAt) || l.ValidSeconds == 0 {
		return false
	}
	return l.ValidSeconds == 0xffffffff || now.Before(l.LastReplyAt.Add(time.Duration(l.ValidSeconds)*time.Second))
}

func (l PDLease) Live(now time.Time) PDLease {
	l = copyPDLease(l)
	l.PreviousPrefixes = l.ValidPreviousPrefixes(now)
	if !l.HasFreshTransactionEvidence(now) {
		l.CurrentPrefix = ""
		if l.Phase == daemonapi.ResourcePhaseBound {
			l.Phase = daemonapi.ResourcePhaseExpired
		}
		if l.Phase == "" {
			l.Phase = daemonapi.ResourcePhasePending
		}
	}
	reason := l.Phase
	ready := l.CurrentPrefix != "" && l.Phase == daemonapi.ResourcePhaseBound
	for _, condition := range l.Conditions {
		if condition.Type == "LeaseReady" && (condition.Status == daemonapi.ConditionTrue) == ready {
			reason = condition.Reason
			break
		}
	}
	at := l.LastObservedAt
	if l.Phase == daemonapi.ResourcePhaseExpired {
		reason = string(pdclient.StateExpired)
		if !l.LastReplyAt.IsZero() && l.ValidSeconds != 0xffffffff {
			at = l.LastReplyAt.Add(time.Duration(l.ValidSeconds) * time.Second)
		}
	}
	l = l.withLeaseReady(reason, at)
	return l
}

func (l PDLease) withLeaseReady(reason string, at time.Time) PDLease {
	status := daemonapi.ConditionFalse
	if l.CurrentPrefix != "" && l.Phase == daemonapi.ResourcePhaseBound {
		status = daemonapi.ConditionTrue
	}
	condition := daemonapi.Condition{Type: "LeaseReady", Status: status, Reason: reason, LastTransitionTime: at}
	for _, previous := range l.Conditions {
		if previous.Type == condition.Type && previous.Status == status && previous.Reason == reason {
			condition.LastTransitionTime = previous.LastTransitionTime
			break
		}
	}
	l.Conditions = []daemonapi.Condition{condition}
	return l
}

func copyPDLease(l PDLease) PDLease {
	l.PreviousPrefixes = append([]PDPreviousPrefix(nil), l.PreviousPrefixes...)
	l.Conditions = append([]daemonapi.Condition(nil), l.Conditions...)
	return l
}

func ClearPDLeaseObservedIdentity(l PDLease) (PDLease, bool) {
	changed := l.DUID != "" || l.DUIDText != "" || l.IAID != "" || l.ServerDUID != ""
	l.DUID, l.DUIDText, l.IAID, l.ServerDUID = "", "", "", ""
	l.CurrentPrefix = ""
	l.PreviousPrefixes = nil
	l.Conditions = nil
	l.LastReplyAt = time.Time{}
	l.SnapshotUpdatedAt = time.Time{}
	l.LeaseUpdatedAt = time.Time{}
	l.T1Seconds, l.T2Seconds, l.PreferredSeconds, l.ValidSeconds = 0, 0, 0, 0
	return l, changed
}

// ObservePDLease replaces all live lease authority with the daemon's complete
// association. Omitted/revoked history is not merged back into that authority.
func ObservePDLease(previous PDLease, snapshot pdclient.Snapshot, now time.Time) PDLease {
	l := previous
	l.CurrentPrefix = ""
	l.PreviousPrefixes = nil
	l.LastReplyAt = time.Time{}
	l.T1Seconds, l.T2Seconds, l.PreferredSeconds, l.ValidSeconds = 0, 0, 0, 0
	l.ServerDUID = snapshot.ServerDUID
	l.DUID = snapshot.ClientDUID
	l.IAID = fmt.Sprint(snapshot.IAID)
	l.LastObservedAt = snapshot.UpdatedAt
	l.SnapshotUpdatedAt = snapshot.UpdatedAt
	l.LeaseUpdatedAt = snapshot.LeaseUpdatedAt
	l.Phase = string(snapshot.State)
	l.Health = daemonapi.HealthOK
	if snapshot.LastError != "" || snapshot.InfoLastError != "" {
		l.Health = daemonapi.HealthDegraded
	}
	switch snapshot.State {
	case pdclient.StateBound, pdclient.StateRenewing, pdclient.StateRebinding:
		l.Phase = daemonapi.ResourcePhaseBound
	case pdclient.StateSoliciting, pdclient.StateRequesting:
		l.Phase = daemonapi.ResourcePhaseAcquiring
	case pdclient.StateExpired:
		l.Phase = daemonapi.ResourcePhaseExpired
	case pdclient.StateIdle, pdclient.StateReleased:
		l.Phase = daemonapi.ResourcePhaseIdle
	}
	if snapshot.State != pdclient.StateBound && snapshot.State != pdclient.StateRenewing && snapshot.State != pdclient.StateRebinding {
		return l.withLeaseReady(string(snapshot.State), snapshot.UpdatedAt).Live(now)
	}
	for _, entry := range snapshot.Prefixes {
		prefix, err := netip.ParsePrefix(entry.Prefix)
		if err != nil || !prefix.Addr().Is6() || entry.AcquiredAt.IsZero() || entry.AcquiredAt.After(now) || entry.Preferred < 0 || entry.Preferred > entry.Valid || entry.Valid <= 0 || entry.Valid > 0xffffffff {
			continue
		}
		expires := entry.AcquiredAt.Add(time.Duration(entry.Valid) * time.Second)
		if entry.Valid == 0xffffffff {
			expires = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
		}
		if !now.Before(expires) {
			continue
		}
		canonical := prefix.Masked().String()
		if canonical == snapshot.CurrentPrefix {
			l.CurrentPrefix = canonical
			l.LastPrefix = canonical
			l.LastReplyAt = entry.AcquiredAt
			l.T1Seconds = uint32(snapshot.T1Seconds)
			l.T2Seconds = uint32(snapshot.T2Seconds)
			l.PreferredSeconds = uint32(entry.Preferred)
			l.ValidSeconds = uint32(entry.Valid)
		} else {
			l.PreviousPrefixes = append(l.PreviousPrefixes, PDPreviousPrefix{Prefix: canonical, ExpiresAt: expires})
		}
	}
	l = l.withLeaseReady(string(snapshot.State), snapshot.UpdatedAt)
	return l.Live(now)
}

func validatePDLease(l PDLease) error {
	for _, value := range []string{l.CurrentPrefix, l.LastPrefix} {
		if value == "" {
			continue
		}
		p, err := netip.ParsePrefix(value)
		if err != nil || !p.Addr().Is6() || p != p.Masked() {
			return fmt.Errorf("invalid canonical delegated prefix %q", value)
		}
	}
	for _, previous := range l.PreviousPrefixes {
		p, err := netip.ParsePrefix(previous.Prefix)
		if err != nil || !p.Addr().Is6() || p != p.Masked() || previous.ExpiresAt.IsZero() {
			return fmt.Errorf("invalid previous delegated prefix %q", previous.Prefix)
		}
	}
	if l.PreferredSeconds > l.ValidSeconds {
		return fmt.Errorf("delegated preferred lifetime exceeds valid lifetime")
	}
	return nil
}

func (s *MemoryStore) ReadPDLease(name string) (PDLease, bool, error) {
	s.pdMu.RLock()
	defer s.pdMu.RUnlock()
	l, ok := s.PDLeases[name]
	return copyPDLease(l), ok, nil
}

func mergePDLeaseObservation(current, next PDLease) (PDLease, error) {
	if current.Config() != next.Config() {
		return PDLease{}, ErrStalePDLeaseSource
	}
	if !current.SnapshotUpdatedAt.IsZero() && next.SnapshotUpdatedAt.Before(current.SnapshotUpdatedAt) {
		return current, nil
	}
	return next, nil
}

func (s *MemoryStore) savePDLeaseLocked(name string, l PDLease) error {
	if err := validatePDLease(l); err != nil {
		return err
	}
	if s.PDLeases == nil {
		s.PDLeases = make(map[string]PDLease)
	}
	s.PDLeases[name] = copyPDLease(l)
	return nil
}

func (s *MemoryStore) SavePDLease(name string, l PDLease) error {
	s.pdMu.Lock()
	defer s.pdMu.Unlock()
	if current, ok := s.PDLeases[name]; ok {
		var err error
		l, err = mergePDLeaseObservation(current, l)
		if err != nil {
			return err
		}
	}
	return s.savePDLeaseLocked(name, l)
}

func (s *MemoryStore) ConfigurePDLease(name string, config PDLeaseConfig) (PDLease, bool, error) {
	s.pdMu.Lock()
	defer s.pdMu.Unlock()
	l, changed := configurePDLease(s.PDLeases[name], config)
	err := s.savePDLeaseLocked(name, l)
	return copyPDLease(l), changed, err
}

func (s *MemoryStore) SavePDLeaseSnapshot(name string, snapshot pdclient.Snapshot) error {
	s.pdMu.Lock()
	defer s.pdMu.Unlock()
	l := s.PDLeases[name]
	if snapshot.T1Seconds < 0 || snapshot.T1Seconds > 0xffffffff || snapshot.T2Seconds < 0 || snapshot.T2Seconds > 0xffffffff {
		return fmt.Errorf("PD snapshot timers exceed protocol bounds")
	}
	if !samePDLeaseSource(l, name, snapshot) {
		return fmt.Errorf("PD snapshot does not match configured source %s", name)
	}
	if snapshot.UpdatedAt.Before(l.SnapshotUpdatedAt) {
		return nil
	}
	return s.savePDLeaseLocked(name, ObservePDLease(l, snapshot, s.Now()))
}

func (s *SQLiteStore) SavePDLease(name string, l PDLease) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists, err := s.readPDLeaseLocked(name)
	if err != nil {
		return err
	}
	if exists {
		var err error
		l, err = mergePDLeaseObservation(current, l)
		if err != nil {
			return err
		}
	}
	return s.savePDLeaseLocked(name, l)
}

func (s *SQLiteStore) ConfigurePDLease(name string, config PDLeaseConfig) (PDLease, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, _, err := s.readPDLeaseLocked(name)
	if err != nil {
		return PDLease{}, false, err
	}
	l, changed := configurePDLease(l, config)
	err = s.savePDLeaseLocked(name, l)
	return l, changed, err
}

func (s *SQLiteStore) SavePDLeaseSnapshot(name string, snapshot pdclient.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, _, err := s.readPDLeaseLocked(name)
	if err != nil {
		return err
	}
	if snapshot.T1Seconds < 0 || snapshot.T1Seconds > 0xffffffff || snapshot.T2Seconds < 0 || snapshot.T2Seconds > 0xffffffff {
		return fmt.Errorf("PD snapshot timers exceed protocol bounds")
	}
	if !samePDLeaseSource(l, name, snapshot) {
		return fmt.Errorf("PD snapshot does not match configured source %s", name)
	}
	if snapshot.UpdatedAt.Before(l.SnapshotUpdatedAt) {
		return nil
	}
	return s.savePDLeaseLocked(name, ObservePDLease(l, snapshot, s.now().UTC()))
}

func (s *SQLiteStore) ReadPDLease(name string) (PDLease, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readPDLeaseLocked(name)
}

func (s *SQLiteStore) readPDLeaseLocked(name string) (PDLease, bool, error) {
	if s.closed {
		return PDLease{}, false, sql.ErrConnDone
	}
	var raw sql.NullString
	err := s.db.QueryRow(`SELECT json_extract(status,'$.observation') FROM objects WHERE api_version=? AND kind='DHCPv6PrefixDelegation' AND name=?`, "net.routerd.net/v1alpha1", name).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !raw.Valid) {
		return PDLease{}, false, nil
	}
	if err != nil {
		return PDLease{}, false, err
	}
	var l PDLease
	if err := json.Unmarshal([]byte(raw.String), &l); err != nil {
		return PDLease{}, false, fmt.Errorf("decode PD observation %s: %w", name, err)
	}
	if err := validatePDLease(l); err != nil {
		return PDLease{}, false, err
	}
	return l, true, nil
}

func (s *SQLiteStore) savePDLeaseLocked(name string, l PDLease) error {
	if err := validatePDLease(l); err != nil {
		return err
	}
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if s.closed {
		return sql.ErrConnDone
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	ref := objectRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "DHCPv6PrefixDelegation", Name: name}
	_, err = s.db.Exec(`INSERT INTO objects(api_version,kind,name,uid,resource_version,observed_generation,status,created_at,modified_at)
 VALUES(?,?,?,?,1,?,json_object('observation',json(?)),?,?)
 ON CONFLICT(api_version,kind,name) DO UPDATE SET resource_version=resource_version+1,observed_generation=excluded.observed_generation,status=json_set(coalesce(objects.status,'{}'),'$.observation',json(?)),modified_at=excluded.modified_at`, ref.APIVersion, ref.Kind, name, ref.APIVersion+"/"+ref.Kind+"/"+name, nullGeneration(s.generation), string(data), now, now, string(data))
	if err != nil {
		return s.noteStorageWriteErrorLocked(err)
	}
	s.statusWriteCount++
	s.incrementStatusKindWriteLocked(ref.Kind)
	return nil
}

// PDLeaseStatus is a read-only display/condition projection. It is never written
// back as lease authority and does not expose encoded JSON as state variables.
func PDLeaseStatus(l PDLease, now time.Time) map[string]any {
	return pdLeaseStatus(l.Live(now))
}

func pdLeaseStatus(l PDLease) map[string]any {
	previous := make([]any, 0, len(l.PreviousPrefixes))
	for _, prefix := range l.PreviousPrefixes {
		previous = append(previous, map[string]any{"prefix": prefix.Prefix, "expiresAt": prefix.ExpiresAt.UTC().Format(time.RFC3339Nano)})
	}
	timestamp := func(value time.Time) string {
		if value.IsZero() {
			return ""
		}
		return value.UTC().Format(time.RFC3339Nano)
	}
	conditions := make([]any, 0, len(l.Conditions))
	for _, condition := range l.Conditions {
		conditions = append(conditions, map[string]any{"type": condition.Type, "status": condition.Status, "severity": condition.Severity, "reason": condition.Reason, "message": condition.Message, "lastTransitionTime": timestamp(condition.LastTransitionTime), "observedGeneration": condition.ObservedGeneration})
	}
	return map[string]any{"currentPrefix": l.CurrentPrefix, "lastPrefix": l.LastPrefix, "previousPrefixes": previous, "t1Seconds": l.T1Seconds, "t2Seconds": l.T2Seconds, "preferredSeconds": l.PreferredSeconds, "validSeconds": l.ValidSeconds, "lastObservedAt": timestamp(l.LastObservedAt), "lastReplyAt": timestamp(l.LastReplyAt), "leaseUpdatedAt": timestamp(l.LeaseUpdatedAt), "duid": l.DUID, "duidText": l.DUIDText, "serverDUID": l.ServerDUID, "iaid": l.IAID, "expectedDUID": l.ExpectedDUID, "client": l.Client, "profile": l.Profile, "uplinkIfname": l.UplinkIfname, "downstreamIfname": l.DownstreamIfname, "prefixLength": l.PrefixLength, "phase": l.Phase, "health": l.Health, "conditions": conditions}
}

func pdInformationField(key string) bool {
	switch key {
	case "aftrName", "dnsServers", "sntpServers", "domainSearch", "infoPending", "infoStatusCode", "infoLastError", "infoUpdatedAt", "infoRefreshAt":
		return true
	}
	return false
}

// Generic diagnostic strings remain at this existing persistence/status edge.
// Structured DHCP information comes from the typed snapshot, never JSON text
// embedded in the daemon's display map.
func pdDaemonObserved(snapshot pdclient.Snapshot, resource *daemonapi.ResourceStatus) map[string]any {
	observed := map[string]any{}
	if resource != nil {
		for key, value := range resource.Observed {
			if pdObservationField(key) || pdInformationField(key) || key == "phase" || key == "health" || key == "conditions" || key == "leaseState" || key == "interface" || key == "prefixCount" || key == "lastError" {
				continue
			}
			if key == "packetRing" {
				if parsed, err := strconv.Atoi(value); err == nil {
					observed[key] = parsed
				}
				continue
			}
			observed[key] = value
		}
	}
	observed["aftrName"], observed["dnsServers"], observed["sntpServers"], observed["domainSearch"] = snapshot.AFTRName, append([]string(nil), snapshot.DNSServers...), append([]string(nil), snapshot.SNTPServers...), append([]string(nil), snapshot.DomainSearch...)
	observed["infoPending"], observed["infoStatusCode"], observed["infoLastError"] = snapshot.InfoPending, snapshot.InfoStatusCode, snapshot.InfoLastError
	observed["interface"], observed["leaseState"], observed["prefixCount"], observed["lastError"] = snapshot.Interface, string(snapshot.State), len(snapshot.Prefixes), snapshot.LastError
	if !snapshot.InfoUpdatedAt.IsZero() {
		observed["infoUpdatedAt"] = snapshot.InfoUpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !snapshot.InfoRefreshAt.IsZero() {
		observed["infoRefreshAt"] = snapshot.InfoRefreshAt.UTC().Format(time.RFC3339Nano)
	}
	return observed
}

func pdObservationField(key string) bool {
	switch key {
	case "leaseUpdatedAt", "snapshotUpdatedAt", "configuredClientDUID", "configuredIAID", "currentPrefix", "lastPrefix", "previousPrefixes", "prefix", "t1", "t2", "pltime", "vltime", "t1Seconds", "t2Seconds", "preferredSeconds", "validSeconds", "lastObservedAt", "lastReplyAt", "duid", "duidText", "serverDUID", "iaid", "expectedDUID", "client", "profile", "uplinkIfname", "downstreamIfname", "prefixLength":
		return true
	}
	return false
}

func cleanPDStatus(status map[string]any) objectStatus {
	clean := objectStatus{}
	for key, value := range status {
		if key == "_variables" || key == "observation" || pdObservationField(key) || pdInformationField(key) {
			continue
		}
		if key == "observed" {
			observed := map[string]any{}
			switch fields := value.(type) {
			case map[string]any:
				for field, item := range fields {
					if !pdObservationField(field) && field != "phase" && field != "health" && field != "conditions" {
						observed[field] = item
					}
				}
			case map[string]string:
				for field, item := range fields {
					if !pdObservationField(field) && field != "phase" && field != "health" && field != "conditions" {
						observed[field] = item
					}
				}
			}
			if len(observed) > 0 {
				clean[key] = observed
			}
			continue
		}
		clean[key] = value
	}
	return clean
}

func projectPDStatus(status map[string]any, raw string, now time.Time) error {
	var document struct {
		Observation *PDLease `json:"observation"`
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return err
	}
	return projectTypedPDStatus(status, document.Observation, now)
}

func projectTypedPDStatus(status map[string]any, observation *PDLease, now time.Time) error {
	if observation != nil {
		live := observation.Live(now)
		observation = &live
	}
	return projectCommittedPDStatus(status, observation)
}

func projectCommittedPDStatus(status map[string]any, observation *PDLease) error {
	clean := cleanPDStatus(status)
	for key := range status {
		delete(status, key)
	}
	for key, value := range clean {
		status[key] = value
	}
	if observation == nil {
		if status["phase"] == daemonapi.ResourcePhaseBound {
			status["phase"] = daemonapi.ResourcePhasePending
		}
		return nil
	}
	if err := validatePDLease(*observation); err != nil {
		return err
	}
	projection := pdLeaseStatus(*observation)
	if status["reason"] != "WhenFalse" && status["reason"] != "DependsOnFalse" {
		delete(status, "phase")
		delete(status, "health")
		delete(status, "conditions")
	}
	status["observation"] = projection
	// Operational suppression owns the resource's public phase and aliases.
	// The canonical observation remains available for explicit lease reads and
	// bounded standby staging, without promoting a suppressed resource to ready.
	if status["reason"] == "WhenFalse" || status["reason"] == "DependsOnFalse" {
		return nil
	}
	if fields, ok := status["observed"].(map[string]any); ok {
		for key, value := range fields {
			if pdInformationField(key) {
				status[key] = value
			}
		}
	}
	for key, value := range projection {
		if key == "phase" || key == "health" || key == "conditions" {
			if _, exists := status[key]; exists {
				if key != "phase" || status[key] != daemonapi.ResourcePhaseBound || value == daemonapi.ResourcePhaseBound {
					continue
				}
			}
		}
		status[key] = value
	}
	return nil
}
