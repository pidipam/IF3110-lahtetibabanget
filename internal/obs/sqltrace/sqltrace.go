package sqltrace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("dbmp/sql")

const (
	maxQueryLen = 2000
	maxParamLen = 64
	masked      = "***"
)

type Tracer struct {
	Target     string
	DatabaseID int64
	Level      slog.Level
	Logger     *slog.Logger
}

var _ pgx.QueryTracer = (*Tracer)(nil)

type startKey struct{}

type startData struct {
	at     time.Time
	query  string
	params []any
	span   trace.Span
}

func (t *Tracer) logger() *slog.Logger {
	if t.Logger != nil {
		return t.Logger
	}
	return slog.Default()
}

func (t *Tracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	query := MaskQuery(data.SQL)
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		ctx, span = tracer.Start(ctx, "db.query", trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("db.system", "postgresql"),
				attribute.String("db.target", t.Target),
				attribute.Int64("db.connection_id", t.DatabaseID),
				attribute.String("db.statement", query),
			))
	} else {
		span = nil
	}
	return context.WithValue(ctx, startKey{}, &startData{at: time.Now(), query: query, params: MaskParams(data.SQL, data.Args), span: span})
}

func (t *Tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	sd, ok := ctx.Value(startKey{}).(*startData)
	if !ok {
		return
	}
	dur := time.Since(sd.at)
	attrs := []any{
		"event", "sql.query",
		"db_target", t.Target,
		"query", sd.query,
		"params", sd.params,
		"duration_ms", float64(dur.Microseconds()) / 1000,
	}
	if t.DatabaseID != 0 {
		attrs = append(attrs, "database", t.DatabaseID)
	}
	if sd.span != nil {
		sc := sd.span.SpanContext()
		attrs = append(attrs, "trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
	}
	level := t.Level
	if data.Err != nil {
		state := SQLState(data.Err)
		attrs = append(attrs, "status", "error", "sqlstate", state, "error", errorMessage(data.Err))
		if sd.span != nil {
			sd.span.RecordError(data.Err)
			sd.span.SetStatus(codes.Error, state)
		}
		if level < slog.LevelWarn && !errors.Is(data.Err, context.Canceled) {
			level = slog.LevelWarn
		}
	} else {
		rows := data.CommandTag.RowsAffected()
		attrs = append(attrs, "status", "ok", "rows", rows, "command", data.CommandTag.String())
		if sd.span != nil {
			sd.span.SetAttributes(attribute.Int64("db.rows", rows))
		}
	}
	if sd.span != nil {
		sd.span.End()
	}
	t.logger().Log(ctx, level, "sql.query", attrs...)
}

func SQLState(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return ""
}

func errorMessage(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Message
	}
	return err.Error()
}

var (
	secretLiteral  = regexp.MustCompile(`(?i)\b(password|passwd|secret|token)(\s*(?:=|to)?\s*)'(?:[^']|'')*'`)
	dsnPassword    = regexp.MustCompile(`([a-z][a-z0-9+.-]*://[^:/@\s']+:)[^@\s']+@`)
	sensitiveQuery = regexp.MustCompile(`(?i)password|passwd|secret|token|session|csrf|credential|encrypted|hash`)
)

func MaskQuery(q string) string {
	q = secretLiteral.ReplaceAllString(q, "${1}${2}'"+masked+"'")
	q = dsnPassword.ReplaceAllString(q, "${1}"+masked+"@")
	if len(q) > maxQueryLen {
		q = q[:maxQueryLen] + "...(dipotong)"
	}
	return q
}

func MaskParams(query string, args []any) []any {
	if len(args) == 0 {
		return []any{}
	}
	sensitive := sensitiveQuery.MatchString(query)
	out := make([]any, 0, len(args))
	for _, a := range args {
		if _, ok := a.(pgx.QueryExecMode); ok {
			continue
		}
		var r any
		switch v := a.(type) {
		case nil:
			r = nil
		case []byte:
			r = masked
		case string:
			if sensitive {
				r = masked
			} else if len(v) > maxParamLen {
				r = v[:maxParamLen] + "..."
			} else {
				r = v
			}
		case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, time.Time:
			r = v
		default:
			if sensitive {
				r = masked
			} else {
				r = fmt.Sprintf("%T", v)
			}
		}
		out = append(out, r)
	}
	return out
}
