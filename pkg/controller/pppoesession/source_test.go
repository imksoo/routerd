// SPDX-License-Identifier: BSD-3-Clause

package pppoesession

import (
	"errors"
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/pppoeclient"
)

func TestDaemonProjectionIsBoundToCurrentSource(t *testing.T) {
	for _, mismatch := range []string{"none", "token", "interface", "identity", "reload", "unavailable", "read-error"} {
		t.Run(mismatch, func(t *testing.T) {
			previous := map[string]any{"phase": "Connected", "gateway": "192.0.2.1", "currentAddress": "192.0.2.2"}
			store := mapStore{api.NetAPIVersion + "/PPPoESession/wan": previous}
			source := Source{Interface: "ens18", Device: "ppp-wan", SupervisorOwner: "new-owner", ConfigHash: "new-config"}
			source.Identity = pppoeclient.IdentityForSpec(api.PPPoESessionSpec{Username: "new-subscriber"})
			identity := source.Identity
			current := source
			ready := true
			var sourceErr error
			failure := errors.New("supervisor observation failed")
			observed := map[string]string{"interface": "ens18", "ifname": "ppp-wan", "supervisorOwner": "new-owner", "currentAddress": "198.51.100.2", "peerAddress": "198.51.100.1"}
			switch mismatch {
			case "token":
				observed["supervisorOwner"] = "old-owner"
			case "interface":
				observed["interface"] = "old0"
			case "identity":
				source.SupervisorOwner = "" // explicit external socket still requires public identity
				current.SupervisorOwner = ""
				identity.Username = "old-subscriber"
			case "reload":
				current.ConfigHash = "subsequent-config"
			case "unavailable":
				ready = false
			case "read-error":
				sourceErr = failure
			}
			controller := Controller{Store: store, Source: func(string) (Source, bool, error) { return current, ready, sourceErr }}
			status := daemonapi.DaemonStatus{Resources: []daemonapi.ResourceStatus{{Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "PPPoESession", Name: "wan"}, PPPoEIdentity: &identity, Phase: "Connected", Observed: observed}}}
			err := controller.applyDaemonStatus(t.Context(), "wan", source, status)
			got := store.ObjectStatus(api.NetAPIVersion, "PPPoESession", "wan")
			if mismatch == "read-error" {
				if !errors.Is(err, failure) || !reflect.DeepEqual(got, previous) {
					t.Fatalf("unknown source revoked or replaced accepted projection: %+v err=%v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mismatch == "none" {
				if got["phase"] != "Connected" || got["gateway"] != "198.51.100.1" {
					t.Fatalf("current source rejected: %+v", got)
				}
			} else if got["phase"] != "Pending" || got["gateway"] != "" || got["currentAddress"] != "" {
				t.Fatalf("stale source promoted current negotiated state: %+v", got)
			}
		})
	}
}
