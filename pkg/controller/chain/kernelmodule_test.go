// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestKernelModuleControllerLoadsAndPersistsModules(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "KernelModule"}, Metadata: api.ObjectMeta{Name: "router-kernel"}, Spec: api.KernelModuleSpec{
			Modules:    []string{"nf_conntrack", "wireguard"},
			Runtime:    boolPtr(true),
			Persistent: true,
		}},
	}}}
	store := mapStore{}
	dir := t.TempDir()
	procModules := filepath.Join(dir, "modules")
	if err := os.WriteFile(procModules, nil, 0644); err != nil {
		t.Fatal(err)
	}
	var commands []string
	controller := KernelModuleController{
		Router:          router,
		Store:           store,
		OSName:          "ubuntu",
		BaseDir:         dir,
		ProcModulesPath: procModules,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	gotCommands := strings.Join(commands, "\n")
	for _, want := range []string{"modprobe nf_conntrack", "modprobe wireguard"} {
		if !strings.Contains(gotCommands, want) {
			t.Fatalf("commands missing %q:\n%s", want, gotCommands)
		}
	}
	dataBytes, err := os.ReadFile(filepath.Join(dir, "90-routerd-router-kernel.conf"))
	if err != nil {
		t.Fatalf("read modules file: %v", err)
	}
	data := string(dataBytes)
	if !strings.Contains(data, "nf_conntrack\nwireguard") {
		t.Fatalf("modules file = %q", data)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "KernelModule", "router-kernel")
	if status["phase"] != "Applied" || status["changed"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func TestKernelModuleControllerSkipsLoadedModules(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "KernelModule"}, Metadata: api.ObjectMeta{Name: "router-kernel"}, Spec: api.KernelModuleSpec{
			Modules: []string{"nf_conntrack", "wireguard"},
			Runtime: boolPtr(true),
		}},
	}}}
	store := mapStore{}
	dir := t.TempDir()
	procModules := filepath.Join(dir, "modules")
	if err := os.WriteFile(procModules, []byte("nf_conntrack 200704 2 - Live 0x0\nwireguard 94208 0 - Live 0x0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var commands []string
	controller := KernelModuleController{
		Router:          router,
		Store:           store,
		OSName:          "ubuntu",
		ProcModulesPath: procModules,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 0 {
		t.Fatalf("loaded modules should not be modprobed, commands = %#v", commands)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "KernelModule", "router-kernel")
	if status["phase"] != "Applied" || status["changed"] != false {
		t.Fatalf("status = %#v", status)
	}
}
