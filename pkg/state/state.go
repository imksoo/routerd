package state

import (
	"encoding/json"
	"errors"
	"os"
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

type Store struct {
	Variables map[string]Value `json:"variables"`
	now       func() time.Time
}

func New() *Store {
	return &Store{Variables: map[string]Value{}, now: time.Now}
}

func Load(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}
	store := New()
	if err := json.Unmarshal(data, store); err != nil {
		return nil, err
	}
	if store.Variables == nil {
		store.Variables = map[string]Value{}
	}
	return store, nil
}

func (s *Store) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func (s *Store) Get(name string) Value {
	value, ok := s.Variables[name]
	if !ok || value.Status == "" {
		now := s.now().UTC()
		return Value{Status: StatusUnknown, Since: now, UpdatedAt: now}
	}
	return value
}

func (s *Store) Set(name, value, reason string) Value {
	if value == "" {
		return s.Unset(name, reason)
	}
	return s.set(name, StatusSet, value, reason)
}

func (s *Store) Unset(name, reason string) Value {
	return s.set(name, StatusUnset, "", reason)
}

func (s *Store) Forget(name, reason string) Value {
	return s.set(name, StatusUnknown, "", reason)
}

func (s *Store) set(name, status, value, reason string) Value {
	now := s.now().UTC()
	current := s.Get(name)
	since := current.Since
	if current.Status != status || current.Value != value {
		since = now
	}
	next := Value{Status: status, Value: value, Reason: reason, Since: since, UpdatedAt: now}
	s.Variables[name] = next
	return next
}

func (s *Store) Age(name string) time.Duration {
	return s.now().UTC().Sub(s.Get(name).Since)
}

func (s *Store) Now() time.Time {
	return s.now().UTC()
}
