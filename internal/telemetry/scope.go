package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/kubewarden/sbomscanner/internal/version"
)

// moduleRoot is the Go module path of this repository.
// It is the prefix added to every instrumentation scope,
// so the wire format stays spec-conformant (full import path) even though call sites pass a short, repo-relative name.
const moduleRoot = "github.com/kubewarden/sbomscanner"

// Tracer returns a Tracer scoped to moduleRoot + "/" + pkgPath.
// pkgPath is the repo-relative directory of the calling package, e.g. "internal/handlers".
// Pass "" to scope at the module root.
//
// The OTel instrumentation-version attribute is set from internal/version.Version on every Tracer returned,
// so each span carries the binary's build version.
//
// Convention: do NOT cache the returned Tracer in a package-level variable.
// Either call telemetry.Tracer(...) inline at the span start,
// or take a trace.Tracer via constructor injection.
func Tracer(pkgPath string) trace.Tracer {
	return otel.Tracer(scopeName(pkgPath), trace.WithInstrumentationVersion(version.Version))
}

// Meter returns a Meter scoped the same way Tracer is.
// The same "no globals" convention applies.
func Meter(pkgPath string) metric.Meter {
	return otel.Meter(scopeName(pkgPath), metric.WithInstrumentationVersion(version.Version))
}

func scopeName(pkgPath string) string {
	if pkgPath == "" {
		return moduleRoot
	}
	return moduleRoot + "/" + pkgPath
}
