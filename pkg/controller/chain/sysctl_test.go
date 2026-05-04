package chain

import (
	"context"
	"strings"
	"testing"

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
)

func TestSysctlControllerAppliesRuntimeValue(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "ipv4-forwarding"}, Spec: api.SysctlSpec{
			Key:     "net.ipv4.ip_forward",
			Value:   "1",
			Runtime: boolPtr(true),
		}},
	}}}
	store := mapStore{}
	values := map[string]string{"net.ipv4.ip_forward": "0"}
	var commands []string
	controller := SysctlController{
		Router: router,
		Store:  store,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "sysctl" && len(args) == 2 && args[0] == "-n" {
				return []byte(values[args[1]] + "\n"), nil
			}
			if name == "sysctl" && len(args) == 2 && args[0] == "-w" {
				parts := strings.SplitN(args[1], "=", 2)
				values[parts[0]] = parts[1]
				return []byte(args[1] + "\n"), nil
			}
			t.Fatalf("unexpected command %s %v", name, args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(commands, "\n"); !strings.Contains(got, "sysctl -w net.ipv4.ip_forward=1") {
		t.Fatalf("commands = %q", got)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "Sysctl", "ipv4-forwarding")
	if status["phase"] != "Applied" || status["currentValue"] != "1" || status["changed"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func TestSysctlControllerSkipsRuntimeDisabled(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "persistent-only"}, Spec: api.SysctlSpec{
			Key:     "net.ipv4.ip_forward",
			Value:   "1",
			Runtime: boolPtr(false),
		}},
	}}}
	store := mapStore{}
	controller := SysctlController{
		Router: router,
		Store:  store,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			t.Fatalf("command should not run for runtime=false")
			return nil, nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "Sysctl", "persistent-only")
	if status["phase"] != "Skipped" || status["reason"] != "RuntimeDisabled" {
		t.Fatalf("status = %#v", status)
	}
}

func TestSysctlControllerAppliesRouterProfileAndSkipsOptional(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "SysctlProfile"}, Metadata: api.ObjectMeta{Name: "router-runtime"}, Spec: api.SysctlProfileSpec{
			Profile: "router-linux",
			Runtime: boolPtr(true),
			Overrides: map[string]string{
				"net.netfilter.nf_conntrack_max": "524288",
			},
		}},
	}}}
	store := mapStore{}
	values := map[string]string{}
	var commands []string
	controller := SysctlController{
		Router: router,
		Store:  store,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "sysctl" && len(args) == 2 && args[0] == "-n" {
				if args[1] == "net.netfilter.nf_conntrack_buckets" {
					return nil, errTestCommand
				}
				return []byte(values[args[1]] + "\n"), nil
			}
			if name == "sysctl" && len(args) == 2 && args[0] == "-w" {
				parts := strings.SplitN(args[1], "=", 2)
				values[parts[0]] = parts[1]
				return []byte(args[1] + "\n"), nil
			}
			t.Fatalf("unexpected command %s %v", name, args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(commands, "\n")
	if !strings.Contains(got, "sysctl -w net.netfilter.nf_conntrack_max=524288") {
		t.Fatalf("commands = %q", got)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "SysctlProfile", "router-runtime")
	if status["phase"] != "Degraded" {
		t.Fatalf("status = %#v", status)
	}
}

func TestSysctlControllerTreatsWhitespaceEquivalentValuesAsUnchanged(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "tcp-rmem"}, Spec: api.SysctlSpec{
			Key:     "net.ipv4.tcp_rmem",
			Value:   "4096 87380 16777216",
			Runtime: boolPtr(true),
		}},
	}}}
	store := mapStore{}
	bus := &recordingBus{}
	var commands []string
	controller := SysctlController{
		Router: router,
		Store:  store,
		Bus:    bus,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "sysctl" && len(args) == 2 && args[0] == "-n" {
				return []byte("4096\t87380\t16777216\n"), nil
			}
			if name == "sysctl" && len(args) == 2 && args[0] == "-w" {
				t.Fatalf("sysctl -w should not run for equivalent values")
			}
			t.Fatalf("unexpected command %s %v", name, args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(commands, "\n")
	if strings.Contains(got, "sysctl -w") {
		t.Fatalf("commands = %q", got)
	}
	if len(bus.events) != 0 {
		t.Fatalf("events = %#v, want none", bus.events)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "Sysctl", "tcp-rmem")
	if status["phase"] != "Applied" || status["changed"] != false {
		t.Fatalf("status = %#v", status)
	}
}

func TestSysctlControllerAcceptsAtLeastRoundedValues(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "rmem-max"}, Spec: api.SysctlSpec{
			Key:     "net.core.rmem_max",
			Value:   "16777216",
			Compare: "atLeast",
			Runtime: boolPtr(true),
		}},
	}}}
	store := mapStore{}
	var commands []string
	controller := SysctlController{
		Router: router,
		Store:  store,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "sysctl" && len(args) == 2 && args[0] == "-n" {
				return []byte("33554432\n"), nil
			}
			if name == "sysctl" && len(args) == 2 && args[0] == "-w" {
				t.Fatalf("sysctl -w should not run when current value is above minimum")
			}
			t.Fatalf("unexpected command %s %v", name, args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(commands, "\n"); strings.Contains(got, "sysctl -w") {
		t.Fatalf("commands = %q", got)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "Sysctl", "rmem-max")
	if status["phase"] != "Applied" || status["changed"] != false || status["compare"] != "atLeast" {
		t.Fatalf("status = %#v", status)
	}
}

func TestSysctlControllerAppliesAtLeastWhenBelowMinimum(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: "rmem-max"}, Spec: api.SysctlSpec{
			Key:     "net.core.rmem_max",
			Value:   "16777216",
			Compare: "atLeast",
			Runtime: boolPtr(true),
		}},
	}}}
	store := mapStore{}
	values := map[string]string{"net.core.rmem_max": "212992"}
	var commands []string
	controller := SysctlController{
		Router: router,
		Store:  store,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "sysctl" && len(args) == 2 && args[0] == "-n" {
				return []byte(values[args[1]] + "\n"), nil
			}
			if name == "sysctl" && len(args) == 2 && args[0] == "-w" {
				parts := strings.SplitN(args[1], "=", 2)
				values[parts[0]] = parts[1]
				return []byte(args[1] + "\n"), nil
			}
			t.Fatalf("unexpected command %s %v", name, args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(commands, "\n"); !strings.Contains(got, "sysctl -w net.core.rmem_max=16777216") {
		t.Fatalf("commands = %q", got)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "Sysctl", "rmem-max")
	if status["phase"] != "Applied" || status["changed"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func boolPtr(v bool) *bool {
	return &v
}

type recordingBus struct {
	events []daemonapi.DaemonEvent
}

func (b *recordingBus) Publish(ctx context.Context, event daemonapi.DaemonEvent) error {
	_ = ctx
	b.events = append(b.events, event)
	return nil
}
