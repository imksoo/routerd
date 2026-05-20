// SPDX-License-Identifier: BSD-3-Clause

package version

const Version = "v20260520.2227"

var Commit = ""

func String() string {
	if Commit == "" {
		return Version
	}
	return Version + " (" + Commit + ")"
}
