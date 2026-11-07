// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

func mustWhenFilteredRouter(t *testing.T, resolved bool, router *api.Router, store resourcequery.StateStore) *api.Router {
	t.Helper()
	var result *api.Router
	var err error
	if resolved {
		result, err = resourcequery.FilterRouterByResolvedWhen(router, store)
	} else {
		result, err = resourcequery.FilterRouterByWhen(router, store)
	}
	if err != nil {
		t.Fatal(err)
	}
	return result
}
