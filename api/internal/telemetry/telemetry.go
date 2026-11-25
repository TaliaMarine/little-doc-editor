// Package telemetry wires Decision 27's observability: Prometheus metrics
// (served on a dedicated listener, not through the public ingress) and
// OpenTelemetry tracing exported over OTLP when OTEL_EXPORTER_OTLP_ENDPOINT
// is set.
//
// Request paths are never recorded verbatim in metrics labels or span
// attributes: /api/v1/share/{token} and /api/v1/invites/{token} carry bearer
// tokens, so only the matched route pattern (or a redacted path) is used.
package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/TaliaMarine/little-doc-editor/api/internal/config"
)

var (
	registry = prometheus.NewRegistry()

	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "lde_api_http_requests_total",
		Help: "HTTP requests handled, by method, matched route and status code.",
	}, []string{"method", "route", "status"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "lde_api_http_request_duration_seconds",
		Help:    "HTTP request latency by method and matched route.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})

	httpInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "lde_api_http_requests_in_flight",
		Help: "HTTP requests currently being served.",
	})

	registerOnce sync.Once
)

func init() {
	registerOnce.Do(func() {
		registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
			httpRequests, httpDuration, httpInFlight)
	})
}

// MetricsHandler serves the Prometheus exposition format.
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry})
}

// RegisterPool exposes pgxpool statistics. Call once per process.
func RegisterPool(pool *pgxpool.Pool) error {
	gauge := func(name, help string, f func(*pgxpool.Stat) float64) prometheus.Collector {
		return prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help}, func() float64 { return f(pool.Stat()) })
	}
	for _, c := range []prometheus.Collector{
		gauge("lde_api_db_pool_acquired_conns", "Connections currently in use.", func(s *pgxpool.Stat) float64 { return float64(s.AcquiredConns()) }),
		gauge("lde_api_db_pool_idle_conns", "Idle connections.", func(s *pgxpool.Stat) float64 { return float64(s.IdleConns()) }),
		gauge("lde_api_db_pool_total_conns", "Total connections.", func(s *pgxpool.Stat) float64 { return float64(s.TotalConns()) }),
	} {
		if err := registry.Register(c); err != nil {
			return err
		}
	}
	return nil
}

// Middleware records metrics and a server span for every request. It must
// be installed outermost so it sees the final status.
func Middleware(redact func(*http.Request) string) func(http.Handler) http.Handler {
	tracer := otel.Tracer("little-doc-editor/api")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			httpInFlight.Inc()
			defer httpInFlight.Dec()

			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, span := tracer.Start(ctx, r.Method, trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()

			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r.WithContext(ctx))

			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			httpRequests.WithLabelValues(r.Method, route, fmt.Sprint(status)).Inc()
			httpDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())

			span.SetName(r.Method + " " + route)
			span.SetAttributes(
				attribute.String("http.request.method", r.Method),
				attribute.String("http.route", route),
				attribute.String("url.path", redact(r)),
				attribute.Int("http.response.status_code", status),
			)
			if status >= 500 {
				span.SetStatus(codes.Error, http.StatusText(status))
			}
		})
	}
}

// Init configures tracing. With no OTLP endpoint it does nothing (the no-op
// tracer stays in place, so Middleware costs almost nothing). The exporter
// reads OTEL_EXPORTER_OTLP_{ENDPOINT,HEADERS,TIMEOUT,INSECURE...} itself, so
// TLS/auth settings use the standard OTel environment variables.
func Init(ctx context.Context, cfg config.Config) (shutdown func(context.Context) error, err error) {
	if strings.TrimSpace(cfg.OTelEndpoint) == "" {
		return func(context.Context) error { return nil }, nil
	}
	var exp sdktrace.SpanExporter
	switch cfg.OTelProtocol {
	case "grpc":
		exp, err = otlptracegrpc.New(ctx)
	default:
		exp, err = otlptracehttp.New(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
	}
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithAttributes(attribute.String("service.name", "little-doc-editor-api")),
	)
	if err != nil {
		return nil, err
	}
	// WithFromEnv is applied first, so OTEL_SERVICE_NAME still wins via merge below.
	res2, _ := resource.New(ctx, resource.WithFromEnv())
	if merged, err := resource.Merge(res, res2); err == nil {
		res = merged
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return tp.Shutdown, nil
}
