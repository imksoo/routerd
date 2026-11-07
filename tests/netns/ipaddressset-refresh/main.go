// SPDX-License-Identifier: BSD-3-Clause

// Drives the production resolver and nftables updater in an isolated namespace.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/state"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: ipaddressset-refresh CONFIG STATE_DIRECTORY")
	}
	router, err := config.Load(os.Args[1])
	if err != nil {
		return err
	}
	store, err := state.OpenSQLite(filepath.Join(os.Args[2], "address-sets.db"))
	if err != nil {
		return err
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	controller := chain.IPAddressSetController{Router: router, Store: store, RuntimeDir: os.Args[2]}
	if err := controller.Reconcile(ctx); err != nil {
		return err
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "IPAddressSet" {
			continue
		}
		status, err := store.ReadObjectStatus(resource.APIVersion, resource.Kind, resource.Metadata.Name)
		if err != nil {
			return err
		}
		if status["phase"] != "Applied" {
			return fmt.Errorf("%s did not apply: %v", resource.ID(), status)
		}
	}
	return nil
}
