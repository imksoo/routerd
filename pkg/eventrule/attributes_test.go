// SPDX-License-Identifier: BSD-3-Clause

package eventrule

import (
	"testing"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestAttributeInterpolationDoesNotExpandEventData(t *testing.T) {
	event := daemonapi.DaemonEvent{
		Type: "routerd.input",
		Resource: &daemonapi.ResourceRef{
			Name: "${event.type}", Kind: "Interface", APIVersion: "net.routerd/v1alpha1",
		},
		Attributes: map[string]string{"input": "${correlation}", "empty": ""},
	}
	template := "${attributes.input}|${resource.name}|${event.type}|${correlation}|${count}|${attributes.empty}|${attributes.missing}|${unknown}|${unfinished"
	want := "${correlation}|${event.type}|routerd.input|${count}|3||${attributes.missing}|${unknown}|${unfinished"
	for i := 0; i < 100; i++ {
		if got := renderValue(template, event, "${count}", 3); got != want {
			t.Fatalf("interpolation changed literal event data: %q, want %q", got, want)
		}
	}
}
