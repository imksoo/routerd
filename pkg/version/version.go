// SPDX-License-Identifier: BSD-3-Clause

package version

var Version = "v20261127.1713"

var Commit = ""

func String() string {
	if Commit == "" {
		return Version
	}
	return Version + " (" + Commit + ")"
}
