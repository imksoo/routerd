// SPDX-License-Identifier: BSD-3-Clause
//go:build !linux && !freebsd

package dhcpeventqueue

import "errors"

func supportedPlatform() error {
	return errors.New("durable DHCP event queue requires Linux or FreeBSD")
}
func privatePath(string, bool) error      { return supportedPlatform() }
func lockConsumer(string) (func(), error) { return nil, supportedPlatform() }
