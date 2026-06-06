// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/hybrid"
)

type HybridRouteController struct {
	Router          *api.Router
	EffectiveRouter *api.Router
	Lowerings       []hybrid.HybridLowering
	Store           Store
}

func (c HybridRouteController) Reconcile(ctx context.Context) error {
	_ = ctx
	if c.Router == nil || c.Store == nil {
		return nil
	}
	effective := c.Router
	if c.EffectiveRouter != nil {
		effective = c.EffectiveRouter
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion {
			continue
		}
		switch resource.Kind {
		case "HybridRoute":
			status := hybrid.StatusForHybridRoute(*effective, resource, c.Lowerings, c.Store)
			if err := c.Store.SaveObjectStatus(api.HybridAPIVersion, "HybridRoute", resource.Metadata.Name, status); err != nil {
				return err
			}
		}
	}
	return nil
}
