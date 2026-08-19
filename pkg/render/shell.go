// SPDX-License-Identifier: BSD-3-Clause

package render

import "strings"

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
