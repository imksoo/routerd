package dohproxy

import (
	"context"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/dohproxy"
)

type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

type Controller struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	DryRun bool
}

func (c Controller) Start(ctx context.Context) {
	_ = c.Reconcile(ctx)
}

func (c Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "DoHProxy" {
			continue
		}
		spec, err := resource.DoHProxySpec()
		if err != nil {
			return err
		}
		spec = dohproxy.NormalizeSpec(spec)
		phase := "Pending"
		if c.DryRun {
			phase = "Applied"
		}
		status := map[string]any{
			"phase":         phase,
			"backend":       spec.Backend,
			"listenAddress": spec.ListenAddress,
			"listenPort":    spec.ListenPort,
			"updatedAt":     time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "DoHProxy", resource.Metadata.Name, status); err != nil {
			return err
		}
		if c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd"}, "routerd.doh-proxy.configured", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DoHProxy", Name: resource.Metadata.Name}
			event.Attributes = map[string]string{"backend": spec.Backend, "listenAddress": spec.ListenAddress}
			_ = c.Bus.Publish(ctx, event)
		}
	}
	return nil
}
