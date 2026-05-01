//go:build !linux

package dhcp6recorder

import "fmt"

type AFPacketSource struct{}

func NewAFPacketSource(ifname string) (*AFPacketSource, error) {
	return nil, fmt.Errorf("DHCPv6 packet recorder is not implemented on this OS for interface %s", ifname)
}
