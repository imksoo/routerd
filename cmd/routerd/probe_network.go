// SPDX-License-Identifier: BSD-3-Clause

package main

import (
 "context"
 "fmt"
 "net"
 "strings"
 "time"

 "github.com/imksoo/routerd/pkg/api"
 "github.com/imksoo/routerd/pkg/controlapi"
 "github.com/imksoo/routerd/pkg/healthcheck"
 routerstate "github.com/imksoo/routerd/pkg/state"
)

const maxDaemonProbeAttempts=8

func daemonEgressProbe(ctx context.Context,router *api.Router,store *routerstate.SQLiteStore,target string)([]controlapi.ProbeCheck,error){
 checks,err:=resourcePhaseProbe(router,store,"EgressRoutePolicy",target);if err!=nil{return nil,err}
 if router==nil{return checks,nil}
 attempts:=0
 for _,resource:=range selectInspectionResources(router.Spec.Resources,"EgressRoutePolicy",target){
  policy,err:=resource.EgressRoutePolicySpec();if err!=nil{return nil,err}
  for _,candidate:=range policy.Candidates{
   if candidate.Enabled!=nil&&!*candidate.Enabled{continue}
   refs:=[]string{candidate.HealthCheck}
   for _,next:=range candidate.Targets{refs=append(refs,next.HealthCheck)}
   found:=false
   for _,ref:=range refs{
    if ref==""{continue};found=true
    name:=resource.Kind+"/"+resource.Metadata.Name+" candidate/"+candidate.Name+" HealthCheck/"+ref
    if attempts>=maxDaemonProbeAttempts{checks=append(checks,unobservedProbe(name,"probe attempt limit reached"));continue}
    if ctx.Err()!=nil{checks=append(checks,unobservedProbe(name,"request budget exhausted before this attempt"));continue}
    var declared *api.Resource
    for i:=range router.Spec.Resources{if router.Spec.Resources[i].APIVersion==api.NetAPIVersion&&router.Spec.Resources[i].Kind=="HealthCheck"&&router.Spec.Resources[i].Metadata.Name==ref{declared=&router.Spec.Resources[i];break}}
    if declared==nil{checks=append(checks,unobservedProbe(name,"configured HealthCheck resource is unavailable"));continue}
    spec,err:=declared.HealthCheckSpec();if err!=nil{return nil,err}
    if spec.Enabled!=nil&&!*spec.Enabled{checks=append(checks,unobservedProbe(name,"configured HealthCheck is disabled"));continue}
    if strings.TrimSpace(spec.Target)==""||spec.TargetSource!=""&&spec.TargetSource!="static"{checks=append(checks,unobservedProbe(name,"no current literal/static probe target; dynamic target resolution is unavailable"));continue}
    spec,err=healthcheck.ResolveSpecWithStoreForResource(router,store,ref,spec);if err!=nil{return nil,err}
    if spec.SourceInterface!=""{if _,err:=net.InterfaceByName(spec.SourceInterface);err!=nil{checks=append(checks,unobservedProbe(name,"native source interface cannot be observed: "+err.Error()));continue}}
    attempts++
    timeout:=2*time.Second
    if configured,err:=time.ParseDuration(spec.Timeout);err==nil&&configured>0&&configured<timeout{timeout=configured}
    attemptCtx,cancel:=context.WithTimeout(ctx,timeout);started:=time.Now().UTC()
    result:=healthcheck.Probe(attemptCtx,spec);cancel()
    if ctx.Err()!=nil{return nil,ctx.Err()}
    check:=controlapi.ProbeCheck{Name:name,Kind:"communication",Protocol:spec.Probe().Protocol,Target:net.JoinHostPort(spec.Target,fmt.Sprint(spec.Probe().Port)),ObservedAt:started,DurationMilliseconds:time.Since(started).Milliseconds(),FailureKind:result.FailureKind,BindingInterface:spec.SourceInterface,BindingMark:spec.FwMark,Status:"fail",Detail:result.Message}
    if result.OK{check.Status="pass"}else if result.FailureKind==healthcheck.FailureKindPermission||result.FailureKind==healthcheck.FailureKindOther{check.Status="warn";check.Detail="unobserved: probe could not establish communication: "+result.Message}
    checks=append(checks,check)
   }
   if !found{checks=append(checks,unobservedProbe(resource.ID()+" candidate/"+candidate.Name,"candidate has no configured HealthCheck target"))}
  }
 }
 checks=append(checks,controlapi.ProbeCheck{Name:"probe scope",Status:"warn",Kind:"scope",Detail:"router-originated configured HealthCheck attempts do not establish forwarded client traffic, NAT or failover"})
 return checks,nil
}

func unobservedProbe(name,detail string)controlapi.ProbeCheck{return controlapi.ProbeCheck{Name:name,Status:"warn",Kind:"scope",Detail:"unobserved: "+detail}}
