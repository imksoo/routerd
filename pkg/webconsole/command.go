// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/hostcmd"
)

func commandOutputTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, hostCommandPath(name), args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// A descendant retaining stdout must not keep a cancelled observation open.
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), ctx.Err())
	}
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, message)
		}
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

func hostCommandPath(name string) string {
	return hostcmd.Resolve(name)
}
