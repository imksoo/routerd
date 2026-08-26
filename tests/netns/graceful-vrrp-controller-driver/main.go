// SPDX-License-Identifier: BSD-3-Clause

// graceful-vrrp-controller-driver exercises the real Linux address lifecycle
// from the VRRP controller. It refuses to run outside an explicitly prepared
// network namespace test.
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/vrrp"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type statusStore struct {
	statuses map[string]map[string]any
	ready    bool
	now      time.Time
}

func (s *statusStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s.statuses[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s *statusStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	if status := s.statuses[apiVersion+"/"+kind+"/"+name]; status != nil {
		return status
	}
	return map[string]any{}
}

func (s *statusStore) Get(name string) routerstate.Value {
	if name == "DSLiteTunnel/dslite-ready.phase" && s.ready {
		return routerstate.Value{Status: routerstate.StatusSet, Value: "Up", Since: s.now, UpdatedAt: s.now}
	}
	return routerstate.Value{Status: routerstate.StatusUnknown, Since: s.now, UpdatedAt: s.now}
}

func (s *statusStore) Age(string) time.Duration { return 0 }
func (s *statusStore) Now() time.Time           { return s.now }

func main() {
	if os.Getenv("ROUTERD_NETNS_TEST_TOKEN") == "" {
		fatal(errors.New("ROUTERD_NETNS_TEST_TOKEN is required"))
	}
	if len(os.Args) != 4 {
		fatal(errors.New("usage: graceful-vrrp-controller-driver <runtime-dir> <config-path> <ifname>"))
	}
	runtimeDir, configPath, ifname := os.Args[1], os.Args[2], os.Args[3]
	store := &statusStore{statuses: map[string]map[string]any{}, now: time.Now().UTC()}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "lan"},
			Spec:     api.InterfaceSpec{IfName: ifname},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
			Metadata: api.ObjectMeta{Name: "lan-gw-v4"},
			Spec: api.VirtualAddressSpec{
				Family: "ipv4", Interface: "lan", Address: "172.18.0.1/32", Mode: "vrrp",
				VRRP: api.VirtualAddressVRRPSpec{
					VirtualRouterID: 18,
					Priority:        150,
					Peers:           []string{"172.18.0.3"},
					GracefulActivation: &api.VirtualAddressVRRPGracefulActivationSpec{
						ReadyWhen: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"DSLiteTunnel/dslite-ready.phase": {Equals: "Up"}}},
						Timeout:   "5s",
					},
				},
			},
		},
	}}}
	if err := writeElectionRole(runtimeDir, "lan-gw-v4", "master"); err != nil {
		fatal(err)
	}
	controller := &vrrp.Controller{
		Router:              router,
		Store:               store,
		ConfigPath:          configPath,
		Systemctl:           "systemctl",
		IP:                  "ip",
		Arping:              "arping",
		OperatingSystem:     platform.OSLinux,
		KeepalivedActiveTTL: -1,
		VRRPRuntimeDir:      runtimeDir,
		Now:                 func() time.Time { return store.now },
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			line := name + " " + strings.Join(args, " ")
			if name == "systemctl" || name == "/usr/local/sbin/routerd-vrrp-vmac" {
				return []byte("test stub: " + line), nil
			}
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		fatal(fmt.Errorf("preparing reconcile: %w", err))
	}
	assertStatus(store, "Preparing", false)
	assertAddress(ifname, false)

	store.ready = true
	store.now = store.now.Add(time.Second)
	if err := controller.Reconcile(context.Background()); err != nil {
		fatal(fmt.Errorf("ready reconcile: %w", err))
	}
	assertStatus(store, "Ready", true)
	assertAddress(ifname, true)

	if err := writeElectionRole(runtimeDir, "lan-gw-v4", "backup"); err != nil {
		fatal(err)
	}
	store.ready = false
	store.now = store.now.Add(time.Second)
	if err := controller.Reconcile(context.Background()); err != nil {
		fatal(fmt.Errorf("backup reconcile: %w", err))
	}
	assertStatus(store, "Standby", false)
	assertAddress(ifname, false)
	fmt.Println("graceful VRRP controller address lifecycle passed")
}

func assertStatus(store *statusStore, state string, advertised bool) {
	status := store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", "lan-gw-v4")
	if status["activationState"] != state || status["vipAdvertised"] != advertised {
		fatal(fmt.Errorf("activation status = %#v, want state=%s advertised=%t", status, state, advertised))
	}
}

func assertAddress(ifname string, want bool) {
	output, err := exec.Command("ip", "-4", "-o", "addr", "show", "dev", ifname).CombinedOutput()
	if err != nil {
		fatal(fmt.Errorf("inspect address: %w: %s", err, strings.TrimSpace(string(output))))
	}
	present := strings.Contains(string(output), "172.18.0.1/32")
	if present != want {
		fatal(fmt.Errorf("VIP present=%t, want %t: %s", present, want, strings.TrimSpace(string(output))))
	}
}

func writeElectionRole(runtimeDir, resource, role string) error {
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(resource))
	path := filepath.Join(runtimeDir, fmt.Sprintf("vrrp-election-%x.role", sum[:8]))
	return os.WriteFile(path, []byte(role+"\n"), 0644)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
