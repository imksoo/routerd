// SPDX-License-Identifier: BSD-3-Clause

package statusvalue

import "testing"

func TestTextAndField(t *testing.T) {
	var nilPointer *int
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{name: "nil", want: ""},
		{name: "trimmed string", value: " value ", want: "value"},
		{name: "number", value: 42, want: "42"},
		{name: "typed nil", value: nilPointer, want: "<nil>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Text(tc.value); got != tc.want {
				t.Fatalf("Text(%#v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
	if got := Field(nil, "value"); got != "" {
		t.Fatalf("Field(nil) = %q, want empty", got)
	}
	if got := Field(map[string]any{"value": " text "}, "value"); got != "text" {
		t.Fatalf("Field(value) = %q, want text", got)
	}
}

func TestBooleanCodecs(t *testing.T) {
	for _, tc := range []struct {
		value      any
		strict     bool
		strictOK   bool
		extended   bool
		extendedOK bool
		orFalse    bool
	}{
		{value: true, strict: true, strictOK: true, extended: true, extendedOK: true, orFalse: true},
		{value: false, strict: false, strictOK: true, extended: false, extendedOK: true, orFalse: false},
		{value: " TRUE ", strict: true, strictOK: true, extended: true, extendedOK: true, orFalse: true},
		{value: " false ", strict: false, strictOK: true, extended: false, extendedOK: true, orFalse: false},
		{value: "yes", strict: false, strictOK: false, extended: true, extendedOK: true, orFalse: false},
		{value: "0", strict: false, strictOK: false, extended: false, extendedOK: true, orFalse: false},
		{value: "unknown", strict: false, strictOK: false, extended: false, extendedOK: false, orFalse: false},
		{value: 1, strict: false, strictOK: false, extended: false, extendedOK: false, orFalse: false},
	} {
		if got, ok := StrictBool(tc.value); got != tc.strict || ok != tc.strictOK {
			t.Fatalf("StrictBool(%#v) = (%t, %t), want (%t, %t)", tc.value, got, ok, tc.strict, tc.strictOK)
		}
		if got, ok := ExtendedBool(tc.value); got != tc.extended || ok != tc.extendedOK {
			t.Fatalf("ExtendedBool(%#v) = (%t, %t), want (%t, %t)", tc.value, got, ok, tc.extended, tc.extendedOK)
		}
		if got := BoolOrFalse(tc.value); got != tc.orFalse {
			t.Fatalf("BoolOrFalse(%#v) = %t, want %t", tc.value, got, tc.orFalse)
		}
	}
}

func TestAddress(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  string
	}{
		{value: "", want: ""},
		{value: " 10.0.0.1/24 ", want: "10.0.0.1"},
		{value: "2001:db8::1/64", want: "2001:db8::1"},
		{value: "  invalid  ", want: "invalid"},
		{value: "10.0.0.1", want: "10.0.0.1"},
	} {
		if got := Address(tc.value); got != tc.want {
			t.Fatalf("Address(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}
}
