// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"strings"
	"testing"
)

func TestRenderNTPDConfig(t *testing.T) {
	data := renderNTPDConfig([]string{"ntp.jst.mfeed.ad.jp", "ntp.nict.jp"})
	if got, want := string(data), "server ntp.jst.mfeed.ad.jp iburst\n"; !strings.Contains(got, want) {
		t.Fatalf("config missing %q:\n%s", want, got)
	}
	if got, want := string(data), "server ntp.nict.jp iburst\n"; !strings.Contains(got, want) {
		t.Fatalf("config missing %q:\n%s", want, got)
	}
}

func TestRenderNTPDConfigWithListenAddresses(t *testing.T) {
	data := renderNTPDConfig([]string{"ntp.jst.mfeed.ad.jp"}, []string{"192.168.160.4", "2409:10:3d60:1250::4/64"})
	for _, want := range []string{
		"interface ignore all\n",
		"interface listen 127.0.0.1\n",
		"interface listen ::1\n",
		"interface listen 192.168.160.4\n",
		"interface listen 2409:10:3d60:1250::4\n",
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("config missing %q:\n%s", want, string(data))
		}
	}
}
