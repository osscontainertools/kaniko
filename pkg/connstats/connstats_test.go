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

package connstats

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func resetHosts(t *testing.T) {
	t.Cleanup(func() {
		countHosts.Store(false)
		hostsMu.Lock()
		hosts = map[string]*hostCounters{}
		hostsMu.Unlock()
	})
}

func TestHostCountersOff(t *testing.T) {
	resetHosts(t)
	_, client := net.Pipe()
	dial := WrapDial(func(context.Context, string, string) (net.Conn, error) { return client, nil })
	conn, err := dial(context.Background(), "tcp", "registry.example:443")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, ok := conn.(*countedConn); !ok {
		t.Errorf("without CountHosts the conn must stay a *countedConn, got %T", conn)
	}
	if len(Hosts()) != 0 {
		t.Errorf("hosts counted without CountHosts: %v", Hosts())
	}
}

func TestHostCounters(t *testing.T) {
	resetHosts(t)
	CountHosts()
	body := strings.Repeat("x", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		io.WriteString(w, body)
	}))
	defer srv.Close()

	tr := srv.Client().Transport.(*http.Transport).Clone()
	tr.DialContext = WrapDial((&net.Dialer{}).DialContext)
	client := &http.Client{Transport: Trace(tr)}
	for range 2 {
		resp, err := client.Post(srv.URL, "text/plain", strings.NewReader(strings.Repeat("y", 1000)))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(got) != body {
			t.Fatal("unexpected response body")
		}
	}
	tr.CloseIdleConnections()

	h, ok := Hosts()["127.0.0.1"]
	if !ok {
		t.Fatalf("no counters for 127.0.0.1: %v", Hosts())
	}
	if h.Requests != 2 || h.Conns != 1 || h.Reused != 1 {
		t.Errorf("requests=%d conns=%d reused=%d, want 2, 1, 1", h.Requests, h.Conns, h.Reused)
	}
	// bodies plus HTTP framing
	if h.BytesWritten < 2000 || h.BytesRead < 2*4096 {
		t.Errorf("bytes written=%d read=%d", h.BytesWritten, h.BytesRead)
	}
}
