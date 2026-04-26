package resource

const (
	ActionEnsure  = "ensure"
	ActionDelete  = "delete"
	ActionObserve = "observe"
)

type Artifact struct {
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Owner      string            `json:"owner,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type Intent struct {
	Artifact  Artifact
	Action    string
	ApplyWith string
}

type OwnershipPolicy func(Artifact) bool

func Orphans(desired, actual []Artifact, managed OwnershipPolicy) []Artifact {
	desiredCounts := map[string]int{}
	for _, artifact := range desired {
		desiredCounts[artifact.Identity()]++
	}
	var orphans []Artifact
	for _, artifact := range actual {
		if managed != nil && !managed(artifact) {
			continue
		}
		id := artifact.Identity()
		if desiredCounts[id] > 0 {
			desiredCounts[id]--
			continue
		}
		orphans = append(orphans, artifact)
	}
	return orphans
}

func (a Artifact) Identity() string {
	return a.Kind + "/" + a.Name
}
