// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// telemetryServiceName is the default service.name reported on all spans when
// the operator has not set OTEL_SERVICE_NAME. It identifies tsdproxy in
// backends like OpenObserve/Jaeger/Tempo instead of the OTel SDK's generic
// "unknown_service:go" default.
const telemetryServiceName = "tsdproxy"

// processInstanceID is generated once per process so every span a single
// tsdproxy instance emits shares the same service.instance.id. This lets
// dashboards distinguish restarts/scale-out and group spans by process.
var processInstanceID = uuid.NewString()

// NewPropagator returns the W3C TraceContext + Baggage composite propagator
// tsdproxy uses for trace-context injection. Exposed so callers (including
// tests) can pass it explicitly to otelhttp.WithPropagators rather than
// depending on the global.
func NewPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

// InitTracer creates and registers a global OpenTelemetry tracer provider.
// It returns the provider AND the propagator it installed, so callers can pass
// the propagator explicitly to otelhttp (WithPropagators) instead of relying
// on the global. Relying on the global is fragile: any later SetTextMapPropagator
// call (e.g. from an imported library or a stray test) would silently break W3C
// trace-context propagation to upstreams. Threading it explicitly makes the
// contract local and verifiable.
//
// The provider must be shut down on application exit.
func InitTracer(ctx context.Context, endpoint string, insecure bool) (*sdktrace.TracerProvider, propagation.TextMapPropagator, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}
	if insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating OTLP exporter: %w", err)
	}

	res, err := buildResource(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("error building OTel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	// Set globals for back-compat with any code that reads them directly, but
	// first-party instrumentation should prefer the returned values.
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(prop)

	return tp, prop, nil
}

// buildResource assembles the Resource attached to every span this process
// emits: service identity, version, a per-process instance id, host name and
// the SDK description.
//
// Detector order matters: resource.New merges detectors last-value-wins, so
// WithFromEnv() is applied LAST. That lets an operator override any of these
// defaults (most importantly service.name) via OTEL_SERVICE_NAME /
// OTEL_RESOURCE_ATTRIBUTES without rebuilding the binary.
func buildResource(ctx context.Context) (*resource.Resource, error) {
	defaults := []resource.Option{
		// Code-provided defaults — lowest precedence.
		resource.WithAttributes(
			semconv.ServiceName(telemetryServiceName),
			semconv.ServiceVersion(GetVersion()),
			semconv.ServiceInstanceID(processInstanceID),
		),
		// Auto-detected attributes: host.name, host.arch, host.os.*,
		// process.*, telemetry.sdk.* — enrich spans without extra config.
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcessPID(),
		resource.WithProcessExecutableName(),
		resource.WithProcessRuntimeName(),
		resource.WithProcessRuntimeVersion(),
		resource.WithTelemetrySDK(),
		// Env vars (OTEL_SERVICE_NAME / OTEL_RESOURCE_ATTRIBUTES) applied last
		// so they override the code defaults above.
		resource.WithFromEnv(),
	}
	return resource.New(ctx, defaults...)
}
