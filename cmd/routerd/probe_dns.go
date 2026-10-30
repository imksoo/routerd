// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/miekg/dns"
)

type daemonDNSQuestion struct {
	name       string
	kind       uint16
	expected   netip.Addr
	unobserved string
}

func daemonDNSProbe(ctx context.Context, router *api.Router, store *routerstate.SQLiteStore, target string) ([]controlapi.ProbeCheck, error) {
	return serveProbeChecks(ctx, router, store, "dns", target)
}

func daemonDNSProbeWithCurrent(ctx context.Context, router *api.Router, store *routerstate.SQLiteStore, target string, current probeCurrentRouter) ([]controlapi.ProbeCheck, error) {
	checks, err := resourcePhaseProbe(router, store, "DNSResolver", target)
	if err != nil {
		return nil, err
	}
	if router == nil {
		return checks, nil
	}
	attempts := 0
	selected, _ := selectProbeResources(router.Spec.Resources, "DNSResolver", target)
	for _, resource := range selected {
		spec, err := resource.DNSResolverSpec()
		if err != nil {
			return nil, err
		}
		if len(spec.Listen) == 0 {
			checks = append(checks, unobservedProbe(resource.ID(), "no explicit configured DNS listener"))
			continue
		}
		for listenIndex, listen := range spec.Listen {
			if ctx.Err() == context.Canceled {
				return nil, ctx.Err()
			}
			if attempts >= maxDaemonProbeAttempts || len(checks) >= 24 || ctx.Err() != nil {
				return append(checks, unobservedProbe("remaining DNS targets", "probe attempt or observation limit reached; remaining configured listeners are unprobed")), nil
			}
			addresses, limited, err := daemonDNSProbeAddresses(store, listen)
			if err != nil {
				return nil, err
			}
			if limited {
				checks = append(checks, unobservedProbe("remaining DNS addresses", "address preparation limit reached; remaining addresses or sources are unprobed"))
			}
			if len(addresses) == 0 {
				checks = append(checks, unobservedProbe(resource.ID()+" listener/"+listen.Name, "listen address is unresolved"))
				continue
			}
			question, ok, err := daemonDNSProbeQuestion(router, resource.Metadata.Name, spec, listen)
			if err != nil {
				return nil, err
			}
			if !ok {
				reason := question.unobserved
				if reason == "" {
					reason = "no configured static local question or root-forwarding source is selected"
				}
				checks = append(checks, unobservedProbe(resource.ID()+" listener/"+listen.Name, reason))
				continue
			}
			for _, address := range addresses {
				endpoint, ok := daemonDNSProbeEndpoint(address, listen.Port)
				if !ok {
					checks = append(checks, unobservedProbe(resource.ID(), "listen address is not a usable literal IP"))
					continue
				}
				for _, protocol := range []string{"udp", "tcp"} {
					name := resource.ID() + " " + protocol + " " + endpoint
					if attempts >= maxDaemonProbeAttempts || len(checks) >= 24 {
						return append(checks, unobservedProbe("remaining DNS targets", "probe attempt or observation limit reached; remaining configured listeners are unprobed")), nil
					}
					if ctx.Err() != nil {
						if ctx.Err() == context.Canceled {
							return nil, ctx.Err()
						}
						return append(checks, unobservedProbe("remaining DNS targets", "request budget exhausted; remaining configured listeners are unprobed")), nil
					}
					attempts++
					eligible, err := daemonDNSAttemptCurrent(ctx, current, store, resource.Metadata.Name, listenIndex, endpoint, question)
					if err != nil {
						return nil, err
					}
					if !eligible {
						return append(checks, unobservedProbe(name, "current DNS source or listener changed before this attempt")), nil
					}
					attemptCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
					started := time.Now().UTC()
					request := new(dns.Msg)
					request.SetQuestion(question.name, question.kind)
					response, exchangeErr := exchangeDaemonDNSProbe(attemptCtx, protocol, endpoint, request)
					cancel()
					if ctx.Err() != nil && ctx.Err() != context.DeadlineExceeded {
						return nil, ctx.Err()
					}
					check := controlapi.ProbeCheck{Name: name, Status: "fail", Kind: "communication", Protocol: "dns/" + protocol, Target: endpoint, ObservedAt: started, DurationMilliseconds: time.Since(started).Milliseconds()}
					if ctx.Err() == nil {
						eligible, err = daemonDNSAttemptCurrent(ctx, current, store, resource.Metadata.Name, listenIndex, endpoint, question)
						if err != nil {
							return nil, err
						}
						if !eligible {
							check.Status, check.FailureKind, check.Detail = "warn", "stale_source", "unobserved: current DNS source or listener changed during this exchange; remaining attempts are unprobed"
							return append(checks, check), nil
						}
					}
					if exchangeErr != nil {
						check.Detail = exchangeErr.Error()
						check.FailureKind = "transport"
						if errors.Is(exchangeErr, syscall.EPERM) || errors.Is(exchangeErr, syscall.EACCES) {
							check.Status = "warn"
							check.FailureKind = "permission"
							check.Detail = "unobserved: DNS socket capability unavailable: " + exchangeErr.Error()
						}
					} else if response != nil && response.Rcode != dns.RcodeSuccess {
						check.Detail = "DNS transport replied but resolution returned " + dns.RcodeToString[response.Rcode] + " for " + question.name
						check.FailureKind = "dns_rcode"
					} else if !validDaemonDNSReply(request, response, question) {
						check.Detail = "DNS reply did not contain the configured expected answer for " + question.name
						check.FailureKind = "dns_answer"
					} else {
						check.Status = "pass"
						check.Detail = "current DNS reply matches " + question.name + " " + dns.TypeToString[question.kind]
					}
					checks = append(checks, check)
				}
			}
		}
	}
	checks = append(checks, controlapi.ProbeCheck{Name: "probe scope", Status: "warn", Kind: "scope", Detail: "unobserved for LAN-client forwarding and other conditional sources: these DNS queries originate on this router"})
	return checks, nil
}

// Probe preparation has its own small bound: a request cannot spend its whole
// lifetime scanning speculative source/ref/record alternatives before any I/O.
const maxDaemonDNSQuestionItems = 64

func daemonDNSProbeQuestion(router *api.Router, resolverName string, spec api.DNSResolverSpec, listen api.DNSResolverListenSpec) (daemonDNSQuestion, bool, error) {
	type sourceSelection struct {
		kind    string
		matches []string
		refs    []string
	}
	var sources []sourceSelection
	prepared := 0
	addSource := func(name, kind string, matches, refs []string) bool {
		prepared++
		if prepared > maxDaemonDNSQuestionItems || len(matches) > maxDaemonDNSQuestionItems || len(refs) > maxDaemonDNSQuestionItems {
			return false
		}
		if len(listen.Sources) > 0 && !slices.Contains(listen.Sources, name) {
			return true
		}
		sources = append(sources, sourceSelection{kind: kind, matches: matches, refs: refs})
		return true
	}
	// attachForwarders replaces inline sources whenever this resolver has any
	// DNSForwarder resources. Retain the exact resource order, including broad
	// forwarders that can shadow later local-zone declarations.
	hasForwarders := false
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "DNSForwarder" {
			continue
		}
		forwarder, err := resource.DNSForwarderSpec()
		if err != nil {
			return daemonDNSQuestion{}, false, err
		}
		if strings.TrimPrefix(forwarder.Resolver, "DNSResolver/") != resolverName {
			continue
		}
		hasForwarders = true
		kind := "forward"
		if len(forwarder.ZoneRefs) > 0 {
			kind = "zone"
		}
		if !addSource(resource.Metadata.Name, kind, forwarder.Match, forwarder.ZoneRefs) {
			return daemonDNSQuestion{unobserved: "question preparation limit reached"}, false, nil
		}
	}
	if !hasForwarders {
		for _, source := range spec.Sources {
			if !addSource(source.Name, source.Kind, source.Match, source.ZoneRef) {
				return daemonDNSQuestion{unobserved: "question preparation limit reached"}, false, nil
			}
		}
	}
	matches := func(source sourceSelection, name string) bool {
		if len(source.matches) == 0 {
			return true
		}
		for _, match := range source.matches {
			if match == "" || dns.IsSubDomain(dns.Fqdn(strings.ToLower(match)), strings.ToLower(name)) {
				return true
			}
		}
		return false
	}
	firstSource := func(name string) int {
		for i, source := range sources {
			if matches(source, name) {
				return i
			}
		}
		return -1
	}
	for sourceIndex, source := range sources {
		if source.kind != "zone" {
			continue
		}
		for _, ref := range source.refs {
			prepared++
			if prepared > maxDaemonDNSQuestionItems {
				return daemonDNSQuestion{unobserved: "question preparation limit reached"}, false, nil
			}
			for _, zone := range router.Spec.Resources {
				if zone.APIVersion != api.NetAPIVersion || zone.Kind != "DNSZone" || zone.Metadata.Name != strings.TrimPrefix(ref, "DNSZone/") {
					continue
				}
				declared, err := zone.DNSZoneSpec()
				if err != nil {
					return daemonDNSQuestion{}, false, err
				}
				apex := declared.Zone
				if strings.TrimSpace(apex) == "" {
					apex = zone.Metadata.Name
				}
				for _, record := range declared.Records {
					prepared++
					if prepared > maxDaemonDNSQuestionItems {
						return daemonDNSQuestion{unobserved: "question preparation limit reached"}, false, nil
					}
					if strings.Contains(record.Hostname, "*") {
						continue
					}
					name := dns.Fqdn(record.Hostname)
					if !dns.IsSubDomain(dns.Fqdn(apex), name) {
						name = dns.Fqdn(strings.TrimSuffix(record.Hostname, ".") + "." + strings.TrimSuffix(apex, "."))
					}
					// Checking membership alone is insufficient: an earlier selected
					// forwarder can send this private question outside its local zone.
					if firstSource(name) != sourceIndex {
						continue
					}
					if ip, err := netip.ParseAddr(record.IPv4); err == nil && ip.Is4() {
						return daemonDNSQuestion{name: name, kind: dns.TypeA, expected: ip}, true, nil
					}
					if ip, err := netip.ParseAddr(record.IPv6); err == nil && ip.Is6() {
						return daemonDNSQuestion{name: name, kind: dns.TypeAAAA, expected: ip}, true, nil
					}
				}
			}
		}
	}
	// The public root question is safe only when its first matching selected
	// source is actually a forwarder, rather than a local authoritative zone.
	if i := firstSource("."); i >= 0 && (sources[i].kind == "forward" || sources[i].kind == "upstream") {
		return daemonDNSQuestion{name: ".", kind: dns.TypeNS}, true, nil
	}
	return daemonDNSQuestion{}, false, nil
}

func validDaemonDNSReply(request, response *dns.Msg, question daemonDNSQuestion) bool {
	if response == nil || !response.Response || response.Truncated || response.Id != request.Id || response.Opcode != request.Opcode || response.Rcode != dns.RcodeSuccess || len(response.Question) != 1 {
		return false
	}
	q := response.Question[0]
	if !strings.EqualFold(q.Name, question.name) || q.Qtype != question.kind || q.Qclass != dns.ClassINET {
		return false
	}
	for _, rr := range response.Answer {
		if rr.Header().Class != dns.ClassINET || !strings.EqualFold(rr.Header().Name, question.name) {
			continue
		}
		switch answer := rr.(type) {
		case *dns.A:
			if question.kind == dns.TypeA {
				ip, ok := netip.AddrFromSlice(answer.A)
				if ok && ip.Unmap() == question.expected {
					return true
				}
			}
		case *dns.AAAA:
			if question.kind == dns.TypeAAAA {
				ip, ok := netip.AddrFromSlice(answer.AAAA)
				if ok && ip == question.expected {
					return true
				}
			}
		case *dns.NS:
			if question.kind == dns.TypeNS && answer.Ns != "" {
				return true
			}
		}
	}
	return false
}

func exchangeDaemonDNSProbe(ctx context.Context, protocol, endpoint string, request *dns.Msg) (*dns.Msg, error) {
	client := dns.Client{Net: protocol, Timeout: 2 * time.Second, UDPSize: 1232}
	connection, err := client.DialContext(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	response, _, err := client.ExchangeWithConnContext(ctx, request, connection)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return response, err
}
