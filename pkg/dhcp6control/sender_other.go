//go:build !linux && !freebsd

package dhcp6control

import (
	"context"
	"fmt"
)

type AFPacketSender struct{}

func (AFPacketSender) SendFrame(context.Context, string, []byte) error {
	return fmt.Errorf("active DHCPv6 frame sender is not implemented on this OS")
}
