package chain

import (
	"context"
	"strings"
	"testing"

	"routerd/pkg/api"
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

func boolPtr(v bool) *bool {
	return &v
}
