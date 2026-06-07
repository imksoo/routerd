// SPDX-License-Identifier: BSD-3-Clause

package version

import "testing"

func TestStringIncludesCommitWhenInjected(t *testing.T) {
	oldVersion := Version
	oldCommit := Commit
	t.Cleanup(func() {
		Version = oldVersion
		Commit = oldCommit
	})

	Version = "vtest-version"
	Commit = ""
	if got := String(); got != Version {
		t.Fatalf("String() = %q, want %q", got, Version)
	}

	Commit = "abc1234"
	if got, want := String(), Version+" (abc1234)"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
