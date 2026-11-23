package obs

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/pidipam/IF3110-lahtetibabanget/internal/migration/workflow"
	"github.com/pidipam/IF3110-lahtetibabanget/internal/queue"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("dbmp")

func NewLogger(level string) *slog.Logger {
	return newLogger(level, os.Stdout)
}

func newLogger(level string, w io.Writer) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: l}))
}

func InitTracer(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, err
	}
	res, err := sdkresource.New(ctx, sdkresource.WithAttributes(attribute.String("service.name", serviceName)))
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return provider.Shutdown, nil
}

func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

func Traceparent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

func ContextWithTraceparent(ctx context.Context, traceparent string) context.Context {
	if traceparent == "" {
		return ctx
	}
	carrier := propagation.MapCarrier{"traceparent": traceparent}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

type tracedQueue struct{ next workflow.Queue }

func WrapQueue(q workflow.Queue) workflow.Queue {
	return &tracedQueue{next: q}
}

func (q *tracedQueue) Enqueue(ctx context.Context, executionID int64, delay time.Duration) (string, error) {
	if tp := Traceparent(ctx); tp != "" {
		ctx = queue.WithTraceparent(ctx, tp)
	}
	return q.next.Enqueue(ctx, executionID, delay)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := StartSpan(ctx, r.Method+" "+r.URL.Path,
			attribute.String("http.method", r.Method),
			attribute.String("http.route", r.URL.Path))
		defer span.End()
		if tp := Traceparent(ctx); tp != "" {
			w.Header().Set("traceparent", tp)
		}
		rec := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(ctx))
		if rec.status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(rec.status))
		}
	})
}
