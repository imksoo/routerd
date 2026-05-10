// SPDX-License-Identifier: BSD-3-Clause

//go:build !freebsd

package main

import (
	"context"
	"fmt"

	"routerd/pkg/logstore"
)

func runPflogDaemon(_ context.Context, opts options, _ *logstore.FirewallLog) error {
	return fmt.Errorf("--pflog-interface %s is only supported on FreeBSD", opts.pflogInterface)
}
