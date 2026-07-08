// SPDX-License-Identifier: BSD-3-Clause

//go:build !freebsd

package main

import (
	"context"
	"fmt"

	"github.com/imksoo/routerd/pkg/logstore"
	routerotel "github.com/imksoo/routerd/pkg/otel"
)

func runPflogDaemon(_ context.Context, opts options, _ *logstore.FirewallLog, _ firewallEntryRecorder, _ *routerotel.Runtime) error {
	return fmt.Errorf("--pflog-interface %s is only supported on FreeBSD", opts.pflogInterface)
}

func watchPFStateExpireLoop(_ context.Context, _ options, _ *logstore.FirewallLog) {}
