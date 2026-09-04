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
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// enables the exchange when set to the backend's token endpoint.
const ExchangeURLEnv = "KANIKO_TELEMETRY_EXCHANGE_URL"

const (
	IDTokenEnv     = "KANIKO_TELEMETRY_ID_TOKEN"
	IDTokenFileEnv = "KANIKO_TELEMETRY_ID_TOKEN_FILE"
)

const (
	AudienceEnv     = "KANIKO_TELEMETRY_AUDIENCE"
	defaultAudience = "kaniko-telemetry"
)

const (
	githubRequestURLEnv   = "ACTIONS_ID_TOKEN_REQUEST_URL"
	githubRequestTokenEnv = "ACTIONS_ID_TOKEN_REQUEST_TOKEN"
)

const loginTimeout = 10 * time.Second

const errorBodyLimit = 4 << 10

// errNoSource means this source is not configured, so the next one gets a turn.
// Any other error ends the search.
var errNoSource = errors.New("not configured")

type tokenSource struct {
	name string
	get  func(ctx context.Context, client *http.Client, audience string) (string, error)
}

var tokenSources = []tokenSource{
	{IDTokenEnv, envIDToken},
	{IDTokenFileEnv, fileIDToken},
	{"GitHub Actions", githubIDToken},
}

func login(ctx context.Context) string {
	exchangeURL := os.Getenv(ExchangeURLEnv)
	if exchangeURL == "" {
		return ""
	}
	if err := requireSecure(exchangeURL); err != nil {
		logrus.Warnf("ingest token exchange refused: %s: %v", ExchangeURLEnv, err)
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	client := &http.Client{CheckRedirect: refuseRedirect}

	audience := defaultAudience
	if a := os.Getenv(AudienceEnv); a != "" {
		audience = a
	}

	identity, source, err := forgeToken(ctx, client, audience)
	if errors.Is(err, errNoSource) {
		logrus.Warnf("%s is set but this job has no identity token: set %s, or grant id-token: write on GitHub Actions",
			ExchangeURLEnv, IDTokenEnv)
		return ""
	}
	if err != nil {
		logrus.Warnf("ingest token exchange refused: %v", err)
		return ""
	}
	warnAudienceMismatch(identity, audience)

	token, err := exchange(ctx, client, exchangeURL, identity)
	if err != nil {
		logrus.Warnf("ingest token exchange refused: %v", err)
		return ""
	}
	logrus.Infof("ingest token issued from %s for tenant %s, expires %s", source, token.Tenant, token.ExpiresAt)
	return token.Token
}

// exportHeaders returns what the exporter sends and how it authenticated, the
// latter recorded as kaniko.telemetry.auth.
func exportHeaders(ctx context.Context) (map[string]string, string) {
	headers := otlpHeaders()
	if _, ok := headers[authHeader]; ok {
		return headers, "env"
	}
	token := login(ctx)
	if token == "" {
		return headers, "none"
	}
	headers[authHeader] = "Bearer " + token
	return headers, "exchange"
}

const authHeader = "authorization"

// keep in sync with the SDK's own parse: signal-specific replaces generic
// rather than merging, and only the value is percent-decoded.
var headerEnvs = []string{"OTEL_EXPORTER_OTLP_HEADERS", "OTEL_EXPORTER_OTLP_TRACES_HEADERS"}

// otlpHeaders re-reads what the SDK already read from the environment, because
// WithHeaders replaces that map wholesale instead of merging into it.
func otlpHeaders() map[string]string {
	headers := map[string]string{}
	for _, env := range headerEnvs {
		raw := os.Getenv(env)
		if raw == "" {
			continue
		}
		headers = map[string]string{}
		for _, pair := range strings.Split(raw, ",") {
			name, value, found := strings.Cut(pair, "=")
			if !found {
				continue
			}
			decoded, err := url.PathUnescape(strings.TrimSpace(value))
			if err != nil {
				continue
			}
			headers[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(decoded)
		}
	}
	return headers
}

func forgeToken(ctx context.Context, client *http.Client, audience string) (string, string, error) {
	for _, src := range tokenSources {
		token, err := src.get(ctx, client, audience)
		if errors.Is(err, errNoSource) {
			continue
		}
		if err != nil {
			return "", src.name, fmt.Errorf("%s: %w", src.name, err)
		}
		return token, src.name, nil
	}
	return "", "", errNoSource
}

func envIDToken(_ context.Context, _ *http.Client, _ string) (string, error) {
	token := strings.TrimSpace(os.Getenv(IDTokenEnv))
	if token == "" {
		return "", errNoSource
	}
	return token, nil
}

func fileIDToken(_ context.Context, _ *http.Client, _ string) (string, error) {
	path := os.Getenv(IDTokenFileEnv)
	if path == "" {
		return "", errNoSource
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return token, nil
}

// githubIDToken asks the runner for a token. The request token is not an
// identity token: it buys one, once per call.
func githubIDToken(ctx context.Context, client *http.Client, audience string) (string, error) {
	raw, requestToken := os.Getenv(githubRequestURLEnv), os.Getenv(githubRequestTokenEnv)
	if raw == "" || requestToken == "" {
		return "", errNoSource
	}
	if err := requireSecure(raw); err != nil {
		return "", fmt.Errorf("%s: %w", githubRequestURLEnv, err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s: %w", githubRequestURLEnv, err)
	}
	q := u.Query()
	q.Set("audience", audience)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+requestToken)
	req.Header.Set("Accept", "application/json; api-version=2.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("runner refused an id token: %s: %s", resp.Status, bytes.TrimSpace(body))
	}
	var minted struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &minted); err != nil {
		return "", fmt.Errorf("runner response is not the expected JSON: %w", err)
	}
	if minted.Value == "" {
		return "", errors.New("runner returned an empty id token")
	}
	return minted.Value, nil
}

type ingestToken struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	Tenant    string `json:"tenant"`
}

func exchange(ctx context.Context, client *http.Client, exchangeURL, identity string) (ingestToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeURL, nil)
	if err != nil {
		return ingestToken{}, err
	}
	req.Header.Set("Authorization", "Bearer "+identity)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return ingestToken{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
	if err != nil {
		return ingestToken{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return ingestToken{}, fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(body))
	}
	var token ingestToken
	if err := json.Unmarshal(body, &token); err != nil {
		return ingestToken{}, fmt.Errorf("exchange response is not the expected JSON: %w", err)
	}
	if token.Token == "" {
		return ingestToken{}, errors.New("exchange returned no token")
	}
	return token, nil
}

// requireSecure keeps a bearer credential off a cleartext connection, loopback
// excepted so a backend can be developed against without a certificate.
func requireSecure(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && loopback(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("%q would send a credential in the clear; use https", raw)
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// refuseRedirect stops a 302 from disclosing the token to whatever host the
// response names, which is why the exchange gets its own client.
func refuseRedirect(req *http.Request, _ []*http.Request) error {
	return fmt.Errorf("refusing a redirect to %s: it would disclose the token", req.URL.Host)
}

// warnAudienceMismatch turns what the backend reports as a bare 401 into the
// actual problem. On GitLab the audience is the pipeline's to set, not kaniko's.
func warnAudienceMismatch(token, want string) {
	got := audiences(token)
	if len(got) == 0 || slices.Contains(got, want) {
		return
	}
	logrus.Warnf("identity token audience is %v, but the exchange expects %q: set the audience your CI mints with", got, want)
}

// audiences reads the aud claim without verifying anything: diagnostics only,
// the backend is the authority on this token.
func audiences(token string) []string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims struct {
		Aud json.RawMessage `json:"aud"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	var one string
	if err := json.Unmarshal(claims.Aud, &one); err == nil {
		return []string{one}
	}
	var many []string
	if err := json.Unmarshal(claims.Aud, &many); err == nil {
		return many
	}
	return nil
}
