// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package chain

// FreeBSD retains the duplicated flag in ifconfig observations. Its existing
// observation path owns DAD; Linux needs the transient deletion notification.
func watchIPv6DAD(int, string) (ipv6DADWatcher, error) { return nil, nil }
