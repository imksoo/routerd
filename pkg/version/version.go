// SPDX-License-Identifier: BSD-3-Clause

package version

const Version = "v20260525.0112"

var Commit = ""

func String() string {
	if Commit == "" {
		return Version
	}
	return Version + " (" + Commit + ")"
}
