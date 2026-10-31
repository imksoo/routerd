// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	daemonsource "github.com/imksoo/routerd/pkg/source/daemon"
)

type daemonEventSourceSpec struct {
	Daemon      daemonapi.DaemonRef
	Socket      string
	Replay      bool
	PublishTail bool
}

type daemonEventSourceState struct {
	Spec   daemonEventSourceSpec
	Cancel context.CancelFunc
	Done   <-chan struct{}
}

func daemonEventSourceKey(kind, resource string) string { return kind + "/" + resource }

func (r *Runner) daemonEventSourceSpecs(router *api.Router, observers []mobilityARPObserverDaemonSpec) (map[string]daemonEventSourceSpec, error) {
	workers, err := observedDHCPv6Workers(router, r.Store)
	if err != nil {
		return nil, err
	}
	specs := map[string]daemonEventSourceSpec{}
	for _, resource := range router.Spec.Resources {
		binary := ""
		publishTail := false
		switch resource.Kind {
		case "DHCPv4Client":
			binary = "routerd-dhcpv4-client"
		case "HealthCheck":
			spec, err := resource.HealthCheckSpec()
			if err != nil || healthCheckDisabled(spec) {
				continue
			}
			binary = "routerd-healthcheck"
		case "PPPoESession":
			binary = "routerd-pppoe-client"
		}
		if binary == "" {
			continue
		}
		name := resource.Metadata.Name
		socket := r.Opts.DaemonSockets[name]
		if socket == "" {
			socket = defaultClientSocket(binary, name)
		}
		specs[daemonEventSourceKey(binary, name)] = daemonEventSourceSpec{Daemon: daemonapi.DaemonRef{Name: binary + "-" + name, Kind: binary, Instance: name}, Socket: socket, PublishTail: publishTail}
	}
	for _, worker := range workers {
		name, binary := worker.Name, "routerd-dhcpv6-client"
		specs[daemonEventSourceKey(binary, name)] = daemonEventSourceSpec{Daemon: daemonapi.DaemonRef{Name: binary + "-" + name, Kind: binary, Instance: name}, Socket: worker.socket(r.Opts.DaemonSockets), PublishTail: true}
	}
	for _, observer := range observers {
		if r.arpObserverReady(observer.ResourceName) {
			name := observer.ResourceName
			specs[daemonEventSourceKey("routerd-arp-observer", name)] = daemonEventSourceSpec{Daemon: daemonapi.DaemonRef{Name: "routerd-arp-observer-" + name, Kind: "routerd-arp-observer", Instance: name}, Socket: observer.Socket, Replay: true}
		}
	}
	return specs, nil
}

func (r *Runner) refreshDaemonEventSources(ctx context.Context) error {
	prepared, err := r.currentPreparedRouter()
	if err != nil {
		return err
	}
	observers, err := r.mobilityARPObserverDaemonSpecs()
	if err != nil {
		return err
	}
	return r.reconcileDaemonEventSources(ctx, prepared.declared, observers)
}

// Unchanged readers keep their daemon cursor through controller reloads.
// Only a complete desired-source observation authorizes reader replacement.
func (r *Runner) reconcileDaemonEventSources(ctx context.Context, router *api.Router, observers []mobilityARPObserverDaemonSpec) error {
	desired, err := r.daemonEventSourceSpecs(router, observers)
	if err != nil {
		return err
	}
	r.daemonSourceMu.Lock()
	defer r.daemonSourceMu.Unlock()
	if !r.daemonSourcesEnabled {
		return nil
	}
	for key, state := range r.daemonSources {
		if spec, wanted := desired[key]; wanted && spec == state.Spec {
			select {
			case <-state.Done:
			default:
				delete(desired, key)
				continue
			}
		}
		state.Cancel()
		select {
		case <-state.Done:
			delete(r.daemonSources, key)
		case <-ctx.Done():
			return fmt.Errorf("stop daemon event source %s: %w", key, ctx.Err())
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.daemonSources == nil {
		r.daemonSources = map[string]daemonEventSourceState{}
	}
	for key, spec := range desired {
		ownerCtx, cancel := context.WithCancel(r.clientDaemonContext)
		done := make(chan struct{})
		source := daemonsource.DaemonSource{Daemon: spec.Daemon, Socket: spec.Socket, Publisher: r.Bus, Replay: spec.Replay, PublishTail: spec.PublishTail}
		r.daemonSources[key] = daemonEventSourceState{Spec: spec, Cancel: cancel, Done: done}
		go func() {
			defer close(done)
			if err := source.Run(ownerCtx); err != nil && ownerCtx.Err() == nil {
				logger := r.Opts.Logger
				if logger == nil {
					logger = slog.Default()
				}
				logger.Warn("daemon event source stopped", "daemon", spec.Daemon.Kind, "resource", spec.Daemon.Instance, "error", err)
			}
		}()
	}
	return nil
}

func (r *Runner) stopDaemonEventSources() {
	r.daemonSourceMu.Lock()
	defer r.daemonSourceMu.Unlock()
	r.daemonSourcesEnabled = false
	for _, state := range r.daemonSources {
		state.Cancel()
	}
	for key, state := range r.daemonSources {
		<-state.Done
		delete(r.daemonSources, key)
	}
}

func (r *Runner) pauseDaemonEventSource(kind, resource string) {
	r.daemonSourceMu.Lock()
	defer r.daemonSourceMu.Unlock()
	key := daemonEventSourceKey(kind, resource)
	if state, found := r.daemonSources[key]; found {
		state.Cancel()
		<-state.Done
		delete(r.daemonSources, key)
	}
}

// Client supervisors belong to serve, not to individual controller generations:
// unchanged clients must survive a warm generation replacement. On final exit,
// cancellation is asynchronous; Done includes exec.Wait for directly spawned
// children. Only this Runner's in-memory supervisors are canceled and joined.
// Systemd-managed units and processes outside these states remain untouched.
func (r *Runner) stopSupervisedClientDaemons() {
	r.supervisedMu.Lock()
	states := make([]supervisedDaemonState, 0, len(r.clientDaemonStates))
	for _, state := range r.clientDaemonStates {
		if state.Cancel != nil {
			state.Cancel()
		}
		states = append(states, state)
	}
	r.supervisedMu.Unlock()
	for _, state := range states {
		<-state.Done
		r.supervisedMu.Lock()
		key := supervisedDaemonKey(state.Spec.Binary, state.Spec.ResourceName)
		if current, found := r.clientDaemonStates[key]; found && current.Done == state.Done {
			delete(r.clientDaemonStates, key)
		}
		r.supervisedMu.Unlock()
	}
}

// Wait acknowledges actual controller-generation, supervised-client and event-source exit.
func (r *Runner) Wait(ctx context.Context) error {
	r.reloadMu.Lock()
	var done <-chan struct{} = r.runtimeDone
	if done == nil {
		done = r.runtimeCleanupDone
	}
	r.reloadMu.Unlock()
	if done == nil {
		return nil // Start did not establish a running runtime.
	}
	select {
	case <-done:
		r.reloadMu.Lock()
		defer r.reloadMu.Unlock()
		return r.runtimeExitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) waitControllerCleanup() {
	r.reloadMu.Lock()
	done := r.runtimeCleanupDone
	r.reloadMu.Unlock()
	if done != nil {
		<-done
	}
}
