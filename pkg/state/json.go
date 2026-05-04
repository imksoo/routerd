package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

type Store interface {
	Get(name string) Value
	Set(name, value, reason string) Value
	Unset(name, reason string) Value
	Forget(name, reason string) Value
	Delete(name string)
	Age(name string) time.Duration
	Now() time.Time
	Save(path string) error
	Variables() map[string]Value
}

type GenerationStore interface {
	BeginGeneration(configHash string) (int64, error)
	FinishGeneration(generation int64, phase string, warnings []string) error
	CurrentGeneration() int64
}

type EventRecorder interface {
	RecordEvent(apiVersion, kind, name, eventType, reason, message string) error
	Events(apiVersion, kind, name string, limit int) []Event
}

type EventQuery struct {
	Limit    int
	SinceID  int64
	Topic    string
	Kind     string
	Name     string
	Resource string
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

type ObjectGenerationReader interface {
	ObjectGeneration(apiVersion, kind, name string) int64
}

type ObjectStatusStore interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type ObjectStatus struct {
	APIVersion string         `json:"apiVersion" yaml:"apiVersion"`
	Kind       string         `json:"kind" yaml:"kind"`
	Name       string         `json:"name" yaml:"name"`
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

type JSONStore struct {
	Values map[string]Value `json:"variables"`
	now    func() time.Time
}

func New() Store {
	return NewJSON()
}

func NewJSON() *JSONStore {
	return &JSONStore{Values: map[string]Value{}, now: time.Now}
}

func Load(path string) (Store, error) {
	return Open(path)
}

func LoadJSON(path string) (*JSONStore, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewJSON(), nil
	}
	if err != nil {
		return nil, err
	}
	store := NewJSON()
	if err := json.Unmarshal(data, store); err != nil {
		return nil, err
	}
	if store.Values == nil {
		store.Values = map[string]Value{}
	}
	return store, nil
}

func Open(path string) (Store, error) {
	if path == "" {
		return NewJSON(), nil
	}
	if filepath.Ext(path) == ".json" {
		return LoadJSON(path)
	}
	return OpenSQLite(path)
}

func (s *JSONStore) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func (s *JSONStore) Get(name string) Value {
	value, ok := s.Values[name]
	if !ok || value.Status == "" {
		now := s.now().UTC()
		return Value{Status: StatusUnknown, Since: now, UpdatedAt: now}
	}
	return value
}

func (s *JSONStore) Set(name, value, reason string) Value {
	if value == "" {
		return s.Unset(name, reason)
	}
	return s.set(name, StatusSet, value, reason)
}

func (s *JSONStore) Unset(name, reason string) Value {
	return s.set(name, StatusUnset, "", reason)
}

func (s *JSONStore) Forget(name, reason string) Value {
	return s.set(name, StatusUnknown, "", reason)
}

func (s *JSONStore) Delete(name string) {
	delete(s.Values, name)
}

func (s *JSONStore) set(name, status, value, reason string) Value {
	now := s.now().UTC()
	current := s.Get(name)
	since := current.Since
	if current.Status != status || current.Value != value {
		since = now
	}
	next := Value{Status: status, Value: value, Reason: reason, Since: since, UpdatedAt: now}
	s.Values[name] = next
	return next
}

func (s *JSONStore) Age(name string) time.Duration {
	return s.now().UTC().Sub(s.Get(name).Since)
}

func (s *JSONStore) Now() time.Time {
	return s.now().UTC()
}

func (s *JSONStore) Variables() map[string]Value {
	out := map[string]Value{}
	for key, value := range s.Values {
		out[key] = value
	}
	return out
}

func (s *JSONStore) DeleteObject(apiVersion, kind, name string) error {
	for key := range s.Values {
		ref := objectRefForKey(key)
		if ref.APIVersion == apiVersion && ref.Kind == kind && ref.Name == name {
			delete(s.Values, key)
		}
	}
	return nil
}
