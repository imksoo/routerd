package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"routerd/pkg/controlapi"
	"routerd/pkg/platform"
)

var platformDefaults, _ = platform.Current()

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return errors.New("missing command")
	}
	switch args[0] {
	case "status":
		return statusCommand(args[1:], stdout)
	case "show":
		return showCommand(args[1:], stdout, stderr)
	case "reconcile":
		return reconcileCommand(args[1:], stdout)
	case "plan":
		return reconcileCommand(append([]string{"--dry-run"}, args[1:]...), stdout)
	case "help", "-h", "--help":
		usage(stdout)
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func showCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return errors.New("missing show subcommand")
	}
	switch args[0] {
	case "napt", "conntrack":
		return showNAPTCommand(args[1:], stdout)
	default:
		return fmt.Errorf("unknown show subcommand %q", args[0])
	}
}

func statusCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	status, err := controlapi.NewUnixClient(*socketPath).Status(ctx)
	if err != nil {
		return err
	}
	return writeJSON(stdout, status)
}

func reconcileCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	dryRun := fs.Bool("dry-run", false, "plan without applying changes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(*socketPath).Reconcile(ctx, controlapi.ReconcileRequest{DryRun: *dryRun})
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func showNAPTCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("show napt", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 10*time.Second, "request timeout")
	limit := fs.Int("limit", 100, "maximum entries to return; 0 returns only summary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	table, err := controlapi.NewUnixClient(*socketPath).NAPT(ctx, *limit)
	if err != nil {
		return err
	}
	return writeJSON(stdout, table)
}

func writeJSON(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func defaultRuntimeDir() string {
	return platformDefaults.RuntimeDir
}

func defaultSocketPath() string {
	return platformDefaults.SocketFile()
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerctl <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  status [--socket <path>]")
	fmt.Fprintln(w, "  show napt [--socket <path>] [--limit <n>]")
	fmt.Fprintln(w, "  plan [--socket <path>]")
	fmt.Fprintln(w, "  reconcile [--socket <path>] [--dry-run]")
}
