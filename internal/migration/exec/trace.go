package exec

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Span anak dari span worker "migration.execute" (API -> queue -> worker -> lock -> batch/query).
var tracer = otel.Tracer("dbmp/exec")

func startSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

func endSpan(sp trace.Span, err error) {
	if err != nil {
		sp.RecordError(err)
		sp.SetStatus(codes.Error, err.Error())
	}
	sp.End()
}

// traceAttrs menambahkan trace_id/span_id ke log agar log dan span bisa dikorelasikan.
func traceAttrs(ctx context.Context) []any {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	return []any{"trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String()}
}

var secretRe = regexp.MustCompile(`(?i)(password|passwd|secret|token)(\s*(=|to)?\s*)'[^']*'`)

// logSQL menyamarkan literal kredensial lalu memotong SQL untuk dicatat di log.
func logSQL(sql string) string {
	sql = secretRe.ReplaceAllString(sql, "${1}${2}'***'")
	if len(sql) > 2000 {
		return sql[:2000] + "...(dipotong)"
	}
	return sql
}

var sqlstateRe = regexp.MustCompile(`SQLSTATE ([0-9A-Z]{5})`)

// sqlStateOf mengambil SQLSTATE dari error mentah atau dari pesan terbungkus ("SQLSTATE 22012").
func sqlStateOf(err error) string {
	if err == nil {
		return ""
	}
	if c := SQLState(err); c != "" {
		return c
	}
	if m := sqlstateRe.FindStringSubmatch(err.Error()); m != nil {
		return m[1]
	}
	return ""
}
