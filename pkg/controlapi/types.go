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

func NewError(message string) Error {
	return Error{
		TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "Error"},
		Error:    ErrorStatus{Message: message},
	}
}
