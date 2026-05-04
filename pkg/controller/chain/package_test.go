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

func TestParseOSReleaseID(t *testing.T) {
	if got := parseOSReleaseID("NAME=\"NixOS\"\nID=nixos\n"); got != "nixos" {
		t.Fatalf("ID = %q", got)
	}
}

type testCommandError struct{}

func (testCommandError) Error() string { return "command failed" }

var errTestCommand = testCommandError{}
