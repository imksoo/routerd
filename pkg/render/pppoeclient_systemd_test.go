package render

import (
	"strings"
	"testing"
)

func TestPPPoEClientSystemdUnit(t *testing.T) {
	got := string(PPPoEClientSystemdUnit("/usr/local/sbin/routerd-pppoe-client", "softether", "ens18", "open@open.ad.jp", "open"))
	for _, want := range []string{
		"Description=routerd PPPoE client softether",
		"--resource softether",
		"--interface ens18",
		"--username \"open@open.ad.jp\"",
		"ProtectSystem=strict",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("unit missing %q:\n%s", want, got)
		}
	}
}
