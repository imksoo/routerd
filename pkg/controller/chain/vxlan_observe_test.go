// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"errors"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestVXLANSegmentInspectionRequiresCurrentNativeOwnerReadback(t *testing.T) {
	controller, native, _, _ := segmentOwnerFixture(t)
	if platform.CurrentOS() == platform.OSFreeBSD {
		segment, _ := controller.Router.Spec.Resources[2].VXLANSegmentSpec()
		segment.Remotes = segment.Remotes[:1]
		controller.Router.Spec.Resources[2].Spec = segment
	}
	controller.Router.TypeMeta = api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}
	controller.Router.Metadata.Name = "test"
	engine := apply.New(t.Context())
	engine.Command = func(string, ...string) ([]byte, error) {
		return nil, errors.New("test-owned unavailable host observation")
	}
	engine.Segments = &controller
	inspect := func(phase, present string) {
		t.Helper()
		beforeCreate, beforeConfigure, beforeRemove := native.creates, native.configured, native.removed
		result, err := engine.Observe(controller.Router)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, resource := range result.Resources {
			if resource.ID != api.NetAPIVersion+"/VXLANSegment/segment" {
				continue
			}
			found = true
			if resource.Phase != phase || resource.Observed["present"] != present {
				t.Fatalf("segment inspection=%+v, want %s/%s", resource, phase, present)
			}
		}
		if !found || phase != "Healthy" && result.Phase == "Healthy" {
			t.Fatalf("segment observation was omitted or lost from aggregate result: %+v", result)
		}
		if native.creates != beforeCreate || native.configured != beforeConfigure || native.removed != beforeRemove {
			t.Fatal("read-only inspection executed a native effect")
		}
	}
	inspect("Drifted", "false")
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if platform.CurrentOS() == platform.OSLinux && len(native.link.Flood) != 2 {
		t.Fatalf("Linux multiple-remote coverage was lost: %+v", native.link.Flood)
	}
	inspect("Healthy", "true")
	owner := oneSegmentOwner(t, controller)
	owner.Identity.Pending = true
	if err := controller.saveOwner(owner); err != nil {
		t.Fatal(err)
	}
	inspect("Blocked", "unknown")
	owner.Identity.Pending = false
	if err := controller.saveOwner(owner); err != nil {
		t.Fatal(err)
	}
	native.link.Flood = nil
	inspect("Blocked", "unknown")
	native.link.Flood = append([]string(nil), owner.Plan.Remotes...)
	native.link.Token = "foreign"
	inspect("Blocked", "unknown")
	native.link.Token = owner.Identity.Token
	native.readErr = errors.New("native read failed")
	inspect("Blocked", "unknown")
	native.readErr = nil
	if err := controller.removeOwner(api.NetAPIVersion + "/VXLANSegment/segment"); err != nil {
		t.Fatal(err)
	}
	// Presence without its receipt must not be described as native absence.
	inspect("Blocked", "unknown")
	engine.Segments = nil
	inspect("Blocked", "unknown")
}
