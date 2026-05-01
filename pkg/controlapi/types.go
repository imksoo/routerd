package controlapi

import (
	"time"

	"routerd/pkg/apply"
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

type DHCP6EventRequest struct {
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

type DHCP6EventResult struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Accepted bool   `json:"accepted" yaml:"accepted"`
	Resource string `json:"resource" yaml:"resource"`
}

type NAPTTable struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta        `json:"metadata" yaml:"metadata"`
	Status   observe.NAPTTable `json:"status" yaml:"status"`
}

type Error struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Error    ErrorStatus `json:"error" yaml:"error"`
}

type ErrorStatus struct {
	Message string `json:"message" yaml:"message"`
}

func NewNAPTTable(table *observe.NAPTTable) NAPTTable {
	if table == nil {
		table = &observe.NAPTTable{}
	}
	return NAPTTable{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "NAPTTable"},
		Metadata: ObjectMeta{Name: "conntrack"},
		Status:   *table,
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

func NewDHCP6EventResult(resource string) DHCP6EventResult {
	return DHCP6EventResult{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "DHCP6EventResult"},
		Accepted: true,
		Resource: resource,
	}
}

func NewError(message string) Error {
	return Error{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "Error"},
		Error:    ErrorStatus{Message: message},
	}
}
