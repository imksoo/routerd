package webconsole

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"routerd/pkg/apply"
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
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"phase": "Healthy"`, `"generation": 7`, `"HealthCheck"`, `"napt"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("summary missing %q:\n%s", want, rec.Body.String())
		}
	}
}

func TestHandlerRejectsWriteMethods(t *testing.T) {
	handler := New(Options{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/summary", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
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
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("mobile layout CSS missing %q:\n%s", want, body)
		}
	}
}
