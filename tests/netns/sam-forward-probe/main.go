// SPDX-License-Identifier: BSD-3-Clause

// This executable is only a netns test driver. Never run it in a host namespace.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/state"
)

func main() {
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		panic(err)
	}
	host, err := os.Readlink("/proc/1/ns/net")
	if err != nil || current == host {
		fmt.Fprintln(os.Stderr, "SAM test driver requires a separate network namespace")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := chain.SAMController{Router: &api.Router{}, Store: state.NewJSON()}
	if err := c.Reconcile(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
