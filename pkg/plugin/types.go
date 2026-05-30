// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

const (
	// PluginAPIVersion is the API group for routerd plugin protocol objects.
	PluginAPIVersion = api.PluginAPIVersion
)

// PluginRequest is the JSON object routerd sends to a trusted local plugin on
// stdin. Plugins are local executables installed under the platform plugin
// directory, not remote code fetched by routerd.
type PluginRequest struct {
	api.TypeMeta `yaml:",inline" json:",inline"`
	Metadata     api.ObjectMeta    `yaml:"metadata" json:"metadata"`
	Spec         PluginRequestSpec `yaml:"spec" json:"spec"`
}

// PluginRequestSpec describes the reconcile trigger and generation context for
// one plugin invocation.
type PluginRequestSpec struct {
	Trigger                   TriggerRef `yaml:"trigger" json:"trigger"`
	StartupConfigHash         string     `yaml:"startupConfigHash" json:"startupConfigHash"`
	EffectiveGeneration       int64      `yaml:"effectiveGeneration" json:"effectiveGeneration"`
	PreviousDynamicGeneration int64      `yaml:"previousDynamicGeneration" json:"previousDynamicGeneration"`
	Now                       time.Time  `yaml:"now" json:"now"`
	// Events carries the matched federation events that triggered this
	// invocation (EventSubscriptionController, ADR 0006 Phase 3). It is empty
	// for interval/manual/event-bus triggers. SECURITY: these are observed
	// facts only — routerd never places config or secrets here, preserving the
	// no-config/no-secret-to-plugin guarantee.
	Events []PluginMatchedEvent `yaml:"events,omitempty" json:"events,omitempty"`
}

// PluginMatchedEvent is a lightweight DTO describing a federation event that a
// subscription matched, delivered to the plugin on stdin. It is intentionally
// distinct from PluginResult.Status.Events (PluginEvent, an outbound
// informational event a plugin emits) and deliberately does NOT import
// pkg/federation, so pkg/plugin stays free of federation/state dependencies.
type PluginMatchedEvent struct {
	ID         string            `json:"id" yaml:"id"`
	Group      string            `json:"group" yaml:"group"`
	SourceNode string            `json:"sourceNode,omitempty" yaml:"sourceNode,omitempty"`
	Type       string            `json:"type" yaml:"type"`
	Subject    string            `json:"subject,omitempty" yaml:"subject,omitempty"`
	DedupeKey  string            `json:"dedupeKey,omitempty" yaml:"dedupeKey,omitempty"`
	Payload    map[string]string `json:"payload,omitempty" yaml:"payload,omitempty"`
	ObservedAt time.Time         `json:"observedAt,omitempty" yaml:"observedAt,omitempty"`
	ExpiresAt  time.Time         `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`
}

// TriggerRef identifies why a plugin was invoked.
type TriggerRef struct {
	Type  string `yaml:"type" json:"type"`
	Topic string `yaml:"topic,omitempty" json:"topic,omitempty"`
}

// PluginResult is the JSON object a plugin writes to stdout. routerd always
// validates plugin output before storing dynamic configuration or rendering an
// effective configuration.
type PluginResult struct {
	api.TypeMeta `yaml:",inline" json:",inline"`
	Metadata     api.ObjectMeta     `yaml:"metadata" json:"metadata"`
	Status       PluginResultStatus `yaml:"status" json:"status"`
}

// PluginResultStatus describes dynamic resources, directives, display-only
// action plans, and events observed by a plugin.
type PluginResultStatus struct {
	ObservedAt  time.Time                              `yaml:"observedAt" json:"observedAt"`
	TTL         string                                 `yaml:"ttl" json:"ttl"`
	Resources   []api.Resource                         `yaml:"resources" json:"resources"`
	Directives  []dynamicconfig.DynamicConfigDirective `yaml:"directives" json:"directives"`
	ActionPlans []ActionPlan                           `yaml:"actionPlans" json:"actionPlans"`
	Events      []PluginEvent                          `yaml:"events" json:"events"`
}

// ActionPlan is a plugin-proposed provider operation for dry-run and display
// only. MVP routerd never executes ActionPlans.
type ActionPlan struct {
	Name     string            `yaml:"name" json:"name"`
	Provider string            `yaml:"provider" json:"provider"`
	Action   string            `yaml:"action" json:"action"`
	Target   map[string]string `yaml:"target" json:"target"`
	Undo     *ActionUndo       `yaml:"undo,omitempty" json:"undo,omitempty"`
}

// ActionUndo describes the inverse provider action for display only.
type ActionUndo struct {
	Action string `yaml:"action" json:"action"`
}

// PluginEvent is an informational event emitted by a plugin.
type PluginEvent struct {
	Type       string            `yaml:"type" json:"type"`
	Message    string            `yaml:"message" json:"message"`
	Attributes map[string]string `yaml:"attributes,omitempty" json:"attributes,omitempty"`
}
