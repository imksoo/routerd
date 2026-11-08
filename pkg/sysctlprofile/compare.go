// SPDX-License-Identifier: BSD-3-Clause

package sysctlprofile

import (
	"fmt"
	"strconv"
	"strings"
)

func ValueMatches(current, expected, compare string) (bool, error) {
	switch compare {
	case "", "exact":
		return current == expected, nil
	case "atLeast":
		currentFields := strings.Fields(current)
		expectedFields := strings.Fields(expected)
		if len(currentFields) != len(expectedFields) {
			return false, fmt.Errorf("field count mismatch")
		}
		for i := range expectedFields {
			c, err := strconv.ParseInt(currentFields[i], 10, 64)
			if err != nil {
				return false, err
			}
			e, err := strconv.ParseInt(expectedFields[i], 10, 64)
			if err != nil {
				return false, err
			}
			if c < e {
				return false, nil
			}
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported compare %q", compare)
	}
}
