package render

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestTimesyncdConfig(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"},
				Metadata: api.ObjectMeta{Name: "system-time"},
				Spec: api.NTPClientSpec{
					Provider: "systemd-timesyncd",
					Managed:  true,
					Source:   "static",
					Servers:  []string{"pool.ntp.org", "time.google.com"},
				},
			},
		}},
	}
	data, err := TimesyncdConfig(router)
	if err != nil {
		t.Fatalf("render timesyncd: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "[Time]\nNTP=pool.ntp.org time.google.com\n") {
		t.Fatalf("unexpected timesyncd config:\n%s", got)
	}
}

func TestTimesyncdConfigClearsGlobalNTPWhenInterfaceScoped(t *testing.T) {
	router := &api.Router{
		Spec: api.RouterSpec{Resources: []api.Resource{
			{
				TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"},
				Metadata: api.ObjectMeta{Name: "system-time"},
				Spec: api.NTPClientSpec{
					Provider:  "systemd-timesyncd",
					Managed:   true,
					Source:    "static",
					Interface: "wan",
					Servers:   []string{"pool.ntp.org"},
				},
			},
		}},
	}
	data, err := TimesyncdConfig(router)
	if err != nil {
		t.Fatalf("render timesyncd: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "[Time]\nNTP=\n") {
		t.Fatalf("unexpected timesyncd config:\n%s", got)
	}
}
