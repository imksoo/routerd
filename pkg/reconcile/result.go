package reconcile

import "time"

type Result struct {
	Generation         int64               `json:"generation"`
	Timestamp          time.Time           `json:"timestamp"`
	Phase              string              `json:"phase"`
	Resources          []ResourceResult    `json:"resources"`
	Orphans            []OrphanedArtifact  `json:"orphans,omitempty"`
	AdoptionCandidates []AdoptionCandidate `json:"adoptionCandidates,omitempty"`
	AdoptedArtifacts   []AdoptedArtifact   `json:"adoptedArtifacts,omitempty"`
	Warnings           []string            `json:"warnings,omitempty"`
}

type ResourceResult struct {
	ID         string            `json:"id"`
	Phase      string            `json:"phase"`
	Observed   map[string]string `json:"observed,omitempty"`
	Plan       []string          `json:"plan,omitempty"`
	Artifacts  []ArtifactIntent  `json:"artifacts,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
	Conditions []Condition       `json:"conditions,omitempty"`
}

type Condition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type OrphanedArtifact struct {
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	Reason      string            `json:"reason"`
	Remediation string            `json:"remediation,omitempty"`
	Observed    map[string]string `json:"observed,omitempty"`
}

type ArtifactIntent struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Action    string `json:"action"`
	ApplyWith string `json:"applyWith,omitempty"`
}

type AdoptionCandidate struct {
	Kind     string            `json:"kind"`
	Name     string            `json:"name"`
	Owner    string            `json:"owner"`
	Reason   string            `json:"reason"`
	Desired  map[string]string `json:"desired,omitempty"`
	Observed map[string]string `json:"observed,omitempty"`
}

type AdoptedArtifact struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Owner string `json:"owner"`
}
