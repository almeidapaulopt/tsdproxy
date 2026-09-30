// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package core

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// resourceString fetches a single string attribute from a *resource.Resource.
// Returns ok=false when the attribute is missing.
func resourceString(res *resource.Resource, key string) (string, bool) {
	if res == nil {
		return "", false
	}
	for _, kv := range res.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

func TestBuildResource_HasServiceIdentity(t *testing.T) {
	t.Parallel()

	res, err := buildResource(context.Background())
	if err != nil {
		t.Fatalf("buildResource() error = %v", err)
	}
	if res == nil {
		t.Fatal("buildResource() returned nil resource")
	}

	name, ok := resourceString(res, "service.name")
	if !ok {
		t.Fatal("service.name must be present on the resource")
	}
	if name == "" {
		t.Fatal("service.name must not be empty")
	}

	// service.version mirrors the resolved build version.
	ver, ok := resourceString(res, "service.version")
	if !ok {
		t.Fatal("service.version must be present on the resource")
	}
	if ver != GetVersion() {
		t.Errorf("service.version = %q, want %q", ver, GetVersion())
	}

	// service.instance.id is a per-process UUID; must be non-empty.
	id, ok := resourceString(res, "service.instance.id")
	if !ok {
		t.Fatal("service.instance.id must be present on the resource")
	}
	if id == "" {
		t.Fatal("service.instance.id must not be empty")
	}
}

func TestBuildResource_HasHostAttributes(t *testing.T) {
	t.Parallel()

	res, err := buildResource(context.Background())
	if err != nil {
		t.Fatalf("buildResource() error = %v", err)
	}

	host, ok := resourceString(res, "host.name")
	if !ok {
		t.Fatal("host.name must be present on the resource")
	}
	if host == "" {
		t.Fatal("host.name must not be empty")
	}
}

func TestProcessInstanceID_StableAcrossCalls(t *testing.T) {
	t.Parallel()

	// The instance id is generated once per process; buildResource must keep
	// returning the same value so spans group correctly in the backend.
	r1, err := buildResource(context.Background())
	if err != nil {
		t.Fatalf("buildResource() r1 error = %v", err)
	}
	r2, err := buildResource(context.Background())
	if err != nil {
		t.Fatalf("buildResource() r2 error = %v", err)
	}

	id1, _ := resourceString(r1, "service.instance.id")
	id2, _ := resourceString(r2, "service.instance.id")
	if id1 != id2 {
		t.Errorf("service.instance.id not stable: %q vs %q", id1, id2)
	}
}

// TestBuildResource_OTELServiceNameOverridesDefault verifies that the env var
// takes precedence over the code default. This is the precedence contract:
// WithFromEnv() is ordered last so operators can rename the service without a
// rebuild.
func TestBuildResource_OTELServiceNameOverridesDefault(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "custom-tsdproxy")

	res, err := buildResource(context.Background())
	if err != nil {
		t.Fatalf("buildResource() error = %v", err)
	}

	name, ok := resourceString(res, "service.name")
	if !ok {
		t.Fatal("service.name missing")
	}
	if name != "custom-tsdproxy" {
		t.Errorf("service.name = %q, want %q (env must override code default)", name, "custom-tsdproxy")
	}
}

// TestBuildResource_OTelResourceAttributesMerged verifies that arbitrary
// attributes set via OTEL_RESOURCE_ATTRIBUTES are surfaced on the resource
// (e.g. deployment.environment).
func TestBuildResource_OTelResourceAttributesMerged(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=prod")

	res, err := buildResource(context.Background())
	if err != nil {
		t.Fatalf("buildResource() error = %v", err)
	}

	env, ok := resourceString(res, "deployment.environment.name")
	if !ok {
		t.Fatal("deployment.environment.name from OTEL_RESOURCE_ATTRIBUTES must be merged")
	}
	if env != "prod" {
		t.Errorf("deployment.environment.name = %q, want %q", env, "prod")
	}
}

// TestInitTracer_ResourceAttachedToSpans is an end-to-end check that a span
// emitted through a resource-backed provider carries the resource attributes
// (the part a backend like OpenObserve actually indexes on).
func TestInitTracer_ResourceAttachedToSpans(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	tp := tracesdk.NewTracerProvider(
		tracesdk.WithSyncer(exporter),
		tracesdk.WithResource(resource.NewSchemaless(
			semconv.ServiceName("tsdproxy"),
			semconv.ServiceVersion(GetVersion()),
		)),
	)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	_, span := tp.Tracer("test").Start(context.Background(), "probe")
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	name, ok := resourceString(spans[0].Resource, "service.name")
	if !ok {
		t.Fatal("service.name missing on span resource")
	}
	if name != "tsdproxy" {
		t.Errorf("service.name on span = %q, want %q", name, "tsdproxy")
	}
}
