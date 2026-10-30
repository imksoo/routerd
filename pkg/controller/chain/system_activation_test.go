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
	"github.com/imksoo/routerd/pkg/render"
)

func TestSystemdActivationRetriesUnacknowledgedInstalledInput(t *testing.T) {
	for _, failed := range []string{"daemon-reload", "restart"} {
		t.Run(failed, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture.service")
			spec := api.SystemdUnitSpec{ExecStart: []string{"/usr/bin/true"}}
			failure := errors.New("activation failed")
			fail := true
			var commands []string
			command := func(_ context.Context, name string, args ...string) ([]byte, error) {
				commands = append(commands, strings.Join(append([]string{name}, args...), " "))
				if fail && name == "systemctl" && args[0] == failed {
					fail = false
					return nil, failure
				}
				return nil, nil // Old service remains active, even after failed restart.
			}
			if _, err := (SystemdUnitController{}).applySystemdUnit(t.Context(), "fixture", path, "fixture.service", spec, []byte("config A"), command); !errors.Is(err, failure) {
				t.Fatalf("initial activation = %v", err)
			}
			if receipt, err := artifactActivationReceipt(path); err != nil || receipt.AppliedDigest != "" {
				t.Fatalf("failed effect was acknowledged: %+v %v", receipt, err)
			}
			commands = nil
			// A new owner has no in-memory changed flag. Only the durable receipt
			// can distinguish installed bytes from an acknowledged activation.
			if _, err := (SystemdUnitController{}).applySystemdUnit(t.Context(), "fixture", path, "fixture.service", spec, []byte("config A"), command); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"systemctl daemon-reload", "systemctl restart fixture.service"} {
				if !commandLineContains(commands, want) {
					t.Fatalf("lost retry %s: %v", want, commands)
				}
			}
			commands = nil
			if _, err := (SystemdUnitController{}).applySystemdUnit(t.Context(), "fixture", path, "fixture.service", spec, []byte("config A"), command); err != nil {
				t.Fatal(err)
			}
			if commandLineContains(commands, "systemctl restart fixture.service") {
				t.Fatalf("acknowledged unchanged service restarted: %v", commands)
			}
			commands = nil
			if _, err := (SystemdUnitController{}).applySystemdUnit(t.Context(), "fixture", path, "fixture.service", spec, []byte("config B"), command); err != nil {
				t.Fatal(err)
			}
			if !commandLineContains(commands, "systemctl restart fixture.service") {
				t.Fatalf("config-only input change was not consumed: %v", commands)
			}
		})
	}
}

func TestSystemdRemovalRetainsIntentUntilStopAndReloadSucceed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.service")
	spec := api.SystemdUnitSpec{ExecStart: []string{"/usr/bin/true"}}
	if err := os.WriteFile(path, render.SystemdUnit("fixture", spec), 0o644); err != nil {
		t.Fatal(err)
	}
	stopFailure, reloadFailure := errors.New("stop failed"), errors.New("reload failed")
	stage := "stop"
	var commands []string
	command := func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		if args[0] == "disable" && stage == "stop" {
			return nil, stopFailure
		}
		if args[0] == "daemon-reload" && stage == "reload" {
			return nil, reloadFailure
		}
		return nil, nil
	}
	remove := func() error {
		_, err := (SystemdUnitController{}).applySystemdUnit(t.Context(), "fixture", path, "fixture.service", api.SystemdUnitSpec{State: "absent"}, nil, command)
		return err
	}
	if err := remove(); !errors.Is(err, stopFailure) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("failed stop unlinked the owner's unit: %v", err)
	}
	stage = "reload"
	if err := remove(); !errors.Is(err, reloadFailure) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected successful stop then unlink: %v", err)
	}
	if receipt, err := artifactActivationReceipt(path); err != nil || !receipt.Removing {
		t.Fatalf("lost post-unlink pending removal: %+v %v", receipt, err)
	}
	stage, commands = "success", nil
	if err := remove(); err != nil {
		t.Fatal(err)
	}
	if !commandLineContains(commands, "systemctl daemon-reload") {
		t.Fatalf("process restart lost removal activation: %v", commands)
	}
	if _, err := os.Stat(path + activationReceiptSuffix); !os.IsNotExist(err) {
		t.Fatalf("completed removal retained receipt: %v", err)
	}
}

func TestNetworkAdoptionRetriesAfterInstalledFileAndRemovalFailures(t *testing.T) {
	dir := t.TempDir()
	c := NetworkAdoptionController{NetworkdDropinBase: filepath.Join(dir, "network"), ResolvedDropinDir: filepath.Join(dir, "resolved")}
	spec := api.NetworkAdoptionSpec{SystemdNetworkd: api.NetworkAdoptionNetworkdSpec{DisableDHCPv4: true}, SystemdResolved: api.NetworkAdoptionResolvedSpec{DisableDNSStubListener: true}}
	failure := errors.New("activation failed")
	failReconfigure, failResolved := true, true
	var commands []string
	command := func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		if name == "networkctl" && args[0] == "reconfigure" && failReconfigure {
			failReconfigure = false
			return nil, failure
		}
		if name == "systemctl" && args[0] == "restart" && args[1] == "systemd-resolved.service" && failResolved {
			failResolved = false
			return nil, failure
		}
		return nil, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, _, err := c.applyNetworkAdoption(t.Context(), "wan", "fixture0", spec, command); !errors.Is(err, failure) {
			t.Fatalf("attempt %d lost failure: %v", attempt, err)
		}
	}
	commands = nil
	if _, _, err := c.applyNetworkAdoption(t.Context(), "wan", "fixture0", spec, command); err != nil {
		t.Fatal(err)
	}
	if !commandLineContains(commands, "systemctl restart systemd-resolved.service") {
		t.Fatalf("unchanged resolved bytes lost failed activation: %v", commands)
	}
	if commandLineContains(commands, "networkctl reconfigure fixture0") {
		t.Fatalf("already acknowledged networkd activation repeated: %v", commands)
	}
	failResolved = true
	if _, _, err := c.cleanupStaleResolvedAdoptionDropins(t.Context(), nil, command); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	commands = nil
	if _, changed, err := (NetworkAdoptionController{ResolvedDropinDir: c.ResolvedDropinDir}).cleanupStaleResolvedAdoptionDropins(t.Context(), nil, command); err != nil || !changed {
		t.Fatalf("new owner lost unlinked dropin removal: changed=%v err=%v", changed, err)
	}
	if !commandLineContains(commands, "systemctl restart systemd-resolved.service") {
		t.Fatalf("lost stale removal reload: %v", commands)
	}
}

func TestLongLivedBGPProbeUsesConfiguredHelperPaths(t *testing.T) {
	dir := t.TempDir()
	c := SystemdUnitController{BGPSocketPath: filepath.Join(dir, "gobgp.sock"), BGPControlSocketPath: filepath.Join(dir, "admin.sock")}
	var commands []string
	command := func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		return nil, nil
	}
	if !c.longLivedHelperSocketsPresent(t.Context(), render.BGPUnitName, command) {
		t.Fatal("configured sockets not accepted")
	}
	for _, socket := range []string{c.BGPSocketPath, c.BGPControlSocketPath} {
		if !commandLineContains(commands, "test -S "+socket) {
			t.Fatalf("configured socket was not probed: %v", commands)
		}
	}
	if strings.Contains(strings.Join(commands, "\n"), "/run/routerd/bgp/") {
		t.Fatalf("default socket can spuriously restart configured helper: %v", commands)
	}
}

func TestHealthCheckStopFailurePreservesUnitAndUnacknowledgedInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routerd-healthcheck@fixture.service")
	enabled := false
	spec := api.HealthCheckSpec{Enabled: &enabled, Target: "192.0.2.1"}
	c := SystemdUnitController{Router: &api.Router{}, SystemdSystemDir: dir}
	failure := errors.New("stop failed")
	fail := true
	var commands []string
	command := func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		if args[0] == "disable" && fail {
			return nil, failure
		}
		return nil, nil
	}
	if _, _, err := c.applyHealthCheckSystemdUnit(t.Context(), path, filepath.Base(path), "fixture", spec, nil, command); !errors.Is(err, failure) {
		t.Fatalf("disabled helper reported success after failed stop: %v", err)
	}
	if receipt, err := artifactActivationReceipt(path); err != nil || receipt.AppliedDigest != "" || receipt.PendingDigest == "" {
		t.Fatalf("disabled effect was acknowledged: %+v %v", receipt, err)
	}
	fail = false
	if _, _, err := c.applyHealthCheckSystemdUnit(t.Context(), path, filepath.Base(path), "fixture", spec, nil, command); err != nil {
		t.Fatal(err)
	}
	// Removed intent uses the same acknowledged-stop owner, including when a
	// stale scan runs in a new process with only the on-disk receipt.
	fail = true
	if err := c.cleanupStaleHealthCheckUnits(t.Context(), nil, command); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stale cleanup discarded the live owner's artifact: %v", err)
	}
	fail = false
	if err := c.cleanupStaleHealthCheckUnits(t.Context(), nil, command); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("acknowledged stale removal did not finish: %v", err)
	}
}

func TestSystemdActivationPreservesForeignArtifactAndUnreadableReceipt(t *testing.T) {
	for _, corruptReceipt := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign file", true: "invalid receipt"}[corruptReceipt], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture.service")
			spec := api.SystemdUnitSpec{ExecStart: []string{"/usr/bin/true"}}
			data := []byte("[Unit]\nDescription=operator service\n")
			if corruptReceipt {
				data = render.SystemdUnit("fixture", spec)
				if err := os.WriteFile(path+activationReceiptSuffix, []byte(`{"pendingDigest":"bad"}`), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			calls := 0
			command := func(context.Context, string, ...string) ([]byte, error) { calls++; return nil, nil }
			if _, err := (SystemdUnitController{}).applySystemdUnit(t.Context(), "fixture", path, "fixture.service", spec, nil, command); err == nil {
				t.Fatal("unknown ownership/receipt allowed activation")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(data) || calls != 0 {
				t.Fatalf("failed precondition changed native intent: data=%q calls=%d err=%v", got, calls, err)
			}
		})
	}
}

func TestAbsentSystemdArtifactDoesNotAdoptAnUnownedRunningUnit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routerd-eventd.service")
	calls := 0
	command := func(context.Context, string, ...string) ([]byte, error) {
		calls++
		return []byte("active"), nil
	}
	if changed, err := (SystemdUnitController{}).removeSystemdArtifact(t.Context(), path, filepath.Base(path), command); err != nil || changed || calls != 0 {
		t.Fatalf("unit name was adopted as process ownership: changed=%v calls=%d err=%v", changed, calls, err)
	}
	if pending, err := legacyEventdUnitNeedsCleanup(path); err != nil || pending {
		t.Fatalf("legacy name without owned artifact was adopted: pending=%v err=%v", pending, err)
	}
}

func TestStaleSystemdScanDoesNotTreatDirectoryReadFailureAsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("unreadable unit catalog"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	controller := SystemdUnitController{Router: &api.Router{}, SystemdSystemDir: path}
	if err := controller.cleanupStaleHealthCheckUnits(t.Context(), nil, func(context.Context, string, ...string) ([]byte, error) {
		calls++
		return nil, nil
	}); err == nil || calls != 0 {
		t.Fatalf("unknown stale-unit observation accepted: calls=%d err=%v", calls, err)
	}
}

func TestNetworkAdoptionRetainsActivationUntilLANRouteCleanupSucceeds(t *testing.T) {
	dir := t.TempDir()
	c := NetworkAdoptionController{NetworkdDropinBase: dir, Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "fixture0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan-server"}, Spec: api.DHCPv4ServerSpec{Interface: "lan"}},
	}}}}
	spec := api.NetworkAdoptionSpec{SystemdNetworkd: api.NetworkAdoptionNetworkdSpec{DisableIPv6RA: true}}
	failure := errors.New("route cleanup failed")
	fail, flushes := true, 0
	command := func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "ip" {
			flushes++
			if fail {
				return nil, failure
			}
		}
		return nil, nil
	}
	if _, _, err := c.applyNetworkAdoption(t.Context(), "lan", "fixture0", spec, command); !errors.Is(err, failure) {
		t.Fatalf("route cleanup failure was acknowledged: %v", err)
	}
	fail = false
	if _, _, err := c.applyNetworkAdoption(t.Context(), "lan", "fixture0", spec, command); err != nil || flushes != 2 {
		t.Fatalf("installed input lost required cleanup retry: flushes=%d err=%v", flushes, err)
	}
	reload := false
	spec.Reload = &reload
	spec.SystemdNetworkd.DisableDHCPv4 = true
	if _, _, err := c.applyNetworkAdoption(t.Context(), "lan", "fixture0", spec, command); err != nil || flushes != 2 {
		t.Fatalf("render-only intent changed native routes: flushes=%d err=%v", flushes, err)
	}
}
