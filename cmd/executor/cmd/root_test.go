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

package cmd

import (
	"testing"

	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/constants"
	"github.com/osscontainertools/kaniko/testutil"
)

func TestEffectiveSourceContextPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		srcContext string
		bucket     string
		want       string
	}{
		{
			name:       "local context takes precedence over bucket",
			srcContext: "/workspace",
			bucket:     "other-bucket",
			want:       "/workspace",
		},
		{
			name:       "bucket takes precedence over remote context",
			srcContext: "https://example.com/context.tar.gz",
			bucket:     "gs://bucket",
			want:       "gs://bucket",
		},
		{
			name:       "plain bucket defaults to GCS",
			srcContext: "https://example.com/context.tar.gz",
			bucket:     "bucket",
			want:       constants.GCSBuildContextPrefix + "bucket",
		},
		{
			name:       "remote context is used when bucket is empty",
			srcContext: "https://example.com/context.tar.gz",
			want:       "https://example.com/context.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveSourceContext(tt.srcContext, tt.bucket); got != tt.want {
				t.Fatalf("effectiveSourceContext() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveSourceContextRecordsEffectiveBucket(t *testing.T) {
	previousInputPolicyFlag := config.FF.InputPolicy
	config.FF.InputPolicy = true
	t.Cleanup(func() { config.FF.InputPolicy = previousInputPolicyFlag })

	bucketPath := t.TempDir()
	bucketContext := constants.LocalDirBuildContextPrefix + bucketPath
	opts := &config.KanikoOptions{
		SrcContext: "https://example.com/context.tar.gz",
		Bucket:     bucketContext,
	}
	if err := resolveSourceContext(opts); err != nil {
		t.Fatal(err)
	}
	if opts.InputContext != bucketContext {
		t.Fatalf("InputContext = %q, want bucket context %q", opts.InputContext, bucketContext)
	}
	if opts.SrcContext != bucketPath {
		t.Fatalf("SrcContext = %q, want unpacked bucket context %q", opts.SrcContext, bucketPath)
	}
}

func TestSkipPath(t *testing.T) {
	tests := []struct {
		description string
		path        string
		expected    bool
	}{
		{
			description: "path is a http url",
			path:        "http://test",
			expected:    true,
		},
		{
			description: "path is a https url",
			path:        "https://test",
			expected:    true,
		},
		{
			description: "path is a empty",
			path:        "",
			expected:    true,
		},
		{
			description: "path is already abs",
			path:        "/tmp/test",
			expected:    true,
		},
		{
			description: "path is relative",
			path:        ".././test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			testutil.CheckDeepEqual(t, tt.expected, shdSkip(tt.path))
		})
	}
}

func TestIsUrl(t *testing.T) {
	tests := []struct {
		description string
		path        string
		expected    bool
	}{
		{
			description: "path is a http url",
			path:        "http://test",
			expected:    true,
		},
		{
			description: "path is a https url",
			path:        "https://test",
			expected:    true,
		},
		{
			description: "path is a empty",
			path:        "",
		},
		{
			description: "path is already abs",
			path:        "/tmp/test",
		},
		{
			description: "path is relative",
			path:        ".././test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			testutil.CheckDeepEqual(t, tt.expected, isURL(tt.path))
		})
	}
}
