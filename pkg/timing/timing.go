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
	"encoding/json"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// For testing
var currentTimeFunc = time.Now

// DefaultRun is the default "singleton" TimedRun instance.
var DefaultRun = NewTimedRun()

var (
	tracer    trace.Tracer
	parentCtx context.Context
)

// SetTracer wires (or, with a nil tracer, unwires) span creation into every
// subsequent Start; ctx carries the parent span. Called by pkg/tracing.
func SetTracer(ctx context.Context, t trace.Tracer) {
	parentCtx = ctx
	tracer = t
}

// TracingEnabled reports whether a tracer is installed, i.e. whether timers
// currently mint spans. Timing itself is always on.
func TracingEnabled() bool {
	return tracer != nil
}

// TimedRun provides a running store of how long is spent in each category.
type TimedRun struct {
	cl         sync.Mutex
	categories map[string]time.Duration // protected by cl
}

// Stop stops the specified timer and increments the time spent in that category.
func (tr *TimedRun) Stop(t *Timer) {
	stop := currentTimeFunc()
	tr.cl.Lock()
	if _, ok := tr.categories[t.category]; !ok {
		tr.categories[t.category] = 0
	}
	tr.categories[t.category] += stop.Sub(t.startTime)
	tr.cl.Unlock()
	if t.span != nil {
		t.span.End()
	}
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

// Start starts a new Timer and returns it.
func Start(category string) *Timer {
	t := Timer{
		category:  category,
		startTime: currentTimeFunc(),
	}
	if tracer != nil && !noSpanCategories[category] {
		// Span names must stay low-cardinality (backends aggregate on them);
		// the full command text lives in the kaniko.command attribute. The
		// category keeps the full text — it is also the BENCHMARK_FILE key.
		name := category
		if strings.HasPrefix(category, "Command: ") {
			name = "Command"
		}
		_, t.span = tracer.Start(parentCtx, name)
		t.span.SetAttributes(attribute.String("kaniko.phase", phaseFor(category)))
	}
	return &t
}

// NewTimedRun returns an initialized TimedRun instance.
func NewTimedRun() *TimedRun {
	tr := TimedRun{
		categories: map[string]time.Duration{},
	}
	return &tr
}

// Timer represents a running timer.
type Timer struct {
	category  string
	startTime time.Time
	span      trace.Span
}

// SetAttributes forwards attributes to the timer's span; no-op when tracing
// is off (the span is nil).
func (t *Timer) SetAttributes(kv ...attribute.KeyValue) {
	if t.span != nil {
		t.span.SetAttributes(kv...)
	}
}

func JSON() (string, error) {
	return DefaultRun.JSON()
}

func (tr *TimedRun) JSON() (string, error) {
	b, err := json.Marshal(tr.categories)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
