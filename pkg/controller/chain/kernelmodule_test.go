// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

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
