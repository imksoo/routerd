package resource

import "testing"

func TestOrphansUsesArtifactIdentityAndManagedPolicy(t *testing.T) {
	desired := []Artifact{
		{Kind: "linux.ipv4.fwmarkRule", Name: "priority=10,mark=0x111,table=111"},
	}
	actual := []Artifact{
		{Kind: "linux.ipv4.fwmarkRule", Name: "priority=10,mark=0x111,table=111", Attributes: map[string]string{"mark": "0x111"}},
		{Kind: "linux.ipv4.fwmarkRule", Name: "priority=10000,mark=0x100,table=100", Attributes: map[string]string{"mark": "0x100"}},
		{Kind: "linux.ipv4.fwmarkRule", Name: "priority=500,mark=0x900,table=900", Attributes: map[string]string{"mark": "0x900"}},
	}
	got := Orphans(desired, actual, func(a Artifact) bool {
		return a.Attributes["mark"] != "0x900"
	})
	if len(got) != 1 {
		t.Fatalf("orphans = %+v, want one", got)
	}
	if got[0].Name != "priority=10000,mark=0x100,table=100" {
		t.Fatalf("orphan = %+v", got[0])
	}
}
