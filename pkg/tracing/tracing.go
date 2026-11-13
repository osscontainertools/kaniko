//go:build otel

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

package tracing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
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
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/osscontainertools/kaniko/pkg/assert"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/connstats"
	"github.com/osscontainertools/kaniko/pkg/timing"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/osscontainertools/kaniko/pkg/version"
	"github.com/sirupsen/logrus"
)

var (
	mu       sync.Mutex
	provider *sdktrace.TracerProvider
	rootSpan trace.Span
	deferred *deferredProcessor
	opts     *config.KanikoOptions
	exported chan struct{}
)

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

// otlptracehttp >= 1.45 no longer appends the OTLP signal path itself.
func tracesEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	if u.Path != "" && u.Path != "/" {
		return endpoint
	}
	return u.JoinPath("/v1/traces").String()
}

func Init(ctx context.Context, o *config.KanikoOptions) {
	endpoint := os.Getenv(EndpointEnv)
	if endpoint == "" {
		return
	}
	if strings.HasPrefix(endpoint, "http://") {
		logrus.Warnf("%s uses plaintext http: spans (including Dockerfile content) are sent unencrypted", EndpointEnv)
	}
	dp := &deferredProcessor{}
	// Deliberately NOT otel.SetTracerProvider: kaniko takes its tracer from
	// the provider directly, and the global would silently switch on client
	// spans in the vendored GCS/GCR transports, polluting the trace.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(dp),
		sdktrace.WithRawSpanLimits(spanLimits()),
	)
	tracer := tp.Tracer("github.com/osscontainertools/kaniko")
	sctx, span := tracer.Start(ctx, "build")

	mu.Lock()
	provider, rootSpan, deferred, opts = tp, span, dp, o
	mu.Unlock()

	timing.SetTracer(sctx, tracer)

	// hook, not import, so assert does not depend on tracing
	assert.OnAssertionViolation = onAssertion
	logrus.RegisterExitHandler(func() { Shutdown(fmt.Errorf("process exited via logrus.Fatal")) })
}

func Export(ctx context.Context) {
	mu.Lock()
	defer mu.Unlock()
	exportLocked(ctx)
}

func exportLocked(ctx context.Context) {
	endpoint := os.Getenv(EndpointEnv)
	dp := deferred
	if dp == nil {
		return
	}
	deferred = nil
	// Read the Dockerfile once: reused for build_id and the content attribute.
	content, cerr := os.ReadFile(opts.DockerfilePath)
	if cerr != nil {
		logrus.Debugf("tracing: Dockerfile not readable, kaniko.dockerfile.content omitted: %v", cerr)
	}

	span := rootSpan
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
	ignorePath := util.DockerignorePath(opts.DockerfilePath, opts.SrcContext)
	if ignorePath != "" {
		ignoreContent, ierr := os.ReadFile(ignorePath)
		if ierr != nil {
			logrus.Debugf("tracing: .dockerignore not readable, kaniko.dockerignore.content omitted: %v", ierr)
		} else {
			span.SetAttributes(attribute.String("kaniko.dockerignore.content", string(ignoreContent)))
		}
	}

	envHeaders, req := exportAuth()
	o := *opts
	done := make(chan struct{})
	exported = done
	go func() {
		defer close(done)
		loginStart := time.Now()
		headers, auth := exportHeaders(ctx, envHeaders, req)
		loginEnd := time.Now()
		span.SetAttributes(attribute.String("kaniko.telemetry.auth", auth))
		if auth != "env" {
			timing.RecordChild(span, "Exchanging telemetry token", loginStart, loginEnd)
		}
		exp, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(tracesEndpoint(endpoint)),
			otlptracehttp.WithHeaders(headers),
		)
		if err != nil {
			logrus.Debugf("tracing disabled: failed to create OTLP exporter: %v", err)
			dp.drop()
			return
		}
		res, err := buildResource(ctx, &o, content)
		if err != nil {
			logrus.Debugf("tracing: partial resource, continuing: %v", err)
		}
		dp.attach(sdktrace.NewBatchSpanProcessor(exp), res)
	}()
}

// The provider's resource is fixed before the build is known, so every span is
// handed on with the resource Export resolved.
type deferredProcessor struct {
	mu      sync.Mutex
	next    sdktrace.SpanProcessor
	res     *resource.Resource
	held    []sdktrace.ReadOnlySpan
	dropped bool
}

type withResource struct {
	sdktrace.ReadOnlySpan
	res *resource.Resource
}

func (s withResource) Resource() *resource.Resource { return s.res }

func (p *deferredProcessor) attach(next sdktrace.SpanProcessor, res *resource.Resource) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next, p.res = next, res
	for _, s := range p.held {
		next.OnEnd(withResource{s, res})
	}
	p.held = nil
}

func (p *deferredProcessor) drop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.held, p.dropped = nil, true
}

func (p *deferredProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}

func (p *deferredProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.next != nil:
		p.next.OnEnd(withResource{s, p.res})
	case !p.dropped:
		p.held = append(p.held, s)
	}
}

func (p *deferredProcessor) downstream() sdktrace.SpanProcessor {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.next
}

func (p *deferredProcessor) Shutdown(ctx context.Context) error {
	next := p.downstream()
	if next == nil {
		return nil
	}
	return next.Shutdown(ctx)
}

func (p *deferredProcessor) ForceFlush(ctx context.Context) error {
	next := p.downstream()
	if next == nil {
		return nil
	}
	return next.ForceFlush(ctx)
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

func registryAttrs(s connstats.Stats) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int64("kaniko.registry.sockets.opened", s.SocketsOpened),
		attribute.Int64("kaniko.registry.sockets.closed", s.SocketsClosed),
		attribute.Int64("kaniko.registry.sockets.open_at_exit", s.SocketsOpen),
		attribute.Int64("kaniko.registry.sockets.peak", s.PeakSockets),
		attribute.Int64("kaniko.registry.requests", s.Requests),
		attribute.Int64("kaniko.registry.requests.reused", s.Reused),
		attribute.Int64("kaniko.registry.tls.handshakes", s.TLSHandshakes),
		attribute.Int64("kaniko.registry.tls.ms", s.TLSTime.Milliseconds()),
		attribute.Int64("kaniko.registry.dial.ms", s.DialTime.Milliseconds()),
		attribute.Int64("kaniko.registry.idle.ms", s.IdleTime.Milliseconds()),
	}
}

func SetPlan(plan string) {
	mu.Lock()
	defer mu.Unlock()
	if rootSpan != nil {
		rootSpan.SetAttributes(attribute.String("kaniko.plan", plan))
	}
}

// buildResource is what every span carries. WithFromEnv comes last so
// OTEL_RESOURCE_ATTRIBUTES overrides both what kaniko knows and what it read
// off the CI system.
func buildResource(ctx context.Context, opts *config.KanikoOptions, content []byte) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(buildAttrs(opts, content)...),
		resource.WithAttributes(ciAttrs()...),
		resource.WithFromEnv(),
	)
}

// buildAttrs holds what kaniko knows; the CI system contributes the rest
// through ciAttrs. build_id groups runs of the same build.
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

// buildID groups runs of the same build. The CI job is the best answer where
// there is one: it survives edits to the Dockerfile, which the content hash
// does not. Content-addressed otherwise, the path fallback being near-constant
// across a fleet (everything mounts /workspace/Dockerfile).
func buildID(path, target string, content []byte) string {
	id := os.Getenv(BuildIDEnv)
	if id != "" {
		return id
	}
	id = ciBuildID(path, target)
	if id != "" {
		return id
	}

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
	exportCtx, cancelExport := context.WithTimeout(context.Background(), shutdownFlushTimeout)
	defer cancelExport()
	exportLocked(exportCtx)
	if exported != nil {
		<-exported
	}
	if rootSpan != nil {
		rootSpan.SetAttributes(registryAttrs(connstats.Snapshot())...)
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
