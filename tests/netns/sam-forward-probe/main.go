// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

// This executable is only a netns test driver. Never run it in a host namespace.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/chain"
)

type statusStore map[string]map[string]any

func (s statusStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return s[apiVersion+"/"+kind+"/"+name]
}

func (s statusStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s statusStore) MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error {
	status := s.ObjectStatus(apiVersion, kind, name)
	if status == nil {
		status = map[string]any{}
	}
	for key, value := range updates {
		status[key] = value
	}
	return s.SaveObjectStatus(apiVersion, kind, name, status)
}

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
	c := chain.SAMController{Router: &api.Router{}, Store: statusStore{}}
	if err := c.Reconcile(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
