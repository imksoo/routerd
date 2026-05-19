// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"sort"
	"sync"
	"time"
)

type ControllerRuntimeStore struct {
	mu          sync.RWMutex
	controllers map[string]ControllerStatus
}

func NewControllerRuntimeStore(base []ControllerStatus) *ControllerRuntimeStore {
	store := &ControllerRuntimeStore{controllers: map[string]ControllerStatus{}}
	store.SetBase(base)
	return store
}

func (s *ControllerRuntimeStore) SetBase(base []ControllerStatus) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.controllers == nil {
		s.controllers = map[string]ControllerStatus{}
	}
	for _, controller := range base {
		current := s.controllers[controller.Name]
		controller.Interval = current.Interval
		controller.LastTrigger = current.LastTrigger
		controller.LastReconcileTime = current.LastReconcileTime
		controller.LastSuccessTime = current.LastSuccessTime
		controller.LastReloadAt = current.LastReloadAt
		controller.LastRestartAt = current.LastRestartAt
		controller.LastChangeReason = current.LastChangeReason
		controller.NextReconcileTime = current.NextReconcileTime
		controller.ReconcileCount = current.ReconcileCount
		controller.ReconcileErrorCount = current.ReconcileErrorCount
		controller.LastDuration = current.LastDuration
		controller.MaxDuration = current.MaxDuration
		controller.AverageDuration = current.AverageDuration
		controller.LastDurationMillis = current.LastDurationMillis
		controller.MaxDurationMillis = current.MaxDurationMillis
		controller.AverageDurationMillis = current.AverageDurationMillis
		controller.LastError = current.LastError
		s.controllers[controller.Name] = controller
	}
}

func (s *ControllerRuntimeStore) Snapshot() []ControllerStatus {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ControllerStatus, 0, len(s.controllers))
	for _, controller := range s.controllers {
		out = append(out, controller)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *ControllerRuntimeStore) ControllerStarted(name string, interval time.Duration) {
	if s == nil || name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	controller := s.controllerLocked(name)
	controller.Interval = interval.String()
	s.controllers[name] = controller
}

func (s *ControllerRuntimeStore) ControllerReconciled(name, trigger string, interval, duration time.Duration, err error) {
	if s == nil || name == "" {
		return
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	controller := s.controllerLocked(name)
	controller.Interval = interval.String()
	controller.LastTrigger = trigger
	controller.LastReconcileTime = ptrTime(now)
	controller.NextReconcileTime = ptrTime(now.Add(interval))
	controller.ReconcileCount++
	controller.LastDuration = duration.String()
	controller.LastDurationMillis = durationMillis(duration)
	if duration > durationFromMillis(controller.MaxDurationMillis) {
		controller.MaxDuration = duration.String()
		controller.MaxDurationMillis = durationMillis(duration)
	}
	previousTotal := controller.AverageDurationMillis * float64(controller.ReconcileCount-1)
	controller.AverageDurationMillis = (previousTotal + durationMillis(duration)) / float64(controller.ReconcileCount)
	controller.AverageDuration = durationFromMillis(controller.AverageDurationMillis).String()
	if err != nil {
		controller.ReconcileErrorCount++
		controller.LastError = err.Error()
	} else {
		controller.LastError = ""
		controller.LastSuccessTime = ptrTime(now)
	}
	s.controllers[name] = controller
}

func (s *ControllerRuntimeStore) controllerLocked(name string) ControllerStatus {
	if s.controllers == nil {
		s.controllers = map[string]ControllerStatus{}
	}
	controller := s.controllers[name]
	if controller.Name == "" {
		controller.Name = name
		controller.Mode = "unknown"
		controller.Reason = ControllerModeReasonUnknown
	}
	return controller
}

func durationMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func durationFromMillis(value float64) time.Duration {
	return time.Duration(value * float64(time.Millisecond))
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
