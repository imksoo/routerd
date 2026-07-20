// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package bgp

func defaultFIBSyncer() FIBSyncer {
	return newFreeBSDFIBSyncer(nil)
}
