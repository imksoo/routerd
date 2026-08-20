// SPDX-License-Identifier: BSD-3-Clause

package stringutil

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	if got := FirstNonEmpty("", " \t", " value ", "later"); got != " value " {
		t.Fatalf("FirstNonEmpty() = %q, want %q", got, " value ")
	}
	if got := FirstNonEmpty("", " \t"); got != "" {
		t.Fatalf("FirstNonEmpty(blank) = %q, want empty", got)
	}
}

func TestFirstNonBlank(t *testing.T) {
	if got := FirstNonBlank("", " \t", " value ", "later"); got != "value" {
		t.Fatalf("FirstNonBlank() = %q, want %q", got, "value")
	}
	if got := FirstNonBlank("", " \t"); got != "" {
		t.Fatalf("FirstNonBlank(blank) = %q, want empty", got)
	}
}

func TestFirstPresent(t *testing.T) {
	if got := FirstPresent("", " ", "value"); got != " " {
		t.Fatalf("FirstPresent() = %q, want whitespace value", got)
	}
}

func TestUniqueTrimmedSorted(t *testing.T) {
	got := UniqueTrimmedSorted([]string{" beta ", "", "alpha", "beta", "  "})
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("UniqueTrimmedSorted() = %#v", got)
	}
	if got := UniqueTrimmedSorted(nil); got != nil {
		t.Fatalf("UniqueTrimmedSorted(nil) = %#v, want nil", got)
	}
}

func TestResourceName(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{input: " CloudEdge/10.77.60.20 ", want: "cloudedge-10-77-60-20"},
		{input: "a--b", want: "a--b"},
		{input: "---", want: "resource"},
	} {
		if got := ResourceName(tc.input); got != tc.want {
			t.Errorf("ResourceName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestConservativeName(t *testing.T) {
	for _, tc := range []struct {
		input, fallback, want string
	}{
		{input: " CloudEdge/10.77.60.20 ", fallback: "default", want: "cloudedge-10-77-60-20"},
		{input: "a--b", fallback: "default", want: "a-b"},
		{input: "---", fallback: "mobility", want: "mobility"},
	} {
		if got := ConservativeName(tc.input, tc.fallback); got != tc.want {
			t.Errorf("ConservativeName(%q, %q) = %q, want %q", tc.input, tc.fallback, got, tc.want)
		}
	}
}
