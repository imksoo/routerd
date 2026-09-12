// SPDX-License-Identifier: BSD-3-Clause

package framework

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/lock"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestFaultControllerPanicCompletesObservation(t *testing.T) {
	const payload = "private-controller-payload"
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"string", payload},
		{"error", errors.New(payload)},
		{"nil", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			reader, spans := installReconcileSDK(t)
			var logs bytes.Buffer
			observer := &recordingResourceObserver{}
			gate := &sync.RWMutex{}
			locker := lock.NewResourceLocker()
			runner := Runner{Locker: locker, MutationGate: gate, Exclusive: true, Observer: observer,
				Logger: slog.New(slog.NewTextHandler(&logs, nil))}
			calls := 0
			controller := FuncController{ControllerName: "fault", PeriodicFunc: func(context.Context) (bool, error) {
				calls++
				if calls == 1 {
					panic(tc.value)
				}
				return false, nil
			}}
			err := runner.RunOnce(ctx, controller)
			var panicErr *PanicError
			if err == nil {
				t.Error("RunOnce returned nil after controller panic")
			} else if strings.Contains(err.Error(), payload) {
				t.Errorf("panic payload leaked in returned error: %v", err)
			}
			if !errors.As(err, &panicErr) || panicErr.Controller != "fault" || panicErr.Stage != "reconcile" {
				t.Errorf("missing typed controller panic: %v", err)
			}
			got := observer.snapshot()
			if len(got) != 1 || got[0].err == nil || got[0].trigger != "once" {
				t.Errorf("observer calls = %+v, want one failed once reconcile", got)
			}
			assertReconcileSDK(t, reader, spans, true)
			if strings.Contains(logs.String(), payload) {
				t.Error("panic payload leaked into logs")
			}
			for _, span := range spans.Ended() {
				if strings.Contains(fmt.Sprint(span.Events(), span.Attributes()), payload) {
					t.Error("panic payload leaked into spans")
				}
			}
			if !gate.TryLock() {
				t.Fatal("mutation gate remained locked after panic")
			}
			gate.Unlock()
			retryCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			if err := runner.RunOnce(retryCtx, controller); err != nil {
				t.Fatalf("same-key retry failed: %v", err)
			}
			if calls != 2 {
				t.Fatalf("controller calls = %d, want 2", calls)
			}
		})
	}
}

func TestFaultControllerNormalResultsKeepSDKContract(t *testing.T) {
	sentinel := errors.New("ordinary failure")
	for _, tc := range []struct {
		name string
		err  error
	}{{"success", nil}, {"error", sentinel}} {
		t.Run(tc.name, func(t *testing.T) {
			reader, spans := installReconcileSDK(t)
			err := (Runner{}).RunOnce(context.Background(), FuncController{ControllerName: "fault", PeriodicFunc: func(context.Context) (bool, error) {
				return false, tc.err
			}})
			if !errors.Is(err, tc.err) {
				t.Fatalf("RunOnce = %v, want %v", err, tc.err)
			}
			assertReconcileSDK(t, reader, spans, tc.err != nil)
		})
	}
}

func installReconcileSDK(t *testing.T) (*sdkmetric.ManualReader, *tracetest.SpanRecorder) {
	t.Helper()
	// These tests are deliberately serial: Reconcile uses the global providers.
	previousMeter, previousTracer := otel.GetMeterProvider(), otel.GetTracerProvider()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	otel.SetMeterProvider(mp)
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetMeterProvider(previousMeter)
		otel.SetTracerProvider(previousTracer)
		if err := mp.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
		if err := tp.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return reader, spans
}

func assertReconcileSDK(t *testing.T, reader *sdkmetric.ManualReader, spans *tracetest.SpanRecorder, wantError bool) {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	wantAttrs := attribute.NewSet(attribute.String("routerd.controller.name", "fault"),
		attribute.String("routerd.controller.trigger", "once"), attribute.Bool("routerd.controller.error", wantError))
	var counter, histogram uint64
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			switch metric.Name {
			case "routerd.controller.reconcile":
				for _, point := range metric.Data.(metricdata.Sum[int64]).DataPoints {
					if point.Attributes.Equals(&wantAttrs) {
						counter += uint64(point.Value)
					}
				}
			case "routerd.controller.reconcile.duration_ms":
				for _, point := range metric.Data.(metricdata.Histogram[float64]).DataPoints {
					if point.Attributes.Equals(&wantAttrs) {
						histogram += point.Count
					}
				}
			}
		}
	}
	if counter != 1 || histogram != 1 {
		t.Errorf("SDK counter=%d histogram count=%d, want 1 each (error=%t)", counter, histogram, wantError)
	}
	ended := spans.Ended()
	if len(spans.Started()) != 1 || len(ended) != 1 {
		t.Fatalf("SDK spans started=%d ended=%d, want 1 each", len(spans.Started()), len(ended))
	}
	var exceptions int
	for _, event := range ended[0].Events() {
		if event.Name == "exception" {
			exceptions++
		}
	}
	wantExceptions := 0
	if wantError {
		wantExceptions = 1
	}
	if exceptions != wantExceptions {
		t.Errorf("SDK RecordError exceptions=%d, want %d", exceptions, wantExceptions)
	}
}

func TestFaultControllerPanicResetsAdaptiveInterval(t *testing.T) {
	observer := &recordingResourceObserver{}
	intervals := adaptiveReconcileIntervalsForMax(time.Minute)
	level, updates := 4, 0
	err := runLockedObservedInterval(context.Background(), slog.Default(), lock.NewResourceLocker(), observer,
		"fault:periodic", "fault", "periodic", "", "", intervals[level], func(err error) time.Duration {
			updates++
			level = nextAdaptiveReconcileLevel(level, false, err, len(intervals)-1)
			return intervals[level]
		}, func(context.Context) error { panic("failure") })
	if err == nil || updates != 1 || level != 0 {
		t.Fatalf("error=%v updates=%d level=%d, want error, 1, 0", err, updates, level)
	}
	got := observer.snapshot()
	if len(got) != 1 || got[0].interval != intervals[0] || got[0].err == nil {
		t.Fatalf("observer calls=%+v", got)
	}
}

type panicResultObserver struct{ calls int }

func (*panicResultObserver) ControllerStarted(string, time.Duration) {}
func (p *panicResultObserver) ControllerReconciled(string, string, time.Duration, time.Duration, error) {
	p.calls++
	panic("private-observer-payload")
}

func TestFaultObserverPanicPreservesControllerErrorWithoutRetry(t *testing.T) {
	sentinel := errors.New("ordinary controller failure")
	for _, tc := range []struct {
		name string
		err  error
	}{{"controller success", nil}, {"controller failure", sentinel}} {
		t.Run(tc.name, func(t *testing.T) {
			reader, spans := installReconcileSDK(t)
			observer := &panicResultObserver{}
			gate := &sync.RWMutex{}
			err := (Runner{Observer: observer, MutationGate: gate, Exclusive: true}).RunOnce(context.Background(),
				FuncController{ControllerName: "fault", PeriodicFunc: func(context.Context) (bool, error) { return false, tc.err }})
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Errorf("original controller error lost: %v", err)
			}
			if err == nil || strings.Contains(err.Error(), "private-observer-payload") {
				t.Errorf("unsafe observer panic error: %v", err)
			}
			var panicErr *PanicError
			if !errors.As(err, &panicErr) || panicErr.Stage != "framework" {
				t.Errorf("missing framework panic classification: %v", err)
			}
			// Controller telemetry is already complete. An observer panic must
			// not rewrite or repeat the measured controller result.
			assertReconcileSDK(t, reader, spans, tc.err != nil)
			if observer.calls != 1 {
				t.Errorf("observer calls=%d, want 1", observer.calls)
			}
			if !gate.TryLock() {
				t.Fatal("observer panic retained mutation gate")
			}
			gate.Unlock()
		})
	}
}

func TestFaultControllerPanicContinuesRunOnce(t *testing.T) {
	sentinel := errors.New("second failure")
	called := false
	err := (Runner{}).RunOnce(context.Background(),
		FuncController{ControllerName: "first", PeriodicFunc: func(context.Context) (bool, error) { panic("first failure") }},
		FuncController{ControllerName: "second", PeriodicFunc: func(context.Context) (bool, error) { called = true; return false, sentinel }})
	if !called || !errors.Is(err, sentinel) {
		t.Fatalf("subsequent controller called=%t error=%v", called, err)
	}
	if err == nil || err.Error() == sentinel.Error() {
		t.Fatalf("panic missing from aggregated errors: %v", err)
	}
}

func TestFaultBootstrapKeepsContextOnlyReturn(t *testing.T) {
	observer := &recordingResourceObserver{}
	sentinel := errors.New("ordinary failure")
	err := (Runner{Observer: observer}).Bootstrap(context.Background(),
		FuncController{ControllerName: "panic", ReconcileFunc: func(context.Context, daemonapi.DaemonEvent) error { panic("failure") }},
		FuncController{ControllerName: "error", ReconcileFunc: func(context.Context, daemonapi.DaemonEvent) error { return sentinel }})
	if err != nil {
		t.Fatalf("Bootstrap changed context-only return: %v", err)
	}
	got := observer.snapshot()
	if len(got) != 2 || got[0].err == nil || !errors.Is(got[1].err, sentinel) {
		t.Fatalf("Bootstrap observations=%+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	err = (Runner{}).Bootstrap(ctx, FuncController{ReconcileFunc: func(context.Context, daemonapi.DaemonEvent) error { cancel(); return sentinel }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Bootstrap cancellation=%v", err)
	}
}

func TestFaultLockCancellationSkipsControllerAndObserver(t *testing.T) {
	locker := lock.NewResourceLocker()
	unlock, err := locker.Lock(context.Background(), "held")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observer := &recordingResourceObserver{}
	err = runLocked(ctx, slog.Default(), locker, observer, "held", "fault", "event", "", "", time.Second,
		func(context.Context) error { t.Error("controller called without its lock"); return nil })
	if !errors.Is(err, context.Canceled) || len(observer.snapshot()) != 0 {
		t.Fatalf("lock cancellation error=%v observations=%v", err, observer.snapshot())
	}
}
