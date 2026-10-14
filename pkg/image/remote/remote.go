/*
Copyright 2018 Google LLC

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

package remote

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/creds"
	"github.com/osscontainertools/kaniko/pkg/mounts"
	"github.com/osscontainertools/kaniko/pkg/timing"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/sirupsen/logrus"
)

type resolvedImage struct {
	image v1.Image
	index []byte
}

var (
	manifestCache   = make(map[string]resolvedImage)
	remoteImageFunc = getImage
)

// ResetManifestCache forgets every resolved image, so the next lookup records
// its mount source again after a mounts.Reset.
func ResetManifestCache() {
	manifestCache = make(map[string]resolvedImage)
}

func getImage(ref name.Reference, options ...remote.Option) (resolvedImage, error) {
	desc, err := remote.Get(ref, options...)
	if err != nil {
		return resolvedImage{}, err
	}
	img, err := desc.Image()
	if err != nil {
		return resolvedImage{}, err
	}
	if desc.MediaType.IsIndex() {
		return resolvedImage{image: img, index: desc.Manifest}, nil
	}
	return resolvedImage{image: img}, nil
}

// RetrieveRemoteImage retrieves the manifest for the specified image from the specified registry
func RetrieveRemoteImage(image string, opts config.RegistryOptions, customPlatform string) (v1.Image, error) {
	img, _, err := RetrieveRemoteImageAndIndex(image, opts, customPlatform)
	return img, err
}

func RetrieveRemoteImageAndIndex(image string, opts config.RegistryOptions, customPlatform string) (v1.Image, []byte, error) {
	logrus.Infof("Retrieving image manifest %s", image)

	cached, ok := manifestCache[image]
	if ok {
		logrus.Infof("Returning cached image manifest")
		return cached.image, cached.index, nil
	}
	t := timing.Start("Retrieving Source Image")
	defer t.End()

	ref, err := name.ParseReference(image, name.WeakValidation)
	if err != nil {
		return nil, nil, err
	}

	if newRegURLs, found := opts.RegistryMaps[ref.Context().RegistryStr()]; found {
		for _, registryMapping := range newRegURLs {
			regToMapTo, repositoryPrefix := parseRegistryMapping(registryMapping)

			insecurePull := opts.InsecurePull || opts.InsecureRegistries.Contains(regToMapTo)

			remappedRepository, err := remapRepository(ref.Context(), regToMapTo, repositoryPrefix, insecurePull)
			if err != nil {
				return nil, nil, err
			}

			remappedRef := setNewRepository(ref, remappedRepository)

			logrus.Infof("Retrieving image %s from mapped registry %s", remappedRef, regToMapTo)
			// outside retryFunc, otherwise every retry starts from an empty connection pool
			tr, remoteOpts := remoteOptions(regToMapTo, opts, customPlatform)
			retryFunc := func() (resolvedImage, error) {
				image, err := remoteImageFunc(remappedRef, remoteOpts...)
				if err != nil && (!config.FF.PoolRegistryConnections || util.DropPooledOnAuth(tr, err)) {
					tr, remoteOpts = remoteOptions(regToMapTo, opts, customPlatform)
				}
				return image, err
			}

			var remoteImage resolvedImage
			if remoteImage, err = util.RetryWithResult(retryFunc, opts.ImageDownloadRetry, 1000); err != nil {
				logrus.Warnf("Failed to retrieve image %s from remapped registry %s: %s. Will try with the next registry, or fallback to the original registry.", remappedRef, regToMapTo, err)
				continue
			}

			manifestCache[image] = remoteImage
			if config.FF.CrossRepoMount {
				// a registry-map implies that the original also holds the image,
				// and we might push there, so record it as a possible mount reference too.
				mounts.RecordImage(remoteImage.image, remappedRef.Context())
				mounts.RecordGuess(remoteImage.image, ref.Context())
			}

			return remoteImage.image, remoteImage.index, nil
		}

		if len(newRegURLs) > 0 && opts.SkipDefaultRegistryFallback {
			return nil, nil, fmt.Errorf("image not found on any configured mapped registries for %s", ref)
		}
	}

	registryName := ref.Context().RegistryStr()
	if opts.InsecurePull || opts.InsecureRegistries.Contains(registryName) {
		newReg, err := name.NewRegistry(registryName, name.WeakValidation, name.Insecure)
		if err != nil {
			return nil, nil, err
		}
		ref = setNewRegistry(ref, newReg)
	}

	logrus.Infof("Retrieving image %s from registry %s", ref, registryName)

	tr, remoteOpts := remoteOptions(registryName, opts, customPlatform)
	retryFunc := func() (resolvedImage, error) {
		image, err := remoteImageFunc(ref, remoteOpts...)
		if err != nil && (!config.FF.PoolRegistryConnections || util.DropPooledOnAuth(tr, err)) {
			tr, remoteOpts = remoteOptions(registryName, opts, customPlatform)
		}
		return image, err
	}

	var remoteImage resolvedImage
	if remoteImage, err = util.RetryWithResult(retryFunc, opts.ImageDownloadRetry, 1000); remoteImage.image != nil {
		manifestCache[image] = remoteImage
		if config.FF.CrossRepoMount {
			mounts.RecordImage(remoteImage.image, ref.Context())
		}
	}

	return remoteImage.image, remoteImage.index, err
}

// remapRepository adds the {repositoryPrefix}/ to the original repo, and normalizes with an additional library/ if necessary
func remapRepository(repo name.Repository, regToMapTo string, repositoryPrefix string, insecurePull bool) (name.Repository, error) {
	if insecurePull {
		return name.NewRepository(repositoryPrefix+repo.RepositoryStr(), name.WithDefaultRegistry(regToMapTo), name.WeakValidation, name.Insecure)
	} else {
		return name.NewRepository(repositoryPrefix+repo.RepositoryStr(), name.WithDefaultRegistry(regToMapTo), name.WeakValidation)
	}
}

func setNewRepository(ref name.Reference, newRepo name.Repository) name.Reference {
	switch r := ref.(type) {
	case name.Tag:
		r.Repository = newRepo
		return r
	case name.Digest:
		r.Repository = newRepo
		return r
	default:
		return ref
	}
}

func setNewRegistry(ref name.Reference, newReg name.Registry) name.Reference {
	switch r := ref.(type) {
	case name.Tag:
		r.Registry = newReg
		return r
	case name.Digest:
		r.Registry = newReg
		return r
	default:
		return ref
	}
}

func remoteOptions(registryName string, opts config.RegistryOptions, customPlatform string) (http.RoundTripper, []remote.Option) {
	tr, err := util.MakeTransport(opts, registryName)
	// The MakeTransport function will only return errors if there was a problem
	// with registry certificates (Verification or mTLS)
	if err != nil {
		logrus.Fatalf("Unable to setup transport for registry %q: %v", customPlatform, err)
	}

	// The platform value has previously been validated.
	platform, err := v1.ParsePlatform(customPlatform)
	if err != nil {
		logrus.Fatalf("Invalid platform %q: %v", customPlatform, err)
	}

	remoteOpts := []remote.Option{remote.WithTransport(tr), remote.WithAuthFromKeychain(creds.GetKeychain(&opts)), remote.WithPlatform(*platform)}

	if config.FF.PoolRegistryConnections {
		reuse, err := util.ReusePuller(tr, remoteOpts...)
		if err != nil {
			logrus.Fatalf("Unable to setup puller for registry %q: %v", registryName, err)
		}
		remoteOpts = append(remoteOpts, reuse...)
	}
	return tr, remoteOpts
}

// Parse the registry mapping
// example: regMapping = "registry.example.com/subdir1/subdir2" will return registry.example.com and subdir1/subdir2/
func parseRegistryMapping(regMapping string) (string, string) {
	// Split the registry mapping by first slash
	regURL, repositoryPrefix, _ := strings.Cut(regMapping, "/")

	// Normalize with a trailing slash if not empty
	if repositoryPrefix != "" && !strings.HasSuffix(repositoryPrefix, "/") {
		repositoryPrefix = repositoryPrefix + "/"
	}

	return regURL, repositoryPrefix
}
