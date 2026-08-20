// SPDX-License-Identifier: BSD-3-Clause

package version

var Version = "v20260820.1911"

var Commit = ""

func String() string {
	if Commit == "" {
		return Version
	}
	return Version + " (" + Commit + ")"
}
