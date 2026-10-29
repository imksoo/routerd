// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"github.com/vishvananda/netlink"
	"testing"
)

func TestExistingVMACRequiresExactNativeTypeParentAndMode(t *testing.T) {
	entry := vmac{parent: "wan", ifname: "wan-vmac"}
	good := netlink.Macvlan{LinkAttrs: netlink.LinkAttrs{Name: "wan-vmac", ParentIndex: 7}, Mode: netlink.MACVLAN_MODE_PRIVATE}
	if err := validateVMACNativeOwnership(entry, 7, &good); err != nil {
		t.Fatal(err)
	}
	wrongParent := good
	wrongParent.ParentIndex = 8
	wrongMode := good
	wrongMode.Mode = netlink.MACVLAN_MODE_BRIDGE
	for _, link := range []netlink.Link{&netlink.Dummy{LinkAttrs: good.LinkAttrs}, &wrongParent, &wrongMode} {
		if err := validateVMACNativeOwnership(entry, 7, link); err == nil {
			t.Fatalf("foreign native link authorized: %+v", link)
		}
	}
}

func TestCreateCollisionCannotAuthorizeFollowingVMACMutations(t *testing.T) {
	calls := 0
	err := runVMACCommands("activate", vmac{parent: "wan", ifname: "wan-vmac"}, [][]string{{"ip", "link", "add"}, {"ip", "link", "set", "dev", "wan-vmac", "up"}}, runHooks{
		command: func(string, ...string) ([]byte, error) {
			calls++
			return []byte("File exists"), errors.New("collision")
		},
		inspectVMAC: func(vmac) (vmacRuntimeState, error) { return vmacRuntimeState{}, errors.New("foreign parent") },
	})
	if err == nil || calls != 1 {
		t.Fatalf("foreign collision continued mutations: calls=%d err=%v", calls, err)
	}
}
