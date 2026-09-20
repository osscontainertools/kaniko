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

package util

import (
	"errors"
	"net/http"
	"sync"

	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// keyed on the transport, which MakeTransport already keys on the registry and the
// options it was built from, so different TLS settings cannot share a puller
var (
	poolMu  sync.Mutex
	pullers = map[http.RoundTripper]*remote.Puller{}
	pushers = map[http.RoundTripper]*remote.Pusher{}
)

// ReusePuller shares one puller per transport, so every read through it shares the
// fetcher, and with it the /v2/ ping and the token exchange. The puller is built
// from the caller's own options because remote.Reuse takes precedence over the
// options of the call it is passed to. It is not keyed on the platform: remote.Get
// resolves an index against the platform of the call, not the one the puller was
// built with.
func ReusePuller(rt http.RoundTripper, opts ...remote.Option) ([]remote.Option, error) {
	poolMu.Lock()
	defer poolMu.Unlock()
	puller, ok := pullers[rt]
	if !ok {
		var err error
		puller, err = remote.NewPuller(opts...)
		if err != nil {
			return nil, err
		}
		pullers[rt] = puller
	}
	return []remote.Option{remote.Reuse(puller)}, nil
}

// ReusePusher shares one pusher per transport, so pushes share the fetcher and its
// token.
func ReusePusher(rt http.RoundTripper, opts ...remote.Option) ([]remote.Option, error) {
	poolMu.Lock()
	defer poolMu.Unlock()
	pusher, ok := pushers[rt]
	if !ok {
		var err error
		pusher, err = remote.NewPusher(opts...)
		if err != nil {
			return nil, err
		}
		pushers[rt] = pusher
	}
	return []remote.Option{remote.Reuse(pusher)}, nil
}

// DropPooledOnAuth drops the pooled puller and pusher when the registry rejected
// their credential, so the caller's next attempt resolves it again. They hold the
// credential they resolved and would otherwise re-present it for the whole build.
func DropPooledOnAuth(rt http.RoundTripper, err error) bool {
	if err == nil {
		return false
	}

	var transportErr *transport.Error
	if !errors.As(err, &transportErr) {
		return false
	}
	if transportErr.StatusCode != http.StatusUnauthorized && transportErr.StatusCode != http.StatusForbidden {
		return false
	}

	poolMu.Lock()
	defer poolMu.Unlock()
	delete(pullers, rt)
	delete(pushers, rt)
	return true
}
