// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"errors"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"testing"
)

type nativeReadFaultStore struct {
	mapStore
	err error
}

func (s nativeReadFaultStore) ReadObjectStatus(string, string, string) (map[string]any, error) {
	return nil, s.err
}

func TestMobilityStoreNativeOwnershipReadUsesErrorfulBackend(t *testing.T) {
	base := mapStore{api.RouterAPIVersion + "/Router/" + dynamicconfig.MobilityNativeStatusName: {"proof": "stored"}}
	store := mobilityStore{evented: eventedStore{Store: base}}
	status, err := store.ReadObjectStatus(api.RouterAPIVersion, "Router", dynamicconfig.MobilityNativeStatusName)
	if err != nil || status["proof"] != "stored" {
		t.Fatalf("native read not forwarded: %#v %v", status, err)
	}
	sentinel := errors.New("native read rejected")
	store.evented.Store = nativeReadFaultStore{mapStore: base, err: sentinel}
	if _, err := store.ReadObjectStatus(api.RouterAPIVersion, "Router", dynamicconfig.MobilityNativeStatusName); !errors.Is(err, sentinel) {
		t.Fatalf("native read failure lost: %v", err)
	}
	if _, err := (mobilityStore{}).ReadObjectStatus(api.RouterAPIVersion, "Router", dynamicconfig.MobilityNativeStatusName); err == nil {
		t.Fatal("missing backend fabricated empty native state")
	}
}
