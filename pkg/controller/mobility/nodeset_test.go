// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"github.com/imksoo/routerd/pkg/api"
)

func mobilityNodeSetResource(name string, nodes []api.SAMNodeSpec) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMNodeSet"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.SAMNodeSetSpec{Nodes: nodes},
	}
}
