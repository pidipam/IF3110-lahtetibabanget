package exec

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestLogSQLRedactsSecretsAndTruncates(t *testing.T) {
	got := logSQL("CREATE USER bob PASSWORD 'rahasia123'; ALTER ROLE x SET password = 's3cr3t'")
	if strings.Contains(got, "rahasia123") || strings.Contains(got, "s3cr3t") {
		t.Fatalf("kredensial harus disamarkan: %s", got)
	}
	if !strings.Contains(got, "'***'") {
		t.Fatalf("penanda samaran hilang: %s", got)
	}
	long := logSQL(strings.Repeat("x", 5000))
	if len(long) > 2100 {
		t.Fatalf("SQL panjang harus dipotong: %d", len(long))
	}
}

// API -> queue -> worker -> lock -> batch/query: span anak muncul dan log membawa trace_id/span_id.
func TestSpansAndLogCorrelation(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	r := newRig(t)
	var buf bytes.Buffer
	r.h.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	r.bigTable(2500)
	id := r.ready(r.batchMig("batch", batchUp, batchDown, 1000))

	ctx, parent := tp.Tracer("test").Start(r.ctx, "migration.execute")
	r.must(r.h.Handle(ctx, id))
	parent.End()

	names := map[string]bool{}
	var traceID string
	for _, s := range rec.Ended() {
		names[s.Name()] = true
		if s.Name() == "migration.execute" {
			traceID = s.SpanContext().TraceID().String()
		}
	}
	for _, want := range []string{"lock.acquire", "migration.run", "batch.execute"} {
		if !names[want] {
			t.Fatalf("span %q tidak ada: %v", want, names)
		}
	}
	for _, s := range rec.Ended() {
		if s.Name() != "migration.execute" && s.SpanContext().TraceID().String() != traceID {
			t.Fatalf("span %q harus satu trace dengan worker", s.Name())
		}
	}
	out := buf.String()
	if !strings.Contains(out, `"trace_id":"`+traceID+`"`) || !strings.Contains(out, `"span_id"`) {
		t.Fatalf("log harus memuat trace_id/span_id yang sama:\n%s", out)
	}
	if !strings.Contains(out, `"event":"batch.start"`) || !strings.Contains(out, `"query"`) {
		t.Fatalf("log batch harus memuat query dan params:\n%s", out)
	}
}

// Spesifikasi: setiap attempt (juga yang gagal) dilog dengan actor, revisi, SQL yang dijalankan, dan SQLSTATE.
func TestFailedAttemptLogsSQLAndSQLState(t *testing.T) {
	r := newRig(t)
	var buf bytes.Buffer
	r.h.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	id := r.ready(r.mig("gagal", "CREATE TABLE zz_f(id int); SELECT 1/0", "DROP TABLE zz_f"))
	r.run(id)
	out := buf.String()
	for _, want := range []string{`"event":"sql.start"`, `"event":"sql.rollback"`, `"event":"execution.result"`, `"event":"history.failure_recorded"`,
		`"sqlstate":"22012"`, `"actor":`, `"revision":1`, `"attempt":1`, `SELECT 1/0`} {
		if !strings.Contains(out, want) {
			t.Fatalf("log attempt gagal harus memuat %s:\n%s", want, out)
		}
	}
}
