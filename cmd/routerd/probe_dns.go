// SPDX-License-Identifier: BSD-3-Clause

package main

import (
 "context"
 "fmt"
 "net"
 "net/netip"
 "slices"
 "strconv"
 "strings"
 "time"

 "github.com/miekg/dns"
 "github.com/imksoo/routerd/pkg/api"
 "github.com/imksoo/routerd/pkg/controlapi"
 "github.com/imksoo/routerd/pkg/resourcequery"
 routerstate "github.com/imksoo/routerd/pkg/state"
)

type daemonDNSQuestion struct{name string;kind uint16;expected netip.Addr}

func daemonDNSProbe(ctx context.Context,router *api.Router,store *routerstate.SQLiteStore,target string)([]controlapi.ProbeCheck,error){
 checks,err:=resourcePhaseProbe(router,store,"DNSResolver",target);if err!=nil{return nil,err}
 if router==nil{return checks,nil}
 attempts:=0
 for _,resource:=range selectInspectionResources(router.Spec.Resources,"DNSResolver",target){
  spec,err:=resource.DNSResolverSpec();if err!=nil{return nil,err}
  if len(spec.Listen)==0{checks=append(checks,unobservedProbe(resource.ID(),"no explicit configured DNS listener"));continue}
  for _,listen:=range spec.Listen{
   addresses:=slices.Clone(listen.Addresses)
   for _,source:=range listen.AddressFrom{values,err:=resourcequery.ReadValues(store,source);if err!=nil{return nil,err};addresses=append(addresses,values...)}
   if len(addresses)==0{checks=append(checks,unobservedProbe(resource.ID()+" listener/"+listen.Name,"listen address is unresolved"));continue}
   question,ok,err:=daemonDNSProbeQuestion(router,resource.Metadata.Name,spec,listen);if err!=nil{return nil,err}
   if !ok{checks=append(checks,unobservedProbe(resource.ID()+" listener/"+listen.Name,"no configured static local question or root-forwarding source is selected"));continue}
   for _,address:=range addresses{
    ip,err:=netip.ParseAddr(strings.TrimSpace(address));if err!=nil{if prefix,e:=netip.ParsePrefix(strings.TrimSpace(address));e==nil{ip=prefix.Addr()}else{checks=append(checks,unobservedProbe(resource.ID(),"listen address is not a usable literal IP"));continue}}
    if ip.IsUnspecified(){if ip.Is4(){ip=netip.MustParseAddr("127.0.0.1")}else{ip=netip.IPv6Loopback()}}
    port:=listen.Port;if port==0{port=5053}
    endpoint:=net.JoinHostPort(ip.String(),strconv.Itoa(port))
    for _,protocol:=range []string{"udp","tcp"}{
     name:=resource.ID()+" "+protocol+" "+endpoint
     if attempts>=maxDaemonProbeAttempts{checks=append(checks,unobservedProbe(name,"probe attempt limit reached"));continue}
     if ctx.Err()!=nil{checks=append(checks,unobservedProbe(name,"request budget exhausted before this attempt"));continue}
     attempts++;attemptCtx,cancel:=context.WithTimeout(ctx,2*time.Second);started:=time.Now().UTC()
     request:=new(dns.Msg);request.SetQuestion(question.name,question.kind)
     response,_,exchangeErr:=(&dns.Client{Net:protocol,Timeout:2*time.Second,UDPSize:1232}).ExchangeContext(attemptCtx,request,endpoint);cancel()
     if ctx.Err()!=nil{return nil,ctx.Err()}
     check:=controlapi.ProbeCheck{Name:name,Status:"fail",Kind:"communication",Protocol:"dns/"+protocol,Target:endpoint,ObservedAt:started,DurationMilliseconds:time.Since(started).Milliseconds()}
     if exchangeErr!=nil{check.Detail=exchangeErr.Error();check.FailureKind="transport"}else if !validDaemonDNSReply(request,response,question){check.Detail="DNS reply did not contain the configured expected answer for "+question.name;check.FailureKind="dns_answer"}else{check.Status="pass";check.Detail="current DNS reply matches "+question.name+" "+dns.TypeToString[question.kind]}
     checks=append(checks,check)
    }
   }
  }
 }
 checks=append(checks,controlapi.ProbeCheck{Name:"probe scope",Status:"warn",Kind:"scope",Detail:"DNS queries originate on this router; success does not establish LAN-client reachability or every conditional DNS source"})
 return checks,nil
}

func daemonDNSProbeQuestion(router *api.Router,resolverName string,spec api.DNSResolverSpec,listen api.DNSResolverListenSpec)(daemonDNSQuestion,bool,error){
 var refs []string;root:=false
 addSource:=func(name string,matches,zoneRefs []string){if len(listen.Sources)>0&&!slices.Contains(listen.Sources,name){return};refs=append(refs,zoneRefs...);if len(zoneRefs)==0{for _,match:=range matches{if strings.TrimSpace(match)=="."{root=true}}}}
 for _,source:=range spec.Sources{addSource(source.Name,source.Match,source.ZoneRef)}
 for _,resource:=range router.Spec.Resources{if resource.APIVersion!=api.NetAPIVersion||resource.Kind!="DNSForwarder"{continue};forwarder,err:=resource.DNSForwarderSpec();if err!=nil{return daemonDNSQuestion{},false,err};if strings.TrimPrefix(forwarder.Resolver,"DNSResolver/")!=resolverName{continue};addSource(resource.Metadata.Name,forwarder.Match,forwarder.ZoneRefs)}
 for _,ref:=range refs{for _,zone:=range router.Spec.Resources{if zone.Kind!="DNSZone"||zone.Metadata.Name!=strings.TrimPrefix(ref,"DNSZone/"){continue};declared,err:=zone.DNSZoneSpec();if err!=nil{return daemonDNSQuestion{},false,err};for _,record:=range declared.Records{if strings.Contains(record.Hostname,"*"){continue};name:=dns.Fqdn(record.Hostname);if !dns.IsSubDomain(dns.Fqdn(declared.Zone),name){name=dns.Fqdn(strings.TrimSuffix(record.Hostname,".")+"."+strings.TrimSuffix(declared.Zone,"."))};if ip,err:=netip.ParseAddr(record.IPv4);err==nil&&ip.Is4(){return daemonDNSQuestion{name:name,kind:dns.TypeA,expected:ip},true,nil};if ip,err:=netip.ParseAddr(record.IPv6);err==nil&&ip.Is6(){return daemonDNSQuestion{name:name,kind:dns.TypeAAAA,expected:ip},true,nil}}}}
 if root{return daemonDNSQuestion{name:".",kind:dns.TypeNS},true,nil};return daemonDNSQuestion{},false,nil
}

func validDaemonDNSReply(request,response *dns.Msg,question daemonDNSQuestion)bool{
 if response==nil||!response.Response||response.Truncated||response.Id!=request.Id||response.Opcode!=request.Opcode||response.Rcode!=dns.RcodeSuccess||len(response.Question)!=1{return false}
 q:=response.Question[0];if !strings.EqualFold(q.Name,question.name)||q.Qtype!=question.kind||q.Qclass!=dns.ClassINET{return false}
 for _,rr:=range response.Answer{if !strings.EqualFold(rr.Header().Name,question.name){continue};switch answer:=rr.(type){case *dns.A:if question.kind==dns.TypeA{ip,ok:=netip.AddrFromSlice(answer.A);if ok&&ip.Unmap()==question.expected{return true}};case *dns.AAAA:if question.kind==dns.TypeAAAA{ip,ok:=netip.AddrFromSlice(answer.AAAA);if ok&&ip==question.expected{return true}};case *dns.NS:if question.kind==dns.TypeNS&&answer.Ns!=""{return true}}}
 return false
}

var _=fmt.Sprintf
