// SPDX-License-Identifier: BSD-3-Clause

// Package mapsort contains stable map-key ordering helpers.
package mapsort

import "sort"

// Keys returns all keys in lexical order. It returns a non-nil empty slice for
// nil or empty maps so callers preserve existing JSON and status contracts.
func Keys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
