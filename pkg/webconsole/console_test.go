package webconsole

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"routerd/pkg/apply"
	"routerd/pkg/logstore"
	"routerd/pkg/observe"
	routerstate "routerd/pkg/state"
)

type fakeStore struct {
	resources []routerstate.ObjectStatus
	events    []routerstate.StoredEvent
}

func (s fakeStore) Get(string) routerstate.Value                            { return routerstate.Value{} }
func (s fakeStore) Set(string, string, string) routerstate.Value            { return routerstate.Value{} }
func (s fakeStore) Unset(string, string) routerstate.Value                  { return routerstate.Value{} }
func (s fakeStore) Forget(string, string) routerstate.Value                 { return routerstate.Value{} }
func (s fakeStore) Delete(string)                                           {}
func (s fakeStore) Age(string) time.Duration                                { return 0 }
func (s fakeStore) Now() time.Time                                          { return time.Now() }
func (s fakeStore) Save(string) error                                       { return nil }
func (s fakeStore) Variables() map[string]routerstate.Value                 { return nil }
func (s fakeStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) { return s.resources, nil }
func (s fakeStore) ListEvents(routerstate.EventQuery) ([]routerstate.StoredEvent, error) {
	return s.events, nil
}

func TestHandlerServesReadOnlySummary(t *testing.T) {
	queryLog := t.TempDir() + "/dns-queries.db"
	trafficLog := t.TempDir() + "/traffic-flows.db"
	dnsLog, err := logstore.OpenDNSQueryLog(queryLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := dnsLog.Record(reqContext(), logstore.DNSQuery{Timestamp: time.Now(), ClientAddress: "172.18.0.2", QuestionName: "example.com", QuestionType: "A", Answers: []string{"93.184.216.34"}, ResponseCode: "NOERROR"}); err != nil {
		t.Fatal(err)
	}
	_ = dnsLog.Close()
	flows, err := logstore.OpenTrafficFlowLog(trafficLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := flows.UpsertActive(context.Background(), logstore.TrafficFlow{StartedAt: time.Now(), ClientAddress: "172.18.0.2", PeerAddress: "93.184.216.34", Protocol: "tcp"}); err != nil {
		t.Fatal(err)
	}
	_ = flows.Close()
	handler := New(Options{
		Store: fakeStore{
			resources: []routerstate.ObjectStatus{{APIVersion: "net.routerd.net/v1alpha1", Kind: "HealthCheck", Name: "internet", Status: map[string]any{"phase": "Healthy"}}},
			events:    []routerstate.StoredEvent{{ID: 1, Topic: "routerd.test", CreatedAt: time.Date(2026, 5, 4, 1, 2, 3, 0, time.UTC)}},
		},
		Result: func() *apply.Result {
			return &apply.Result{Phase: "Healthy", Generation: 7, Resources: []apply.ResourceResult{{ID: "x", Phase: "Healthy"}}}
		},
		NAPT: func(limit int) (*observe.NAPTTable, error) {
			return &observe.NAPTTable{Count: 3, Max: 262144}, nil
		},
		DNSQueryLogPath:    queryLog,
		TrafficFlowLogPath: trafficLog,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"phase": "Healthy"`, `"generation": 7`, `"HealthCheck"`, `"napt"`, `"dnsQueries"`, `"trafficFlows"`, "example.com"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("summary missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func reqContext() context.Context { return context.Background() }

func TestHandlerRejectsWriteMethods(t *testing.T) {
	handler := New(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/summary", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandlerServesDNSQueries(t *testing.T) {
	queryLog := t.TempDir() + "/dns-queries.db"
	dnsLog, err := logstore.OpenDNSQueryLog(queryLog)
	if err != nil {
		t.Fatal(err)
	}
	if err := dnsLog.Record(context.Background(), logstore.DNSQuery{Timestamp: time.Now(), ClientAddress: "172.18.0.2", QuestionName: "www.example.com", QuestionType: "AAAA", ResponseCode: "NOERROR"}); err != nil {
		t.Fatal(err)
	}
	_ = dnsLog.Close()
	handler := New(Options{DNSQueryLogPath: queryLog})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/dns-queries?since=1h&limit=10", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "www.example.com") {
		t.Fatalf("dns queries missing row:\n%s", rec.Body.String())
	}
}

func TestHandlerServesTrafficFlows(t *testing.T) {
	path := t.TempDir() + "/traffic-flows.db"
	flowLog, err := logstore.OpenTrafficFlowLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := flowLog.UpsertActive(context.Background(), logstore.TrafficFlow{StartedAt: time.Now(), ClientAddress: "172.18.0.2", PeerAddress: "1.1.1.1", PeerPort: 443, Protocol: "tcp"}); err != nil {
		t.Fatal(err)
	}
	_ = flowLog.Close()
	handler := New(Options{TrafficFlowLogPath: path})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/traffic-flows?since=1h&limit=10", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "1.1.1.1") {
		t.Fatalf("traffic flows missing row:\n%s", rec.Body.String())
	}
}

func TestHandlerRendersUsableBasePath(t *testing.T) {
	handler := New(Options{BasePath: "/"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `const base = "/"`) {
		t.Fatalf("base path was not a JavaScript string literal:\n%s", body)
	}
	if strings.Contains(body, `const base = "\"/\""`) {
		t.Fatalf("base path was double quoted:\n%s", body)
	}
}

func TestHandlerRendersMobileSafeLayout(t *testing.T) {
	handler := New(Options{Title: "homert02"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`overflow-x:hidden`,
		`text-overflow:ellipsis`,
		`@media (max-width:640px)`,
		`overflow-x:auto`,
		`white-space:nowrap`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("mobile layout CSS missing %q:\n%s", want, body)
		}
	}
}

func TestHandlerRendersCompactTrafficAndEvents(t *testing.T) {
	handler := New(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`api/summary?events=15&napt=30`,
		`function dnsLabelMap`,
		`function clientTrafficRows`,
		`Client Traffic`,
		`dst-label`,
		`proto-tcp`,
		`state-established`,
		`["proto","state","flow","timeout"]`,
		`function flowCell`,
		`function sameReverse`,
		`function returnDetails`,
		`function natDelta`,
		`function remember`,
		`class="flash"`,
		`@keyframes flash`,
		`class="flow-summary"`,
		`class="return-button">return</span>`,
		`<span>nat</span>`,
		`.slice(0,15)`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("console markup missing %q:\n%s", want, body)
		}
	}
}
