// SPDX-License-Identifier: BSD-3-Clause
package vrrp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestEmptyRouterKeepsKeepalivedConfigUntilVMACRemovalSucceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keepalived.conf")
	data := keepalivedConfigHeader + "vrrp_instance old {\n notify_master \"/usr/sbin/routerd-vrrp-vmac activate --vmac wan,wan-vmac,02:00:5e:00:01:01,,false\"\n}\n"
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("VMAC deletion failed")
	fail := true
	stopped := false
	c := Controller{Router: &api.Router{}, Store: mapStore{}, OperatingSystem: platform.OSLinux, ConfigPath: path, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "systemctl" && args[0] == "stop" {
			stopped = true
			return nil, nil
		}
		if filepath.Base(name) == "routerd-vrrp-vmac" && len(args) == 2 && args[0] == "remove-config" && args[1] == path {
			if !stopped {
				t.Fatal("VMAC removal preceded keepalived stop")
			}
			if fail {
				return nil, failure
			}
			return nil, nil
		}
		t.Fatalf("unexpected command %s %v", name, args)
		return nil, nil
	}}
	if err := c.Reconcile(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("VMAC removal was skipped or hidden: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("retry lost original VMAC ownership configuration: %v", err)
	}
	fail = false
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("completed removal retained config: %v", err)
	}
}
