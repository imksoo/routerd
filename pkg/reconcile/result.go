package reconcile

import "time"

type Result struct {
	Generation int64            `json:"generation"`
	Timestamp  time.Time        `json:"timestamp"`
	Phase      string           `json:"phase"`
	Resources  []ResourceResult `json:"resources"`
	Warnings   []string         `json:"warnings,omitempty"`
}

type ResourceResult struct {
	ID         string            `json:"id"`
	Phase      string            `json:"phase"`
	Observed   map[string]string `json:"observed,omitempty"`
	Plan       []string          `json:"plan,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
	Conditions []Condition       `json:"conditions,omitempty"`
}

type Condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}
