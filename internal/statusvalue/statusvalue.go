// SPDX-License-Identifier: BSD-3-Clause

// Package statusvalue contains the small, deliberately permissive codecs used
// at object-status compatibility boundaries.
package statusvalue

import (
	"fmt"
	"net/netip"
	"strings"
)

// Text converts a status value to trimmed display text.
func Text(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

// Field returns one map-shaped status field as display text.
func Field(status map[string]any, key string) string {
	if status == nil {
		return ""
	}
	return Text(status[key])
}

// StrictBool accepts only bool values and true/false strings.
func StrictBool(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

// ExtendedBool additionally accepts yes/no and 1/0 strings.
func ExtendedBool(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "yes", "1":
			return true, true
		case "false", "no", "0":
			return false, true
		}
	}
	return false, false
}

// BoolOrFalse accepts bool values and true strings, treating all other values
// as false. It intentionally uses EqualFold to preserve legacy callers.
func BoolOrFalse(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

// Address removes a prefix length from a valid IP prefix, otherwise returning
// the trimmed input unchanged.
func Address(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Addr().String()
	}
	return value
}
