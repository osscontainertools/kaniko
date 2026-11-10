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

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/sirupsen/logrus"
)

var pushPins, pullPins sync.Map

type anonymousKeychain struct{}

func (anonymousKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	return authn.Anonymous, nil
}

type probingKeychain struct {
	opts      *config.RegistryOptions
	keychains []authn.Keychain
}

func (k probingKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	kc, ok := pushPins.Load(target.String())
	if !ok {
		kc, ok = pullPins.Load(target.String())
	}
	if !ok {
		repo, isRepo := target.(name.Repository)
		if isRepo {
			kc, ok = probePull(k.opts, k.keychains, repo)
		}
	}
	if !ok {
		return authn.NewMultiKeychain(k.keychains...).Resolve(target)
	}
	return kc.(authn.Keychain).Resolve(target)
}

func probePull(opts *config.RegistryOptions, keychains []authn.Keychain, repo name.Repository) (authn.Keychain, bool) {
	candidates := append(keychains[:len(keychains):len(keychains)], anonymousKeychain{})
	for _, kc := range candidates {
		auth, err := kc.Resolve(repo)
		switch {
		case err != nil:
			logrus.Debugf("failed to resolve a credential for %s: %v", repo, err)
		case auth == authn.Anonymous && kc != anonymousKeychain{}:
		default:
			err = canPull(opts, repo, auth)
			if err == nil {
				pullPins.Store(repo.String(), kc)
				return kc, true
			}
			logrus.Debugf("credential cannot pull from %s: %v", repo, err)
		}
	}
	return nil, false
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

func PinPushCredential(keychain authn.Keychain, repo name.Repository, check func(authn.Keychain) error) (bool, error) {
	k, ok := keychain.(probingKeychain)
	if !ok {
		return false, nil
	}
	for _, kc := range k.keychains {
		auth, err := kc.Resolve(repo)
		if err != nil {
			return false, err
		}
		if auth != authn.Anonymous {
			err = check(kc)
			if err == nil {
				pushPins.Store(repo.String(), kc)
				return true, nil
			}
			logrus.Debugf("credential cannot push to %s: %v", repo, err)
		}
	}
	return false, nil
}
