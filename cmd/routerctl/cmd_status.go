// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/imksoo/routerd/pkg/controlapi"
)

func statusCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"routerd の現在の status (resource phase / conditions など) を読み取り専用 socket 経由で取得する。",
			"routerctl status -o json\n"+
				"routerctl status -o yaml\n"+
				"routerctl status --socket /run/routerd/status.sock")
	}
	socketPath := fs.String("socket", defaultStatusSocketPath(), "routerd read-only status Unix domain socket path")
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	output := "json"
	jsonOutput := fs.Bool("json", false, "output JSON")
	fs.StringVar(&output, "o", output, "output format: json, yaml")
	fs.StringVar(&output, "output", output, "output format: json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *jsonOutput {
		output = "json"
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	status, err := controlapi.NewUnixClient(*socketPath).Status(ctx)
	if err != nil {
		return err
	}
	switch output {
	case "", "json":
		return writeJSON(stdout, status)
	case "yaml":
		return writeYAML(stdout, status)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}
