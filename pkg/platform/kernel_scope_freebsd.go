// SPDX-License-Identifier: BSD-3-Clause

package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/subprocessio"
	"golang.org/x/sys/unix"
)

func KernelNetworkScope(ctx context.Context) (string, error) {
	boot, err := unix.SysctlTimeval("kern.boottime")
	if err != nil {
		return "", err
	}
	// VNET jails may reuse interface indexes independently of the host. ps's
	// native jid field binds the receipt to the process's actual jail as well.
	readCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	cmd := exec.CommandContext(readCtx, "/bin/ps", "-o", "jid=", "-p", strconv.Itoa(os.Getpid()))
	output := subprocessio.NewCapture(128)
	cmd.Stdout = output
	if err := subprocessio.Run(cmd); err != nil {
		return "", fmt.Errorf("read native address jail identity: %w", err)
	}
	jid, err := strconv.ParseUint(strings.TrimSpace(output.String()), 10, 32)
	if err != nil || output.Exceeded() {
		return "", fmt.Errorf("invalid native address jail identity")
	}
	return fmt.Sprintf("freebsd:%d:%d:%d", boot.Sec, boot.Usec, jid), nil
}
