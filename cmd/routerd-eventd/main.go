// SPDX-License-Identifier: BSD-3-Clause

// Command routerd-eventd is the CloudEdge Event Federation transport daemon
// (ADR 0006, Phase 2). It receives signed federation events from peers over
// HTTP, persists them idempotently, pushes locally-recorded events to
// configured peers with HMAC signing and bounded retries, and enforces
// EventGroup retention.
//
// Unlike the read-only observer daemons (ra-observer, dns-resolver), eventd
// opens the SQLite state store directly because it must persist received events
// and per-peer delivery state. This is a deliberate, documented exception.
//
// SCOPE: transport only. EventSubscription, plugin triggering,
// DynamicConfigPart generation, ARP observers, provider mutation, and
// controller/systemd auto-supervision are out of scope for this chunk.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/eventd"
	routerotel "github.com/imksoo/routerd/pkg/otel"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type options struct {
	configFile string
	stateFile  string
}

func run(args []string, stdout io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "daemon":
			return daemonCommand(args[1:])
		case "selftest":
			return selftest(args[1:], stdout)
		case "help", "-h", "--help":
			usage(stdout)
			return nil
		}
	}
	return daemonCommand(args)
}

func parseOptions(name string, args []string) (options, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := options{}
	fs.StringVar(&opts.configFile, "config-file", "", "eventd runtime config JSON (required)")
	fs.StringVar(&opts.stateFile, "state-file", "", "override config statePath (state DB file)")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.configFile == "" {
		return options{}, fmt.Errorf("--config-file is required")
	}
	return opts, nil
}

// selftest loads + validates the config and prints the effective config (with
// the secret omitted) without binding any socket.
func selftest(args []string, stdout io.Writer) error {
	opts, err := parseOptions("selftest", args)
	if err != nil {
		return err
	}
	cfg, err := eventd.LoadConfig(opts.configFile)
	if err != nil {
		return err
	}
	if opts.stateFile != "" {
		cfg.StatePath = opts.stateFile
	}
	cfg.SecretFile = "(redacted)"
	return json.NewEncoder(stdout).Encode(cfg)
}

func daemonCommand(args []string) error {
	opts, err := parseOptions("daemon", args)
	if err != nil {
		return err
	}
	cfg, err := eventd.LoadConfig(opts.configFile)
	if err != nil {
		return err
	}
	if opts.stateFile != "" {
		cfg.StatePath = opts.stateFile
	}

	secret, err := eventd.ReadSecretFile(cfg.SecretFile)
	if err != nil {
		return fmt.Errorf("read secret: %w", err)
	}

	store, err := routerstate.OpenSQLite(cfg.StatePath)
	if err != nil {
		return fmt.Errorf("open state store %s: %w", cfg.StatePath, err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	telemetry, err := routerotel.Setup(ctx, "routerd-eventd")
	if err != nil {
		return fmt.Errorf("otel setup: %w", err)
	}
	defer telemetry.ShutdownGracefully()
	metrics := eventd.NewMetrics(telemetry.Meter)

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			log.Printf("routerd-eventd: shutdown signal received")
			cancel()
		case <-ctx.Done():
		}
	}()

	receiver := eventd.NewReceiver(store, secret, cfg.Group, cfg.NodeName, cfg.ReplayWindow, time.Now)
	receiver.SetMetrics(metrics)
	pruner := eventd.NewPruner(store, cfg.Group, cfg.Retention, cfg.PruneInterval, time.Now)
	pruner.SetMetrics(metrics)
	go pruner.Run(ctx, func(err error) {
		log.Printf("routerd-eventd: prune error: %v", err)
	})

	// Drive the outbox: push locally-originated events to peers. A node with no
	// peers (or a push-only node) runs this as a harmless no-op / push-only.
	pusher := eventd.NewPusher(store, secret, cfg.Peers, cfg.PushRetry, &http.Client{Timeout: 30 * time.Second}, time.Now, time.Sleep)
	pusher.SetMetrics(metrics)
	outbox := eventd.NewOutbox(store, store, pusher, cfg.Group, cfg.NodeName, cfg.PushInterval, time.Now)
	outbox.SetMetrics(metrics)
	go outbox.Run(ctx, func(err error) {
		log.Printf("routerd-eventd: outbox error: %v", err)
	})

	return serve(ctx, cfg, receiver)
}

// serve binds the HTTP receiver and blocks until ctx is cancelled, then
// gracefully shuts the server down.
func serve(ctx context.Context, cfg eventd.Config, receiver *eventd.Receiver) error {
	if cfg.Listen.Port <= 0 {
		// No receiver bind configured: idle (push-only) until shutdown.
		log.Printf("routerd-eventd: no listen port configured; running push/prune only")
		<-ctx.Done()
		return nil
	}
	// Bind to the specific configured address (NOT 0.0.0.0 unless the operator
	// explicitly set it).
	addr := net.JoinHostPort(cfg.Listen.Address, strconv.Itoa(cfg.Listen.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	server := &http.Server{Handler: receiver.Handler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("routerd-eventd: listening on %s group=%s node=%s", addr, cfg.Group, cfg.NodeName)
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd-eventd daemon --config-file /path/eventd.json [--state-file /path/state.db]")
	fmt.Fprintln(w, "       routerd-eventd selftest --config-file /path/eventd.json")
}
