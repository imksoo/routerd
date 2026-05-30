// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

// Literal secret strings and file paths planted in the source config. The
// critical redaction test asserts NONE of these appear in the JSON the plugin
// would receive.
const (
	wgPrivKey    = "WG-PRIVATE-KEY-SUPERSECRET-AAAA"
	wgPrivFile   = "/etc/routerd/wg-private.key"
	ipsecPSK     = "IPSEC-PSK-SUPERSECRET-BBBB"
	bgpPassword  = "BGP-MD5-PASSWORD-CCCC"
	pppoePass    = "PPPOE-PASSWORD-DDDD"
	pppoePassF   = "/etc/routerd/pppoe.secret"
	vrrpAuthFile = "/etc/routerd/vrrp-auth.key"
	vrrpAuthEnv  = "VRRP_AUTH_ENV_NAME"
	groupSecretF = "/etc/routerd/eventgroup-hmac.key"
)

func secretBearingRouter() *api.Router {
	return &api.Router{
		Spec: api.RouterSpec{
			Resources: []api.Resource{
				{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
					Metadata: api.ObjectMeta{Name: "wg0"},
					Spec: api.WireGuardInterfaceSpec{
						PrivateKey:     wgPrivKey,
						PrivateKeyFile: wgPrivFile,
						ListenPort:     51820,
						MTU:            1420,
					},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection"},
					Metadata: api.ObjectMeta{Name: "ipsec0"},
					Spec: api.IPsecConnectionSpec{
						LocalAddress:  "203.0.113.1",
						RemoteAddress: "198.51.100.1",
						PreSharedKey:  ipsecPSK,
						LeftSubnet:    "10.0.0.0/24",
						RightSubnet:   "10.1.0.0/24",
					},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
					Metadata: api.ObjectMeta{Name: "bgp-peer"},
					Spec: api.BGPPeerSpec{
						RouterRef:    "bgp-router",
						PeerASN:      65001,
						Peers:        []string{"10.0.0.2"},
						Password:     bgpPassword,
						PasswordFrom: api.SecretValueSourceSpec{File: "/etc/routerd/bgp.secret", Env: "BGP_SECRET_ENV"},
					},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"},
					Metadata: api.ObjectMeta{Name: "pppoe0"},
					Spec: api.PPPoESessionSpec{
						Interface:    "eth0",
						Username:     "ppp-user",
						Password:     pppoePass,
						PasswordFile: pppoePassF,
					},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"},
					Metadata: api.ObjectMeta{Name: "vip0"},
					Spec: api.VirtualAddressSpec{
						Family:    "ipv4",
						Interface: "eth0",
						Address:   "203.0.113.10/32",
						Mode:      "vrrp",
						VRRP: api.VirtualAddressVRRPSpec{
							VirtualRouterID:    51,
							AuthenticationFrom: api.SecretValueSourceSpec{File: vrrpAuthFile, Env: vrrpAuthEnv},
						},
					},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
					Metadata: api.ObjectMeta{Name: "grp0"},
					Spec: api.EventGroupSpec{
						Auth: api.EventGroupAuth{
							Mode:       "hmac",
							SecretRef:  "grp-secret-ref",
							SecretFile: groupSecretF,
						},
					},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"},
					Metadata: api.ObjectMeta{Name: "azure-1"},
					Spec: api.CloudProviderProfileSpec{
						Provider:     "azure",
						Capabilities: []string{"secondary-ip"},
						Auth:         api.ProviderAuth{Mode: "external-command", Command: "/usr/local/bin/azure-helper"},
					},
				},
			},
		},
	}
}

// TestBuildPluginContextRedactsAllSecrets is THE critical security test: it
// allowlists secret-bearing resources, then asserts the JSON the plugin would
// receive contains none of the literal secret values nor any secret file path,
// while keeping non-secret fields.
func TestBuildPluginContextRedactsAllSecrets(t *testing.T) {
	router := secretBearingRouter()
	allow := []api.PluginContextResourceRef{
		{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface", Name: "wg0"},
		{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection", Name: "ipsec0"},
		{APIVersion: api.NetAPIVersion, Kind: "BGPPeer", Name: "bgp-peer"},
		{APIVersion: api.NetAPIVersion, Kind: "PPPoESession", Name: "pppoe0"},
		{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress", Name: "vip0"},
		{APIVersion: api.FederationAPIVersion, Kind: "EventGroup", Name: "grp0"},
		{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile", Name: "azure-1"},
	}

	ctx, err := BuildPluginContext(allow, router.Spec.Resources)
	if err != nil {
		t.Fatalf("BuildPluginContext: %v", err)
	}
	if len(ctx.Resources) != len(allow) {
		t.Fatalf("want %d context resources, got %d", len(allow), len(ctx.Resources))
	}

	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("marshal context: %v", err)
	}
	jsonStr := string(data)

	// Every secret value AND every secret file path must be ABSENT.
	forbidden := []string{
		wgPrivKey, wgPrivFile,
		ipsecPSK,
		bgpPassword, "/etc/routerd/bgp.secret", "BGP_SECRET_ENV",
		pppoePass, pppoePassF,
		vrrpAuthFile, vrrpAuthEnv,
		groupSecretF, "grp-secret-ref",
	}
	for _, secret := range forbidden {
		if strings.Contains(jsonStr, secret) {
			t.Errorf("redaction leak: plugin context JSON contains %q\nJSON: %s", secret, jsonStr)
		}
	}

	// Non-secret fields must survive.
	expected := []string{
		"51820",                       // WireGuard listenPort
		"203.0.113.1",                 // IPsec localAddress
		"10.0.0.0/24",                 // IPsec leftSubnet
		"65001",                       // BGP peerASN
		"ppp-user",                    // PPPoE username
		"203.0.113.10/32",             // VirtualAddress address
		"azure",                       // CloudProviderProfile provider
		"external-command",            // provider auth.mode
		"/usr/local/bin/azure-helper", // provider auth.command (NOT a secret)
		"secondary-ip",                // capability
	}
	for _, want := range expected {
		if !strings.Contains(jsonStr, want) {
			t.Errorf("non-secret field missing from context JSON: %q\nJSON: %s", want, jsonStr)
		}
	}
}

func TestBuildPluginContextDefaultDeny(t *testing.T) {
	router := secretBearingRouter()
	ctx, err := BuildPluginContext(nil, router.Spec.Resources)
	if err != nil {
		t.Fatalf("BuildPluginContext: %v", err)
	}
	if len(ctx.Resources) != 0 {
		t.Fatalf("empty allowlist must yield empty context, got %d resources", len(ctx.Resources))
	}
	// Empty allowlist must also serialize without a resources field (omitempty).
	data, _ := json.Marshal(ctx)
	if strings.Contains(string(data), "resources") {
		t.Fatalf("default-deny context should be empty JSON, got %s", data)
	}
}

func TestBuildPluginContextSelectsOnlyAllowlisted(t *testing.T) {
	router := secretBearingRouter()
	// Allowlist only the WireGuard interface; the others (incl. their secrets)
	// must be entirely absent.
	allow := []api.PluginContextResourceRef{
		{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface", Name: "wg0"},
	}
	ctx, err := BuildPluginContext(allow, router.Spec.Resources)
	if err != nil {
		t.Fatalf("BuildPluginContext: %v", err)
	}
	if len(ctx.Resources) != 1 {
		t.Fatalf("want 1 resource, got %d", len(ctx.Resources))
	}
	if ctx.Resources[0].Kind != "WireGuardInterface" {
		t.Fatalf("want WireGuardInterface, got %s", ctx.Resources[0].Kind)
	}
	data, _ := json.Marshal(ctx)
	jsonStr := string(data)
	// A non-allowlisted resource and its secret must not appear at all.
	for _, absent := range []string{"IPsecConnection", ipsecPSK, bgpPassword, "ppp-user"} {
		if strings.Contains(jsonStr, absent) {
			t.Errorf("non-allowlisted content leaked: %q in %s", absent, jsonStr)
		}
	}
}

func TestBuildPluginContextMissingRefSkipped(t *testing.T) {
	router := secretBearingRouter()
	allow := []api.PluginContextResourceRef{
		{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface", Name: "wg0"},
		{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface", Name: "does-not-exist"},
	}
	ctx, err := BuildPluginContext(allow, router.Spec.Resources)
	if err != nil {
		t.Fatalf("missing ref must not error, got %v", err)
	}
	if len(ctx.Resources) != 1 {
		t.Fatalf("missing ref should be skipped, want 1 resource got %d", len(ctx.Resources))
	}
}

func TestBuildPluginContextDoesNotMutateSource(t *testing.T) {
	router := secretBearingRouter()
	allow := []api.PluginContextResourceRef{
		{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface", Name: "wg0"},
		{APIVersion: api.NetAPIVersion, Kind: "IPsecConnection", Name: "ipsec0"},
	}
	if _, err := BuildPluginContext(allow, router.Spec.Resources); err != nil {
		t.Fatalf("BuildPluginContext: %v", err)
	}

	// The original typed specs must still carry their secrets intact.
	wg, ok := router.Spec.Resources[0].Spec.(api.WireGuardInterfaceSpec)
	if !ok {
		t.Fatalf("wg spec type changed: %T", router.Spec.Resources[0].Spec)
	}
	if wg.PrivateKey != wgPrivKey || wg.PrivateKeyFile != wgPrivFile {
		t.Fatalf("source WireGuard secret mutated: %+v", wg)
	}
	ipsec, ok := router.Spec.Resources[1].Spec.(api.IPsecConnectionSpec)
	if !ok {
		t.Fatalf("ipsec spec type changed: %T", router.Spec.Resources[1].Spec)
	}
	if ipsec.PreSharedKey != ipsecPSK {
		t.Fatalf("source IPsec PSK mutated: %q", ipsec.PreSharedKey)
	}
}
