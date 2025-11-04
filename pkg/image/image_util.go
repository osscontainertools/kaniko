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

package image

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/osscontainertools/kaniko/pkg/cache"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/constants"
	"github.com/osscontainertools/kaniko/pkg/image/remote"
	"github.com/osscontainertools/kaniko/pkg/timing"
	"github.com/osscontainertools/kaniko/pkg/util"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/sirupsen/logrus"
)

var (
	// RetrieveRemoteImage downloads an image from a remote location
	RetrieveRemoteImage = remote.RetrieveRemoteImage
	retrieveTarImage    = tarballImage
)

// RetrieveSourceImage returns the base image of the stage at index
func RetrieveSourceImage(stage config.KanikoStage, opts *config.KanikoOptions) (v1.Image, error) {
	t := timing.Start("Retrieving Source Image")
	defer timing.DefaultRun.Stop(t)
	var buildArgs []string

	for _, marg := range stage.MetaArgs {
		for _, arg := range marg.Args {
			buildArgs = append(buildArgs, fmt.Sprintf("%s=%s", arg.Key, arg.ValueString()))
		}
	}
	buildArgs = append(buildArgs, opts.BuildArgs...)
	currentBaseName, err := util.ResolveEnvironmentReplacement(stage.BaseName, buildArgs, false)
	if err != nil {
		return nil, err
	}
	// First, check if the base image is a scratch image
	if currentBaseName == constants.NoBaseImage {
		logrus.Info("No base image, nothing to extract")
		return empty.Image, nil
	}
	// Next, check if the base image of the current stage is built from a previous stage
	// If so, retrieve the image from the stored tarball
	if stage.BaseImageStoredLocally {
		if config.EnvBool("FF_KANIKO_OCI_STAGES") {
			return ociImage(stage.BaseImageIndex)
		} else {
			return retrieveTarImage(stage.BaseImageIndex)
		}
	}

	// Finally, check if local caching is enabled
	// If so, look in the local cache before trying the remote registry
	if opts.Cache && opts.CacheDir != "" {
		cachedImage, err := cachedImage(opts, currentBaseName)
		if err != nil {
			switch {
			case cache.IsNotFound(err):
				logrus.Debugf("Image %v not found in cache", currentBaseName)
			case cache.IsExpired(err):
				logrus.Debugf("Image %v found in cache but was expired", currentBaseName)
			default:
				logrus.Errorf("Error while retrieving image from cache: %v %v", currentBaseName, err)
			}
		} else if cachedImage != nil {
			return cachedImage, nil
		}
	}

	// Otherwise, initialize image as usual
	return RetrieveRemoteImage(currentBaseName, opts.RegistryOptions, opts.CustomPlatform)
}

func tarballImage(index int) (v1.Image, error) {
	tarPath := filepath.Join(config.KanikoIntermediateStagesDir, strconv.Itoa(index))
	logrus.Infof("Base image from previous stage %d found, using saved tar at path %s", index, tarPath)
	return tarball.ImageFromPath(tarPath, nil)
}

func ociImage(index int) (v1.Image, error) {
	tarPath := filepath.Join(config.KanikoIntermediateStagesDir, strconv.Itoa(index))
	logrus.Infof("Base image from previous stage %d found, using saved tar at path %s", index, tarPath)
	p, err := layout.FromPath(tarPath)
	if err != nil {
		return nil, err
	}
	idx, err := p.ImageIndex()
	if err != nil {
		return nil, err
	}
	idxManifest, err := idx.IndexManifest()
	if err != nil {
		return nil, err
	}

	if len(idxManifest.Manifests) == 0 {
		return nil, fmt.Errorf("no images found in OCI layout")
	}
	if len(idxManifest.Manifests) > 1 {
		return nil, fmt.Errorf("expected one image, found %d", len(idxManifest.Manifests))
	}

	hash := idxManifest.Manifests[0].Digest
	return p.Image(hash)
}

func cachedImage(opts *config.KanikoOptions, image string) (v1.Image, error) {
	ref, err := name.ParseReference(image, name.WeakValidation)
	if err != nil {
		return nil, err
	}

	// mz320: If we have a digest reference, we can try a cache lookup directly.
	var oldKey string
	var oldErr error
	if d, ok := ref.(name.Digest); ok {
		cacheKey := d.DigestStr()
		img, err := cache.LocalSource(&opts.CacheOptions, cacheKey)
		if err == nil {
			return img, nil
		} else if cache.IsNotFound(err) {
			// mz320: But in case it is a cache miss, not all hope is lost.
			// It could have also been the digest for an image index.
			// The thin wrapper that only points to the image-manifests for different archs.
			// Unfortunately we can't tell a priori and we only store the image manifests as keys.
			// Therefore we don't return and instead try a remote lookup again.
			oldKey = cacheKey
			oldErr = err
		} else {
			return nil, err
		}
	}
	img, err := remote.RetrieveRemoteImage(image, opts.RegistryOptions, opts.CustomPlatform)
	if err != nil {
		return nil, err
	}

	d, err := img.Digest()
	if err != nil {
		return nil, err
	}
	cacheKey := d.String()
	if oldKey != "" && cacheKey == oldKey {
		// mz320: But if the cacheKey didn't change, we indeed were looking
		// at an image manifest, we already confirmed it is not in cache,
		// so we can short-circuit with the previous error here.
		return nil, oldErr
	}
	return cache.LocalSource(&opts.CacheOptions, cacheKey)
}
