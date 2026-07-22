// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
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

func TestKernelModuleControllerFreeBSDLoadsMissingModule(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "KernelModule"}, Metadata: api.ObjectMeta{Name: "router-kernel"}, Spec: api.KernelModuleSpec{
			Modules: []string{"ipfw"},
			Runtime: boolPtr(true),
		}},
	}}}
	store := mapStore{}
	var commands []string
	controller := KernelModuleController{
		Router:  router,
		Store:   store,
		OSName:  "freebsd",
		BaseDir: t.TempDir(),
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			switch name {
			case "kldstat":
				return nil, errors.New("module not found")
			case "kldload":
				return []byte("loaded\n"), nil
			default:
				t.Fatalf("unexpected command %q", name)
				return nil, nil
			}
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(commands, "\n"), "kldstat -q -m ipfw\nkldload ipfw"; got != want {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "KernelModule", "router-kernel")
	if status["phase"] != "Applied" || status["changed"] != true || strings.Join(status["loaded"].([]string), ",") != "ipfw" {
		t.Fatalf("status = %#v", status)
	}
}

func TestKernelModuleControllerFreeBSDSkipsLoadedModule(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "KernelModule"}, Metadata: api.ObjectMeta{Name: "router-kernel"}, Spec: api.KernelModuleSpec{
			Modules: []string{"pf"},
			Runtime: boolPtr(true),
		}},
	}}}
	store := mapStore{}
	var commands []string
	controller := KernelModuleController{
		Router:  router,
		Store:   store,
		OSName:  "freebsd",
		BaseDir: t.TempDir(),
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name != "kldstat" {
				t.Fatalf("preloaded module must not run %q", name)
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(commands, "\n"), "kldstat -q -m pf"; got != want {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "KernelModule", "router-kernel")
	if status["phase"] != "Applied" || status["changed"] != false {
		t.Fatalf("status = %#v", status)
	}
}

func TestKernelModuleControllerFreeBSDPersistsOwnedLoaderDropIn(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "KernelModule"}, Metadata: api.ObjectMeta{Name: "router-kernel"}, Spec: api.KernelModuleSpec{
			Modules:    []string{"pf"},
			Runtime:    boolPtr(false),
			Persistent: true,
		}},
	}}}
	store := mapStore{}
	baseDir := t.TempDir()
	controller := KernelModuleController{
		Router:  router,
		Store:   store,
		OSName:  "freebsd",
		BaseDir: baseDir,
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(baseDir, "90-routerd-router-kernel.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "# Managed by routerd. Do not edit by hand.\npf_load=\"YES\"\n"; got != want {
		t.Fatalf("loader drop-in = %q, want %q", got, want)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "KernelModule", "router-kernel")
	if status["phase"] != "Applied" || status["persistent"] != true {
		t.Fatalf("status = %#v", status)
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status = store.ObjectStatus(api.SystemAPIVersion, "KernelModule", "router-kernel")
	if status["changed"] != false {
		t.Fatalf("second reconcile must be idempotent, status = %#v", status)
	}
}

func TestKernelModuleControllerFreeBSDRefusesForeignPersistenceCollision(t *testing.T) {
	baseDir := t.TempDir()
	path := filepath.Join(baseDir, "90-routerd-router-kernel.conf")
	foreign := []byte("# operator-owned\npf_load=\"NO\"\n")
	if err := os.WriteFile(path, foreign, 0644); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "KernelModule"}, Metadata: api.ObjectMeta{Name: "router-kernel"}, Spec: api.KernelModuleSpec{Modules: []string{"pf"}, Runtime: boolPtr(false), Persistent: true}},
	}}}
	controller := KernelModuleController{Router: router, Store: mapStore{}, OSName: "freebsd", BaseDir: baseDir}
	if err := controller.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "non-routerd") {
		t.Fatalf("foreign collision error = %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(foreign) {
		t.Fatalf("foreign file changed: data=%q err=%v", got, err)
	}
}

func TestKernelModuleControllerLinuxRefusesForeignPersistenceCollision(t *testing.T) {
	baseDir := t.TempDir()
	path := filepath.Join(baseDir, "90-routerd-router-kernel.conf")
	foreign := []byte("# operator-owned\npf\n")
	if err := os.WriteFile(path, foreign, 0644); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "KernelModule"}, Metadata: api.ObjectMeta{Name: "router-kernel"}, Spec: api.KernelModuleSpec{Modules: []string{"pf"}, Runtime: boolPtr(false), Persistent: true}},
	}}}
	controller := KernelModuleController{Router: router, Store: mapStore{}, OSName: "linux", BaseDir: baseDir}
	if err := controller.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "non-routerd") {
		t.Fatalf("foreign collision error = %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(foreign) {
		t.Fatalf("foreign file changed: data=%q err=%v", got, err)
	}
}

func TestKernelModuleControllerFreeBSDRejectsUnsafeLoaderIdentifier(t *testing.T) {
	for _, module := range []string{"pf#comment", "pf=YES", `pf"bad`} {
		t.Run(module, func(t *testing.T) {
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "KernelModule"}, Metadata: api.ObjectMeta{Name: "router-kernel"}, Spec: api.KernelModuleSpec{Modules: []string{module}, Runtime: boolPtr(false), Persistent: true}},
			}}}
			controller := KernelModuleController{Router: router, Store: mapStore{}, OSName: "freebsd", BaseDir: t.TempDir()}
			if err := controller.Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "loader variable identifier") {
				t.Fatalf("unsafe module error = %v", err)
			}
		})
	}
}

func TestKernelModuleControllerFreeBSDAcceptsHyphenatedLoaderIdentifier(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "KernelModule"}, Metadata: api.ObjectMeta{Name: "router-kernel"}, Spec: api.KernelModuleSpec{Modules: []string{"nvidia-modeset"}, Runtime: boolPtr(false), Persistent: true}},
	}}}
	baseDir := t.TempDir()
	controller := KernelModuleController{Router: router, Store: mapStore{}, OSName: "freebsd", BaseDir: baseDir}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(baseDir, "90-routerd-router-kernel.conf"))
	if err != nil || !strings.Contains(string(data), "nvidia-modeset_load=\"YES\"") {
		t.Fatalf("hyphenated loader module persistence: data=%q err=%v", data, err)
	}
}

func TestKernelModuleControllerFreeBSDDerivesRuntimePFModule(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "ClientPolicy"}, Metadata: api.ObjectMeta{Name: "lan-policy"}},
	}}}
	store := mapStore{}
	var commands []string
	controller := KernelModuleController{
		Router:  router,
		Store:   store,
		OSName:  "freebsd",
		BaseDir: t.TempDir(),
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "kldstat" {
				return nil, errors.New("module not found")
			}
			if name == "kldload" && strings.Join(args, " ") == "pf" {
				return nil, nil
			}
			t.Fatalf("unexpected command %q %q", name, args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(commands, "\n"), "kldstat -q -m pf\nkldload pf"; got != want {
		t.Fatalf("commands = %q, want %q", got, want)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "KernelModule", "router-runtime")
	if status["persistent"] != true || status["changed"] != true {
		t.Fatalf("derived FreeBSD status = %#v", status)
	}
}

func TestKernelModuleControllerFreeBSDRemovesOnlyOwnedStaleLoaderDropIns(t *testing.T) {
	baseDir := t.TempDir()
	ownedStale := filepath.Join(baseDir, "90-routerd-old.conf")
	if err := os.WriteFile(ownedStale, []byte(kernelModuleOwnershipHeader+"old_load=\"YES\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(baseDir, "90-routerd-operator.conf")
	if err := os.WriteFile(foreign, []byte("# operator-owned\noperator_load=\"YES\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "KernelModule"}, Metadata: api.ObjectMeta{Name: "router-kernel"}, Spec: api.KernelModuleSpec{Modules: []string{"pf"}, Runtime: boolPtr(false), Persistent: true}},
	}}}
	controller := KernelModuleController{Router: router, Store: mapStore{}, OSName: "freebsd", BaseDir: baseDir}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ownedStale); !os.IsNotExist(err) {
		t.Fatalf("owned stale drop-in still exists: %v", err)
	}
	if data, err := os.ReadFile(foreign); err != nil || !strings.Contains(string(data), "operator_load") {
		t.Fatalf("foreign drop-in was not preserved: data=%q err=%v", data, err)
	}
}

func TestKernelModuleControllerFreeBSDDryRunLeavesOwnedStaleDropIn(t *testing.T) {
	baseDir := t.TempDir()
	owned := filepath.Join(baseDir, "90-routerd-old.conf")
	if err := os.WriteFile(owned, []byte(kernelModuleOwnershipHeader+"old_load=\"YES\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	controller := KernelModuleController{Router: &api.Router{}, Store: mapStore{}, OSName: "freebsd", BaseDir: baseDir, DryRun: true}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(owned); err != nil {
		t.Fatalf("dry run removed owned stale drop-in: %v", err)
	}
	controller.DryRun = false
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Fatalf("owned stale drop-in remained after reconcile: %v", err)
	}
}
