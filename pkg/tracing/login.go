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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const TokenExchangeEndpointEnv = "KANIKO_TELEMETRY_TOKEN_EXCHANGE_ENDPOINT"

const (
	IDTokenEnv     = "KANIKO_TELEMETRY_ID_TOKEN"
	IDTokenFileEnv = "KANIKO_TELEMETRY_ID_TOKEN_FILE"
)

const loginTimeout = 10 * time.Second

func login(ctx context.Context) string {
	exchangeURL := os.Getenv(TokenExchangeEndpointEnv)
	if exchangeURL == "" {
		return ""
	}
	err := requireSecure(exchangeURL)
	if err != nil {
		logrus.Warnf("ingest token exchange refused: %s: %v", TokenExchangeEndpointEnv, err)
		return ""
	}
	// the token rides on every export, so the collector has to be as safe as the exchange
	err = requireSecure(os.Getenv(EndpointEnv))
	if err != nil {
		logrus.Warnf("ingest token exchange refused: %s: %v", EndpointEnv, err)
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	client := &http.Client{CheckRedirect: refuseRedirect}

	identity, source, err := identityToken()
	if err != nil {
		logrus.Warnf("ingest token exchange refused: %v", err)
		return ""
	}
	if identity == "" {
		logrus.Warnf("%s is set but this job has no identity token: set %s or %s",
			TokenExchangeEndpointEnv, IDTokenEnv, IDTokenFileEnv)
		return ""
	}

	token, err := exchange(ctx, client, exchangeURL, identity)
	if err != nil {
		logrus.Warnf("ingest token exchange refused: %v", err)
		return ""
	}
	logrus.Infof("ingest token issued from %s for tenant %s, expires %s", source, token.Tenant, token.ExpiresAt)
	return token.Token
}

// the second return lands on the root span as kaniko.telemetry.auth.
func exportHeaders(ctx context.Context) (map[string]string, string) {
	headers := otlpHeaders()
	if strings.TrimSpace(headers[authHeader]) != "" {
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

// re-reads what the SDK already read from the environment, because WithHeaders
// replaces that map wholesale instead of merging into it. keep the parse in sync
// with the SDK's: signal-specific replaces generic rather than merging, and only
// the value is percent-decoded.
func otlpHeaders() map[string]string {
	raw := os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")
	signal := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS")
	if signal != "" {
		raw = signal
	}

	headers := map[string]string{}
	for _, pair := range strings.Split(raw, ",") {
		name, value, found := strings.Cut(pair, "=")
		decoded, err := url.PathUnescape(strings.TrimSpace(value))
		if !found || err != nil {
			continue
		}
		headers[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(decoded)
	}
	return headers
}

// a source that is configured but broken ends the search, rather than letting
// the next one find a different credential behind the job's back. an empty
// return with no error means nothing was configured.
func identityToken() (string, string, error) {
	exported, configured := os.LookupEnv(IDTokenEnv)
	path := os.Getenv(IDTokenFileEnv)

	switch {
	case configured:
		token := strings.TrimSpace(exported)
		if token == "" {
			return "", "", fmt.Errorf("%s is empty", IDTokenEnv)
		}
		return token, IDTokenEnv, nil

	case path != "":
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", "", fmt.Errorf("%s: %w", IDTokenFileEnv, err)
		}
		token := strings.TrimSpace(string(raw))
		if token == "" {
			return "", "", fmt.Errorf("%s: %s is empty", IDTokenFileEnv, path)
		}
		return token, IDTokenFileEnv, nil

	default:
		return "", "", nil
	}
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ingestToken{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return ingestToken{}, fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(body))
	}
	var token ingestToken
	err = json.Unmarshal(body, &token)
	if err != nil {
		return ingestToken{}, fmt.Errorf("exchange response is not the expected JSON: %w", err)
	}
	if token.Token == "" {
		return ingestToken{}, errors.New("exchange returned no token")
	}
	return token, nil
}

// loopback is excepted so a backend can be developed against without a certificate.
func requireSecure(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme == "https" {
		return nil
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if u.Scheme == "http" && (host == "localhost" || (ip != nil && ip.IsLoopback())) {
		return nil
	}
	return fmt.Errorf("%q would send a credential in the clear, use https", raw)
}

// Go keeps the Authorization header on a redirect to the same host and ignores the
// scheme, so a 302 from https to http would send the token in the clear.
func refuseRedirect(req *http.Request, _ []*http.Request) error {
	return fmt.Errorf("refusing a redirect to %s: it would disclose the token", req.URL.Host)
}
