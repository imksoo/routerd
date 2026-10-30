// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
 "context"
 "net/netip"
 "testing"
 "github.com/imksoo/routerd/pkg/api"
 "github.com/imksoo/routerd/pkg/platform"
)

func TestDSLiteForeignParentCannotAcquireStaticAddress(t *testing.T) {
 parent, native, store := newDSLiteNativeController(t, platform.OSLinux)
 native.links["ds-owned"] = dsliteTunnelObservation{Exists:true, Index:8, Kind:"ip6tnl", MTU:1454}
 parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{TypeMeta:api.TypeMeta{APIVersion:api.NetAPIVersion, Kind:"IPv4StaticAddress"}, Metadata:api.ObjectMeta{Name:"child"}, Spec:api.IPv4StaticAddressSpec{Interface:"ds",Address:"192.0.2.1/24"}})
 present, writes := false, 0
 child := IPv4StaticAddressController{Router:parent.Router, Store:store, ObserveAddresses:func(context.Context,string)(staticAddressSnapshot,error){
  snapshot:=staticAddressSnapshot{Index:8,Scope:"linux:boot-a:17"}
  if present { snapshot.Addresses=[]netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")} }
  return snapshot,nil
 }, Command:func(context.Context,string,...string)error{writes++;present=true;return nil}}
 err:=child.Reconcile(t.Context())
 if writes!=0 || present { t.Fatalf("foreign DS-Lite parent received child address: writes=%d err=%v",writes,err) }
}
