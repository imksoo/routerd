// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestSystemdHealthRemovalRetiresUnhealthyStatusAfterStop(t *testing.T) {
	dir := t.TempDir()
	unit := healthCheckUnitName("removed")
	path := filepath.Join(dir, unit)
	if err := os.WriteFile(path, []byte("# Managed by routerd. Do not edit by hand.\n[Service]\nExecStart=/usr/sbin/routerd-healthcheck\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store := mapStore{api.NetAPIVersion + "/HealthCheck/removed": {"phase": "Unhealthy"}}
	c := SystemdUnitController{Router: &api.Router{}, Store: store, SystemdSystemDir: dir}
	failStop := true
	command := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "disable" && failStop {
			return nil, errors.New("stop unconfirmed")
		}
		return nil, nil
	}
	if err := c.cleanupStaleHealthCheckUnits(t.Context(), nil, command); err == nil {
		t.Fatal("stop failure hidden")
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "removed"); got["phase"] != "Unhealthy" {
		t.Fatalf("status removed before stop: %v", got)
	}
	failStop = false
	if err := c.cleanupStaleHealthCheckUnits(t.Context(), nil, command); err != nil {
		t.Fatal(err)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "removed"); len(got) != 0 {
		t.Fatalf("withdrawn health probe still degrades empty runtime: %v", got)
	}
	if err := c.cleanupStaleHealthCheckUnits(t.Context(), nil, command); err != nil {
		t.Fatalf("repeated withdrawal: %v", err)
	}
}

func TestSystemdHealthRemovalKeepsWhenFalseDeclaration(t *testing.T) {
	store := mapStore{
		api.NetAPIVersion + "/HealthCheck/standby":                              {"phase": "Disabled", "reason": "WhenFalse"},
		api.SystemAPIVersion + "/ServiceUnit/" + healthCheckUnitName("standby"): {"phase": "Removed"},
	}
	declared := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"}, Metadata: api.ObjectMeta{Name: "standby"}}}}}
	c := SystemdUnitController{Router: &api.Router{}, DeclaredRouter: declared, Store: store, SystemdSystemDir: t.TempDir()}
	if err := c.cleanupStaleHealthCheckUnits(t.Context(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "standby"); got["reason"] != "WhenFalse" {
		t.Fatalf("declared standby status lost: %v", got)
	}
}
