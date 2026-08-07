/*
Copyright 2018 Google LLC

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

package timing

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

var (
	tracerMu  sync.Mutex
	tracer    trace.Tracer
	parentCtx context.Context
)

func SetTracer(ctx context.Context, t trace.Tracer) {
	tracerMu.Lock()
	defer tracerMu.Unlock()
	parentCtx = ctx
	tracer = t
}

func TracingEnabled() bool {
	tracerMu.Lock()
	defer tracerMu.Unlock()
	return tracer != nil
}

var noSpanCategories = map[string]bool{
	"Hashing files":                   true,
	"Walking filesystem with timeout": true,
	"Walking filesystem with Stat":    true,
	"Resolving Paths":                 true,
	"Writing tar file":                true,
}

var networkCategories = map[string]bool{
	"Retrieving Source Image": true,
	"Fetching Extra Stages":   true,
	"Pushing cached layer":    true,
	"Pushing cache pointer":   true,
	"Total Push Time":         true,
}

func phaseFor(category string) string {
	if networkCategories[category] {
		return "network"
	}
	return "kaniko"
}

// Start begins a span for category, or a noop span when the category is not traced.
func Start(category string) trace.Span {
	return start(nil, category)
}

// StartChild begins a span for category nested under parent.
func StartChild(parent trace.Span, category string) trace.Span {
	return start(parent, category)
}

func start(parent trace.Span, category string) trace.Span {
	tracerMu.Lock()
	tr, ctx := tracer, parentCtx
	tracerMu.Unlock()
	if tr == nil || noSpanCategories[category] {
		return noop.Span{}
	}
	if parent != nil && parent.SpanContext().IsValid() {
		ctx = trace.ContextWithSpan(ctx, parent)
	}
	_, span := tr.Start(ctx, category)
	span.SetAttributes(attribute.String("kaniko.phase", phaseFor(category)))
	return span
}
