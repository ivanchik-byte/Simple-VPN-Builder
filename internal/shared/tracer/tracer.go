package tracer

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// InitTracer initializes a standard OpenTelemetry TracerProvider with head-based sampling
// and registers the W3C TraceContext propagator globally.
func InitTracer(serviceName string, sampleRatio float64) (*sdktrace.TracerProvider, error) {
	if sampleRatio <= 0 {
		sampleRatio = 0.05 // default 5% head sampling to eliminate performance/memory overhead
	} else if sampleRatio > 1.0 {
		sampleRatio = 1.0
	}

	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp, nil
}

// ShutdownTracer shuts down the TracerProvider gracefully.
func ShutdownTracer(ctx context.Context, tp *sdktrace.TracerProvider) error {
	if tp == nil {
		return nil
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return tp.Shutdown(ctxTimeout)
}
