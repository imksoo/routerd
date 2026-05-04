package chain

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/logstore"
)

func TestLogRetentionControllerDeletesExpiredRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns-queries.db")
	dnsLog, err := logstore.OpenDNSQueryLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := dnsLog.Record(context.Background(), logstore.DNSQuery{Timestamp: time.Now().Add(-48 * time.Hour), ClientAddress: "172.18.0.2", QuestionName: "old.example", QuestionType: "A"}); err != nil {
		t.Fatal(err)
	}
	_ = dnsLog.Close()
	store := mapStore{}
	controller := LogRetentionController{
		Router: &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
			TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogRetention"},
			Metadata: api.ObjectMeta{Name: "default"},
			Spec: api.LogRetentionSpec{Schedule: "daily", IncrementalVacuum: true, Targets: []api.LogRetentionTargetSpec{{
				File: path, Retention: "24h",
			}}},
		}}}},
		Bus:   bus.New(),
		Store: store,
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.SystemAPIVersion, "LogRetention", "default")
	if status["phase"] != "Applied" || status["deleted"] != int64(1) {
		t.Fatalf("status = %#v", status)
	}
}
