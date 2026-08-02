/*
Copyright 2026 OSS Container Tools

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package tracing exports kaniko build traces over OTLP, opt-in via
// KANIKO_TELEMETRY_ENDPOINT.
package tracing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/osscontainertools/kaniko/pkg/assert"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/timing"
	"github.com/osscontainertools/kaniko/pkg/version"
	"github.com/sirupsen/logrus"
)

var (
	mu       sync.Mutex
	provider *sdktrace.TracerProvider
	rootSpan trace.Span
)

// enables tracing when set to an OTLP-HTTP collector URL.
const EndpointEnv = "KANIKO_TELEMETRY_ENDPOINT"

// whether to keep the Dockerfile source out of the trace.
const OmitDockerfileEnv = "KANIKO_TELEMETRY_OMIT_DOCKERFILE"

const shutdownFlushTimeout = 5 * time.Second

// one oversized value gets the whole OTLP batch rejected, not just that attribute.
const attributeValueLengthLimit = 64 * 1024

// pass these raw, WithSpanLimits would clamp an explicit -1 (unlimited) to the default.
func spanLimits() sdktrace.SpanLimits {
	limits := sdktrace.NewSpanLimits()
	_, spanSet := os.LookupEnv("OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT")
	_, generalSet := os.LookupEnv("OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT")
	if !spanSet && !generalSet {
		limits.AttributeValueLengthLimit = attributeValueLengthLimit
	}
	return limits
}

func Init(ctx context.Context, opts *config.KanikoOptions) {
	endpoint := os.Getenv(EndpointEnv)
	if endpoint == "" {
		return
	}
	if strings.HasPrefix(endpoint, "http://") {
		logrus.Warnf("%s uses plaintext http: spans (including Dockerfile content) are sent unencrypted", EndpointEnv)
	}
	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		logrus.Debugf("tracing disabled: failed to create OTLP exporter: %v", err)
		return
	}
	// Read the Dockerfile once: reused for build_id and the content attribute.
	content, cerr := os.ReadFile(opts.DockerfilePath)
	if cerr != nil {
		logrus.Debugf("tracing: Dockerfile not readable, kaniko.dockerfile.content omitted: %v", cerr)
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(buildAttrs(opts, content)...),
		resource.WithFromEnv(),
	)
	if err != nil {
		logrus.Debugf("tracing: partial resource, continuing: %v", err)
	}

	// Deliberately NOT otel.SetTracerProvider: kaniko takes its tracer from
	// the provider directly, and the global would silently switch on client
	// spans in the vendored GCS/GCR transports, polluting the trace.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithRawSpanLimits(spanLimits()),
	)

	tracer := tp.Tracer("github.com/osscontainertools/kaniko")
	sctx, span := tracer.Start(ctx, "build")
	raw, set := os.LookupEnv(OmitDockerfileEnv)
	if set {
		_, perr := strconv.ParseBool(raw)
		if perr != nil {
			logrus.Warnf("%s=%q is not a valid boolean; Dockerfile content WILL be exported", OmitDockerfileEnv, raw)
		}
	}
	if cerr == nil && !config.EnvBool(OmitDockerfileEnv) {
		span.SetAttributes(attribute.String("kaniko.dockerfile.content", string(content)))
	}

	mu.Lock()
	provider, rootSpan = tp, span
	mu.Unlock()

	timing.SetTracer(sctx, tracer)

	// hook, not import, so assert does not depend on tracing
	assert.OnAssertionViolation = onAssertion
	logrus.RegisterExitHandler(func() { Shutdown(fmt.Errorf("process exited via logrus.Fatal")) })
}

// onAssertion flushes before the panic from a violated assertion escapes.
func onAssertion(name, msg string) {
	mu.Lock()
	if rootSpan != nil {
		rootSpan.SetAttributes(attribute.Bool("kaniko.assertion_violated", true))
		rootSpan.AddEvent("assertion violated", trace.WithAttributes(
			attribute.String("kaniko.assertion.name", name),
			attribute.String("kaniko.assertion.message", msg),
		))
	}
	mu.Unlock()
	Shutdown(fmt.Errorf("assertion violated [%s]: %s", name, msg))
}

// buildAttrs holds what kaniko knows; fleet identity comes from
// OTEL_RESOURCE_ATTRIBUTES. build_id groups runs of the same Dockerfile+target.
func buildAttrs(opts *config.KanikoOptions, dockerfile []byte) []attribute.KeyValue {
	target := strings.Join(opts.Target, ",")
	attrs := []attribute.KeyValue{
		semconv.ServiceName("kaniko"),
		attribute.String("kaniko.version", version.Version()),
		attribute.String("kaniko.dockerfile", opts.DockerfilePath),
		attribute.String("kaniko.target", target),
		attribute.String("kaniko.build_id", buildID(opts.DockerfilePath, target, dockerfile)),
	}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "FF_KANIKO_") {
			continue
		}
		k, v, ok := strings.Cut(e, "=")
		if ok {
			attrs = append(attrs, attribute.String("kaniko.ff."+strings.TrimPrefix(k, "FF_KANIKO_"), v))
		}
	}
	return attrs
}

// buildID groups runs building the same Dockerfile+target. Content-addressed
// when the Dockerfile is readable; the path fallback is near-constant across
// a fleet (everything mounts /workspace/Dockerfile), hence the preference.
func buildID(path, target string, content []byte) string {
	src := path
	if content != nil {
		src = string(content)
	}
	sum := sha256.Sum256([]byte(src + "|" + target))
	return hex.EncodeToString(sum[:])[:16]
}

// Shutdown ends the root span with the outcome and flushes. Idempotent. A
// killed process leaves the root span unended, which the backend marks crashed.
func Shutdown(err error) {
	mu.Lock()
	defer mu.Unlock()
	if provider == nil {
		return
	}
	if rootSpan != nil {
		if err != nil {
			rootSpan.SetStatus(codes.Error, err.Error())
		} else {
			rootSpan.SetStatus(codes.Ok, "")
		}
		rootSpan.End()
		rootSpan = nil
	}
	timing.SetTracer(context.Background(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
	defer cancel()
	if sderr := provider.Shutdown(ctx); sderr != nil {
		logrus.Debugf("tracing: shutdown flush failed: %v", sderr)
	}
	provider = nil
}
