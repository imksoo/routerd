// SPDX-License-Identifier: BSD-3-Clause

// Package pdclient contains an experimental, OS-independent DHCPv6-PD
// client core.
//
// The package intentionally does not know how packets reach the network.
// Linux, FreeBSD, and test transports all implement Transport, while this
// package owns DHCPv6 payload encoding/decoding and the prefix delegation
// state machine. The wire model also retains IA_NA address associations; wire
// support alone does not acquire or configure an address on an interface.
package pdclient
