// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"os/exec"

	"github.com/imksoo/routerd/internal/hostcmd"
	"github.com/imksoo/routerd/pkg/subprocessio"
)

func runHostCommand(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, hostcmd.Resolve(name), args...)
	stdout := subprocessio.NewCapture(subprocessio.StdoutLimit)
	stderr := subprocessio.NewCapture(subprocessio.StderrLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := subprocessio.Run(cmd)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if limitErr := subprocessio.LimitError(stdout, stderr); limitErr != nil {
		return nil, nil, limitErr
	}
	return stdout.Bytes(), stderr.Bytes(), err
}
