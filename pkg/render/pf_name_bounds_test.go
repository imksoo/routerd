// SPDX-License-Identifier: BSD-3-Clause
package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestPFForwardAnchorFitsNativeComponentLimit(t *testing.T) {
	router := pfForwardRouter()
	router.TypeMeta = api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}
	router.Metadata.Name = "anchor-length"
	name := strings.Repeat("a", 22)
	router.Spec.Resources[3].Metadata.Name = name
	spec, _ := router.Spec.Resources[6].FirewallRuleSpec()
	spec.FromZone = name
	router.Spec.Resources[6].Spec = spec
	if err := config.ValidateForOS(router, platform.OSFreeBSD); err != nil {
		t.Fatal(err)
	}
	data, err := PFRuntimeFirewall(router, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// FreeBSD releng/14.3 pf.h defines PF_ANCHOR_NAME_SIZE=64;
	// pf_ruleset.c rejects every component whose strlen is >= that size.
	for _, match := range regexp.MustCompile(`(?m)^anchor "([^"]+)"`).FindAllStringSubmatch(string(data), -1) {
		for _, component := range strings.Split(match[1], "/") {
			if len(component) >= 64 {
				t.Errorf("renderer accepted valid config but produced native-invalid %d-byte anchor component %q", len(component), component)
			}
		}
	}
}

func TestPFGeneratedNamesFitNativeBoundsWithLongResourceNames(t *testing.T) {
	router := pfForwardRouter()
	name := strings.Repeat("internal", 9)
	router.Spec.Resources[3].Metadata.Name = name
	router.Spec.Resources[5].Metadata.Name = name + "-other"
	router.Spec.Resources[6].Metadata.Name = strings.Repeat("deny", 24)
	spec, _ := router.Spec.Resources[6].FirewallRuleSpec()
	spec.FromZone = name
	router.Spec.Resources[6].Spec = spec
	data, err := PFRuntimeFirewall(router, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range regexp.MustCompile(`(?:anchor|tag|tagged|label) "([^"]+)"`).FindAllStringSubmatch(string(data), -1) {
		if len(match[1]) >= 64 {
			t.Errorf("native PF name exceeds 63 bytes: %q", match[1])
		}
	}
	anchors := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^anchor "([^"]+)"`).FindAllStringSubmatch(string(data), -1) {
		if anchors[match[1]] {
			t.Errorf("distinct long zone names share anchor %q", match[1])
		}
		anchors[match[1]] = true
	}
	second, err := PFRuntimeFirewall(router, nil, nil)
	if err != nil || string(data) != string(second) {
		t.Fatalf("render changed internal names: %v", err)
	}
}
