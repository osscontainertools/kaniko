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
	"os"
	"testing"

	"github.com/osscontainertools/kaniko/pkg/config"
)

func TestSpanLimits(t *testing.T) {
	tests := []struct {
		name    string
		spanEnv string // "" = unset
		genEnv  string // "" = unset
		want    int
	}{
		{name: "no env applies kaniko cap", want: attributeValueLengthLimit},
		{name: "span env wins", spanEnv: "100", want: 100},
		{name: "general env wins", genEnv: "200", want: 200},
		{name: "explicit unlimited is honored", spanEnv: "-1", want: -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Start from a hermetic env: an inherited OTEL_* limit on the
			// runner would break the "unset" cases. t.Setenv registers the
			// restore; Unsetenv makes the var truly absent.
			for _, k := range []string{"OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT", "OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT"} {
				t.Setenv(k, "")
				os.Unsetenv(k)
			}
			if tc.spanEnv != "" {
				t.Setenv("OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT", tc.spanEnv)
			}
			if tc.genEnv != "" {
				t.Setenv("OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT", tc.genEnv)
			}
			if got := spanLimits().AttributeValueLengthLimit; got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// Pins the attribute-name contract dashboards are built on.
func TestBuildAttrs(t *testing.T) {
	t.Setenv("FF_KANIKO_TRACING_TEST_FLAG", "true")
	opts := &config.KanikoOptions{DockerfilePath: "/workspace/Dockerfile"}

	got := map[string]string{}
	for _, kv := range buildAttrs(opts, []byte("FROM scratch")) {
		got[string(kv.Key)] = kv.Value.AsString()
	}

	if got["service.name"] != "kaniko" {
		t.Errorf("service.name = %q, want kaniko", got["service.name"])
	}
	// FF keys must not double the prefix: kaniko.ff.TRACING_TEST_FLAG,
	// not kaniko.ff.FF_KANIKO_TRACING_TEST_FLAG.
	if got["kaniko.ff.TRACING_TEST_FLAG"] != "true" {
		t.Errorf("kaniko.ff.TRACING_TEST_FLAG = %q, want true", got["kaniko.ff.TRACING_TEST_FLAG"])
	}
	if _, dup := got["kaniko.ff.FF_KANIKO_TRACING_TEST_FLAG"]; dup {
		t.Error("FF key kept its FF_KANIKO_ prefix")
	}
	if got["kaniko.dockerfile"] != "/workspace/Dockerfile" {
		t.Errorf("kaniko.dockerfile = %q", got["kaniko.dockerfile"])
	}
	if got["kaniko.build_id"] == "" {
		t.Error("kaniko.build_id missing")
	}
}

func TestBuildID(t *testing.T) {
	content := []byte("FROM scratch\nRUN true\n")
	// Content-addressed: same content+target => same id, regardless of path.
	if buildID("/a/Dockerfile", "", content) != buildID("/b/Dockerfile", "", content) {
		t.Error("build_id must depend on content, not path, when content is available")
	}
	// Different content => different id.
	if buildID("/a/Dockerfile", "", content) == buildID("/a/Dockerfile", "", []byte("FROM busybox\n")) {
		t.Error("build_id must change with content")
	}
	// Fallback: no content => path-based, distinct from the content id.
	if buildID("/a/Dockerfile", "", nil) == buildID("/a/Dockerfile", "", content) {
		t.Error("path fallback must differ from the content-based id")
	}
	// A readable-but-empty Dockerfile is content-addressed, not path-based.
	if buildID("/a/Dockerfile", "", []byte{}) != buildID("/b/Dockerfile", "", []byte{}) {
		t.Error("empty readable Dockerfile must be content-addressed")
	}
}
