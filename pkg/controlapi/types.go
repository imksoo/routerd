// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const APIVersion = "control.routerd.net/v1alpha1"

type ObjectMeta struct {
	Name string `json:"name" yaml:"name"`
}

type TypeMeta struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
}

type Status struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta   `json:"metadata" yaml:"metadata"`
	Status   StatusStatus `json:"status" yaml:"status"`
}

type StatusStatus struct {
	Phase               string               `json:"phase" yaml:"phase"`
	Generation          int64                `json:"generation,omitempty" yaml:"generation,omitempty"`
	LastApplyTime       time.Time            `json:"lastApplyTime,omitempty" yaml:"lastApplyTime,omitempty"`
	ResourceCount       int                  `json:"resourceCount,omitempty" yaml:"resourceCount,omitempty"`
	ResourcePhaseIssues []ResourcePhaseIssue `json:"resourcePhaseIssues,omitempty" yaml:"resourcePhaseIssues,omitempty"`
	Controllers         []ControllerStatus   `json:"controllers,omitempty" yaml:"controllers,omitempty"`
}

type ResourcePhaseIssue struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
	Name       string `json:"name" yaml:"name"`
	Phase      string `json:"phase" yaml:"phase"`
	Reason     string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message    string `json:"message,omitempty" yaml:"message,omitempty"`
}

type ControllerStatus struct {
	Name                  string                `json:"name" yaml:"name"`
	Mode                  string                `json:"mode" yaml:"mode"`
	Reason                ControllerModeReason  `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message               string                `json:"message,omitempty" yaml:"message,omitempty"`
	ResourceKinds         []string              `json:"resourceKinds,omitempty" yaml:"resourceKinds,omitempty"`
	Interval              string                `json:"interval,omitempty" yaml:"interval,omitempty"`
	LastTrigger           string                `json:"lastTrigger,omitempty" yaml:"lastTrigger,omitempty"`
	LastReconcileTime     *time.Time            `json:"lastReconcileTime,omitempty" yaml:"lastReconcileTime,omitempty"`
	LastSuccessTime       *time.Time            `json:"lastSuccessTime,omitempty" yaml:"lastSuccessTime,omitempty"`
	LastReloadAt          *time.Time            `json:"lastReloadAt,omitempty" yaml:"lastReloadAt,omitempty"`
	LastRestartAt         *time.Time            `json:"lastRestartAt,omitempty" yaml:"lastRestartAt,omitempty"`
	LastChangeReason      string                `json:"lastChangeReason,omitempty" yaml:"lastChangeReason,omitempty"`
	NextReconcileTime     *time.Time            `json:"nextReconcileTime,omitempty" yaml:"nextReconcileTime,omitempty"`
	ReconcileCount        int64                 `json:"reconcileCount,omitempty" yaml:"reconcileCount,omitempty"`
	ReconcileErrorCount   int64                 `json:"reconcileErrorCount,omitempty" yaml:"reconcileErrorCount,omitempty"`
	ConsecutiveErrorCount int64                 `json:"consecutiveErrorCount,omitempty" yaml:"consecutiveErrorCount,omitempty"`
	CurrentError          bool                  `json:"currentError" yaml:"currentError"`
	LastDuration          string                `json:"lastDuration,omitempty" yaml:"lastDuration,omitempty"`
	MaxDuration           string                `json:"maxDuration,omitempty" yaml:"maxDuration,omitempty"`
	MaxDurationAt         *time.Time            `json:"maxDurationAt,omitempty" yaml:"maxDurationAt,omitempty"`
	AverageDuration       string                `json:"averageDuration,omitempty" yaml:"averageDuration,omitempty"`
	LastDurationMillis    float64               `json:"lastDurationMillis,omitempty" yaml:"lastDurationMillis,omitempty"`
	MaxDurationMillis     float64               `json:"maxDurationMillis,omitempty" yaml:"maxDurationMillis,omitempty"`
	AverageDurationMillis float64               `json:"averageDurationMillis,omitempty" yaml:"averageDurationMillis,omitempty"`
	LastError             string                `json:"lastError,omitempty" yaml:"lastError,omitempty"`
	LastErrorTime         *time.Time            `json:"lastErrorTime,omitempty" yaml:"lastErrorTime,omitempty"`
	LastErrorClearedAt    *time.Time            `json:"lastErrorClearedAt,omitempty" yaml:"lastErrorClearedAt,omitempty"`
	ReconcileErrorHistory []ReconcileErrorEntry `json:"reconcileErrorHistory,omitempty" yaml:"reconcileErrorHistory,omitempty"`
}

// ReconcileErrorEntry describes a single failed reconcile attempt. The history
// is kept in-memory per process; restarting routerd clears the history.
type ReconcileErrorEntry struct {
	StartedAt    time.Time `json:"startedAt" yaml:"startedAt"`
	CompletedAt  time.Time `json:"completedAt" yaml:"completedAt"`
	Duration     string    `json:"duration" yaml:"duration"`
	DurationMs   float64   `json:"durationMs" yaml:"durationMs"`
	Trigger      string    `json:"trigger,omitempty" yaml:"trigger,omitempty"`
	ResourceKind string    `json:"resourceKind,omitempty" yaml:"resourceKind,omitempty"`
	ResourceName string    `json:"resourceName,omitempty" yaml:"resourceName,omitempty"`
	Error        string    `json:"error" yaml:"error"`
}

type ControllerModeReason string

const (
	ControllerModeReasonLive            ControllerModeReason = "Live"
	ControllerModeReasonManual          ControllerModeReason = "Manual"
	ControllerModeReasonOSUnsupported   ControllerModeReason = "OSUnsupported"
	ControllerModeReasonDependencyUnmet ControllerModeReason = "DependencyUnmet"
	ControllerModeReasonSpecDisabled    ControllerModeReason = "SpecDisabled"
	ControllerModeReasonUnknown         ControllerModeReason = "Unknown"
)

type Controllers struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta         `json:"metadata" yaml:"metadata"`
	Items    []ControllerStatus `json:"items" yaml:"items"`
}

type ApplyRequest struct {
	TypeMeta      `json:",inline" yaml:",inline"`
	CandidateYAML string `json:"candidateYaml,omitempty" yaml:"candidateYaml,omitempty"`
	Replace       bool   `json:"replace,omitempty" yaml:"replace,omitempty"`
	NoReconcile   bool   `json:"noReconcile,omitempty" yaml:"noReconcile,omitempty"`
	DryRun        bool   `json:"dryRun,omitempty" yaml:"dryRun,omitempty"`
}

type ApplyResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Result   apply.Result `json:"result" yaml:"result"`
}

type PlanRequest struct {
	TypeMeta      `json:",inline" yaml:",inline"`
	CandidateYAML string `json:"candidateYaml,omitempty" yaml:"candidateYaml,omitempty"`
	Replace       bool   `json:"replace,omitempty" yaml:"replace,omitempty"`
}

type PlanResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Result   apply.Result `json:"result" yaml:"result"`
}

type DeleteRequest struct {
	TypeMeta         `json:",inline" yaml:",inline"`
	Target           string `json:"target" yaml:"target"`
	TargetAPIVersion string `json:"targetApiVersion,omitempty" yaml:"targetApiVersion,omitempty"`
	DryRun           bool   `json:"dryRun,omitempty" yaml:"dryRun,omitempty"`
	Force            bool   `json:"force,omitempty" yaml:"force,omitempty"`
	NoReconcile      bool   `json:"noReconcile,omitempty" yaml:"noReconcile,omitempty"`
}

type DeleteResult struct {
	TypeMeta  `json:",inline" yaml:",inline"`
	Deleted   []string      `json:"deleted,omitempty" yaml:"deleted,omitempty"`
	Artifacts []string      `json:"artifacts,omitempty" yaml:"artifacts,omitempty"`
	DryRun    bool          `json:"dryRun,omitempty" yaml:"dryRun,omitempty"`
	Result    *apply.Result `json:"result,omitempty" yaml:"result,omitempty"`
}

type ValidateRequest struct {
	TypeMeta      `json:",inline" yaml:",inline"`
	CandidateYAML string `json:"candidateYaml,omitempty" yaml:"candidateYaml,omitempty"`
	Replace       bool   `json:"replace,omitempty" yaml:"replace,omitempty"`
}

type ValidateResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Valid    bool     `json:"valid" yaml:"valid"`
	Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	Error    string   `json:"error,omitempty" yaml:"error,omitempty"`
}

type SAMEnrollmentClaimSubmitRequest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Claim    api.Resource `json:"claim" yaml:"claim"`
}

type SAMEnrollmentClaimSubmitResult struct {
	TypeMeta      `json:",inline" yaml:",inline"`
	Accepted      bool      `json:"accepted" yaml:"accepted"`
	ClaimRef      string    `json:"claimRef" yaml:"claimRef"`
	DynamicSource string    `json:"dynamicSource" yaml:"dynamicSource"`
	Generation    int64     `json:"generation" yaml:"generation"`
	ObservedAt    time.Time `json:"observedAt" yaml:"observedAt"`
	ExpiresAt     time.Time `json:"expiresAt" yaml:"expiresAt"`
}

type SAMEnrollmentClaimRevokeRequest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Name     string `json:"name" yaml:"name"`
	Reason   string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type SAMEnrollmentClaimRevokeResult struct {
	TypeMeta      `json:",inline" yaml:",inline"`
	Revoked       bool      `json:"revoked" yaml:"revoked"`
	ClaimRef      string    `json:"claimRef" yaml:"claimRef"`
	DynamicSource string    `json:"dynamicSource" yaml:"dynamicSource"`
	Generation    int64     `json:"generation" yaml:"generation"`
	ObservedAt    time.Time `json:"observedAt" yaml:"observedAt"`
	ExpiresAt     time.Time `json:"expiresAt" yaml:"expiresAt"`
	Reason        string    `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type SAMRRSetGetRequest struct {
	Name     string `json:"name" yaml:"name"`
	ClaimRef string `json:"claimRef" yaml:"claimRef"`
}

type SAMRRSetGetResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta   `json:"metadata" yaml:"metadata"`
	RRSet    api.Resource `json:"rrSet" yaml:"rrSet"`
}

type LogLevelRequest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Level    string `json:"level" yaml:"level"`
}

type LogLevelResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Level    string `json:"level" yaml:"level"`
}

type DHCPv6EventRequest struct {
	TypeMeta  `json:",inline" yaml:",inline"`
	Resource  string            `json:"resource" yaml:"resource"`
	Reason    string            `json:"reason,omitempty" yaml:"reason,omitempty"`
	Prefix    string            `json:"prefix,omitempty" yaml:"prefix,omitempty"`
	IAID      string            `json:"iaid,omitempty" yaml:"iaid,omitempty"`
	T1        string            `json:"t1,omitempty" yaml:"t1,omitempty"`
	T2        string            `json:"t2,omitempty" yaml:"t2,omitempty"`
	PLTime    string            `json:"pltime,omitempty" yaml:"pltime,omitempty"`
	VLTime    string            `json:"vltime,omitempty" yaml:"vltime,omitempty"`
	ServerID  string            `json:"serverID,omitempty" yaml:"serverID,omitempty"`
	ClientID  string            `json:"clientID,omitempty" yaml:"clientID,omitempty"`
	SourceLL  string            `json:"sourceLL,omitempty" yaml:"sourceLL,omitempty"`
	SourceMAC string            `json:"sourceMAC,omitempty" yaml:"sourceMAC,omitempty"`
	Env       map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

type DHCPv6EventResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Accepted bool   `json:"accepted" yaml:"accepted"`
	Resource string `json:"resource" yaml:"resource"`
}

type DHCPLeaseEventRequest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Action   string            `json:"action" yaml:"action"`
	MAC      string            `json:"mac,omitempty" yaml:"mac,omitempty"`
	IP       string            `json:"ip" yaml:"ip"`
	Hostname string            `json:"hostname,omitempty" yaml:"hostname,omitempty"`
	Env      map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

type DHCPLeaseEventResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Accepted bool `json:"accepted" yaml:"accepted"`
}

type ConnectionTable struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta              `json:"metadata" yaml:"metadata"`
	Status   observe.ConnectionTable `json:"status" yaml:"status"`
}

type DNSQueriesRequest struct {
	Since         string `json:"since,omitempty" yaml:"since,omitempty"`
	From          string `json:"from,omitempty" yaml:"from,omitempty"`
	To            string `json:"to,omitempty" yaml:"to,omitempty"`
	Client        string `json:"client,omitempty" yaml:"client,omitempty"`
	QName         string `json:"qname,omitempty" yaml:"qname,omitempty"`
	QNameSuffix   string `json:"qnameSuffix,omitempty" yaml:"qnameSuffix,omitempty"`
	ResponseCode  string `json:"rcode,omitempty" yaml:"rcode,omitempty"`
	Upstream      string `json:"upstream,omitempty" yaml:"upstream,omitempty"`
	DurationMinUS int64  `json:"durationMinUs,omitempty" yaml:"durationMinUs,omitempty"`
	Limit         int    `json:"limit,omitempty" yaml:"limit,omitempty"`
}

type DNSQueries struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta          `json:"metadata" yaml:"metadata"`
	Items    []logstore.DNSQuery `json:"items" yaml:"items"`
}

type DNSQueriesAggregate struct {
	TypeMeta  `json:",inline" yaml:",inline"`
	Metadata  ObjectMeta                 `json:"metadata" yaml:"metadata"`
	Aggregate logstore.DNSQueryAggregate `json:"aggregate" yaml:"aggregate"`
}

type TrafficFlowsRequest struct {
	Since      string `json:"since,omitempty" yaml:"since,omitempty"`
	From       string `json:"from,omitempty" yaml:"from,omitempty"`
	To         string `json:"to,omitempty" yaml:"to,omitempty"`
	Client     string `json:"client,omitempty" yaml:"client,omitempty"`
	Peer       string `json:"peer,omitempty" yaml:"peer,omitempty"`
	PeerSuffix string `json:"peerSuffix,omitempty" yaml:"peerSuffix,omitempty"`
	Protocol   string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	Asymmetric bool   `json:"asymmetric,omitempty" yaml:"asymmetric,omitempty"`
	Limit      int    `json:"limit,omitempty" yaml:"limit,omitempty"`
}

type TrafficFlows struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta             `json:"metadata" yaml:"metadata"`
	Items    []logstore.TrafficFlow `json:"items" yaml:"items"`
}

type TrafficFlowsAggregate struct {
	TypeMeta  `json:",inline" yaml:",inline"`
	Metadata  ObjectMeta                    `json:"metadata" yaml:"metadata"`
	Aggregate logstore.TrafficFlowAggregate `json:"aggregate" yaml:"aggregate"`
}

type FirewallLogsRequest struct {
	Since  string `json:"since,omitempty" yaml:"since,omitempty"`
	Action string `json:"action,omitempty" yaml:"action,omitempty"`
	Src    string `json:"src,omitempty" yaml:"src,omitempty"`
	Limit  int    `json:"limit,omitempty" yaml:"limit,omitempty"`
}

type FirewallLogs struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta                  `json:"metadata" yaml:"metadata"`
	Items    []logstore.FirewallLogEntry `json:"items" yaml:"items"`
}

type GetRequest struct {
	Subject     string `json:"subject,omitempty" yaml:"subject,omitempty"`
	Output      string `json:"output,omitempty" yaml:"output,omitempty"`
	EventsLimit int    `json:"eventsLimit,omitempty" yaml:"eventsLimit,omitempty"`
	Limit       int    `json:"limit,omitempty" yaml:"limit,omitempty"`
	SinceID     int64  `json:"sinceId,omitempty" yaml:"sinceId,omitempty"`
	Topic       string `json:"topic,omitempty" yaml:"topic,omitempty"`
	Resource    string `json:"resource,omitempty" yaml:"resource,omitempty"`
	KindFilter  string `json:"kindFilter,omitempty" yaml:"kindFilter,omitempty"`
	NameFilter  string `json:"nameFilter,omitempty" yaml:"nameFilter,omitempty"`
}

type ResourceView struct {
	APIVersion string              `json:"apiVersion" yaml:"apiVersion"`
	Kind       string              `json:"kind" yaml:"kind"`
	Name       string              `json:"name" yaml:"name"`
	Spec       any                 `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status     map[string]any      `json:"status,omitempty" yaml:"status,omitempty"`
	Events     []routerstate.Event `json:"events,omitempty" yaml:"events,omitempty"`
}

type GetResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta                `json:"metadata" yaml:"metadata"`
	Items    []ResourceView            `json:"items,omitempty" yaml:"items,omitempty"`
	Status   *StatusStatus             `json:"status,omitempty" yaml:"status,omitempty"`
	Events   []routerstate.StoredEvent `json:"events,omitempty" yaml:"events,omitempty"`
	Ledger   *LedgerReport             `json:"ledger,omitempty" yaml:"ledger,omitempty"`
	Raw      any                       `json:"raw,omitempty" yaml:"raw,omitempty"`
}

type DescribeRequest struct {
	Target      string `json:"target" yaml:"target"`
	EventsLimit int    `json:"eventsLimit,omitempty" yaml:"eventsLimit,omitempty"`
}

type DescribeResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta   `json:"metadata" yaml:"metadata"`
	Resource ResourceView `json:"resource" yaml:"resource"`
}

type LedgerReport struct {
	Integrity   string                         `json:"integrity,omitempty" yaml:"integrity,omitempty"`
	Generations []routerstate.GenerationRecord `json:"generations,omitempty" yaml:"generations,omitempty"`
}

type ProbeRequest struct {
	Subject string `json:"subject" yaml:"subject"`
	Target  string `json:"target,omitempty" yaml:"target,omitempty"`
}

type ProbeCheck struct {
	Name   string `json:"name" yaml:"name"`
	Status string `json:"status" yaml:"status"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

type ProbeResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta   `json:"metadata" yaml:"metadata"`
	Subject  string       `json:"subject" yaml:"subject"`
	Target   string       `json:"target,omitempty" yaml:"target,omitempty"`
	Checks   []ProbeCheck `json:"checks,omitempty" yaml:"checks,omitempty"`
}

// RuntimeStats reports routerd's own process-level runtime footprint (heap,
// goroutines, GC, and file descriptors). It is collected inside the running
// `routerd serve` process so external tooling (e.g. `routerctl doctor runtime`)
// can observe resource usage without sshing in and poking /proc directly.
type RuntimeStats struct {
	TypeMeta        `json:",inline" yaml:",inline"`
	CollectedAt     time.Time `json:"collectedAt" yaml:"collectedAt"`
	HeapAllocBytes  uint64    `json:"heapAllocBytes" yaml:"heapAllocBytes"`
	HeapInuseBytes  uint64    `json:"heapInuseBytes" yaml:"heapInuseBytes"`
	HeapObjects     uint64    `json:"heapObjects" yaml:"heapObjects"`
	StackInuseBytes uint64    `json:"stackInuseBytes" yaml:"stackInuseBytes"`
	SysBytes        uint64    `json:"sysBytes" yaml:"sysBytes"`
	// CgroupMemory* fields are collected from cgroup v2 when routerd is running
	// under a Linux service cgroup. They explain why systemd MemoryCurrent can be
	// much larger than the routerd process heap/RSS: file cache charged to the
	// service appears here as cgroupFileBytes / cgroupInactiveFileBytes.
	CgroupMemoryCurrentBytes uint64                                      `json:"cgroupMemoryCurrentBytes,omitempty" yaml:"cgroupMemoryCurrentBytes,omitempty"`
	CgroupMemoryPeakBytes    uint64                                      `json:"cgroupMemoryPeakBytes,omitempty" yaml:"cgroupMemoryPeakBytes,omitempty"`
	CgroupAnonBytes          uint64                                      `json:"cgroupAnonBytes,omitempty" yaml:"cgroupAnonBytes,omitempty"`
	CgroupFileBytes          uint64                                      `json:"cgroupFileBytes,omitempty" yaml:"cgroupFileBytes,omitempty"`
	CgroupActiveFileBytes    uint64                                      `json:"cgroupActiveFileBytes,omitempty" yaml:"cgroupActiveFileBytes,omitempty"`
	CgroupInactiveFileBytes  uint64                                      `json:"cgroupInactiveFileBytes,omitempty" yaml:"cgroupInactiveFileBytes,omitempty"`
	CgroupKernelBytes        uint64                                      `json:"cgroupKernelBytes,omitempty" yaml:"cgroupKernelBytes,omitempty"`
	CgroupSlabBytes          uint64                                      `json:"cgroupSlabBytes,omitempty" yaml:"cgroupSlabBytes,omitempty"`
	NumGoroutine             int                                         `json:"numGoroutine" yaml:"numGoroutine"`
	NumGC                    uint32                                      `json:"numGC" yaml:"numGC"`
	GCPauseTotalNs           uint64                                      `json:"gcPauseTotalNs" yaml:"gcPauseTotalNs"`
	LastGC                   time.Time                                   `json:"lastGC,omitempty" yaml:"lastGC,omitempty"`
	StateStatusWriteCount    uint64                                      `json:"stateStatusWriteCount,omitempty" yaml:"stateStatusWriteCount,omitempty"`
	StateStatusSkipCount     uint64                                      `json:"stateStatusSkipCount,omitempty" yaml:"stateStatusSkipCount,omitempty"`
	StateStatusKindStats     map[string]routerstate.StatusKindWriteStats `json:"stateStatusKindStats,omitempty" yaml:"stateStatusKindStats,omitempty"`
	// OpenFDs is a sample-time approximate count of open file descriptors from
	// /proc/self/fd (the transient directory-read fd is excluded). It is 0 when
	// the count is unavailable (e.g. non-Linux, /proc not mounted). Treat it as
	// an indicator, not an exact accounting.
	OpenFDs int    `json:"openFds" yaml:"openFds"`
	MaxFDs  uint64 `json:"maxFds" yaml:"maxFds"` // RLIMIT_NOFILE soft; 0 if unavailable
}

type Error struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Error    ErrorStatus `json:"error" yaml:"error"`
}

type ErrorStatus struct {
	Message string `json:"message" yaml:"message"`
}

func NewConnectionTable(table *observe.ConnectionTable) ConnectionTable {
	if table == nil {
		table = &observe.ConnectionTable{}
	}
	return ConnectionTable{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "ConnectionTable"},
		Metadata: ObjectMeta{Name: "connections"},
		Status:   *table,
	}
}

func NewDNSQueries(rows []logstore.DNSQuery) DNSQueries {
	if rows == nil {
		rows = []logstore.DNSQuery{}
	}
	return DNSQueries{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "DNSQueries"},
		Metadata: ObjectMeta{Name: "dns-queries"},
		Items:    rows,
	}
}

func NewTrafficFlows(rows []logstore.TrafficFlow) TrafficFlows {
	if rows == nil {
		rows = []logstore.TrafficFlow{}
	}
	return TrafficFlows{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "TrafficFlows"},
		Metadata: ObjectMeta{Name: "traffic-flows"},
		Items:    rows,
	}
}

func NewDNSQueriesAggregate(agg logstore.DNSQueryAggregate) DNSQueriesAggregate {
	return DNSQueriesAggregate{
		TypeMeta:  TypeMeta{APIVersion: APIVersion, Kind: "DNSQueriesAggregate"},
		Metadata:  ObjectMeta{Name: "dns-queries-aggregate"},
		Aggregate: agg,
	}
}

func NewTrafficFlowsAggregate(agg logstore.TrafficFlowAggregate) TrafficFlowsAggregate {
	return TrafficFlowsAggregate{
		TypeMeta:  TypeMeta{APIVersion: APIVersion, Kind: "TrafficFlowsAggregate"},
		Metadata:  ObjectMeta{Name: "traffic-flows-aggregate"},
		Aggregate: agg,
	}
}

func NewFirewallLogs(rows []logstore.FirewallLogEntry) FirewallLogs {
	if rows == nil {
		rows = []logstore.FirewallLogEntry{}
	}
	return FirewallLogs{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "FirewallLogs"},
		Metadata: ObjectMeta{Name: "firewall-logs"},
		Items:    rows,
	}
}

func NewGetResult(subject string) GetResult {
	return GetResult{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "GetResult"},
		Metadata: ObjectMeta{Name: subject},
	}
}

func NewDescribeResult(target string, view ResourceView) DescribeResult {
	return DescribeResult{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "DescribeResult"},
		Metadata: ObjectMeta{Name: target},
		Resource: view,
	}
}

func NewProbeResult(subject, target string, checks []ProbeCheck) ProbeResult {
	if checks == nil {
		checks = []ProbeCheck{}
	}
	return ProbeResult{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "ProbeResult"},
		Metadata: ObjectMeta{Name: subject},
		Subject:  subject,
		Target:   target,
		Checks:   checks,
	}
}

func ResourceViewFromResource(res api.Resource, status map[string]any, events []routerstate.Event) ResourceView {
	if status == nil {
		status = map[string]any{}
	}
	if events == nil {
		events = []routerstate.Event{}
	}
	return ResourceView{
		APIVersion: res.APIVersion,
		Kind:       res.Kind,
		Name:       res.Metadata.Name,
		Spec:       res.Spec,
		Status:     status,
		Events:     events,
	}
}

func NewControllers(items []ControllerStatus) Controllers {
	if items == nil {
		items = []ControllerStatus{}
	}
	return Controllers{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "Controllers"},
		Metadata: ObjectMeta{Name: "controllers"},
		Items:    items,
	}
}

func NewStatus(result *apply.Result) Status {
	status := Status{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "Status"},
		Metadata: ObjectMeta{Name: "routerd"},
	}
	if result == nil {
		status.Status.Phase = "Unknown"
		return status
	}
	status.Status.Phase = result.Phase
	status.Status.Generation = result.Generation
	status.Status.LastApplyTime = result.Timestamp
	status.Status.ResourceCount = len(result.Resources)
	return status
}

func NewApplyResult(result *apply.Result) ApplyResult {
	if result == nil {
		result = &apply.Result{}
	}
	return ApplyResult{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "ApplyResult"},
		Result:   *result,
	}
}

func NewPlanResult(result *apply.Result) PlanResult {
	if result == nil {
		result = &apply.Result{}
	}
	return PlanResult{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "PlanResult"},
		Result:   *result,
	}
}

func NewValidateResult(valid bool, warnings []string, message string) ValidateResult {
	if warnings == nil {
		warnings = []string{}
	}
	return ValidateResult{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "ValidateResult"},
		Valid:    valid,
		Warnings: warnings,
		Error:    message,
	}
}

func NewSAMEnrollmentClaimSubmitResult(claimRef, source string, generation int64, observedAt, expiresAt time.Time) SAMEnrollmentClaimSubmitResult {
	return SAMEnrollmentClaimSubmitResult{
		TypeMeta:      TypeMeta{APIVersion: APIVersion, Kind: "SAMEnrollmentClaimSubmitResult"},
		Accepted:      true,
		ClaimRef:      claimRef,
		DynamicSource: source,
		Generation:    generation,
		ObservedAt:    observedAt,
		ExpiresAt:     expiresAt,
	}
}

func NewSAMEnrollmentClaimRevokeResult(claimRef, source string, generation int64, observedAt, expiresAt time.Time, reason string) SAMEnrollmentClaimRevokeResult {
	return SAMEnrollmentClaimRevokeResult{
		TypeMeta:      TypeMeta{APIVersion: APIVersion, Kind: "SAMEnrollmentClaimRevokeResult"},
		Revoked:       true,
		ClaimRef:      claimRef,
		DynamicSource: source,
		Generation:    generation,
		ObservedAt:    observedAt,
		ExpiresAt:     expiresAt,
		Reason:        reason,
	}
}

func NewSAMRRSetGetResult(name string, rrSet api.Resource) SAMRRSetGetResult {
	return SAMRRSetGetResult{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "SAMRRSetGetResult"},
		Metadata: ObjectMeta{
			Name: name,
		},
		RRSet: rrSet,
	}
}

func NewLogLevelResult(level string) LogLevelResult {
	return LogLevelResult{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "LogLevelResult"},
		Level:    level,
	}
}

func NewDHCPv6EventResult(resource string) DHCPv6EventResult {
	return DHCPv6EventResult{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "DHCPv6EventResult"},
		Accepted: true,
		Resource: resource,
	}
}

func NewDHCPLeaseEventResult() DHCPLeaseEventResult {
	return DHCPLeaseEventResult{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "DHCPLeaseEventResult"},
		Accepted: true,
	}
}

func NewRuntimeStats() RuntimeStats {
	return RuntimeStats{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "RuntimeStats"},
	}
}

func NewError(message string) Error {
	return Error{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "Error"},
		Error:    ErrorStatus{Message: message},
	}
}
