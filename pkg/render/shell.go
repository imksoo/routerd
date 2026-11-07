// SPDX-License-Identifier: BSD-3-Clause

package render

import "strings"

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// Preserve literal command text while assigning it inside a shell double-quoted
// variable. Its inner argument quotes are interpreted only by the later eval.
func shellDoubleQuoteContents(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "$", "\\$", "`", "\\`").Replace(value)
}
