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
	"compress/gzip"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

// backend is a telemetry backend that actually verifies. It signs forge tokens
// with one key and ingest tokens with another, its exchange refuses anything it
// cannot verify, and its OTLP receiver refuses spans that arrive without a token
// it minted. Both halves of the path are therefore load-bearing here, where the
// stubs in login_test.go only record what they were sent.
type backend struct {
	forgeKey  *rsa.PrivateKey
	ingestKey *rsa.PrivateKey

	exchange  *httptest.Server
	collector *httptest.Server

	mu          sync.Mutex
	spans       []*tracepb.Span
	resources   map[string]string
	refusals    []string
	contentType string
}

const (
	forgeIssuer  = "https://gitlab.example.com"
	ingestIssuer = "https://ingest.example.com"
	tenant       = "acme"
)

func newBackend(t *testing.T) *backend {
	t.Helper()
	b := &backend{forgeKey: testKey(t), ingestKey: testKey(t), resources: map[string]string{}}

	b.exchange = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := verifyJWT(&b.forgeKey.PublicKey, bearer(r))
		if err != nil {
			b.refuse(w, "forge token was not accepted: "+err.Error(), http.StatusUnauthorized)
			return
		}
		if claims["aud"] != defaultAudience {
			b.refuse(w, fmt.Sprintf("token audience %v is not %s", claims["aud"], defaultAudience), http.StatusForbidden)
			return
		}
		if claims["iss"] != forgeIssuer {
			b.refuse(w, "token issuer is not a registered forge", http.StatusForbidden)
			return
		}
		expires := time.Now().Add(12 * time.Hour)
		_ = json.NewEncoder(w).Encode(ingestToken{
			Token: signJWT(t, b.ingestKey, map[string]any{
				"iss":    ingestIssuer,
				"aud":    defaultAudience,
				"sub":    fmt.Sprintf("%s|%v", forgeIssuer, claims["namespace_id"]),
				"tenant": tenant,
				"exp":    expires.Unix(),
			}),
			ExpiresAt: expires.UTC().Format(time.RFC3339),
			Tenant:    tenant,
		})
	}))
	t.Cleanup(b.exchange.Close)

	b.collector = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := verifyJWT(&b.ingestKey.PublicKey, bearer(r))
		if err != nil {
			b.refuse(w, "ingest token was not accepted: "+err.Error(), http.StatusUnauthorized)
			return
		}
		if claims["tenant"] != tenant {
			b.refuse(w, "ingest token carries no tenant", http.StatusForbidden)
			return
		}
		spans, resources, err := decodeExport(r)
		if err != nil {
			b.refuse(w, err.Error(), http.StatusBadRequest)
			return
		}
		b.mu.Lock()
		b.spans = append(b.spans, spans...)
		for k, v := range resources {
			b.resources[k] = v
		}
		b.contentType = r.Header.Get("Content-Type")
		b.mu.Unlock()
	}))
	t.Cleanup(b.collector.Close)

	return b
}

func (b *backend) refuse(w http.ResponseWriter, reason string, code int) {
	b.mu.Lock()
	b.refusals = append(b.refusals, reason)
	b.mu.Unlock()
	http.Error(w, reason, code)
}

// forgeToken mints what a pipeline's id_tokens: would hand the build.
func (b *backend) forgeToken(t *testing.T, audience string) string {
	t.Helper()
	return signJWT(t, b.forgeKey, map[string]any{
		"iss":          forgeIssuer,
		"aud":          audience,
		"sub":          "project_path:acme/app:ref_type:branch:ref:main",
		"namespace_id": "42",
		"exp":          time.Now().Add(5 * time.Minute).Unix(),
	})
}

func (b *backend) accepted() []*tracepb.Span {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spans
}

func (b *backend) refused() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.refusals
}

func (b *backend) encoding() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.contentType
}

// resourceAttr reads a build attribute, which rides on the resource rather than
// on the span: buildAttrs feeds resource.New.
func (b *backend) resourceAttr(key string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.resources[key]
}

func TestBackendAcceptsWhatKanikoSends(t *testing.T) {
	b := newBackend(t)
	env := newJob(t)
	t.Setenv(IDTokenEnv, b.forgeToken(t, defaultAudience))
	t.Setenv(ExchangeURLEnv, b.exchange.URL)
	t.Setenv(EndpointEnv, b.collector.URL+"/v1/traces")

	env.run(t)

	if refusals := b.refused(); len(refusals) != 0 {
		t.Fatalf("backend refused: %v", refusals)
	}
	// OTLP/HTTP carries protobuf bodies by default; the gRPC transport is what
	// kaniko does not speak, not the encoding.
	if got := b.encoding(); got != "application/x-protobuf" {
		t.Errorf("export Content-Type = %q, want application/x-protobuf", got)
	}
	root := rootSpanOf(t, b.accepted())
	if root.Name != "build" {
		t.Errorf("root span = %q, want build", root.Name)
	}
	if got := spanAttr(root, "kaniko.telemetry.auth"); got != "exchange" {
		t.Errorf("kaniko.telemetry.auth = %q, want exchange", got)
	}
	if got := b.resourceAttr("service.name"); got != "kaniko" {
		t.Errorf("service.name = %q, want kaniko", got)
	}
	if got := b.resourceAttr("kaniko.build_id"); got == "" {
		t.Error("kaniko.build_id missing, so these are not a build's spans")
	}
}

// The credential is load-bearing: an audience the backend does not accept costs
// the build its telemetry rather than being waved through.
func TestBackendRefusesAWrongAudience(t *testing.T) {
	b := newBackend(t)
	env := newJob(t)
	t.Setenv(IDTokenEnv, b.forgeToken(t, "https://some-other-service"))
	t.Setenv(ExchangeURLEnv, b.exchange.URL)
	t.Setenv(EndpointEnv, b.collector.URL+"/v1/traces")

	env.run(t)

	if spans := b.accepted(); len(spans) != 0 {
		t.Errorf("backend accepted %d spans without a valid token", len(spans))
	}
	refusals := strings.Join(b.refused(), "; ")
	if !strings.Contains(refusals, "is not kaniko-telemetry") {
		t.Errorf("exchange did not refuse the audience: %q", refusals)
	}
	if !strings.Contains(refusals, "ingest token was not accepted") {
		t.Errorf("collector accepted an unauthenticated export: %q", refusals)
	}
	if !strings.Contains(env.logs.String(), "ingest token exchange refused") {
		t.Errorf("refusal not logged: %q", env.logs)
	}
}

// A token signed by something other than the forge is not a token.
func TestBackendRefusesAForgedIdentity(t *testing.T) {
	b := newBackend(t)
	env := newJob(t)
	t.Setenv(IDTokenEnv, signJWT(t, testKey(t), map[string]any{
		"iss":          forgeIssuer,
		"aud":          defaultAudience,
		"namespace_id": "42",
		"exp":          time.Now().Add(5 * time.Minute).Unix(),
	}))
	t.Setenv(ExchangeURLEnv, b.exchange.URL)
	t.Setenv(EndpointEnv, b.collector.URL+"/v1/traces")

	env.run(t)

	if spans := b.accepted(); len(spans) != 0 {
		t.Errorf("backend accepted %d spans for a forged identity", len(spans))
	}
	if refusals := strings.Join(b.refused(), "; "); !strings.Contains(refusals, "forge token was not accepted") {
		t.Errorf("exchange did not refuse the signature: %q", refusals)
	}
}

func rootSpanOf(t *testing.T, spans []*tracepb.Span) *tracepb.Span {
	t.Helper()
	for _, s := range spans {
		if len(s.ParentSpanId) == 0 {
			return s
		}
	}
	t.Fatalf("no root span among %d spans", len(spans))
	return nil
}

func spanAttr(span *tracepb.Span, key string) string {
	for _, kv := range span.Attributes {
		if kv.Key == key {
			return kv.Value.GetStringValue()
		}
	}
	return ""
}

func decodeExport(r *http.Request) ([]*tracepb.Span, map[string]string, error) {
	body := io.Reader(r.Body)
	if r.Header.Get("Content-Encoding") == "gzip" {
		unzipped, err := gzip.NewReader(body)
		if err != nil {
			return nil, nil, err
		}
		defer unzipped.Close()
		body = unzipped
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, nil, err
	}
	var export coltracepb.ExportTraceServiceRequest
	if err := proto.Unmarshal(raw, &export); err != nil {
		return nil, nil, fmt.Errorf("body is not an OTLP export: %w", err)
	}
	var spans []*tracepb.Span
	attrs := map[string]string{}
	for _, resource := range export.ResourceSpans {
		for _, kv := range resource.Resource.GetAttributes() {
			attrs[kv.Key] = kv.Value.GetStringValue()
		}
		for _, scope := range resource.ScopeSpans {
			spans = append(spans, scope.Spans...)
		}
	}
	return spans, attrs, nil
}

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func signJWT(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	signing := enc([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + enc(payload)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + enc(sig)
}

func verifyJWT(key *rsa.PublicKey, token string) (map[string]any, error) {
	if token == "" {
		return nil, errors.New("no token presented")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("not a JWT")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return nil, fmt.Errorf("signature: %w", err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	exp, ok := claims["exp"].(float64)
	if !ok || time.Now().After(time.Unix(int64(exp), 0)) {
		return nil, errors.New("expired")
	}
	return claims, nil
}

func bearer(r *http.Request) string {
	scheme, rest, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "bearer") {
		return ""
	}
	return strings.TrimSpace(rest)
}
