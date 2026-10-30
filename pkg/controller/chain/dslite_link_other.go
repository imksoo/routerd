// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package chain

import (
	"context"
	"errors"
)

func setDSLiteAliasByIndex(int, string, string) error {
	return errors.New("DS-Lite indexed cookie installation is only available on Linux")
}

func deleteDSLiteByIndex(context.Context, int, string, string, string) error {
	return errors.New("DS-Lite indexed deletion is only available on Linux")
}
