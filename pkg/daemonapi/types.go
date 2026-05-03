package daemonapi

import "time"

const (
	APIVersion = "daemon.routerd.net/v1alpha1"

	KindDaemonStatus   = "DaemonStatus"
	KindDaemonEvent    = "DaemonEvent"
	KindCommandRequest = "CommandRequest"
	KindCommandResult  = "CommandResult"
)

const (
	HealthOK       = "ok"
	HealthDegraded = "degraded"
	HealthFailed   = "failed"
	HealthUnknown  = "unknown"
)

const (
	PhaseStarting = "Starting"
	PhaseRunning  = "Running"
	PhaseBlocked  = "Blocked"
	PhaseDraining = "Draining"
	PhaseStopped  = "Stopped"
)

const (
	ResourcePhasePending    = "Pending"
	ResourcePhaseIdle       = "Idle"
	ResourcePhaseAcquiring  = "Acquiring"
	ResourcePhaseBound      = "Bound"
	ResourcePhaseRefreshing = "Refreshing"
	ResourcePhaseRebinding  = "Rebinding"
	ResourcePhaseExpired    = "Expired"
	ResourcePhaseLost       = "Lost"
	ResourcePhaseReleased   = "Released"
)

const (
	ConditionTrue    = "True"
	ConditionFalse   = "False"
	ConditionUnknown = "Unknown"
)

const (
	SeverityDebug   = "debug"
	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

const (
	CommandRenew       = "renew"
	CommandRebind      = "rebind"
	CommandRelease     = "release"
	CommandReload      = "reload"
	CommandStop        = "stop"
	CommandStart       = "start"
	CommandFlush       = "flush"
	CommandInfoRequest = "info-request"
)

const (
	EventDaemonStarted   = "routerd.daemon.lifecycle.started"
	EventDaemonReady     = "routerd.daemon.lifecycle.ready"
	EventDaemonStopped   = "routerd.daemon.lifecycle.stopped"
	EventDaemonCrashed   = "routerd.daemon.lifecycle.crashed"
	EventCommandReceived = "routerd.daemon.command.received"
	EventCommandExecuted = "routerd.daemon.command.executed"
	EventCommandRejected = "routerd.daemon.command.rejected"
	EventHealthChanged   = "routerd.daemon.health.changed"

	EventDHCPv6SolicitSent       = "routerd.dhcpv6.client.solicit.sent"
	EventDHCPv6AdvertiseReceived = "routerd.dhcpv6.client.advertise.received"
	EventDHCPv6RequestSent       = "routerd.dhcpv6.client.request.sent"
	EventDHCPv6ReplyReceived     = "routerd.dhcpv6.client.reply.received"
	EventDHCPv6InfoRequestSent   = "routerd.dhcpv6.client.info-request.sent"
	EventDHCPv6InfoReplyReceived = "routerd.dhcpv6.client.info.reply"
	EventDHCPv6PrefixBound       = "routerd.dhcpv6.client.prefix.bound"
	EventDHCPv6PrefixRenewed     = "routerd.dhcpv6.client.prefix.renewed"
	EventDHCPv6PrefixRebound     = "routerd.dhcpv6.client.prefix.rebound"
	EventDHCPv6PrefixExpired     = "routerd.dhcpv6.client.prefix.expired"
	EventDHCPv6ServerLost        = "routerd.dhcpv6.client.server.lost"
)

type TypeMeta struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

type DaemonRef struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Instance string `json:"instance,omitempty"`
}

type ResourceRef struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type DaemonStatus struct {
	TypeMeta `json:",inline"`

	Daemon     DaemonRef         `json:"daemon"`
	Generation int64             `json:"generation,omitempty"`
	Phase      string            `json:"phase"`
	Health     string            `json:"health"`
	Since      time.Time         `json:"since,omitempty"`
	Conditions []Condition       `json:"conditions,omitempty"`
	Resources  []ResourceStatus  `json:"resources,omitempty"`
	Observed   map[string]string `json:"observed,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
}

type Condition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Severity           string    `json:"severity,omitempty"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastTransitionTime time.Time `json:"lastTransitionTime,omitempty"`
	ObservedGeneration int64     `json:"observedGeneration,omitempty"`
}

type ResourceStatus struct {
	Resource   ResourceRef       `json:"resource"`
	Phase      string            `json:"phase"`
	Health     string            `json:"health"`
	Since      time.Time         `json:"since,omitempty"`
	Conditions []Condition       `json:"conditions"`
	Observed   map[string]string `json:"observed,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
}

type DaemonEvent struct {
	TypeMeta `json:",inline"`

	Cursor   string       `json:"cursor,omitempty"`
	Time     time.Time    `json:"time"`
	Daemon   DaemonRef    `json:"daemon"`
	Resource *ResourceRef `json:"resource,omitempty"`
	// Type carries the event topic. The JSON field remains "type" to keep the
	// wire format compact and compatible with earlier daemon API experiments.
	Type       string            `json:"type"`
	Severity   string            `json:"severity"`
	Reason     string            `json:"reason,omitempty"`
	Message    string            `json:"message,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type CommandRequest struct {
	TypeMeta `json:",inline"`

	Command    string            `json:"command"`
	Resource   *ResourceRef      `json:"resource,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type CommandResult struct {
	TypeMeta `json:",inline"`

	Command    string            `json:"command"`
	Accepted   bool              `json:"accepted"`
	Message    string            `json:"message,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

func NewStatus(daemon DaemonRef) DaemonStatus {
	return DaemonStatus{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: KindDaemonStatus},
		Daemon:   daemon,
		Phase:    PhaseStarting,
		Health:   HealthUnknown,
	}
}

func NewEvent(daemon DaemonRef, eventType, severity string) DaemonEvent {
	return DaemonEvent{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: KindDaemonEvent},
		Time:     time.Now().UTC(),
		Daemon:   daemon,
		Type:     eventType,
		Severity: severity,
	}
}
