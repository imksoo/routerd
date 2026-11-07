// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestSystemdUnitPreservesEncodedEnvironmentAndLiteralSemicolon(t *testing.T) {
	got := string(SystemdUnit("fixture.service", api.SystemdUnitSpec{ExecStart: []string{"/bin/echo", ";", "literal"}, Environment: []string{"OTEL_RESOURCE_ATTRIBUTES=site=edge%2Clab"}}))
	if !strings.Contains(got, `Environment="OTEL_RESOURCE_ATTRIBUTES=site=edge%%2Clab"`) || !strings.Contains(got, `ExecStart=/bin/echo \; literal`) {
		t.Fatalf("unit expanded a literal environment specifier or command separator:\n%s", got)
	}
	if got := systemdEnvironmentLines([]string{"HEADER=a%20b"}); got != "Environment=\"HEADER=a%%20b\"\n" {
		t.Fatalf("synthetic daemon environment changed encoded value: %s", got)
	}
}

func TestSystemdUnitPreservesEmptyAndMultilineArguments(t *testing.T) {
	unit := string(SystemdUnit("arguments.service", api.SystemdUnitSpec{ExecStart: []string{"/bin/echo", "", "first\nsecond"}, ExecStartPre: []string{"/usr/bin/test", "-z", ""}}))
	if !strings.Contains(unit, "ExecStart=/bin/echo \"\" \"first\\nsecond\"\n") {
		t.Fatalf("service argv lost empty argument or split a literal newline into unit directives:\n%s", unit)
	}
	if !strings.Contains(unit, "ExecStartPre=/usr/bin/test -z \"\"\n") {
		t.Fatalf("pre-start argv lost its required empty argument:\n%s", unit)
	}
}
