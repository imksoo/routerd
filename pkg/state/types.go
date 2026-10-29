// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"errors"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	StatusUnknown = "unknown"
	StatusUnset   = "unset"
	StatusSet     = "set"
)

type Value struct {
	Status    string    `json:"status"`
	Value     string    `json:"value,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Since     time.Time `json:"since"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Reader interface {
	Get(name string) Value
	ReadVariable(name string) (Value, error)
	Age(name string) time.Duration
	Now() time.Time
	Variables() map[string]Value
}

// Store returns mutation errors to the operation owner. Persistent stores commit
// before returning a successful value; memory stores explicitly remain transient.
type Store interface {
	Reader
	Set(name, value, reason string) (Value, error)
	Unset(name, reason string) (Value, error)
	Forget(name, reason string) (Value, error)
	Delete(name string) error
}

type GenerationStore interface {
	BeginGeneration(configHash string) (int64, error)
	FinishGeneration(generation int64, phase string, warnings []string) error
	CurrentGeneration() int64
}

type GenerationConfigRecorder interface {
	RecordGenerationConfig(generation int64, configYAML string) error
}

type LatestGenerationReader interface {
	LatestGeneration() int64
}

type GenerationRecord struct {
	Generation int64     `json:"generation" yaml:"generation"`
	StartedAt  time.Time `json:"startedAt" yaml:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty" yaml:"finishedAt,omitempty"`
	Phase      string    `json:"phase,omitempty" yaml:"phase,omitempty"`
	ConfigHash string    `json:"configHash,omitempty" yaml:"configHash,omitempty"`
	HasYAML    bool      `json:"hasYaml" yaml:"hasYaml"`
}

type GenerationHistoryReader interface {
	ListGenerations(limit int) ([]GenerationRecord, error)
	GenerationConfig(generation int64) (string, bool, error)
}

type EventRecorder interface {
	RecordEvent(apiVersion, kind, name, eventType, reason, message string) error
	Events(apiVersion, kind, name string, limit int) []Event
}

type EventQuery struct {
	Limit     int
	SinceID   int64
	Ascending bool
	Topic     string
	Kind      string
	Name      string
	Resource  string
}

type StoredEvent struct {
	ID                 int64          `json:"id" yaml:"id"`
	APIVersion         string         `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind               string         `json:"kind,omitempty" yaml:"kind,omitempty"`
	Name               string         `json:"name,omitempty" yaml:"name,omitempty"`
	Type               string         `json:"type" yaml:"type"`
	Reason             string         `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message            string         `json:"message,omitempty" yaml:"message,omitempty"`
	Generation         int64          `json:"generation,omitempty" yaml:"generation,omitempty"`
	CreatedAt          time.Time      `json:"createdAt" yaml:"createdAt"`
	Topic              string         `json:"topic,omitempty" yaml:"topic,omitempty"`
	SourceKind         string         `json:"sourceKind,omitempty" yaml:"sourceKind,omitempty"`
	SourceInstance     string         `json:"sourceInstance,omitempty" yaml:"sourceInstance,omitempty"`
	ResourceAPIVersion string         `json:"resourceApiVersion,omitempty" yaml:"resourceApiVersion,omitempty"`
	ResourceKind       string         `json:"resourceKind,omitempty" yaml:"resourceKind,omitempty"`
	ResourceName       string         `json:"resourceName,omitempty" yaml:"resourceName,omitempty"`
	Severity           string         `json:"severity,omitempty" yaml:"severity,omitempty"`
	Attributes         map[string]any `json:"attributes,omitempty" yaml:"attributes,omitempty"`
}

type EventLister interface {
	ListEvents(query EventQuery) ([]StoredEvent, error)
}

type EventConsumerCursorStore interface {
	LoadOrInitializeEventConsumerCursor(consumer string) (int64, error)
	SaveEventConsumerCursor(consumer string, cursor int64) error
}

// FederationEventStore is the cross-node Event Federation store surface
// (ADR 0006), distinct from EventLister (observability events).
type FederationEventStore interface {
	RecordFederationEvent(rec EventRecord) error
	ListFederationEvents(group string, includeExpired bool, now int64) ([]EventRecord, error)
}

// FederationDeliveryStore is the read surface for per-(event,peer) delivery
// tracking in the event_deliveries table (ADR 0006, Phase 2).
type FederationDeliveryStore interface {
	ListDeliveries(eventID, peer string) ([]DeliveryRecord, error)
	ListDeliveriesFiltered(group, eventID, peer, status string) ([]DeliveryRecord, error)
}

// FederationDeliverySummaryStore provides aggregated per-(group, peer) delivery
// statistics for observability and doctor checks. Read-only.
type FederationDeliverySummaryStore interface {
	ListDeliverySummary(group, peer, eventType string, includeExpired bool, now time.Time) ([]DeliverySummaryRow, error)
}

// SubscriptionRunStore provides read access to EventSubscription processing
// records (event_subscription_runs table, ADR 0006 Phase 3).
type SubscriptionRunStore interface {
	ListSubscriptionRuns(subscription string) ([]SubscriptionRun, error)
}

type DynamicConfigPartLister interface {
	ListDynamicConfigParts() ([]DynamicConfigPartRecord, error)
	GetDynamicConfigPartsBySource(source string) ([]DynamicConfigPartRecord, error)
}

type PluginRunLister interface {
	ListPluginRuns(plugin string) ([]PluginRunRecord, error)
}

type ObjectGenerationReader interface {
	ObjectGeneration(apiVersion, kind, name string) int64
}

type ObjectStatusStore interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type ObjectStatus struct {
	APIVersion string         `json:"apiVersion" yaml:"apiVersion"`
	Kind       string         `json:"kind" yaml:"kind"`
	Name       string         `json:"name" yaml:"name"`
	Owner      string         `json:"owner,omitempty" yaml:"owner,omitempty"`
	ManagedBy  string         `json:"managedBy,omitempty" yaml:"managedBy,omitempty"`
	Management string         `json:"management,omitempty" yaml:"management,omitempty"`
	Status     map[string]any `json:"status" yaml:"status"`
}

type ObjectStatusLister interface {
	ListObjectStatuses() ([]ObjectStatus, error)
}

type ObjectDeleteStore interface {
	DeleteObject(apiVersion, kind, name string) error
}

type ObjectApplySourceStore interface {
	SaveObjectApplySource(apiVersion, kind, name, path string) error
	ObjectApplySource(apiVersion, kind, name string) string
}

type Event struct {
	ID         int64     `json:"id" yaml:"id"`
	APIVersion string    `json:"apiVersion" yaml:"apiVersion"`
	Kind       string    `json:"kind" yaml:"kind"`
	Name       string    `json:"name" yaml:"name"`
	Type       string    `json:"type" yaml:"type"`
	Reason     string    `json:"reason" yaml:"reason"`
	Message    string    `json:"message" yaml:"message"`
	Generation int64     `json:"generation" yaml:"generation"`
	CreatedAt  time.Time `json:"createdAt" yaml:"createdAt"`
}

type MemoryStore struct {
	Values   map[string]Value `json:"variables"`
	pdMu     sync.RWMutex
	PDLeases map[string]PDLease `json:"pdLeases,omitempty"`
	now      func() time.Time
}

func New() Store {
	return NewMemory()
}

func NewMemory() *MemoryStore {
	return &MemoryStore{Values: map[string]Value{}, now: time.Now}
}

func Load(path string) (Store, error) {
	return Open(path)
}

func LoadReadOnly(path string) (Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return NewMemory(), nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return NewMemory(), nil
	} else if err != nil {
		return nil, err
	}
	return OpenSQLiteReadOnlyImmutable(path)
}

// LoadReadOnlyLive opens SQLite read-only without immutable=1 so a live WAL
// is visible. Callers snapshotting an active routerd state database use this;
// LoadReadOnly retains immutable semantics for quiescent artifact directories.
func LoadReadOnlyLive(path string) (Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return NewMemory(), nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return NewMemory(), nil
	} else if err != nil {
		return nil, err
	}
	return OpenSQLiteReadOnly(path)
}

func Open(path string) (Store, error) {
	if path == "" {
		return NewMemory(), nil
	}
	return OpenSQLite(path)
}

func (s *MemoryStore) Get(name string) Value {
	value, ok := s.Values[name]
	if !ok || value.Status == "" {
		now := s.now().UTC()
		return Value{Status: StatusUnknown, Since: now, UpdatedAt: now}
	}
	return value
}

func (s *MemoryStore) ReadVariable(name string) (Value, error) {
	return s.Get(name), nil
}

// MemoryStore contains variables only; an object status is an absent observation.
func (s *MemoryStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	if apiVersion == "net.routerd.net/v1alpha1" && kind == "DHCPv6PrefixDelegation" {
		if l, ok, err := s.ReadPDLease(name); err == nil && ok {
			projection := PDLeaseStatus(l, s.Now())
			projection["observation"] = PDLeaseStatus(l, s.Now())
			return projection, nil
		}
	}
	return nil, nil
}

func (s *MemoryStore) Set(name, value, reason string) (Value, error) {
	if value == "" {
		return s.Unset(name, reason)
	}
	return s.set(name, StatusSet, value, reason)
}

func (s *MemoryStore) Unset(name, reason string) (Value, error) {
	return s.set(name, StatusUnset, "", reason)
}

func (s *MemoryStore) Forget(name, reason string) (Value, error) {
	return s.set(name, StatusUnknown, "", reason)
}

func (s *MemoryStore) Delete(name string) error {
	delete(s.Values, name)
	return nil
}

func (s *MemoryStore) set(name, status, value, reason string) (Value, error) {
	now := s.now().UTC()
	current := s.Get(name)
	since := current.Since
	if current.Status != status || current.Value != value {
		since = now
	}
	next := Value{Status: status, Value: value, Reason: reason, Since: since, UpdatedAt: now}
	s.Values[name] = next
	return next, nil
}

func (s *MemoryStore) Age(name string) time.Duration {
	return s.now().UTC().Sub(s.Get(name).Since)
}

func (s *MemoryStore) Now() time.Time {
	return s.now().UTC()
}

func (s *MemoryStore) Variables() map[string]Value {
	out := map[string]Value{}
	for key, value := range s.Values {
		out[key] = value
	}
	return out
}

// Transient PD observations use the same read-only projection as persisted rows.
func (s *MemoryStore) ListObjectStatuses() ([]ObjectStatus, error) {
	s.pdMu.RLock()
	defer s.pdMu.RUnlock()
	var out []ObjectStatus
	for name, lease := range s.PDLeases {
		out = append(out, ObjectStatus{APIVersion: "net.routerd.net/v1alpha1", Kind: "DHCPv6PrefixDelegation", Name: name, Status: PDLeaseStatus(lease, s.Now())})
	}
	return out, nil
}

func (s *MemoryStore) DeleteObject(apiVersion, kind, name string) error {
	if apiVersion == "net.routerd.net/v1alpha1" && kind == "DHCPv6PrefixDelegation" {
		s.pdMu.Lock()
		delete(s.PDLeases, name)
		s.pdMu.Unlock()
	}
	for key := range s.Values {
		ref := objectRefForKey(key)
		if ref.APIVersion == apiVersion && ref.Kind == kind && ref.Name == name {
			delete(s.Values, key)
		}
	}
	return nil
}
