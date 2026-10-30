// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package vxlan

import (
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
	"testing"
)

func TestVXLANDFNativeWireRoundTrip(t *testing.T) {
	for _, mode := range []string{"unset", "set", "inherit"} {
		request, err := linuxVXLANDFRequest(42, mode)
		if err != nil {
			t.Fatal(err)
		}
		wire := request.Serialize()[unix.NLMSG_HDRLEN:]
		actual, err := decodeLinuxVXLANDF([][]byte{wire}, 42)
		if err != nil || actual != mode {
			t.Fatalf("%s decoded as %q: %v", mode, actual, err)
		}
		if _, err := decodeLinuxVXLANDF([][]byte{wire}, 43); err == nil {
			t.Fatal("accepted other interface's DF mode")
		}
	}
}

func TestVXLANDFReadbackRequiresAnObservedMode(t *testing.T) {
	message := nl.NewIfInfomsg(unix.AF_UNSPEC)
	message.Index = 42
	if _, err := decodeLinuxVXLANDF([][]byte{message.Serialize()}, 42); err == nil {
		t.Fatal("missing DF readback accepted")
	}
	for _, index := range []int{0, -1} {
		if _, err := linuxVXLANDFRequest(index, "inherit"); err == nil {
			t.Fatal("missing native index accepted")
		}
	}
	if _, err := linuxVXLANDFRequest(42, "guess"); err == nil {
		t.Fatal("unsupported DF mode accepted")
	}
}

func TestVXLANLearningAndDFMustMatchNativeReadback(t *testing.T) {
	config, owner, native := wildcardSegmentFixture(t, "0.0.0.0")
	observed, err := linuxSegmentAttributes(owner.Scope, native)
	if err != nil {
		t.Fatal(err)
	}
	config.NoLearning = true
	if err := CheckSegmentReadback(config, owner, observed); err == nil {
		t.Fatal("learning-enabled link fulfilled nolearning intent")
	}
	native.Learning = false
	observed, err = linuxSegmentAttributes(owner.Scope, native)
	if err != nil {
		t.Fatal(err)
	}
	config.OuterDF = "inherit"
	if err := CheckSegmentReadback(config, owner, observed); err == nil {
		t.Fatal("unobserved DF fulfilled explicit DF intent")
	}
	observed.Config.OuterDF = "inherit"
	if err := CheckSegmentReadback(config, owner, observed); err != nil {
		t.Fatal(err)
	}
}
