// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	gobgpserver "github.com/osrg/gobgp/v3/pkg/server"

	"routerd/pkg/version"
)

const defaultSocketPath = "/run/routerd/bgp/gobgp.sock"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "daemon"
	if len(args) > 0 && args[0] == "version" {
		fmt.Println(version.String())
		return nil
	}
	if len(args) > 0 && args[0] != "daemon" {
		return fmt.Errorf("unknown command %q", args[0])
	}
	if len(args) > 0 {
		args = args[1:]
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	socketPath := fs.String("socket", defaultSocketPath, "GoBGP gRPC Unix socket path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *socketPath == "" {
		return fmt.Errorf("--socket is required")
	}
	if err := os.MkdirAll(filepath.Dir(*socketPath), 0755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	_ = os.Remove(*socketPath)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	server := gobgpserver.NewBgpServer(gobgpserver.GrpcListenAddress("unix://" + *socketPath))
	go server.Serve()
	logger.Info("routerd-bgp daemon started", "socket", *socketPath, "version", version.String())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	logger.Info("routerd-bgp daemon stopping")
	server.Stop()
	_ = os.Remove(*socketPath)
	return nil
}
