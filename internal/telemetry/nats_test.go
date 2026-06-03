package telemetry

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// TestNATSCarrier_RoundTrip injects a context into NATS headers and extracts it back,
// asserting the same trace context is recovered.
// This is the exact path used by NatsPublisher / NatsSubscriber in Task 1.
func TestNATSCarrier_RoundTrip(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := tp.Tracer("test").Start(context.Background(), "produce")
	defer span.End()
	want := span.SpanContext()

	msg := &nats.Msg{Subject: "sbomscanner.test"}
	InjectNATS(ctx, msg)

	require.NotEmpty(t, msg.Header.Get("traceparent"),
		"traceparent header must be set by InjectNATS")

	got := ExtractNATS(context.Background(), msg.Header)
	gotSC := trace.SpanContextFromContext(got)

	assert.Equal(t, want.TraceID(), gotSC.TraceID())
	assert.Equal(t, want.SpanID(), gotSC.SpanID())
}

// TestInjectNATS_NilMsg is a no-op,
// so callers don't have to guard.
func TestInjectNATS_NilMsg(t *testing.T) {
	assert.NotPanics(t, func() {
		InjectNATS(context.Background(), nil)
	})
}

// TestInjectNATS_CreatesHeader lazily allocates msg.Header when it is nil,
// instead of panicking on map assignment.
func TestInjectNATS_CreatesHeader(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	msg := &nats.Msg{Subject: "sbomscanner.test"} // Header is nil
	InjectNATS(ctx, msg)

	require.NotNil(t, msg.Header)
	assert.NotEmpty(t, msg.Header.Get("traceparent"))
}

// TestExtractNATS_NilHeader returns the parent context unchanged,
// so consumers can call it without pre-checking.
func TestExtractNATS_NilHeader(t *testing.T) {
	parent := context.Background()
	got := ExtractNATS(parent, nil)
	assert.Equal(t, parent, got)
}

// TestExtractNATS_NoTraceparent returns a context with an invalid SpanContext,
// when the header has no propagation keys.
func TestExtractNATS_NoTraceparent(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	hdr := nats.Header{}
	hdr.Set("Nats-Msg-Id", "abc")

	got := ExtractNATS(context.Background(), hdr)
	assert.False(t, trace.SpanContextFromContext(got).IsValid())
}

// TestNATSHeaderCarrier_Keys returns every header key,
// matching the TextMapCarrier contract.
func TestNATSHeaderCarrier_Keys(t *testing.T) {
	hdr := nats.Header{}
	hdr.Set("traceparent", "00-...")
	hdr.Set("Nats-Msg-Id", "id")

	keys := natsHeaderCarrier(hdr).Keys()
	assert.ElementsMatch(t, []string{"traceparent", "Nats-Msg-Id"}, keys)
}

// TestNATSHeaderCarrier_GetMissing returns an empty string for absent keys,
// per the TextMapCarrier contract.
func TestNATSHeaderCarrier_GetMissing(t *testing.T) {
	assert.Equal(t, "", natsHeaderCarrier(nats.Header{}).Get("traceparent"))
}
