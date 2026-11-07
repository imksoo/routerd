// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/hostcmd"
	"github.com/imksoo/routerd/pkg/subprocessio"
)

func commandOutputTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, hostCommandPath(name), args...)
	stdout := subprocessio.NewCapture(subprocessio.StdoutLimit)
	stderr := subprocessio.NewCapture(subprocessio.StderrLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// A descendant retaining stdout must not keep a cancelled observation open.
	cmd.WaitDelay = 100 * time.Millisecond
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), ctx.Err())
	}
	if limitErr := subprocessio.LimitError(stdout, stderr); limitErr != nil {
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), limitErr)
	}
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return stdout.Bytes(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, message)
		}
		return stdout.Bytes(), fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

func hostCommandPath(name string) string {
	return hostcmd.Resolve(name)
}
