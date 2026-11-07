// SPDX-License-Identifier: BSD-3-Clause

package ingressdrain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

const keyPrefix = "ingressDrain."

type Reader interface {
	ReadVariable(name string) (routerstate.Value, error)
	Now() time.Time
}

type State struct {
	Service      string `json:"service" yaml:"service"`
	Backend      string `json:"backend" yaml:"backend"`
	DrainedAt    string `json:"drainedAt" yaml:"drainedAt"`
	DrainedUntil string `json:"drainedUntil,omitempty" yaml:"drainedUntil,omitempty"`
	Reason       string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

func Key(service, backend string) string {
	return keyPrefix + strings.TrimSpace(service) + "." + strings.TrimSpace(backend)
}

func Drain(store routerstate.Store, service, backend string, duration time.Duration) (State, error) {
	if store == nil {
		return State{}, fmt.Errorf("state store is required")
	}
	service = strings.TrimSpace(service)
	backend = strings.TrimSpace(backend)
	if service == "" || backend == "" {
		return State{}, fmt.Errorf("service and backend are required")
	}
	now := store.Now().UTC()
	state := State{
		Service:   service,
		Backend:   backend,
		DrainedAt: now.Format(time.RFC3339Nano),
		Reason:    "ManualDrain",
	}
	if duration > 0 {
		state.DrainedUntil = now.Add(duration).Format(time.RFC3339Nano)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return State{}, err
	}
	if _, err := store.Set(Key(service, backend), string(data), "IngressServiceBackendDrained"); err != nil {
		return State{}, err
	}
	return state, nil
}

func Undrain(store routerstate.Store, service, backend string) error {
	if store == nil {
		return fmt.Errorf("state store is required")
	}
	service = strings.TrimSpace(service)
	backend = strings.TrimSpace(backend)
	if service == "" || backend == "" {
		return fmt.Errorf("service and backend are required")
	}
	return store.Delete(Key(service, backend))
}

// Current evaluates expiry without changing the operator-owned drain record.
func Current(store Reader, service, backend string) (State, bool, error) {
	if store == nil {
		return State{}, false, fmt.Errorf("drain observation store is required")
	}
	value, err := store.ReadVariable(Key(service, backend))
	if err != nil {
		return State{}, false, err
	}
	if value.Status != routerstate.StatusSet {
		return State{}, false, nil
	}
	var state State
	if err := json.Unmarshal([]byte(value.Value), &state); err != nil {
		return State{}, false, fmt.Errorf("decode drain observation: %w", err)
	}
	if state.Service != strings.TrimSpace(service) || state.Backend != strings.TrimSpace(backend) {
		return State{}, false, fmt.Errorf("drain observation identity does not match requested backend")
	}
	if strings.TrimSpace(state.DrainedUntil) == "" {
		return state, true, nil
	}
	until, err := time.Parse(time.RFC3339Nano, state.DrainedUntil)
	if err != nil {
		return State{}, false, fmt.Errorf("decode drain deadline: %w", err)
	}
	if !store.Now().UTC().Before(until) {
		return State{}, false, nil
	}
	return state, true, nil
}
