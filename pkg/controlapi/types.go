package controlapi

import (
	"time"

	"routerd/pkg/apply"
	"routerd/pkg/logstore"
	"routerd/pkg/observe"
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
	Phase         string    `json:"phase" yaml:"phase"`
	Generation    int64     `json:"generation,omitempty" yaml:"generation,omitempty"`
	LastApplyTime time.Time `json:"lastApplyTime,omitempty" yaml:"lastApplyTime,omitempty"`
	ResourceCount int       `json:"resourceCount,omitempty" yaml:"resourceCount,omitempty"`
}

type ApplyRequest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	DryRun   bool `json:"dryRun,omitempty" yaml:"dryRun,omitempty"`
}

type ApplyResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Result   apply.Result `json:"result" yaml:"result"`
}

type DeleteRequest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Target   string `json:"target" yaml:"target"`
	DryRun   bool   `json:"dryRun,omitempty" yaml:"dryRun,omitempty"`
}

type DeleteResult struct {
	TypeMeta  `json:",inline" yaml:",inline"`
	Deleted   []string `json:"deleted,omitempty" yaml:"deleted,omitempty"`
	Artifacts []string `json:"artifacts,omitempty" yaml:"artifacts,omitempty"`
	DryRun    bool     `json:"dryRun,omitempty" yaml:"dryRun,omitempty"`
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
	Since  string `json:"since,omitempty" yaml:"since,omitempty"`
	Client string `json:"client,omitempty" yaml:"client,omitempty"`
	QName  string `json:"qname,omitempty" yaml:"qname,omitempty"`
	Limit  int    `json:"limit,omitempty" yaml:"limit,omitempty"`
}

type DNSQueries struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta          `json:"metadata" yaml:"metadata"`
	Items    []logstore.DNSQuery `json:"items" yaml:"items"`
}

type TrafficFlowsRequest struct {
	Since  string `json:"since,omitempty" yaml:"since,omitempty"`
	Client string `json:"client,omitempty" yaml:"client,omitempty"`
	Peer   string `json:"peer,omitempty" yaml:"peer,omitempty"`
	Limit  int    `json:"limit,omitempty" yaml:"limit,omitempty"`
}

type TrafficFlows struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta             `json:"metadata" yaml:"metadata"`
	Items    []logstore.TrafficFlow `json:"items" yaml:"items"`
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

func NewError(message string) Error {
	return Error{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "Error"},
		Error:    ErrorStatus{Message: message},
	}
}
