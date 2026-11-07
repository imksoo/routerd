// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestDeclaredPrefixDelegationNamesKeepsDeclaredPDWhenEffectiveIsEmpty(t *testing.T) {
	declared := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}}}}}
	if got := declaredPrefixDelegationNames(&api.Router{}, declared); !reflect.DeepEqual(got, []string{"wan-pd"}) {
		t.Fatalf("names = %#v", got)
	}
}
