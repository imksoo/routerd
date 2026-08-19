// SPDX-License-Identifier: BSD-3-Clause

// Package stringutil contains small, shared string helpers.
package stringutil

import (
	"sort"
	"strings"
)

// FirstNonEmpty returns the first value that is not blank. It preserves the
// selected value rather than returning its trimmed form.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// FirstNonBlank returns the first non-blank value after trimming whitespace.
// Use FirstNonEmpty when callers must preserve the selected value verbatim.
func FirstNonBlank(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// FirstPresent returns the first non-empty value without trimming it.
func FirstPresent(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// UniqueTrimmedSorted drops blank values, trims the remaining values, removes
// duplicates, and returns a stable lexical order. It returns nil when empty.
func UniqueTrimmedSorted(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// ResourceName returns the stable, conservative name fragment used by
// generated resources. It intentionally preserves a dash for every
// non-alphanumeric rune so existing generated names stay stable.
func ResourceName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "resource"
	}
	return out
}

// ConservativeName returns a stable resource-name fragment. It retains only
// lowercase ASCII letters and digits, collapses every other run into one dash,
// and falls back when no usable characters remain.
func ConservativeName(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	if out := strings.Trim(b.String(), "-"); out != "" {
		return out
	}
	return fallback
}
