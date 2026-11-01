// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestSysctlControllerRestoresRetiredProfileRuntimeSetting(t *testing.T) {
	const (
		profileName = "router-runtime"
		key         = "net.ipv6.conf.all.forwarding"
	)
	store := mapStore{}
	values := map[string]string{}
	controller := SysctlController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "SysctlProfile"}, Metadata: api.ObjectMeta{Name: profileName}, Spec: api.SysctlProfileSpec{Profile: "router-linux", Runtime: boolPtr(true)}},
		}}},
		Store: store,
		Command: func(_ context.Context, command string, args ...string) ([]byte, error) {
			if command == "sysctl" && len(args) == 2 && args[0] == "-n" {
				if _, ok := values[args[1]]; !ok {
					values[args[1]] = "0"
				}
				return []byte(values[args[1]] + "\n"), nil
			}
			if command == "sysctl" && len(args) == 2 && args[0] == "-w" {
				parts := strings.SplitN(args[1], "=", 2)
				values[parts[0]] = parts[1]
				return []byte(args[1] + "\n"), nil
			}
			return nil, fmt.Errorf("unexpected command %s %v", command, args)
		},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if values[key] != "1" {
		t.Fatalf("profile did not apply forwarding: %#v", values)
	}
	settingName := profileName + "-" + safeSysctlName(key)
	status := store.ObjectStatus(api.SystemAPIVersion, "SysctlProfile", settingName)
	if status["previousValue"] != "0" {
		t.Fatalf("profile did not retain runtime value: %#v", status)
	}
	controller.Router = &api.Router{}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if values[key] != "0" {
		t.Fatalf("retired profile did not restore forwarding: %#v", values)
	}
	for _, name := range []string{profileName, settingName} {
		if _, exists := store[api.SystemAPIVersion+"/SysctlProfile/"+name]; exists {
			t.Fatalf("retired profile status %q remained", name)
		}
	}
}
