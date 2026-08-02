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

// Package connstats counts the sockets and requests of the transports built by
// util.MakeTransport, so registry connection reuse is visible without a proxy in
// front of the registry. A proxy cannot see through TLS to a real registry.
package connstats

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"time"
)

var (
	socketsOpened atomic.Int64
	socketsClosed atomic.Int64
	socketsOpen   atomic.Int64
	peakOpen      atomic.Int64
	dialTime      atomic.Int64
	requests      atomic.Int64
	reused        atomic.Int64
	handshakes    atomic.Int64
	handshakeTime atomic.Int64
	idleTime      atomic.Int64
)

// DialFunc matches http.Transport.DialContext.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// WrapDial counts every socket the transport opens and closes. net/http reports
// neither to its caller, and httptrace has no close hook.
func WrapDial(dial DialFunc) DialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		start := time.Now()
		conn, err := dial(ctx, network, addr)
		// a dial that fails still spent the time, and a build against an
		// unreachable registry spends most of its time here
		dialTime.Add(int64(time.Since(start)))
		if err != nil {
			return nil, err
		}
		socketsOpened.Add(1)
		open := socketsOpen.Add(1)
		for {
			peak := peakOpen.Load()
			if open <= peak || peakOpen.CompareAndSwap(peak, open) {
				break
			}
		}
		return &countedConn{Conn: conn}, nil
	}
}

type countedConn struct {
	net.Conn
	once sync.Once
}

func (c *countedConn) Close() error {
	c.once.Do(func() {
		socketsClosed.Add(1)
		socketsOpen.Add(-1)
	})
	return c.Conn.Close()
}

// Trace records whether a request reused its connection, how long that
// connection sat idle first, and what the TLS handshake cost.
func Trace(rt http.RoundTripper) http.RoundTripper {
	return &tracedTransport{inner: rt}
}

type tracedTransport struct {
	inner http.RoundTripper
}

func (t *tracedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	requests.Add(1)
	// both handshake hooks run on the goroutine doing the dial
	var start time.Time
	trace := &httptrace.ClientTrace{
		TLSHandshakeStart: func() {
			start = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err != nil {
				return
			}
			handshakes.Add(1)
			handshakeTime.Add(int64(time.Since(start)))
		},
		GotConn: func(info httptrace.GotConnInfo) {
			if info.Reused {
				reused.Add(1)
			}
			if info.WasIdle {
				idleTime.Add(int64(info.IdleTime))
			}
		},
	}
	return t.inner.RoundTrip(r.WithContext(httptrace.WithClientTrace(r.Context(), trace)))
}

// Stats totals the registry traffic so far.
type Stats struct {
	SocketsOpened int64
	SocketsClosed int64
	SocketsOpen   int64
	PeakSockets   int64
	Requests      int64
	Reused        int64
	TLSHandshakes int64
	DialTime      time.Duration
	TLSTime       time.Duration
	IdleTime      time.Duration
}

func Snapshot() Stats {
	return Stats{
		SocketsOpened: socketsOpened.Load(),
		SocketsClosed: socketsClosed.Load(),
		SocketsOpen:   socketsOpen.Load(),
		PeakSockets:   peakOpen.Load(),
		Requests:      requests.Load(),
		Reused:        reused.Load(),
		TLSHandshakes: handshakes.Load(),
		DialTime:      time.Duration(dialTime.Load()),
		TLSTime:       time.Duration(handshakeTime.Load()),
		IdleTime:      time.Duration(idleTime.Load()),
	}
}
