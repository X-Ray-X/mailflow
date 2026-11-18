// Package observability owns the service's metrics registry and optional trace
// export. It is the only place that knows Prometheus or OpenTelemetry types, so
// neither can leak into the published contract.
package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
)

const exporterTimeout = 5 * time.Second

// Setup carries the pieces the HTTP server needs. Metrics is served without
// authentication on the loopback listener; Wrap instruments protected routes.
type Setup struct {
	Metrics  http.Handler
	Wrap     func(http.Handler) http.Handler
	Shutdown func(context.Context) error
}

// New builds a dedicated Prometheus registry and instruments HTTP handlers.
// An empty OTLP endpoint leaves tracing local; a configured one exports traces
// with bounded timeouts without making request handling depend on the collector.
func New(ctx context.Context, otlpEndpoint string, log *slog.Logger) (*Setup, error) {
	if log == nil {
		log = slog.Default()
	}
	registry := prometheus.NewRegistry()
	reader, err := promexporter.New(promexporter.WithRegisterer(registry))
	if err != nil {
		return nil, fmt.Errorf("prometheus exporter: %w", err)
	}
	meterProvider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(meterProvider)

	shutdown := func(shutdownCtx context.Context) error { return meterProvider.Shutdown(shutdownCtx) }
	if otlpEndpoint != "" {
		exporter, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(otlpEndpoint),
			otlptracehttp.WithTimeout(exporterTimeout),
		)
		if err != nil {
			_ = meterProvider.Shutdown(ctx)
			return nil, fmt.Errorf("otlp exporter: %w", err)
		}
		tracerProvider := trace.NewTracerProvider(trace.WithBatcher(exporter))
		otel.SetTracerProvider(tracerProvider)
		previous := shutdown
		shutdown = func(shutdownCtx context.Context) error {
			err := tracerProvider.Shutdown(shutdownCtx)
			if prevErr := previous(shutdownCtx); err == nil {
				err = prevErr
			}
			return err
		}
		log.Info("trace export enabled", "endpoint", otlpEndpoint)
	}

	return &Setup{
		Metrics: promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		Wrap: func(next http.Handler) http.Handler {
			return otelhttp.NewHandler(next, "mailflow-api",
				otelhttp.WithSpanNameFormatter(spanName))
		},
		Shutdown: shutdown,
	}, nil
}

// spanName uses only the matched route pattern, never the raw URL, which can
// carry a message identifier or an address.
func spanName(_ string, r *http.Request) string {
	pattern := r.Pattern
	if pattern == "" {
		pattern = "unmatched"
	}
	return r.Method + " " + pattern
}
