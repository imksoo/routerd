// SPDX-License-Identifier: BSD-3-Clause

// Command netns-provider-inventory is a local integration-test implementation
// of observe.providerPrivateIPs. It reports the current namespace's configured
// router interface and a static list of same-site client addresses supplied by
// the netns integration harness.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

type typeMeta struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

type observeRequest struct {
	typeMeta `json:",inline"`
	Spec     observeSpec `json:"spec"`
}

type observeSpec struct {
	Provider    string            `json:"provider"`
	ProviderRef string            `json:"providerRef,omitempty"`
	SelfNode    string            `json:"selfNode"`
	Pool        string            `json:"pool"`
	Prefix      string            `json:"prefix"`
	SelfNICRef  string            `json:"selfNicRef"`
	SubnetRef   string            `json:"subnetRef,omitempty"`
	Target      map[string]string `json:"target,omitempty"`
}

type observeResult struct {
	typeMeta `json:",inline"`
	Status   observeStatus `json:"status"`
}

type observeStatus struct {
	Status             string            `json:"status"`
	Message            string            `json:"message,omitempty"`
	Error              string            `json:"error,omitempty"`
	Self               *privateIPSelf    `json:"self,omitempty"`
	IPs                []privateIPRecord `json:"ips,omitempty"`
	ObservedCandidates []privateIPRecord `json:"observedCandidates,omitempty"`
	LocalIPs           []privateIPRecord `json:"localIPs,omitempty"`
}

type privateIPSelf struct {
	NICRef            string   `json:"nicRef,omitempty"`
	SubnetRef         string   `json:"subnetRef,omitempty"`
	ResourceRef       string   `json:"resourceRef,omitempty"`
	ResourceType      string   `json:"resourceType,omitempty"`
	PrivateIPs        []string `json:"privateIPs,omitempty"`
	CapturedAddresses []string `json:"capturedAddresses,omitempty"`
	ForwardingEnabled *bool    `json:"forwardingEnabled,omitempty"`
}

type privateIPRecord struct {
	Address       string            `json:"address"`
	NodeRef       string            `json:"nodeRef,omitempty"`
	NICRef        string            `json:"nicRef,omitempty"`
	SubnetRef     string            `json:"subnetRef,omitempty"`
	ProviderRef   string            `json:"providerRef,omitempty"`
	ResourceRef   string            `json:"resourceRef,omitempty"`
	ResourceType  string            `json:"resourceType,omitempty"`
	Primary       bool              `json:"primary,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	InstanceState string            `json:"instanceState,omitempty"`
}

const (
	resultAPIVersion = "providerinventory.routerd.net/v1alpha1"
	resultKind       = "ObservePrivateIPsResult"
	statusSucceeded  = "succeeded"
	statusFailed     = "failed"
)

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "netns-provider-inventory: %v\n", err)
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer) error {
	var req observeRequest
	data, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			return fmt.Errorf("parse ObservePrivateIPsRequest: %w", err)
		}
	}
	res := build(req.Spec)
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

func build(spec observeSpec) observeResult {
	res := observeResult{typeMeta: typeMeta{APIVersion: resultAPIVersion, Kind: resultKind}}
	if strings.TrimSpace(spec.Provider) != "netns" {
		res.Status.Status = statusFailed
		res.Status.Message = "unsupported provider"
		res.Status.Error = "provider must be netns"
		return res
	}
	selfIP := canonicalHost(os.Getenv("ROUTERD_NETNS_SELF_IP"))
	subnet := firstNonEmpty(spec.SubnetRef, os.Getenv("ROUTERD_NETNS_SITE"))
	forwarding := true
	res.Status.Status = statusSucceeded
	res.Status.Self = &privateIPSelf{
		NICRef:            firstNonEmpty(spec.SelfNICRef, spec.Target["interface"], "eth1"),
		SubnetRef:         subnet,
		ResourceRef:       spec.SelfNode,
		ResourceType:      "router-nic",
		PrivateIPs:        nonEmptySlice(selfIP),
		CapturedAddresses: capturedAddresses(),
		ForwardingEnabled: &forwarding,
	}
	for _, item := range strings.Split(os.Getenv("ROUTERD_NETNS_CLIENT_IPS"), ",") {
		parts := strings.SplitN(strings.TrimSpace(item), "=", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		address := canonicalHost(parts[1])
		if name == "" || address == "" {
			continue
		}
		rec := privateIPRecord{
			Address:      address,
			NodeRef:      name,
			NICRef:       name,
			SubnetRef:    subnet,
			ProviderRef:  spec.ProviderRef,
			ResourceRef:  name,
			ResourceType: "instance-nic",
			Primary:      true,
			Tags:         map[string]string{"cloudedge-mobility": "true"},
		}
		res.Status.IPs = append(res.Status.IPs, rec)
		res.Status.ObservedCandidates = append(res.Status.ObservedCandidates, rec)
		res.Status.LocalIPs = append(res.Status.LocalIPs, rec)
	}
	return res
}

func capturedAddresses() []string {
	raw, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, addr := range raw {
		prefix := canonicalHost(addr.String())
		if prefix != "" && strings.HasSuffix(prefix, "/32") {
			out = append(out, prefix)
		}
	}
	return out
}

func canonicalHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if ip, _, err := net.ParseCIDR(raw); err == nil {
		return ip.String() + "/32"
	}
	if ip := net.ParseIP(raw); ip != nil {
		return ip.String() + "/32"
	}
	return raw
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nonEmptySlice(values ...string) []string {
	var out []string
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
