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
	"sync"

	"github.com/google/go-containerregistry/pkg/authn"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/osscontainertools/kaniko/pkg/config"
)

// the platform is part of the key because Puller.Get resolves an index against
// the platform the puller was built with
type pullerKey struct {
	registry string
	platform string
}

var (
	pullerMu sync.Mutex
	pullers  = map[pullerKey]*remote.Puller{}
)

// RegistryPuller hands out one puller per registry and platform, so every read
// against that registry shares the fetcher, and with it the /v2/ ping and the
// token exchange. Returns nil when FF_KANIKO_POOL_REGISTRY_CONNECTIONS is off.
func RegistryPuller(opts config.RegistryOptions, registryName string, keychain authn.Keychain, platform *v1.Platform) (*remote.Puller, error) {
	if !config.FF.PoolRegistryConnections {
		return nil, nil
	}

	key := pullerKey{registry: registryName}
	if platform != nil {
		key.platform = platform.String()
	}
	pullerMu.Lock()
	defer pullerMu.Unlock()
	if puller, ok := pullers[key]; ok {
		return puller, nil
	}

	tr, err := MakeTransport(opts, registryName)
	if err != nil {
		return nil, err
	}
	// remote.Reuse skips the auth and transport options of the call it is passed
	// to, so everything the call sites rely on has to be set here
	pullerOpts := []remote.Option{remote.WithTransport(tr), remote.WithAuthFromKeychain(keychain)}
	if platform != nil {
		pullerOpts = append(pullerOpts, remote.WithPlatform(*platform))
	}
	puller, err := remote.NewPuller(pullerOpts...)
	if err != nil {
		return nil, err
	}
	pullers[key] = puller
	return puller, nil
}
