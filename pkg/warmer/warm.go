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

package warmer

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/osscontainertools/kaniko/pkg/cache"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/dockerfile"
	"github.com/osscontainertools/kaniko/pkg/image/remote"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/sirupsen/logrus"
)

// WarmCache populates the cache
func WarmCache(opts *config.WarmerOptions) error {
	var dockerfileImages []string
	cacheDir := opts.CacheDir
	images := opts.Images

	// if opts.image is empty,we need to parse dockerfilepath to get images list
	if opts.DockerfilePath != "" {
		var err error
		if dockerfileImages, err = ParseDockerfile(opts); err != nil {
			return fmt.Errorf("failed to parse Dockerfile: %w", err)
		}
	}

	// TODO: Implement deduplication logic later.
	images = append(images, dockerfileImages...)

	logrus.Debugf("%s\n", cacheDir)
	logrus.Debugf("%s\n", images)

	errs := 0
	if config.EnvBool("FF_KANIKO_OCI_WARMER") {
		for _, img := range images {
			err := ociWarmToFile(cacheDir, img, opts)
			if err != nil {
				logrus.Warnf("Error while trying to warm image: %v %v", img, err)
				errs++
			}
		}
	} else {
		for _, img := range images {
			err := warmToFile(cacheDir, img, opts)
			if err != nil {
				logrus.Warnf("Error while trying to warm image: %v %v", img, err)
				errs++
			}
		}
	}

	if len(images) == errs {
		return errors.New("failed to warm any of the given images")
	}

	return nil
}

// Download image in temporary files then move files to final destination
func warmToFile(cacheDir, img string, opts *config.WarmerOptions) error {
	f, err := os.CreateTemp(cacheDir, "warmingImage.*")
	if err != nil {
		return err
	}
	// defer called in reverse order
	defer os.Remove(f.Name())
	defer f.Close()

	mtfsFile, err := os.CreateTemp(cacheDir, "warmingManifest.*")
	if err != nil {
		return err
	}
	defer os.Remove(mtfsFile.Name())
	defer mtfsFile.Close()

	cw := &Warmer{
		Remote:         remote.RetrieveRemoteImage,
		Local:          cache.LocalSource,
		TarWriter:      f,
		ManifestWriter: mtfsFile,
	}

	digest, err := cw.Warm(img, opts)
	if err != nil {
		if cache.IsAlreadyCached(err) {
			logrus.Infof("Image already in cache: %v", img)
			return nil
		}
		logrus.Warnf("Error while trying to warm image: %v %v", img, err)
		return err
	}

	finalCachePath := path.Join(cacheDir, digest.String())
	finalMfstPath := finalCachePath + ".json"

	err = os.Rename(f.Name(), finalCachePath)
	if err != nil {
		return err
	}

	err = os.Rename(mtfsFile.Name(), finalMfstPath)
	if err != nil {
		return fmt.Errorf("failed to rename manifest file: %w", err)
	}

	logrus.Debugf("Wrote %s to cache", img)
	return nil
}

// Download image in temporary files then move files to final destination
func ociWarmToFile(cacheDir, img string, opts *config.WarmerOptions) error {
	tmp, err := os.MkdirTemp(cacheDir, "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	cw := &OciWarmer{
		Remote: remote.RetrieveRemoteImage,
		Local:  cache.LocalSource,
		TmpDir: tmp,
	}

	digest, err := cw.Warm(img, opts)
	if err != nil {
		if cache.IsAlreadyCached(err) {
			logrus.Infof("Image already in cache: %v", img)
			return nil
		}
		logrus.Warnf("Error while trying to warm image: %v %v", img, err)
		return err
	}

	finalCachePath := path.Join(cacheDir, digest.String())

	err = os.Rename(tmp, finalCachePath)
	if err != nil {
		return err
	}

	logrus.Debugf("Wrote %s to cache", img)
	return nil
}

// FetchRemoteImage retrieves a Docker image manifest from a remote source.
// github.com/GoogleContainerTools/kaniko/image/remote.RetrieveRemoteImage can be used as
// this type.
type FetchRemoteImage func(image string, opts config.RegistryOptions, customPlatform string) (v1.Image, error)

// FetchLocalSource retrieves a Docker image manifest from a local source.
// github.com/GoogleContainerTools/kaniko/cache.LocalSource can be used as
// this type.
type FetchLocalSource func(*config.CacheOptions, string) (v1.Image, error)

// Warmer is used to prepopulate the cache with a Docker image
type Warmer struct {
	Remote         FetchRemoteImage
	Local          FetchLocalSource
	TarWriter      io.Writer
	ManifestWriter io.Writer
}

// Warm retrieves a Docker image and populates the supplied buffer with the image content and manifest
// or returns an AlreadyCachedErr if the image is present in the cache.
func (w *Warmer) Warm(image string, opts *config.WarmerOptions) (v1.Hash, error) {
	cacheRef, err := name.ParseReference(image, name.WeakValidation)
	if err != nil {
		return v1.Hash{}, fmt.Errorf("failed to verify image name: %s: %w", image, err)
	}

	// mz320: If we have a digest reference, we can try a cache lookup directly.
	var oldKey string
	var oldErr error
	if !opts.Force {
		if d, ok := cacheRef.(name.Digest); ok {
			cacheKey := d.DigestStr()
			_, err := w.Local(&opts.CacheOptions, cacheKey)
			if err == nil || cache.IsExpired(err) {
				return v1.Hash{}, cache.AlreadyCachedErr{}
			} else {
				// mz320: But in case it is a cache miss, not all hope is lost.
				// It could have also been the digest for an image-index.
				// The thin wrapper that only points to the image-manifests for different archs.
				// Unfortunately we can't tell a-priori and we only store the image manifests as keys.
				// Therefore we don't return and instead try a remote lookup again.
				oldKey = cacheKey
				oldErr = err
			}
		}
	}

	img, err := w.Remote(image, opts.RegistryOptions, opts.CustomPlatform)
	if err != nil || img == nil {
		return v1.Hash{}, fmt.Errorf("failed to retrieve image: %s: %w", image, err)
	}

	digest, err := img.Digest()
	if err != nil {
		return v1.Hash{}, fmt.Errorf("failed to retrieve digest: %s: %w", image, err)
	}

	if !opts.Force {
		var err error
		cacheKey := digest.String()
		if oldKey != "" && cacheKey == oldKey {
			// mz320: But if the cacheKey didn't change, we indeed were looking
			// at an image manifest, we already confirmed it is not in cache,
			// so we can short-circuit with the previous error here.
			err = oldErr
		} else {
			_, err = w.Local(&opts.CacheOptions, cacheKey)
		}
		if err == nil || cache.IsExpired(err) {
			return v1.Hash{}, cache.AlreadyCachedErr{}
		}
	}

	err = tarball.Write(cacheRef, img, w.TarWriter)
	if err != nil {
		return v1.Hash{}, fmt.Errorf("failed to write %s to tar buffer: %w", image, err)
	}

	mfst, err := img.RawManifest()
	if err != nil {
		return v1.Hash{}, fmt.Errorf("failed to retrieve manifest for %s: %w", image, err)
	}

	if _, err := w.ManifestWriter.Write(mfst); err != nil {
		return v1.Hash{}, fmt.Errorf("failed to save manifest to buffer for %s: %w", image, err)
	}

	return digest, nil
}

type OciWarmer struct {
	Remote FetchRemoteImage
	Local  FetchLocalSource
	TmpDir string
}

// Warm retrieves a Docker image and populates the supplied buffer with the image content and manifest
// or returns an AlreadyCachedErr if the image is present in the cache.
func (w *OciWarmer) Warm(image string, opts *config.WarmerOptions) (v1.Hash, error) {
	cacheRef, err := name.ParseReference(image, name.WeakValidation)
	if err != nil {
		return v1.Hash{}, fmt.Errorf("failed to verify image name: %s: %w", image, err)
	}

	// mz320: If we have a digest reference, we can try a cache lookup directly.
	var oldKey string
	var oldErr error
	if !opts.Force {
		if d, ok := cacheRef.(name.Digest); ok {
			cacheKey := d.DigestStr()
			_, err := w.Local(&opts.CacheOptions, cacheKey)
			if err == nil || cache.IsExpired(err) {
				return v1.Hash{}, cache.AlreadyCachedErr{}
			} else {
				// mz320: But in case it is a cache miss, not all hope is lost.
				// It could have also been the digest for an image-index.
				// The thin wrapper that only points to the image-manifests for different archs.
				// Unfortunately we can't tell a-priori and we only store the image manifests as keys.
				// Therefore we don't return and instead try a remote lookup again.
				oldKey = cacheKey
				oldErr = err
			}
		}
	}

	img, err := w.Remote(image, opts.RegistryOptions, opts.CustomPlatform)
	if err != nil || img == nil {
		return v1.Hash{}, fmt.Errorf("failed to retrieve image: %s: %w", image, err)
	}

	digest, err := img.Digest()
	if err != nil {
		return v1.Hash{}, fmt.Errorf("failed to retrieve digest: %s: %w", image, err)
	}

	if !opts.Force {
		var err error
		cacheKey := digest.String()
		if oldKey != "" && cacheKey == oldKey {
			// mz320: But if the cacheKey didn't change, we indeed were looking
			// at an image manifest, we already confirmed it is not in cache,
			// so we can short-circuit with the previous error here.
			err = oldErr
		} else {
			_, err = w.Local(&opts.CacheOptions, cacheKey)
		}
		if err == nil || cache.IsExpired(err) {
			return v1.Hash{}, cache.AlreadyCachedErr{}
		}
	}

	p, err := layout.Write(w.TmpDir, empty.Index)
	if err != nil {
		return v1.Hash{}, fmt.Errorf("failed to create ocilayout for: %s: %w", image, err)
	}

	err = p.AppendImage(img, layout.WithAnnotations(map[string]string{
		"org.opencontainers.image.ref.name": cacheRef.Name(),
	}))
	if err != nil {
		return v1.Hash{}, fmt.Errorf("failed to append image %s to ocilayout: %w", image, err)
	}

	return digest, nil
}

func ParseDockerfile(opts *config.WarmerOptions) ([]string, error) {
	var err error
	var d []uint8
	var baseNames []string
	match, _ := regexp.MatchString("^https?://", opts.DockerfilePath)
	if match {
		response, e := http.Get(opts.DockerfilePath) //nolint:noctx
		if e != nil {
			return nil, e
		}
		d, err = io.ReadAll(response.Body)
	} else {
		d, err = os.ReadFile(opts.DockerfilePath)
	}

	if err != nil {
		return nil, fmt.Errorf("reading dockerfile at path %s: %w", opts.DockerfilePath, err)
	}

	stages, metaArgs, err := dockerfile.Parse(d)
	if err != nil {
		return nil, fmt.Errorf("parsing dockerfile: %w", err)
	}

	args := opts.BuildArgs
	for _, marg := range metaArgs {
		for _, arg := range marg.Args {
			args = append(args, fmt.Sprintf("%s=%s", arg.Key, arg.ValueString()))
		}
	}
outer:
	for i, s := range stages {
		resolvedBaseName, err := util.ResolveEnvironmentReplacement(s.BaseName, args, false)
		if err != nil {
			return nil, fmt.Errorf("resolving base name %s: %w", s.BaseName, err)
		}
		// skip stage references ie.
		// FROM base AS target
		for j := range i {
			if stages[j].Name == resolvedBaseName {
				continue outer
			}
		}
		// deduplicate
		for _, x := range baseNames {
			if x == resolvedBaseName {
				continue outer
			}
		}
		baseNames = append(baseNames, resolvedBaseName)
	}
	return baseNames, nil
}
