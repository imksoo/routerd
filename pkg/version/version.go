// SPDX-License-Identifier: BSD-3-Clause

package version

const Version = "v20260522.1045"

var Commit = ""

func String() string {
	if Commit == "" {
		return Version
	}
	return Version + " (" + Commit + ")"
}
