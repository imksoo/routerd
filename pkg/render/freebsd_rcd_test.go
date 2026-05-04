package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestFreeBSDRCDScript(t *testing.T) {
	data, err := FreeBSDRCDScript("routerd-dns-resolver.service", api.SystemdUnitSpec{
		ExecStart:                []string{"/usr/local/sbin/routerd-dns-resolver", "--config", "/usr/local/etc/routerd/dns-resolver.yaml"},
		RuntimeDirectory:         []string{"routerd/dns-resolver"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd/dns-resolver"},
		LogsDirectory:            []string{"routerd"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`# PROVIDE: routerd_dns_resolver`,
		`command="/usr/sbin/daemon"`,
		`procname='/usr/local/sbin/routerd-dns-resolver'`,
		`command_args="-P ${pidfile} -r -f -- '/usr/local/sbin/routerd-dns-resolver' '--config' '/usr/local/etc/routerd/dns-resolver.yaml'"`,
		`mkdir -p '/var/run/routerd/dns-resolver'`,
		`mkdir -p '/var/db/routerd/dns-resolver'`,
		`mkdir -p '/var/log/routerd'`,
		`: ${routerd_dns_resolver_enable:="YES"}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rc.d script missing %q:\n%s", want, got)
		}
	}
}

func TestFreeBSDRenderIncludesSystemdUnitAsRCD(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "SystemdUnit"},
			Metadata: api.ObjectMeta{Name: "routerd-healthcheck@internet.service"},
			Spec: api.SystemdUnitSpec{
				ExecStart:        []string{"/usr/local/sbin/routerd-healthcheck", "daemon", "--resource", "internet"},
				RuntimeDirectory: []string{"routerd/healthcheck"},
				StateDirectory:   []string{"routerd/healthcheck"},
			},
		},
	}}}
	cfg, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg.RCConf), `routerd_healthcheck_internet_enable="YES"`) {
		t.Fatalf("rc.conf missing enable flag:\n%s", cfg.RCConf)
	}
	if _, ok := cfg.RCDScripts["routerd_healthcheck_internet"]; !ok {
		t.Fatalf("rc.d scripts = %#v", cfg.RCDScripts)
	}
}
