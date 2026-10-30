// SPDX-License-Identifier: BSD-3-Clause

package vxlan

import (
	"context"
	"slices"
	"testing"
)

func TestBSDVXLANLearningIsAppliedAndReadFromDriver(t *testing.T) {
	config := SegmentConfig{VNI: 10, Local: "192.0.2.1", Remotes: []string{"192.0.2.2"}, Port: 4789, NoLearning: true}
	if !slices.Contains(bsdSegmentArguments(config), "-vxlanlearn") {
		t.Fatal("nolearning omitted from native command")
	}
	parsed, err := decodeBSDSegmentConfig(bsdVXLANDriverConfig{VNI: 10, Learning: 0})
	if err != nil || !parsed.NoLearning {
		t.Fatalf("driver readback=%+v %v", parsed, err)
	}
	parsed, err = decodeBSDSegmentConfig(bsdVXLANDriverConfig{VNI: 10, Learning: 1})
	if err != nil || parsed.NoLearning {
		t.Fatalf("driver readback=%+v %v", parsed, err)
	}
	native := freeBSDSegmentNative{}
	config.OuterDF = "inherit"
	if _, err := native.Resolve(context.Background(), config, "owned"); err == nil {
		t.Fatal("unsupported FreeBSD outer DF silently accepted")
	}
}
