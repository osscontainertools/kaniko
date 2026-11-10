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

package creds

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/sirupsen/logrus"
)

var (
	pins   sync.Map
	frozen atomic.Pointer[map[string]authn.Keychain]
)

func Open() {
	frozen.Store(nil)
}

// Settle freezes the pins, later lookups of unseen repositories fall back to the source order.
func Settle() {
	m := map[string]authn.Keychain{}
	pins.Range(func(k, v any) bool {
		m[k.(string)] = v.(authn.Keychain)
		return true
	})
	frozen.Store(&m)
}

func loadPin(key string) (authn.Keychain, bool) {
	kc, ok := pins.Load(key)
	if !ok {
		return nil, false
	}
	return kc.(authn.Keychain), true
}

type anonymousKeychain struct{}

func (anonymousKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	return authn.Anonymous, nil
}

type pinnedKeychain struct {
	opts      *config.RegistryOptions
	keychains []authn.Keychain
}

func (k pinnedKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	var kc authn.Keychain
	var ok bool
	settled := frozen.Load()
	if settled != nil {
		kc, ok = (*settled)[target.String()]
	} else {
		kc, ok = loadPin(target.String())
		repo, isRepo := target.(name.Repository)
		if !ok && isRepo {
			PinPull(k.opts, repo)
			kc, ok = loadPin(target.String())
		}
	}
	if !ok {
		return authn.NewMultiKeychain(k.keychains...).Resolve(target)
	}
	return kc.Resolve(target)
}

// When no source may pull, PinPull remembers the plain source order so repo is not probed again.
func PinPull(opts *config.RegistryOptions, repo name.Repository) bool {
	keychains := sourceKeychains(opts)
	for _, kc := range append(keychains, anonymousKeychain{}) {
		auth, err := kc.Resolve(repo)
		switch {
		case err != nil:
			logrus.Debugf("failed to resolve a credential for %s: %v", repo, err)
		case auth == authn.Anonymous && kc != anonymousKeychain{}:
		default:
			err = canPull(opts, repo, auth)
			if err == nil {
				pins.Store(repo.String(), kc)
				return true
			}
			logrus.Debugf("credential cannot pull from %s: %v", repo, err)
		}
	}
	pins.Store(repo.String(), authn.NewMultiKeychain(keychains...))
	return false
}

// canPull lists one tag, because some token servers hand out a token without access instead of refusing.
func canPull(opts *config.RegistryOptions, repo name.Repository, auth authn.Authenticator) error {
	ctx := context.Background()
	rt, err := util.MakeTransport(*opts, repo.RegistryStr())
	if err != nil {
		return err
	}
	tr, err := transport.NewWithContext(ctx, repo.Registry, auth, rt, []string{repo.Scope(transport.PullScope)})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s://%s/v2/%s/tags/list?n=1", repo.Scheme(), repo.RegistryStr(), repo.RepositoryStr())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// a repository that does not exist yet is not a refusal
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("listing tags: %s", resp.Status)
}

func PinPush(opts *config.RegistryOptions, repo name.Repository, check func(authn.Keychain) error) bool {
	for _, kc := range sourceKeychains(opts) {
		auth, err := kc.Resolve(repo)
		switch {
		case err != nil:
			logrus.Debugf("failed to resolve a credential for %s: %v", repo, err)
		case auth == authn.Anonymous:
		default:
			err = check(kc)
			if err == nil {
				pins.Store(repo.String(), kc)
				return true
			}
			logrus.Debugf("credential cannot push to %s: %v", repo, err)
		}
	}
	return false
}
