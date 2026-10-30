// SPDX-License-Identifier: BSD-3-Clause

// Package pdclient contains an OS-independent DHCPv6 address and prefix
// delegation client core.
//
// The package intentionally does not know how packets reach the network.
// Linux, FreeBSD, and test transports all implement Transport, while this
// package owns DHCPv6 payload encoding/decoding and the shared IA_NA/IA_PD
// exchange state machine. Address and prefix lifetimes remain independent;
// platform controllers must apply and withdraw the resulting leases.
package pdclient
