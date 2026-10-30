// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package chain

import "errors"

func setDSLiteAliasByIndex(int, string, string) error {
	return errors.New("DS-Lite indexed cookie installation is only available on Linux")
}
