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
	store.Set(Key(service, backend), string(data), "IngressServiceBackendDrained")
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
	store.Delete(Key(service, backend))
	return nil
}

func Current(store routerstate.Store, service, backend string) (State, bool) {
	if store == nil {
		return State{}, false
	}
	value := store.Get(Key(service, backend))
	if value.Status != routerstate.StatusSet || strings.TrimSpace(value.Value) == "" {
		return State{}, false
	}
	var state State
	if err := json.Unmarshal([]byte(value.Value), &state); err != nil {
		return State{}, false
	}
	if state.Service == "" {
		state.Service = service
	}
	if state.Backend == "" {
		state.Backend = backend
	}
	if expired(store.Now().UTC(), state.DrainedUntil) {
		store.Delete(Key(service, backend))
		return State{}, false
	}
	return state, true
}

func expired(now time.Time, until string) bool {
	if strings.TrimSpace(until) == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, until)
	if err != nil {
		return false
	}
	return !now.Before(parsed)
}
