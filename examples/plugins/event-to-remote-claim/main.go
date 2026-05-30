// SPDX-License-Identifier: BSD-3-Clause

// Command event-to-remote-claim is an EXAMPLE / REFERENCE routerd plugin for
// CloudEdge Event Federation (ADR 0006, Phase 3). It is deliberately
// PROVIDER-AGNOSTIC and DRY-RUN ONLY: it reads the routerd PluginRequest JSON
// from stdin, turns each matched "routerd.client.ipv4.observed" federation
// event into a RemoteAddressClaim resource, and writes a PluginResult to
// stdout. It performs NO network or cloud calls and depends only on the Go
// standard library.
//
// The emitted RemoteAddressClaim carries a provider-agnostic capture spec
// (type "provider-secondary-ip", a placeholder providerRef, configureOSAddress
// false). Actually executing a provider operation to claim the address is out
// of scope for the MVP — provider actionPlan execution is Phase 4/5. This
// plugin only proposes declarative dynamic config; routerd validates it and the
// operator can inspect it with `routerctl dynamic render`.
//
// The plugin mirrors the routerd plugin wire JSON with local structs rather
// than importing pkg/plugin, so it stays a standalone, copyable example.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// pluginRequest mirrors the subset of the routerd PluginRequest wire JSON this
// example needs. routerd places matched federation events under spec.events.
type pluginRequest struct {
	Spec struct {
		Events []matchedEvent `json:"events"`
	} `json:"spec"`
}

// matchedEvent mirrors pkg/plugin.PluginMatchedEvent on the wire.
type matchedEvent struct {
	ID         string            `json:"id"`
	Group      string            `json:"group"`
	SourceNode string            `json:"sourceNode"`
	Type       string            `json:"type"`
	Subject    string            `json:"subject"`
	DedupeKey  string            `json:"dedupeKey"`
	Payload    map[string]string `json:"payload"`
}

// pluginResult mirrors the subset of the routerd PluginResult wire JSON this
// example emits. routerd accepts YAML or JSON; we emit JSON.
type pluginResult struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   objectMeta         `json:"metadata"`
	Status     pluginResultStatus `json:"status"`
}

type pluginResultStatus struct {
	TTL       string     `json:"ttl"`
	Resources []resource `json:"resources"`
}

type resource struct {
	APIVersion string     `json:"apiVersion"`
	Kind       string     `json:"kind"`
	Metadata   objectMeta `json:"metadata"`
	Spec       claimSpec  `json:"spec"`
}

type objectMeta struct {
	Name string `json:"name"`
}

type claimSpec struct {
	DomainRef string       `json:"domainRef"`
	Address   string       `json:"address"`
	OwnerSide string       `json:"ownerSide"`
	Capture   captureSpec  `json:"capture"`
	Delivery  deliverySpec `json:"delivery"`
}

type captureSpec struct {
	Type               string `json:"type"`
	ProviderRef        string `json:"providerRef"`
	ProviderMode       string `json:"providerMode"`
	NICRef             string `json:"nicRef"`
	ConfigureOSAddress bool   `json:"configureOSAddress"`
}

type deliverySpec struct {
	PeerRef         string `json:"peerRef"`
	Mode            string `json:"mode"`
	TunnelInterface string `json:"tunnelInterface"`
}

const (
	observedEventType = "routerd.client.ipv4.observed"
	claimAPIVersion   = "hybrid.routerd.net/v1alpha1"
	claimKind         = "RemoteAddressClaim"
	resultAPIVersion  = "plugin.routerd.net/v1alpha1"
	resultKind        = "PluginResult"
	defaultTTL        = "30m"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "event-to-remote-claim: read stdin: %v\n", err)
		os.Exit(1)
	}

	var req pluginRequest
	if len(data) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			fmt.Fprintf(os.Stderr, "event-to-remote-claim: parse PluginRequest: %v\n", err)
			os.Exit(1)
		}
	}

	resources := make([]resource, 0, len(req.Spec.Events))
	for _, ev := range req.Spec.Events {
		if ev.Type != observedEventType {
			continue
		}
		if claim, ok := claimFromEvent(ev); ok {
			resources = append(resources, claim)
		}
	}

	result := pluginResult{
		APIVersion: resultAPIVersion,
		Kind:       resultKind,
		Metadata:   objectMeta{Name: "event-to-remote-claim"},
		Status: pluginResultStatus{
			TTL:       defaultTTL,
			Resources: resources,
		},
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "event-to-remote-claim: encode PluginResult: %v\n", err)
		os.Exit(1)
	}
}

// claimFromEvent derives a RemoteAddressClaim from one observed client event.
// Everything is taken from the observed fact (subject/payload); the plugin never
// reaches out to a provider. Returns ok=false when there is no usable address.
func claimFromEvent(ev matchedEvent) (resource, bool) {
	address := firstNonEmpty(ev.Payload["address"], ev.Subject)
	if strings.TrimSpace(address) == "" {
		return resource{}, false
	}

	ownerSide := firstNonEmpty(ev.Payload["ownerSide"], "onprem")
	providerRef := firstNonEmpty(ev.Payload["providerRef"], "example-provider")
	peerRef := firstNonEmpty(ev.Payload["peerRef"], "onprem-main")
	// nicRef identifies the provider NIC the secondary IP would attach to. It is
	// provider-specific, so it is sourced from the observed event payload; the
	// fallback is a clearly-marked example placeholder. The MVP never uses it to
	// call a provider — capture is dry-run intent only.
	nicRef := firstNonEmpty(ev.Payload["nicRef"], "example-nic-ref")

	return resource{
		APIVersion: claimAPIVersion,
		Kind:       claimKind,
		Metadata:   objectMeta{Name: claimName(ownerSide, address)},
		Spec: claimSpec{
			DomainRef: ev.Payload["domain"],
			Address:   address,
			OwnerSide: ownerSide,
			Capture: captureSpec{
				// Provider-agnostic placeholder: the real provider operation
				// (assign secondary IP, etc.) is a Phase 4/5 actionPlan and is
				// NOT performed here.
				Type:               "provider-secondary-ip",
				ProviderRef:        providerRef,
				ProviderMode:       "secondary-ip",
				NICRef:             nicRef,
				ConfigureOSAddress: false,
			},
			Delivery: deliverySpec{
				PeerRef:         peerRef,
				Mode:            "route",
				TunnelInterface: "wg-hybrid",
			},
		},
	}, true
}

// claimName builds a deterministic resource name from the owner side and
// address, e.g. ("onprem", "10.88.60.9/32") -> "onprem-10-88-60-9".
func claimName(ownerSide, address string) string {
	host := address
	if idx := strings.IndexByte(host, '/'); idx >= 0 {
		host = host[:idx]
	}
	replacer := strings.NewReplacer(".", "-", ":", "-")
	host = replacer.Replace(host)
	host = strings.Trim(host, "-")
	if host == "" {
		host = "addr"
	}
	if ownerSide == "" {
		ownerSide = "onprem"
	}
	return ownerSide + "-" + host
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
