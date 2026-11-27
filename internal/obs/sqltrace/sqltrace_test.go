package sqltrace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestMaskQuery(t *testing.T) {
	cases := map[string]string{
		"ALTER ROLE app PASSWORD 'rahasia'":                  "ALTER ROLE app PASSWORD '***'",
		"SET password = 'a''b'":                              "SET password = '***'",
		"SELECT dblink('postgres://u:pw@h/db', 'select 1')":  "SELECT dblink('postgres://u:***@h/db', 'select 1')",
		"UPDATE zz_big SET x = x + 1 WHERE id > $1 LIMIT $2": "UPDATE zz_big SET x = x + 1 WHERE id > $1 LIMIT $2",
	}
	for in, want := range cases {
		if got := MaskQuery(in); got != want {
			t.Errorf("MaskQuery(%q) = %q, mau %q", in, got, want)
		}
	}
	long := MaskQuery(strings.Repeat("x", maxQueryLen+10))
	if !strings.HasSuffix(long, "...(dipotong)") || len(long) > maxQueryLen+20 {
		t.Fatalf("query panjang tidak dipotong: %d", len(long))
	}
}

func TestMaskParams(t *testing.T) {
	got := MaskParams("SELECT * FROM sessions WHERE token_hash = $1", []any{"abc", []byte{1}, int64(5)})
	if got[0] != masked || got[1] != masked || got[2] != int64(5) {
		t.Fatalf("query sensitif: %v", got)
	}
	got = MaskParams("SELECT * FROM t WHERE id > $1 AND name = $2", []any{int64(10), strings.Repeat("a", 100), nil})
	if got[0] != int64(10) || got[1] != strings.Repeat("a", maxParamLen)+"..." || got[2] != nil {
		t.Fatalf("query biasa: %v", got)
	}
	if out := MaskParams("SELECT 1", nil); out == nil || len(out) != 0 {
		t.Fatalf("tanpa argumen harus slice kosong: %v", out)
	}
}

func run(t *testing.T, tr *Tracer, sql string, args []any, end pgx.TraceQueryEndData) (map[string]any, tracetest.SpanStubs) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	old := tracer
	tracer = tp.Tracer("test")
	t.Cleanup(func() { tracer = old })

	var buf bytes.Buffer
	tr.Logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	parentCtx, parent := tp.Tracer("test").Start(context.Background(), "parent")
	ctx := tr.TraceQueryStart(parentCtx, nil, pgx.TraceQueryStartData{SQL: sql, Args: args})
	tr.TraceQueryEnd(ctx, nil, end)
	parent.End()

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("log bukan JSON: %v (%s)", err, buf.String())
	}
	var spans tracetest.SpanStubs
	for _, sp := range tracetest.SpanStubsFromReadOnlySpans(rec.Ended()) {
		if sp.Name != "parent" {
			spans = append(spans, sp)
		}
	}
	return got, spans
}

func TestTraceQuerySuccess(t *testing.T) {
	tr := &Tracer{Target: "target", DatabaseID: 7, Level: slog.LevelInfo}
	got, spans := run(t, tr, "SELECT * FROM zz_big WHERE id > $1", []any{int64(3)},
		pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 25")})
	if got["event"] != "sql.query" || got["level"] != "INFO" || got["status"] != "ok" || got["rows"] != float64(25) {
		t.Fatalf("log sukses: %v", got)
	}
	if got["database"] != float64(7) || got["db_target"] != "target" || got["query"] != "SELECT * FROM zz_big WHERE id > $1" {
		t.Fatalf("atribut: %v", got)
	}
	if len(spans) != 1 || spans[0].Name != "db.query" {
		t.Fatalf("span: %+v", spans)
	}
	if got["trace_id"] != spans[0].SpanContext.TraceID().String() || got["span_id"] != spans[0].SpanContext.SpanID().String() {
		t.Fatalf("trace_id/span_id log tidak cocok dengan span: %v", got)
	}
}

func TestTraceQueryErrorHasSQLStateAndNoSecrets(t *testing.T) {
	tr := &Tracer{Target: "platform", Level: slog.LevelDebug}
	pgErr := &pgconn.PgError{Code: "22012", Message: "division by zero"}
	got, spans := run(t, tr, "UPDATE users SET password_hash = $1 WHERE id = $2", []any{"$2a$10$rahasia", int64(1)},
		pgx.TraceQueryEndData{Err: pgErr})
	if got["level"] != "WARN" || got["status"] != "error" || got["sqlstate"] != "22012" || got["error"] != "division by zero" {
		t.Fatalf("log gagal: %v", got)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "rahasia") {
		t.Fatalf("parameter sensitif bocor ke log: %s", raw)
	}
	if _, ok := got["database"]; ok {
		t.Fatal("platform DB tidak punya id koneksi")
	}
	if len(spans) != 1 || spans[0].Status.Description != "22012" {
		t.Fatalf("status span: %+v", spans)
	}
}

func TestTraceQueryEndWithoutStartIsNoop(t *testing.T) {
	(&Tracer{}).TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{Err: errors.New("x")})
}

func TestQueryWithoutParentLogsButStartsNoSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	old := tracer
	tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)).Tracer("test")
	t.Cleanup(func() { tracer = old })
	var buf bytes.Buffer
	tr := &Tracer{Target: "platform", Level: slog.LevelInfo, Logger: slog.New(slog.NewJSONHandler(&buf, nil))}
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 1")})
	if len(rec.Ended()) != 0 {
		t.Fatalf("tanpa induk tidak boleh membuat span akar: %d", len(rec.Ended()))
	}
	if !strings.Contains(buf.String(), `"event":"sql.query"`) || strings.Contains(buf.String(), "trace_id") {
		t.Fatalf("query tetap dilog tanpa trace_id: %s", buf.String())
	}
}

func TestMaskParamsSkipsExecMode(t *testing.T) {
	got := MaskParams("CREATE TABLE t(id int)", []any{pgx.QueryExecModeSimpleProtocol})
	if len(got) != 0 {
		t.Fatalf("opsi eksekusi pgx bukan parameter: %v", got)
	}
}
