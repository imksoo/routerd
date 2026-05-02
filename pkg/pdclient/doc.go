// Package pdclient contains an experimental, OS-independent DHCPv6-PD
// client core.
//
// The package intentionally does not know how packets reach the network.
// Linux, FreeBSD, and test transports all implement Transport, while this
// package owns only DHCPv6 payload encoding/decoding and the prefix
// delegation state machine.
package pdclient
