// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
 "context"
 "net/netip"
 "testing"
 "strings"
 "github.com/imksoo/routerd/pkg/api"
 "github.com/imksoo/routerd/pkg/platform"
)

func TestDSLiteForeignParentCannotAcquireStaticAddress(t *testing.T) {
 parent, native, store := newDSLiteNativeController(t, platform.OSLinux)
 native.links["ds-owned"] = dsliteTunnelObservation{Exists:true, Index:8, Kind:"ip6tnl", MTU:1454}
 parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{TypeMeta:api.TypeMeta{APIVersion:api.NetAPIVersion, Kind:"IPv4StaticAddress"}, Metadata:api.ObjectMeta{Name:"child"}, Spec:api.IPv4StaticAddressSpec{Interface:"ds",Address:"192.0.2.1/24"}})
 present, writes := false, 0
 child := IPv4StaticAddressController{Router:parent.Router, Store:store, DSLite:&parent, ObserveAddresses:func(context.Context,string)(staticAddressSnapshot,error){
  snapshot:=staticAddressSnapshot{Index:8,Scope:"linux:boot-a:17"}
  if present { snapshot.Addresses=[]netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")} }
  return snapshot,nil
 }, Command:func(context.Context,string,...string)error{writes++;present=true;return nil}}
 err:=child.Reconcile(t.Context())
 if writes!=0 || present { t.Fatalf("foreign DS-Lite parent received child address: writes=%d err=%v",writes,err) }
}

func TestDSLiteParentStaticAddressProofAndRecordedCleanup(t *testing.T) {
 for _, scenario := range []string{"owned", "pending", "foreign-after-save", "removed", "removed-foreign", "removed-missing-receipt"} {
  t.Run(scenario,func(t *testing.T){
   parent,native,store:=newDSLiteNativeController(t,platform.OSLinux)
   if err:=parent.reconcile(t.Context());err!=nil{t.Fatal(err)}
   parent.Router.Spec.Resources=append(parent.Router.Spec.Resources,api.Resource{TypeMeta:api.TypeMeta{APIVersion:api.NetAPIVersion,Kind:"IPv4StaticAddress"},Metadata:api.ObjectMeta{Name:"child"},Spec:api.IPv4StaticAddressSpec{Interface:"ds",Address:"192.0.2.1/24"}})
   db:=&staticAddressFaultStore{SQLiteStore:store.SQLiteStore}
   if scenario=="pending" { records,_:=parent.nativeTunnelReceipts();records[0].Pending=true;if err:=parent.saveNativeTunnel(records[0],false);err!=nil{t.Fatal(err)} }
   if scenario=="foreign-after-save" {db.afterPending=func(){link:=native.links["ds-owned"];link.Token="foreign";native.links["ds-owned"]=link}}
   present,adds,deletes:=false,0,0
   child:=IPv4StaticAddressController{Router:parent.Router,Store:db,DSLite:&parent,ObserveAddresses:func(context.Context,string)(staticAddressSnapshot,error){
    snapshot:=staticAddressSnapshot{Index:native.links["ds-owned"].Index,Scope:"linux:boot-a:17"}
    if present{snapshot.Addresses=[]netip.Prefix{netip.MustParsePrefix("192.0.2.1/24")}}
    return snapshot,nil
   },Command:func(_ context.Context,_ string,args ...string)error{if strings.Contains(strings.Join(args," "),"del"){deletes++;present=false}else{adds++;present=true};return nil}}
   err:=child.Reconcile(t.Context())
   if scenario=="pending" || scenario=="foreign-after-save" {if err==nil || adds!=0{t.Fatalf("unproved parent mutated: adds=%d err=%v",adds,err)};return}
   if err!=nil || adds!=1 {t.Fatalf("owned parent rejected: adds=%d err=%v",adds,err)}
   status,_:=db.ReadObjectStatus(api.NetAPIVersion,"IPv4StaticAddress","child")
   receipt,err:=staticReceipt(status);if err!=nil || receipt==nil || !strings.HasPrefix(receipt.ParentToken,"dslite:"){t.Fatalf("missing concrete proof: %+v %v",receipt,err)}
   if scenario=="owned" {return}
   child.Router=&api.Router{}
   if scenario=="removed-foreign" {link:=native.links["ds-owned"];link.Token="foreign";native.links["ds-owned"]=link}
   if scenario=="removed-missing-receipt" {records,_:=parent.nativeTunnelReceipts();if err:=parent.saveNativeTunnel(records[0],true);err!=nil{t.Fatal(err)}}
   err=child.Reconcile(t.Context())
   if scenario=="removed" {if err!=nil || deletes!=1 || present{t.Fatalf("recorded cleanup rejected: deletes=%d err=%v",deletes,err)}} else if err==nil || deletes!=0 || !present {t.Fatalf("foreign cleanup authorized: deletes=%d err=%v",deletes,err)}
  })
 }
}

func TestDSLiteParentRecheckedAfterNativeAddressWait(t *testing.T) {
 parent,native,store:=newDSLiteNativeController(t,platform.OSLinux)
 if err:=parent.reconcile(t.Context());err!=nil{t.Fatal(err)}
 original:=parent.CommandOutput
 parent.CommandOutput=func(ctx context.Context,name string,args ...string)([]byte,error){
  out,err:=original(ctx,name,args...)
  if strings.Join(args," ")=="-j -4 addr show dev ds-owned" {link:=native.links["ds-owned"];link.Token="foreign";native.links["ds-owned"]=link}
  return out,err
 }
 reader:=nativeParentReader{Router:parent.Router,Store:store,DSLite:&parent}
 link,err:=reader.observeDeclared(t.Context(),"ds-owned")
 if err==nil || link.Exists{t.Fatalf("parent changed during inner-address observation admitted child: %+v %v",link,err)}
}
