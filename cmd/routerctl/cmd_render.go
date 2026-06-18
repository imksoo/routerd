// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"fmt"
	"io"
)

func renderCommand(args []string, stdout io.Writer) error {
	_ = stdout
	if len(args) == 0 {
		return errors.New("render requires a supported target")
	}
	return fmt.Errorf("unknown render target %q", args[0])
}
