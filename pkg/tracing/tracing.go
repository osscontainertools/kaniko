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
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/osscontainertools/kaniko/pkg/assert"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/timing"
	"github.com/osscontainertools/kaniko/pkg/version"
	"github.com/sirupsen/logrus"
)

var (
	provider *sdktrace.TracerProvider
	rootSpan trace.Span
)

const EndpointEnv = "KANIKO_TELEMETRY_ENDPOINT"
const shutdownFlushTimeout = 5 * time.Second

func Init(ctx context.Context, opts *config.KanikoOptions) {
	endpoint := os.Getenv(EndpointEnv)
	if endpoint == "" {
		return
	}
	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		logrus.Warnf("tracing disabled: failed to create OTLP exporter: %v", err)
		return
	}
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithAttributes(buildAttrs(opts)...),
	)
	if err != nil {
		logrus.Warnf("tracing: partial resource, continuing: %v", err)
	}
	provider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)

	tracer := provider.Tracer("github.com/osscontainertools/kaniko")
	var sctx context.Context
	sctx, rootSpan = tracer.Start(ctx, "build")
	if content, rerr := os.ReadFile(opts.DockerfilePath); rerr == nil {
		rootSpan.SetAttributes(attribute.String("kaniko.dockerfile.content", string(content)))
	}
	timing.SetTracer(sctx, tracer)

	// hook, not import, so assert does not depend on tracing
	assert.OnAssertionViolation = onAssertion
	logrus.RegisterExitHandler(func() { Shutdown(fmt.Errorf("process exited via logrus.Fatal")) })
}

// onAssertion flushes before the panic from a violated assertion escapes.
func onAssertion(name, msg string) {
	if rootSpan != nil {
		rootSpan.SetAttributes(attribute.Bool("kaniko.assertion_violated", true))
		rootSpan.AddEvent("assertion violated", trace.WithAttributes(
			attribute.String("kaniko.assertion.name", name),
			attribute.String("kaniko.assertion.message", msg),
		))
	}
	Shutdown(fmt.Errorf("assertion violated [%s]: %s", name, msg))
}

// buildAttrs holds what kaniko knows; fleet identity comes from
// OTEL_RESOURCE_ATTRIBUTES. build_id groups runs of the same Dockerfile+target.
func buildAttrs(opts *config.KanikoOptions) []attribute.KeyValue {
	target := strings.Join(opts.Target, ",")
	attrs := []attribute.KeyValue{
		attribute.String("service.name", "kaniko"),
		attribute.String("kaniko.version", version.Version()),
		attribute.String("kaniko.dockerfile", opts.DockerfilePath),
		attribute.String("kaniko.target", target),
		attribute.String("kaniko.build_id", buildID(opts.DockerfilePath, target)),
	}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "FF_KANIKO_") {
			continue
		}
		k, v, ok := strings.Cut(e, "=")
		if ok {
			attrs = append(attrs, attribute.String("kaniko.ff."+k, v))
		}
	}
	return attrs
}

func buildID(dockerfile, target string) string {
	sum := sha256.Sum256([]byte(dockerfile + "|" + target))
	return hex.EncodeToString(sum[:])[:16]
}

// Shutdown ends the root span with the outcome and flushes. Idempotent. A
// killed process leaves the root span unended, which the backend marks crashed.
func Shutdown(err error) {
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
	ctx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
	defer cancel()
	if sderr := provider.Shutdown(ctx); sderr != nil {
		logrus.Warnf("tracing: shutdown flush failed: %v", sderr)
	}
	provider = nil
}
