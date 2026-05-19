// SPDX-License-Identifier: BSD-3-Clause

package version

import "testing"

func TestStringIncludesCommitWhenInjected(t *testing.T) {
	old := Commit
	t.Cleanup(func() { Commit = old })

	Commit = ""
	if got := String(); got != Version {
		t.Fatalf("String() = %q, want %q", got, Version)
	}

	Commit = "abc1234"
	if got, want := String(), Version+" (abc1234)"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
