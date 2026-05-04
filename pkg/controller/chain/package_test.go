package chain

import (
	"context"
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestPackageControllerInstallsMissingUbuntuPackages(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Package"}, Metadata: api.ObjectMeta{Name: "service-deps"}, Spec: api.PackageSpec{
			Packages: []api.OSPackageSetSpec{{
				OS:      packageOSName("linux"),
				Manager: "apt",
				Names:   []string{"dnsmasq-base", "conntrack"},
			}},
		}},
	}}}
	store := mapStore{}
	var commands []string
	controller := PackageController{
		Router: router,
		Store:  store,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "dpkg-query" && args[len(args)-1] == "dnsmasq-base" {
				return nil, errTestCommand
			}
			return []byte("install ok installed"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(commands, "\n")
	if !strings.Contains(got, "apt-get install -y dnsmasq-base") {
		t.Fatalf("commands = %q", got)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "Package", "service-deps")
	if status["phase"] != "Applied" || status["changed"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func TestPackageControllerDryRunReportsMissingPackages(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Package"}, Metadata: api.ObjectMeta{Name: "service-deps"}, Spec: api.PackageSpec{
			Packages: []api.OSPackageSetSpec{{
				OS:      packageOSName("linux"),
				Manager: "apt",
				Names:   []string{"dnsmasq-base"},
			}},
		}},
	}}}
	store := mapStore{}
	var commands []string
	controller := PackageController{
		Router: router,
		Store:  store,
		DryRun: true,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "dpkg-query" {
				return nil, errTestCommand
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(commands, "\n")
	if strings.Contains(got, "apt-get install") {
		t.Fatalf("dry-run should not install packages, commands = %q", got)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "Package", "service-deps")
	if status["phase"] != "Pending" || status["reason"] != "DryRun" {
		t.Fatalf("status = %#v", status)
	}
}

func TestPackageControllerDoesNotInstallWhenPackagesPresent(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Package"}, Metadata: api.ObjectMeta{Name: "service-deps"}, Spec: api.PackageSpec{
			Packages: []api.OSPackageSetSpec{{
				OS:      packageOSName("linux"),
				Manager: "apt",
				Names:   []string{"dnsmasq-base", "conntrack"},
			}},
		}},
	}}}
	store := mapStore{}
	bus := &recordingBus{}
	var commands []string
	controller := PackageController{
		Router: router,
		Store:  store,
		Bus:    bus,
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "dpkg-query" {
				return []byte("install ok installed"), nil
			}
			t.Fatalf("unexpected command %s %v", name, args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(commands, "\n")
	if strings.Contains(got, "apt-get install") {
		t.Fatalf("commands = %q", got)
	}
	if len(bus.events) != 0 {
		t.Fatalf("events = %#v, want none", bus.events)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "Package", "service-deps")
	if status["phase"] != "Applied" || status["changed"] != false {
		t.Fatalf("status = %#v", status)
	}
}

func TestPackageControllerReportsNixOSPackagesAsDeclarative(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Package"}, Metadata: api.ObjectMeta{Name: "service-deps"}, Spec: api.PackageSpec{
			Packages: []api.OSPackageSetSpec{{
				OS:      "nixos",
				Manager: "nix",
				Names:   []string{"dnsmasq", "conntrack-tools"},
			}},
		}},
	}}}
	store := mapStore{}
	controller := PackageController{
		Router: router,
		Store:  store,
		OSName: "nixos",
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			t.Fatalf("NixOS package resources must be rendered declaratively, got command %s %v", name, args)
			return nil, nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "Package", "service-deps")
	if status["phase"] != "Rendered" || status["reason"] != "NixOSDeclarativeOnly" {
		t.Fatalf("status = %#v", status)
	}
}

func TestPackageControllerInstallsMissingFreeBSDPackages(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Package"}, Metadata: api.ObjectMeta{Name: "service-deps"}, Spec: api.PackageSpec{
			Packages: []api.OSPackageSetSpec{{
				OS:      "freebsd",
				Manager: "pkg",
				Names:   []string{"dnsmasq", "bind-tools"},
			}},
		}},
	}}}
	store := mapStore{}
	var commands []string
	controller := PackageController{
		Router: router,
		Store:  store,
		OSName: "freebsd",
		Command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			_ = ctx
			commands = append(commands, strings.Join(append([]string{name}, args...), " "))
			if name == "pkg" && len(args) == 3 && args[0] == "info" && args[2] == "dnsmasq" {
				return nil, errTestCommand
			}
			return []byte("ok"), nil
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(commands, "\n")
	if !strings.Contains(got, "pkg install -y dnsmasq") {
		t.Fatalf("commands = %q", got)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "Package", "service-deps")
	if status["phase"] != "Applied" || status["changed"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func TestParseOSReleaseID(t *testing.T) {
	if got := parseOSReleaseID("NAME=\"NixOS\"\nID=nixos\n"); got != "nixos" {
		t.Fatalf("ID = %q", got)
	}
}

type testCommandError struct{}

func (testCommandError) Error() string { return "command failed" }

var errTestCommand = testCommandError{}
