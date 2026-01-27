package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/splunk/splunk-operator/e2e/framework/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Telemetry wraps OTel tracer and meter plus shared metrics instruments.
type Telemetry struct {
	enabled      bool
	tracer       trace.Tracer
	meter        metric.Meter
	testCounter  metric.Int64Counter
	testDuration metric.Float64Histogram
	stepCounter  metric.Int64Counter
	stepDuration metric.Float64Histogram
}

// Init configures OpenTelemetry exporters and providers.
func Init(ctx context.Context, cfg *config.Config, logger *zap.Logger) (*Telemetry, func(context.Context) error, error) {
	enabled := cfg.OTelEnabled || strings.TrimSpace(cfg.OTelEndpoint) != ""
	if !enabled {
		return &Telemetry{enabled: false}, func(context.Context) error { return nil }, nil
	}
	if strings.TrimSpace(cfg.OTelEndpoint) == "" {
		return nil, nil, fmt.Errorf("otel endpoint required when telemetry is enabled")
	}

	headers := parseKeyValueList(cfg.OTelHeaders)
	metricExporter, err := newMetricExporter(ctx, cfg, headers)
	if err != nil {
		return nil, nil, err
	}
	traceExporter, err := newTraceExporter(ctx, cfg, headers)
	if err != nil {
		return nil, nil, err
	}

	resAttrs := buildResourceAttributes(cfg)
	res, err := resource.New(ctx, resource.WithFromEnv(), resource.WithAttributes(resAttrs...))
	if err != nil {
		return nil, nil, err
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExporter),
	)
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)

	tracer := otel.Tracer("splunk-operator-e2e")
	meter := otel.Meter("splunk-operator-e2e")

	testCounter, _ := meter.Int64Counter("e2e_tests_total")
	testDuration, _ := meter.Float64Histogram("e2e_test_duration_seconds", metric.WithUnit("s"))
	stepCounter, _ := meter.Int64Counter("e2e_steps_total")
	stepDuration, _ := meter.Float64Histogram("e2e_step_duration_seconds", metric.WithUnit("s"))

	shutdown := func(ctx context.Context) error {
		var shutdownErr error
		if err := tracerProvider.Shutdown(ctx); err != nil {
			shutdownErr = err
		}
		if err := meterProvider.Shutdown(ctx); err != nil {
			if shutdownErr != nil {
				shutdownErr = errors.Join(shutdownErr, err)
			} else {
				shutdownErr = err
			}
		}
		return shutdownErr
	}

	logger.Info("otel enabled", zap.String("endpoint", cfg.OTelEndpoint))
	return &Telemetry{
		enabled:      true,
		tracer:       tracer,
		meter:        meter,
		testCounter:  testCounter,
		testDuration: testDuration,
		stepCounter:  stepCounter,
		stepDuration: stepDuration,
	}, shutdown, nil
}

// Enabled reports whether telemetry is active.
func (t *Telemetry) Enabled() bool {
	return t != nil && t.enabled
}

// StartSpan starts a new span with string attributes.
func (t *Telemetry) StartSpan(ctx context.Context, name string, attrs map[string]string) (context.Context, trace.Span) {
	if !t.Enabled() {
		return ctx, nil
	}
	return t.tracer.Start(ctx, name, trace.WithAttributes(toAttributes(attrs)...))
}

// MarkSpan sets span status, records errors, and adds attributes.
func (t *Telemetry) MarkSpan(span trace.Span, status string, err error, attrs map[string]string) {
	if !t.Enabled() || span == nil {
		return
	}
	if len(attrs) > 0 {
		span.SetAttributes(toAttributes(attrs)...)
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, status)
	}
}

// RecordTest records metrics for a test.
func (t *Telemetry) RecordTest(status string, duration time.Duration, attrs map[string]string) {
	if !t.Enabled() {
		return
	}
	baseAttrs := map[string]string{
		"status": status,
	}
	for key, value := range attrs {
		baseAttrs[key] = value
	}
	kvs := toAttributes(baseAttrs)
	t.testCounter.Add(context.Background(), 1, metric.WithAttributes(kvs...))
	t.testDuration.Record(context.Background(), duration.Seconds(), metric.WithAttributes(kvs...))
}

// RecordStep records metrics for a step.
func (t *Telemetry) RecordStep(status string, duration time.Duration, attrs map[string]string) {
	if !t.Enabled() {
		return
	}
	baseAttrs := map[string]string{
		"status": status,
	}
	for key, value := range attrs {
		baseAttrs[key] = value
	}
	kvs := toAttributes(baseAttrs)
	t.stepCounter.Add(context.Background(), 1, metric.WithAttributes(kvs...))
	t.stepDuration.Record(context.Background(), duration.Seconds(), metric.WithAttributes(kvs...))
}

func newMetricExporter(ctx context.Context, cfg *config.Config, headers map[string]string) (*otlpmetricgrpc.Exporter, error) {
	options := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.OTelEndpoint)}
	if cfg.OTelInsecure {
		options = append(options, otlpmetricgrpc.WithInsecure())
	}
	if len(headers) > 0 {
		options = append(options, otlpmetricgrpc.WithHeaders(headers))
	}
	return otlpmetricgrpc.New(ctx, options...)
}

func newTraceExporter(ctx context.Context, cfg *config.Config, headers map[string]string) (*otlptrace.Exporter, error) {
	options := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.OTelEndpoint)}
	if cfg.OTelInsecure {
		options = append(options, otlptracegrpc.WithInsecure())
	}
	if len(headers) > 0 {
		options = append(options, otlptracegrpc.WithHeaders(headers))
	}
	return otlptracegrpc.New(ctx, options...)
}

func buildResourceAttributes(cfg *config.Config) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String(defaultIfEmpty(cfg.OTelServiceName, "splunk-operator-e2e")),
		attribute.String("e2e.run_id", cfg.RunID),
		attribute.String("cluster.provider", cfg.ClusterProvider),
	}
	extra := parseKeyValueList(cfg.OTelResourceAttrs)
	for key, value := range extra {
		attrs = append(attrs, attribute.String(key, value))
	}
	return attrs
}

func parseKeyValueList(value string) map[string]string {
	out := make(map[string]string)
	parts := strings.Split(value, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if key == "" {
			continue
		}
		out[key] = val
	}
	return out
}

func toAttributes(attrs map[string]string) []attribute.KeyValue {
	if len(attrs) == 0 {
		return nil
	}
	kvs := make([]attribute.KeyValue, 0, len(attrs))
	for key, value := range attrs {
		kvs = append(kvs, attribute.String(key, value))
	}
	return kvs
}

func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
