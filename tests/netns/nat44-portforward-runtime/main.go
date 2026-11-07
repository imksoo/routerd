// SPDX-License-Identifier: BSD-3-Clause
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controller/nat44"
	"github.com/imksoo/routerd/pkg/platform"
)

type statusStore struct {
	values map[string]map[string]any
}

func (s *statusStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	if s.values == nil {
		s.values = make(map[string]map[string]any)
	}
	s.values[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s *statusStore) ReadObjectStatus(apiVersion, kind, name string) (map[string]any, error) {
	return s.ObjectStatus(apiVersion, kind, name), nil
}

func (s *statusStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return s.values[apiVersion+"/"+kind+"/"+name]
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: nat44-portforward-runtime CONFIG_FILE NFTABLES_FILE")
		os.Exit(2)
	}
	router, err := config.Load(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	controller := nat44.Controller{
		OS:           platform.OSLinux,
		Router:       router,
		Store:        &statusStore{},
		NftablesPath: os.Args[2],
		NftCommand:   "nft",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := controller.Reconcile(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
