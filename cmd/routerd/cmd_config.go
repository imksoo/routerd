package main

import (
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/resource"
)

func adoptedArtifactsForResult(artifacts []resource.Artifact) []apply.AdoptedArtifact {
	out := make([]apply.AdoptedArtifact, 0, len(artifacts))
	seen := map[string]bool{}
	for _, artifact := range artifacts {
		if seen[artifact.Identity()] {
			continue
		}
		seen[artifact.Identity()] = true
		out = append(out, apply.AdoptedArtifact{
			Kind:  artifact.Kind,
			Name:  artifact.Name,
			Owner: artifact.Owner,
		})
	}
	return out
}
