package dhcp6event

import (
	"fmt"
	"strings"
	"time"

	routerstate "routerd/pkg/state"
)

const apiVersion = "net.routerd.net/v1alpha1"

type Event struct {
	Resource  string
	Reason    string
	Prefix    string
	IAID      string
	T1        string
	T2        string
	PLTime    string
	VLTime    string
	ServerID  string
	ClientID  string
	SourceLL  string
	SourceMAC string
	Env       map[string]string
}

func Apply(store routerstate.Store, event Event) (routerstate.PDLease, error) {
	resource := strings.TrimSpace(event.Resource)
	if resource == "" {
		return routerstate.PDLease{}, fmt.Errorf("empty IPv6PrefixDelegation resource")
	}
	lease, _ := routerstate.PDLeaseFromStore(store, "ipv6PrefixDelegation."+resource)
	normalizeEvent(&event)
	now := store.Now().UTC().Format(time.RFC3339Nano)
	if event.Prefix != "" {
		if lease.CurrentPrefix != event.Prefix {
			lease.LastPrefix = event.Prefix
		}
		lease.CurrentPrefix = event.Prefix
		lease.Prefix = event.Prefix
		lease.LastObservedAt = now
	}
	if event.ServerID != "" {
		lease.ServerID = compactHex(event.ServerID)
	}
	if event.ClientID != "" {
		lease.DUID = compactHex(event.ClientID)
	}
	if event.IAID != "" {
		lease.IAID = event.IAID
	}
	if event.T1 != "" {
		lease.T1 = event.T1
	}
	if event.T2 != "" {
		lease.T2 = event.T2
	}
	if event.PLTime != "" {
		lease.PLTime = event.PLTime
	}
	if event.VLTime != "" {
		lease.VLTime = event.VLTime
	}
	if event.SourceLL != "" {
		lease.SourceLL = event.SourceLL
	}
	if event.SourceMAC != "" {
		lease.SourceMAC = strings.ToLower(event.SourceMAC)
	}
	reason := firstNonEmpty(event.Reason, "DHCP6Event")
	if isReplyReason(reason) {
		lease.LastReplyAt = now
	}
	store.Set("ipv6PrefixDelegation."+resource+".lease", routerstate.EncodePDLease(lease), reason)
	if recorder, ok := store.(routerstate.EventRecorder); ok {
		message := "observed DHCPv6 event"
		if event.Prefix != "" {
			message += " prefix=" + event.Prefix
		}
		_ = recorder.RecordEvent(apiVersion, "IPv6PrefixDelegation", resource, "Normal", reason, message)
	}
	return lease, nil
}

func normalizeEvent(event *Event) {
	env := event.Env
	event.Reason = firstNonEmpty(event.Reason, envValue(env, "reason", "REASON", "interface_order"))
	event.Prefix = firstNonEmpty(event.Prefix,
		envValue(env,
			"new_ia_pd_prefix", "new_ia_pd_0_prefix", "new_ia_pd_1_prefix",
			"new_ip6_prefix", "new_delegated_prefix", "ia_pd_prefix",
		),
	)
	event.IAID = firstNonEmpty(event.IAID, envValue(env, "iaid", "new_ia_pd_iaid", "new_ia_pd_0_iaid", "new_ia_pd_1_iaid"))
	event.T1 = firstNonEmpty(event.T1, envValue(env, "t1", "new_ia_pd_t1", "new_ia_pd_0_t1", "new_ia_pd_1_t1"))
	event.T2 = firstNonEmpty(event.T2, envValue(env, "t2", "new_ia_pd_t2", "new_ia_pd_0_t2", "new_ia_pd_1_t2"))
	event.PLTime = firstNonEmpty(event.PLTime, envValue(env, "pltime", "preferred_lifetime", "new_ia_pd_pltime", "new_ia_pd_0_pltime", "new_ia_pd_1_pltime"))
	event.VLTime = firstNonEmpty(event.VLTime, envValue(env, "vltime", "valid_lifetime", "new_ia_pd_vltime", "new_ia_pd_0_vltime", "new_ia_pd_1_vltime"))
	event.ServerID = firstNonEmpty(event.ServerID, envValue(env, "server_id", "new_server_id", "dhcp6_server_id"))
	event.ClientID = firstNonEmpty(event.ClientID, envValue(env, "client_id", "duid", "dhcp6_client_id"))
	event.SourceLL = firstNonEmpty(event.SourceLL, envValue(env, "source_ll", "server_link_local"))
	event.SourceMAC = firstNonEmpty(event.SourceMAC, envValue(env, "source_mac", "server_mac"))
}

func envValue(env map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(env[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isReplyReason(reason string) bool {
	normalized := strings.ToLower(strings.TrimSpace(reason))
	for _, token := range []string{"bound", "renew", "rebind", "reply", "request"} {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return false
}

func compactHex(value string) string {
	replacer := strings.NewReplacer(":", "", "-", "", " ", "")
	return strings.ToLower(replacer.Replace(strings.TrimSpace(value)))
}
