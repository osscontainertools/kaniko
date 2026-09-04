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
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/sirupsen/logrus"
)

// A build exports through Init and Shutdown, so that is what these drive: the
// assertions are on what the collector received, not on how login got there.
type job struct {
	logs *bytes.Buffer
}

type build struct {
	*job
	exchange  *stub
	runner    *stub
	collector *stub
}

type stub struct {
	*httptest.Server
	mu       sync.Mutex
	requests []*http.Request
}

func (s *stub) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Clone(context.Background()))
}

func (s *stub) calls() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func (s *stub) header(t *testing.T, name string) string {
	t.Helper()
	calls := s.calls()
	if len(calls) == 0 {
		t.Fatalf("%s was never called", s.URL)
	}
	return calls[0].Header.Get(name)
}

func newStub(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *stub {
	t.Helper()
	s := &stub{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		handler(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// newJob blanks every variable the login reads, so an ambient one cannot decide
// the outcome.
func newJob(t *testing.T) *job {
	t.Helper()
	for _, env := range []string{
		IDTokenEnv, IDTokenFileEnv, ExchangeURLEnv, AudienceEnv,
		githubRequestURLEnv, githubRequestTokenEnv,
		"OTEL_EXPORTER_OTLP_HEADERS", "OTEL_EXPORTER_OTLP_TRACES_HEADERS",
	} {
		t.Setenv(env, "")
	}
	return &job{logs: captureLogs(t)}
}

// run exports one build. Shutdown flushes, so the collector has been called by
// the time it returns.
func (j *job) run(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Dockerfile")
	if err := os.WriteFile(path, []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	Init(context.Background(), &config.KanikoOptions{DockerfilePath: path})
	Shutdown(nil)
}

func newBuild(t *testing.T) *build {
	t.Helper()
	b := &build{job: newJob(t)}

	// An empty 200 unmarshals to an empty ExportTraceServiceResponse.
	b.collector = newStub(t, func(w http.ResponseWriter, r *http.Request) {})
	b.exchange = newStub(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ingestToken{
			Token:     "ingest-token",
			ExpiresAt: "2026-09-05T00:00:00Z",
			Tenant:    "acme",
		})
	})
	b.runner = newStub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"value":"` + jwt(t, defaultAudience) + `","count":1}`))
	})

	t.Setenv(EndpointEnv, b.collector.URL+"/v1/traces")
	return b
}

func TestExchangedTokenReachesTheCollector(t *testing.T) {
	b := newBuild(t)
	identity := jwt(t, defaultAudience)
	t.Setenv(IDTokenEnv, identity)
	t.Setenv(ExchangeURLEnv, b.exchange.URL)

	b.run(t)

	if got := b.exchange.header(t, "Authorization"); got != "Bearer "+identity {
		t.Errorf("exchange saw Authorization %q, want the identity token", got)
	}
	if got := b.collector.header(t, "Authorization"); got != "Bearer ingest-token" {
		t.Errorf("collector saw Authorization %q, want the ingest token", got)
	}
}

func TestIdentityTokenFromAFile(t *testing.T) {
	b := newBuild(t)
	identity := jwt(t, defaultAudience)
	path := filepath.Join(t.TempDir(), "token")
	// Trailing newline is what a projected volume or `echo >` leaves behind.
	if err := os.WriteFile(path, []byte(identity+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(IDTokenFileEnv, path)
	t.Setenv(ExchangeURLEnv, b.exchange.URL)

	b.run(t)

	if got := b.exchange.header(t, "Authorization"); got != "Bearer "+identity {
		t.Errorf("exchange saw Authorization %q, want the identity token", got)
	}
}

func TestIdentityTokenFromGitHubActions(t *testing.T) {
	b := newBuild(t)
	t.Setenv(githubRequestURLEnv, b.runner.URL+"/token?api-version=2.0")
	t.Setenv(githubRequestTokenEnv, "request-token")
	t.Setenv(ExchangeURLEnv, b.exchange.URL)

	b.run(t)

	req := b.runner.calls()[0]
	if got := req.URL.Query().Get("audience"); got != defaultAudience {
		t.Errorf("runner asked for audience %q, want %q", got, defaultAudience)
	}
	// The pre-existing parameter survives: audience is added, not substituted.
	if got := req.URL.Query().Get("api-version"); got != "2.0" {
		t.Errorf("api-version = %q, want it kept", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer request-token" {
		t.Errorf("runner saw Authorization %q, want the request token", got)
	}
	if got := b.collector.header(t, "Authorization"); got != "Bearer ingest-token" {
		t.Errorf("collector saw Authorization %q, want the ingest token", got)
	}
}

func TestEnvTokenPreferredOverGitHub(t *testing.T) {
	b := newBuild(t)
	identity := jwt(t, defaultAudience)
	t.Setenv(IDTokenEnv, identity)
	t.Setenv(githubRequestURLEnv, b.runner.URL)
	t.Setenv(githubRequestTokenEnv, "request-token")
	t.Setenv(ExchangeURLEnv, b.exchange.URL)

	b.run(t)

	if len(b.runner.calls()) != 0 {
		t.Error("asked the runner for a token although one was in the environment")
	}
	if got := b.exchange.header(t, "Authorization"); got != "Bearer "+identity {
		t.Errorf("exchange saw Authorization %q, want the environment token", got)
	}
}

// A configured source that fails ends the search rather than falling through to
// the next one.
func TestBrokenSourceIsNotWorkedAround(t *testing.T) {
	b := newBuild(t)
	t.Setenv(IDTokenFileEnv, filepath.Join(t.TempDir(), "absent"))
	t.Setenv(githubRequestURLEnv, b.runner.URL)
	t.Setenv(githubRequestTokenEnv, "request-token")
	t.Setenv(ExchangeURLEnv, b.exchange.URL)

	b.run(t)

	if len(b.runner.calls()) != 0 {
		t.Error("fell through to the runner after a configured source failed")
	}
	if len(b.exchange.calls()) != 0 {
		t.Error("exchanged something after a configured source failed")
	}
	if got := b.collector.header(t, "Authorization"); got != "" {
		t.Errorf("collector saw Authorization %q, want none", got)
	}
}

func TestNoExchangeWithoutItsURL(t *testing.T) {
	b := newBuild(t)
	t.Setenv(IDTokenEnv, jwt(t, defaultAudience))

	b.run(t)

	if len(b.exchange.calls()) != 0 {
		t.Error("exchanged a token although no exchange URL was set")
	}
	if got := b.collector.header(t, "Authorization"); got != "" {
		t.Errorf("collector saw Authorization %q, want none", got)
	}
}

// The header the job set is the escape hatch, so it wins and no identity token
// is spent.
func TestJobsOwnHeaderWins(t *testing.T) {
	b := newBuild(t)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=Bearer%20stored,x-scope=acme")
	t.Setenv(IDTokenEnv, jwt(t, defaultAudience))
	t.Setenv(ExchangeURLEnv, b.exchange.URL)

	b.run(t)

	if len(b.exchange.calls()) != 0 {
		t.Error("exchanged a token although the job set its own header")
	}
	if got := b.collector.header(t, "Authorization"); got != "Bearer stored" {
		t.Errorf("collector saw Authorization %q, want the job's own token", got)
	}
}

// WithHeaders replaces the map the SDK read from the environment, so a header
// the job set for itself has to survive the ingest token being added.
func TestJobsOtherHeadersSurviveTheExchange(t *testing.T) {
	b := newBuild(t)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "x-scope=acme")
	t.Setenv(IDTokenEnv, jwt(t, defaultAudience))
	t.Setenv(ExchangeURLEnv, b.exchange.URL)

	b.run(t)

	if got := b.collector.header(t, "X-Scope"); got != "acme" {
		t.Errorf("collector saw X-Scope %q, want acme", got)
	}
	if got := b.collector.header(t, "Authorization"); got != "Bearer ingest-token" {
		t.Errorf("collector saw Authorization %q, want the ingest token", got)
	}
}

func TestRefusedExchangeStillExports(t *testing.T) {
	b := newBuild(t)
	refusing := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "this account is not enrolled as a tenant", http.StatusForbidden)
	})
	t.Setenv(IDTokenEnv, jwt(t, defaultAudience))
	t.Setenv(ExchangeURLEnv, refusing.URL)

	b.run(t)

	if got := b.collector.header(t, "Authorization"); got != "" {
		t.Errorf("collector saw Authorization %q, want none", got)
	}
	// The backend's connect page and troubleshooting docs promise this wording.
	if !strings.Contains(b.logs.String(), "ingest token exchange refused") {
		t.Errorf("refusal not logged as promised: %q", b.logs)
	}
	if !strings.Contains(b.logs.String(), "not enrolled") {
		t.Errorf("backend explanation dropped: %q", b.logs)
	}
}

func TestCleartextExchangeIsRefused(t *testing.T) {
	b := newBuild(t)
	t.Setenv(IDTokenEnv, jwt(t, defaultAudience))
	t.Setenv(ExchangeURLEnv, "http://ingest.example.com/ingest/token")

	b.run(t)

	if got := b.collector.header(t, "Authorization"); got != "" {
		t.Errorf("collector saw Authorization %q, want none", got)
	}
	if !strings.Contains(b.logs.String(), "use https") {
		t.Errorf("cleartext refusal not logged: %q", b.logs)
	}
}

func TestRedirectedExchangeIsRefused(t *testing.T) {
	b := newBuild(t)
	elsewhere := newStub(t, func(w http.ResponseWriter, r *http.Request) {})
	redirecting := newStub(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	})
	t.Setenv(IDTokenEnv, jwt(t, defaultAudience))
	t.Setenv(ExchangeURLEnv, redirecting.URL)

	b.run(t)

	if len(elsewhere.calls()) != 0 {
		t.Error("followed a redirect and disclosed the token")
	}
	if !strings.Contains(b.logs.String(), "refusing a redirect") {
		t.Errorf("redirect refusal not logged: %q", b.logs)
	}
}

func TestNoTokenReachesTheLog(t *testing.T) {
	b := newBuild(t)
	const identity = "eyJhbGciOiJub25lIn0.eyJhdWQiOiJrYW5pa28tdGVsZW1ldHJ5In0.secret-identity-token"
	t.Setenv(IDTokenEnv, identity)
	t.Setenv(ExchangeURLEnv, b.exchange.URL)

	b.run(t)

	for _, secret := range []string{identity, "secret-identity-token", "ingest-token"} {
		if strings.Contains(b.logs.String(), secret) {
			t.Errorf("a token reached the log: %q", b.logs)
		}
	}
}

func TestAudienceMismatchIsReported(t *testing.T) {
	b := newBuild(t)
	t.Setenv(IDTokenEnv, jwt(t, "https://gitlab.example.com"))
	t.Setenv(ExchangeURLEnv, b.exchange.URL)

	b.run(t)

	if !strings.Contains(b.logs.String(), "audience") {
		t.Errorf("mismatch not reported: %q", b.logs)
	}
}

func TestRequireSecure(t *testing.T) {
	for _, raw := range []string{
		"https://ingest.example.com/ingest/token",
		"http://localhost:8080/ingest/token",
		"http://127.0.0.1:8080/ingest/token",
	} {
		if err := requireSecure(raw); err != nil {
			t.Errorf("requireSecure(%q) = %v, want allowed", raw, err)
		}
	}
	for _, raw := range []string{
		"http://ingest.example.com/ingest/token",
		"ftp://ingest.example.com/ingest/token",
	} {
		if err := requireSecure(raw); err == nil {
			t.Errorf("requireSecure(%q) = nil, want refused", raw)
		}
	}
}

func TestAudiences(t *testing.T) {
	if got := audiences(jwt(t, "kaniko-telemetry")); len(got) != 1 || got[0] != "kaniko-telemetry" {
		t.Errorf("audiences(string aud) = %v", got)
	}
	if got := audiences(jwt(t, []string{"other", "kaniko-telemetry"})); len(got) != 2 {
		t.Errorf("audiences(array aud) = %v", got)
	}
	if got := audiences("not-a-jwt"); got != nil {
		t.Errorf("audiences(opaque) = %v, want nil", got)
	}
}

// jwt builds an unsigned token; only the aud diagnostic ever reads one.
func jwt(t *testing.T, aud any) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"aud": aud})
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc(payload) + "." + enc([]byte("sig"))
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	out, level := logrus.StandardLogger().Out, logrus.GetLevel()
	logrus.SetOutput(&buf)
	logrus.SetLevel(logrus.DebugLevel)
	t.Cleanup(func() {
		logrus.SetOutput(out)
		logrus.SetLevel(level)
	})
	return &buf
}
