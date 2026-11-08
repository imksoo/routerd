// SPDX-License-Identifier: BSD-3-Clause

package sysctlprofile

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func SafeName(name string) string {
	name = strings.Trim(unsafeName.ReplaceAllString(name, "-"), "-.")
	if name == "" {
		name = "sysctl"
	}
	return name
}

func PersistentPath(baseDir, name string) string {
	return filepath.Join(baseDir, "90-routerd-"+SafeName(name)+".conf")
}

func PersistentContents(key, value string) []byte {
	return []byte(fmt.Sprintf("# Managed by routerd. Do not edit by hand.\n%s = %s\n", key, value))
}
