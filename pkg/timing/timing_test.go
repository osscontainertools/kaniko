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
	"testing"

	"go.opentelemetry.io/otel/trace/noop"
)

// Regression for the SetTracer/Start data race: cache-push goroutines call
// Start while the shutdown path unwires the tracer. Run under -race.
func TestSetTracerConcurrentWithStart(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			Start("race-probe").End()
		}
	}()
	tr := noop.NewTracerProvider().Tracer("test")
	for range 1000 {
		SetTracer(context.Background(), tr)
		SetTracer(context.Background(), nil)
	}
	<-done
}
