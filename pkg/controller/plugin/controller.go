// SPDX-License-Identifier: BSD-3-Clause

// Package plugin owns declared local Plugin triggers within one configuration.
// The framework's single worker serializes due-state and process publication.
package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	routerplugin "github.com/imksoo/routerd/pkg/plugin"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type DataStore interface {
	GetDynamicConfigPartsBySource(string) ([]routerstate.DynamicConfigPartRecord, error)
	PublishPluginResult(context.Context, int64, routerstate.DynamicConfigPartRecord) error
}

type PluginRunner func(context.Context, api.PluginSpec, string, routerplugin.RunOptions) (routerplugin.PluginResult, routerplugin.RunOutcome, error)

type interval struct {
	every time.Duration
	due   time.Time
}
type preparedPlugin struct {
	name      string
	spec      api.PluginSpec
	context   routerplugin.PluginContext
	maxTTL    time.Duration
	intervals []interval
	topics    []string
}

type Controller struct {
	Store       DataStore
	Bus         *bus.Bus
	DryRun      bool
	Now         func() time.Time
	Runner      PluginRunner
	plans       []preparedPlugin
	topics      []string
	startupHash string
}

// New prepares startup intent once. It neither invokes plugins nor consumes due
// work, so a preview or standby preparation cannot acknowledge a live trigger.
func New(router *api.Router, store DataStore, eventBus *bus.Bus, dryRun bool) (*Controller, error) {
	c := &Controller{Store: store, Bus: eventBus, DryRun: dryRun}
	if router == nil {
		return c, nil
	}
	sourceTriggers := map[string][]api.PluginTrigger{}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.PluginAPIVersion || resource.Kind != "DynamicConfigSource" {
			continue
		}
		spec, err := resource.DynamicConfigSourceSpec()
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(spec.PluginRef)
		sourceTriggers[name] = append(sourceTriggers[name], spec.Triggers...)
	}
	topics := map[string]bool{"routerd.controller.bootstrap": true}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.PluginAPIVersion || resource.Kind != "Plugin" {
			continue
		}
		spec, err := resource.PluginSpec()
		if err != nil {
			return nil, err
		}
		name := resource.Metadata.Name
		triggers := append(append([]api.PluginTrigger{}, spec.Triggers...), sourceTriggers[name]...)
		delete(sourceTriggers, name)
		if len(triggers) == 0 {
			continue
		}
		plan := preparedPlugin{name: name, spec: spec}
		intervals, eventTopics := map[time.Duration]bool{}, map[string]bool{}
		for _, trigger := range triggers {
			switch strings.TrimSpace(trigger.Type) {
			case "interval":
				every, err := time.ParseDuration(strings.TrimSpace(trigger.Every))
				if err != nil || every <= 0 {
					return nil, fmt.Errorf("Plugin %s has invalid interval %q", name, trigger.Every)
				}
				intervals[every] = true
			case "event":
				eventTopics[strings.TrimSpace(trigger.Topic)] = true
			default:
				return nil, fmt.Errorf("Plugin %s has unsupported trigger %q", name, trigger.Type)
			}
		}
		for every := range intervals {
			plan.intervals = append(plan.intervals, interval{every: every})
		}
		sort.Slice(plan.intervals, func(i, j int) bool { return plan.intervals[i].every < plan.intervals[j].every })
		for topic := range eventTopics {
			plan.topics = append(plan.topics, topic)
			topics[topic] = true
		}
		sort.Strings(plan.topics)
		plan.context, err = routerplugin.BuildPluginContext(spec.Context.Resources, router.Spec.Resources)
		if err != nil {
			return nil, fmt.Errorf("prepare Plugin %s context: %w", name, err)
		}
		plan.maxTTL, err = routerplugin.SourceTTL(router, name)
		if err != nil {
			return nil, err
		}
		c.plans = append(c.plans, plan)
	}
	for name, triggers := range sourceTriggers {
		if len(triggers) != 0 {
			return nil, fmt.Errorf("triggered DynamicConfigSource references missing Plugin %q", name)
		}
	}
	if len(c.plans) != 0 {
		if store == nil && !dryRun {
			return nil, errors.New("declared Plugin triggers require a publication store")
		}
		data, err := json.Marshal(router)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(data)
		c.startupHash = hex.EncodeToString(digest[:])
	}
	for topic := range topics {
		c.topics = append(c.topics, topic)
	}
	sort.Strings(c.topics)
	return c, nil
}

func (c *Controller) Subscriptions() []bus.Subscription {
	return []bus.Subscription{{Topics: c.topics}}
}

func (c *Controller) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Controller) NextAfter() time.Duration {
	if c.DryRun {
		return 0
	}
	now, next := c.now(), time.Duration(0)
	for _, plan := range c.plans {
		for _, interval := range plan.intervals {
			if interval.due.IsZero() || !interval.due.After(now) {
				return time.Millisecond
			}
			if delay := interval.due.Sub(now); next == 0 || delay < next {
				next = delay
			}
		}
	}
	return next
}

func (c *Controller) HandleEvent(ctx context.Context, event daemonapi.DaemonEvent) error {
	topic := event.Type
	if topic == "routerd.controller.bootstrap" || topic == "routerd.controller.periodic" {
		topic = ""
	}
	_, err := c.reconcile(ctx, topic)
	return err
}

func (c *Controller) Periodic(ctx context.Context) (bool, error) { return c.reconcile(ctx, "") }

func (c *Controller) reconcile(ctx context.Context, topic string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if c.DryRun || len(c.plans) == 0 {
		return false, nil
	}
	if c.Store == nil {
		return false, errors.New("Plugin publication store is unavailable")
	}
	worked := false
	var failures []error
	for i := range c.plans {
		if err := ctx.Err(); err != nil {
			return worked, errors.Join(append(failures, err)...)
		}
		plan, now := &c.plans[i], c.now()
		var due []int
		for j, interval := range plan.intervals {
			if interval.due.IsZero() || !interval.due.After(now) {
				due = append(due, j)
			}
		}
		matched := false
		if topic != "" {
			for _, pattern := range plan.topics {
				if bus.MatchTopic(pattern, topic) {
					matched = true
					break
				}
			}
		}
		if len(due) == 0 && !matched {
			continue
		}
		trigger := routerplugin.TriggerRef{Type: "interval"}
		if matched {
			trigger.Type, trigger.Topic = "event", topic
		}
		worked = true
		err := c.invoke(ctx, *plan, trigger, now)
		finished := c.now()
		for _, j := range due {
			plan.intervals[j].due = finished.Add(plan.intervals[j].every)
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("Plugin %s: %w", plan.name, err))
		}
	}
	return worked, errors.Join(failures...)
}

func (c *Controller) invoke(ctx context.Context, plan preparedPlugin, trigger routerplugin.TriggerRef, now time.Time) error {
	source := "Plugin/" + plan.name
	previous, err := c.Store.GetDynamicConfigPartsBySource(source)
	if err != nil {
		return err
	}
	var generation int64
	var last *routerstate.DynamicConfigPartRecord
	for i := range previous {
		if previous[i].Generation > generation {
			generation, last = previous[i].Generation, &previous[i]
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	run := c.Runner
	if run == nil {
		run = routerplugin.Run
	}
	result, _, err := run(ctx, plan.spec, plan.name, routerplugin.RunOptions{Now: now, PreviousGeneration: generation, StartupConfigHash: c.startupHash, Trigger: trigger, Context: plan.context})
	if cancelErr := ctx.Err(); cancelErr != nil {
		return errors.Join(err, cancelErr)
	}
	if err != nil {
		return err
	}
	now = c.now()
	part, err := routerplugin.DynamicConfigPartFromResult(source, generation+1, result, now)
	if err != nil {
		return err
	}
	if err := routerplugin.LimitDynamicConfigLifetime(&part, plan.maxTTL, now); err != nil {
		return err
	}
	record, err := codec.Encode(part)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.Store.PublishPluginResult(ctx, generation, record); err != nil {
		return err
	}
	if c.Bus != nil && (last == nil || last.Digest != record.Digest || last.EffectiveStatus(now) != record.EffectiveStatus(now)) {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "plugin", Kind: "controller"}, dynamicconfig.PartChangedEvent, daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.PluginAPIVersion, Kind: "Plugin", Name: plan.name}
		event.Attributes = map[string]string{"source": source, "digest": record.Digest}
		_ = c.Bus.Publish(ctx, event)
	}
	return nil
}
