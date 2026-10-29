// SPDX-License-Identifier: BSD-3-Clause

package vrrp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestFailedKeepalivedApplyRetriesAfterRestartDespiteMatchingFile(t *testing.T) {
	dir := t.TempDir()
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	failApply := true
	attempts := 0
	controller := Controller{Router: vrrpRouter("vrrp"), Store: store, ConfigPath: filepath.Join(dir, "keepalived.conf"), OperatingSystem: platform.OSLinux}
	controller.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "systemctl" && (args[0] == "reload" || args[0] == "restart") {
			attempts++
			if failApply {
				return nil, errors.New("service apply failed")
			}
		}
		if name == "ip" {
			return []byte("    inet 10.240.70.10/32 scope global\n"), nil
		}
		return nil, nil
	}
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("failed service reload and restart accepted")
	}
	if _, err := os.Stat(controller.ConfigPath); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	controller.Store = store
	failApply = false
	before := attempts
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if attempts <= before {
		t.Fatal("matching desired file concealed failed runtime apply after restart")
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "VirtualAddress", "vip")
	if err != nil || status["backendConfigHash"] == nil {
		t.Fatalf("successful apply receipt absent: %v err=%v", status, err)
	}
}

func TestInvalidKeepalivedCandidatePreservesLiveConfig(t *testing.T) {
	store := mapStore{}
	invalid := false
	controller := Controller{Router: vrrpRouter("vrrp"), Store: store, ConfigPath: filepath.Join(t.TempDir(), "keepalived.conf"), OperatingSystem: platform.OSLinux}
	controller.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "keepalived" && invalid {
			if len(args) != 3 || args[0] != "--config-test" || args[2] == controller.ConfigPath {
				t.Fatalf("live config used for candidate validation: %v", args)
			}
			return nil, errors.New("invalid candidate")
		}
		if name == "ip" {
			return []byte("    inet 10.240.70.10/32 scope global\n"), nil
		}
		return nil, nil
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(controller.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	spec := controller.Router.Spec.Resources[1].Spec.(api.VirtualAddressSpec)
	spec.VRRP.Priority++
	controller.Router.Spec.Resources[1].Spec = spec
	invalid = true
	if err := controller.Reconcile(t.Context()); err == nil {
		t.Fatal("invalid configuration accepted")
	}
	current, err := os.ReadFile(controller.ConfigPath)
	if err != nil || string(current) != string(previous) {
		t.Fatalf("invalid candidate replaced live configuration: err=%v", err)
	}
}

func TestKeepalivedPreservesForeignConfigAndStopsOwnedConfigWithOnlyStaticVIPs(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign", true: "owned"}[owned], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "keepalived.conf")
			data := "vrrp_instance previous {}\n"
			if owned {
				data = keepalivedConfigHeader + data
			}
			if err := os.WriteFile(path, []byte(data), 0644); err != nil {
				t.Fatal(err)
			}
			stops := 0
			controller := Controller{Router: vrrpRouter("static"), Store: mapStore{}, ConfigPath: path, OperatingSystem: platform.OSLinux, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name == "systemctl" && strings.Join(args, " ") == "stop keepalived.service" {
					stops++
				}
				return []byte("    inet 10.240.70.10/32 scope global\n"), nil
			}}
			withNativeVIPFixture(t, &controller, "10.240.70.10/32", true, 0)
			seedOwnedVIP(t, &controller, "vip", "10.240.70.10/32", 0)
			if err := controller.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if owned && stops != 1 || !owned && stops != 0 {
				t.Fatalf("wrong service stop ownership: owned=%v stops=%d", owned, stops)
			}
			current, err := os.ReadFile(path)
			if owned && !os.IsNotExist(err) || !owned && (err != nil || string(current) != data) {
				t.Fatalf("wrong config removal ownership: owned=%v err=%v", owned, err)
			}
		})
	}
}

type failedTrackReader struct {
	mapStore
	kind string
	err  error
}

func (s failedTrackReader) ReadObjectStatus(version, kind, name string) (map[string]any, error) {
	if kind == s.kind {
		return nil, s.err
	}
	return s.mapStore.ReadObjectStatus(version, kind, name)
}

func TestVRRPTrackingReadFailureDoesNotChangeNativePriority(t *testing.T) {
	for _, kind := range []string{"HealthCheck", "VirtualAddress"} {
		t.Run(kind, func(t *testing.T) {
			router := vrrpRouter("vrrp")
			spec := router.Spec.Resources[1].Spec.(api.VirtualAddressSpec)
			spec.Track = []api.ResourceTrackSpec{{Resource: "HealthCheck/uplink", ConfirmConsecutiveUnhealthy: 1}}
			router.Spec.Resources[1].Spec = spec
			failure := errors.New("track status unreadable")
			commands := 0
			controller := Controller{Router: router, Store: failedTrackReader{mapStore: mapStore{}, kind: kind, err: failure}, ConfigPath: filepath.Join(t.TempDir(), "keepalived.conf"), OperatingSystem: platform.OSLinux, Command: func(context.Context, string, ...string) ([]byte, error) {
				commands++
				return nil, nil
			}}
			if err := controller.Reconcile(t.Context()); !errors.Is(err, failure) || commands != 0 {
				t.Fatalf("failed tracking read allowed native mutation: commands=%d err=%v", commands, err)
			}
		})
	}
}
