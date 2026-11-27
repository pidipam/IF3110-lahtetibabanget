package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/queue"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const sampleTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func useTraceContext() {
	otel.SetTextMapPropagator(propagation.TraceContext{})
}

func TestNewLoggerJSON(t *testing.T) {
	var buf bytes.Buffer
	newLogger("info", &buf).Info("halo", "event", "test")
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["msg"] != "halo" || got["event"] != "test" || got["level"] != "INFO" {
		t.Fatalf("log: %v", got)
	}
}

func TestTraceparentRoundTrip(t *testing.T) {
	useTraceContext()
	ctx := ContextWithTraceparent(context.Background(), sampleTraceparent)
	if got := Traceparent(ctx); got != sampleTraceparent {
		t.Fatalf("traceparent: %q", got)
	}
}

type captureQueue struct {
	traceparent string
	jobID       string
}

func (q *captureQueue) Enqueue(ctx context.Context, executionID int64, delay time.Duration) (string, error) {
	q.traceparent = queue.TraceparentFromContext(ctx)
	q.jobID = "job-test"
	return q.jobID, nil
}

func TestWrapQueueMeneruskanTraceparent(t *testing.T) {
	useTraceContext()
	next := &captureQueue{}
	ctx := ContextWithTraceparent(context.Background(), sampleTraceparent)
	jobID, err := WrapQueue(next).Enqueue(ctx, 42, 0)
	if err != nil {
		t.Fatal(err)
	}
	if jobID != "job-test" || next.traceparent != sampleTraceparent {
		t.Fatalf("job=%q traceparent=%q", jobID, next.traceparent)
	}
}

func TestMiddlewareMenyuntikTraceparent(t *testing.T) {
	useTraceContext()
	var seen string
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = Traceparent(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("traceparent", sampleTraceparent)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if seen != sampleTraceparent {
		t.Fatalf("context traceparent: %q", seen)
	}
	if got := rec.Header().Get("traceparent"); got != sampleTraceparent {
		t.Fatalf("response traceparent: %q", got)
	}
}

func TestLoggerAddsTraceIDFromContext(t *testing.T) {
	useTraceContext()
	var buf bytes.Buffer
	ctx := ContextWithTraceparent(context.Background(), sampleTraceparent)
	newLogger("info", &buf).With("event", "x").InfoContext(ctx, "halo")
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" || got["span_id"] != "00f067aa0ba902b7" || got["event"] != "x" {
		t.Fatalf("log tanpa trace_id/span_id: %v", got)
	}
	buf.Reset()
	newLogger("info", &buf).Info("tanpa konteks")
	if bytes.Contains(buf.Bytes(), []byte("trace_id")) {
		t.Fatalf("log tanpa span tidak boleh punya trace_id: %s", buf.String())
	}
}
