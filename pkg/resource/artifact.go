// SPDX-License-Identifier: BSD-3-Clause

package resource

const (
	ActionEnsure  = "ensure"
	ActionDelete  = "delete"
	ActionObserve = "observe"
)

type Artifact struct {
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type Intent struct {
	Artifact  Artifact
	Action    string
	ApplyWith string
}
