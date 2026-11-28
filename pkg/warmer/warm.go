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
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/opencontainers/go-digest"
	"github.com/osscontainertools/kaniko/pkg/addcache"
	"github.com/osscontainertools/kaniko/pkg/cache"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/constants"
	"github.com/osscontainertools/kaniko/pkg/dockerfile"
	"github.com/osscontainertools/kaniko/pkg/executor"
	"github.com/osscontainertools/kaniko/pkg/image/remote"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/sirupsen/logrus"
)

// for testing
var (
	Out             io.Writer        = os.Stdout
	RemoteSource    FetchRemoteImage = remote.RetrieveRemoteImageAndIndex
	LocalSource     FetchLocalSource = cache.LocalSource
	AddSourceLookup                  = addcache.Lookup
)

// WarmImage is an image reference fetched for one platform.
type WarmImage struct {
	Ref      string
	Platform string
}

// WarmCache populates the cache
func WarmCache(opts *config.WarmerOptions) error {
	var dockerfileImages []WarmImage
	var addSources []addcache.Source
	cacheDir := opts.CacheDir
	var images []WarmImage

	for _, image := range opts.Images {
		if opts.RequireDigest {
			_, err := name.NewDigest(image)
			if err != nil {
				return fmt.Errorf("--require-digest: %w", err)
			}
		}
		images = append(images, WarmImage{Ref: image, Platform: opts.CustomPlatform})
	}

	// if opts.image is empty,we need to parse dockerfilepath to get images list
	if opts.DockerfilePath != "" {
		var err error
		switch {
		case config.FF.WarmerExecutorParity:
			dockerfileImages, addSources, err = DockerfileImages(opts)
		case len(opts.Target) > 0:
			err = fmt.Errorf("--target requires FF_KANIKO_WARMER_EXECUTOR_PARITY=true")
		default:
			var baseNames []string
			baseNames, err = ParseDockerfile(opts)
			for _, baseName := range baseNames {
				dockerfileImages = append(dockerfileImages, WarmImage{Ref: baseName, Platform: opts.CustomPlatform})
			}
		}
		if err != nil {
			return fmt.Errorf("failed to parse Dockerfile: %w", err)
		}
	}

	var deduped []WarmImage
	for _, img := range slices.Concat(images, dockerfileImages) {
		if !slices.Contains(deduped, img) {
			deduped = append(deduped, img)
		}
	}
	images = deduped

	logrus.Debugf("%s\n", cacheDir)
	logrus.Debugf("%s\n", images)

	if opts.Dryrun || config.EnvBool("KANIKO_PRINT_PLAN") {
		err := RenderPlan(Out, images, addSources, opts)
		if err != nil {
			return err
		}
		if opts.Dryrun {
			return nil
		}
	}

	var failed []string
	for _, img := range images {
		if err := ociWarmToFile(cacheDir, img, opts); err != nil {
			logrus.Warnf("Error while trying to warm image: %v %v", img.Ref, err)
			failed = append(failed, img.Ref)
		}
	}

	var addFailed []string
	for _, source := range addSources {
		_, err := addcache.Store(cacheDir, source.URL, source.Checksum, opts.Force)
		if err != nil {
			logrus.Warnf("Error while trying to cache ADD source: %v %v", source.URL, err)
			addFailed = append(addFailed, source.URL)
		} else {
			logrus.Infof("Cached ADD source: %v", source.URL)
		}
	}

	var errs []error
	if len(addFailed) > 0 {
		if len(addFailed) == len(addSources) || config.FF.WarmerFailOnError {
			errs = append(errs, fmt.Errorf("failed to cache %d/%d ADD source(s): %s", len(addFailed), len(addSources), strings.Join(addFailed, ", ")))
		} else {
			logrus.Warnf("Failed to cache %d/%d ADD source(s): %s", len(addFailed), len(addSources), strings.Join(addFailed, ", "))
		}
	}
	if len(failed) > 0 {
		if len(failed) == len(images) || config.FF.WarmerFailOnError {
			errs = append(errs, fmt.Errorf("failed to warm %d/%d image(s): %s", len(failed), len(images), strings.Join(failed, ", ")))
		} else {
			logrus.Warnf("Failed to warm %d/%d image(s): %s", len(failed), len(images), strings.Join(failed, ", "))
		}
	}

	return errors.Join(errs...)
}

func RenderPlan(w io.Writer, images []WarmImage, addSources []addcache.Source, opts *config.WarmerOptions) (retErr error) {
	printf := func(format string, args ...any) {
		if retErr == nil {
			_, retErr = fmt.Fprintf(w, format, args...)
		}
	}

	cw := &OciWarmer{
		Remote: RemoteSource,
		Local:  LocalSource,
	}
	var failed []string
	for _, img := range images {
		if img.Platform == opts.CustomPlatform {
			printf("FROM %s\n", img.Ref)
		} else {
			printf("FROM --platform=%s %s\n", img.Platform, img.Ref)
		}
		_, _, index, digest, err := cw.Resolve(img.Ref, img.Platform, opts)
		var indexPath string
		if config.FF.CacheImageIndex && index != nil {
			indexDigest, _, _ := v1.SHA256(bytes.NewReader(index))
			indexPath = path.Join(opts.CacheDir, indexDigest.String()+".index")
		}
		switch {
		case cache.IsAlreadyCached(err):
			printf("  CACHE HIT: %s\n", digest)
			if indexPath != "" {
				printf("  SAVE %s\n", indexPath)
			}
		case err != nil:
			logrus.Warnf("Error while trying to warm image: %v %v", img.Ref, err)
			failed = append(failed, img.Ref)
		default:
			if !opts.Force {
				printf("  CACHE MISS: %s\n", digest)
			}
			if indexPath != "" {
				printf("  SAVE %s\n", indexPath)
			}
			printf("  FETCH %s\n", img.Ref)
			printf("  SAVE %s\n", path.Join(opts.CacheDir, digest.String()))
		}
	}
	for _, source := range addSources {
		cachePath := addcache.Path(opts.CacheDir, source.Checksum)
		printf("ADD %s\n", source.URL)
		if !opts.Force {
			_, ok, err := AddSourceLookup(opts.CacheDir, source.Checksum)
			if err != nil {
				logrus.Warnf("Replacing ADD cache entry for %s: %v", source.URL, err)
			}
			if ok {
				printf("  CACHE HIT: %s\n", cachePath)
				continue
			}
		}
		printf("  CACHE MISS: %s\n", cachePath)
		printf("  FETCH %s\n", source.URL)
		printf("  SAVE %s\n", cachePath)
	}
	if len(failed) > 0 {
		// Without --dryrun the warm that follows reports the failures, after it warmed every image that resolves.
		if retErr == nil && (len(failed) == len(images) || (config.FF.WarmerFailOnError && opts.Dryrun)) {
			return fmt.Errorf("failed to warm %d/%d image(s): %s", len(failed), len(images), strings.Join(failed, ", "))
		}
		logrus.Warnf("Failed to warm %d/%d image(s): %s", len(failed), len(images), strings.Join(failed, ", "))
	}
	return retErr
}

// Download image in temporary files then move files to final destination
func ociWarmToFile(cacheDir string, img WarmImage, opts *config.WarmerOptions) error {
	tmp, err := os.MkdirTemp(cacheDir, "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	cw := &OciWarmer{
		Remote: RemoteSource,
		Local:  LocalSource,
		TmpDir: tmp,
	}

	cacheRef, image, index, digest, err := cw.Resolve(img.Ref, img.Platform, opts)
	if config.FF.CacheImageIndex && index != nil {
		storeErr := cache.StoreIndex(cacheDir, index)
		if storeErr != nil {
			return fmt.Errorf("failed to store image index: %w", storeErr)
		}
	}
	if err != nil {
		if cache.IsAlreadyCached(err) {
			logrus.Infof("Image already in cache: %v", img.Ref)
			return nil
		}
		return err
	}

	finalCachePath := path.Join(cacheDir, digest.String())

	lock, err := acquireCacheLock(cacheDir, digest.String())
	if err != nil {
		return fmt.Errorf("failed to acquire cache lock: %w", err)
	}
	defer lock.Release()

	_, lookupErr := cw.Local(&opts.CacheOptions, digest.String(), img.Platform)
	if lookupErr == nil || cache.IsExpired(lookupErr) {
		logrus.Infof("Image %v became available in cache while waiting for lock; keeping existing copy", img.Ref)
		return nil
	}
	_ = os.RemoveAll(finalCachePath)

	if err := cw.Write(cacheRef, image); err != nil {
		return err
	}

	err = os.Rename(tmp, finalCachePath)
	if err != nil {
		return err
	}

	logrus.Debugf("Wrote %s to cache", img.Ref)
	return nil
}

// FetchRemoteImage retrieves a Docker image manifest from a remote source.
// github.com/GoogleContainerTools/kaniko/image/remote.RetrieveRemoteImageAndIndex can be used as
// this type.
type FetchRemoteImage func(image string, opts config.RegistryOptions, customPlatform string) (v1.Image, []byte, error)

// FetchLocalSource retrieves a Docker image manifest from a local source.
// github.com/GoogleContainerTools/kaniko/cache.LocalSource can be used as
// this type.
type FetchLocalSource func(*config.CacheOptions, string, string) (v1.Image, error)

type OciWarmer struct {
	Remote FetchRemoteImage
	Local  FetchLocalSource
	TmpDir string
}

// Resolve fetches the image manifest and resolves its digest, short-circuiting
// with AlreadyCachedErr if the local cache already holds it.
func (w *OciWarmer) Resolve(image, platform string, opts *config.WarmerOptions) (name.Reference, v1.Image, []byte, v1.Hash, error) {
	cacheRef, err := name.ParseReference(image, name.WeakValidation)
	if err != nil {
		return nil, nil, nil, v1.Hash{}, fmt.Errorf("failed to verify image name: %s: %w", image, err)
	}

	// mz320: If we have a digest reference, we can try a cache lookup directly.
	var oldKey string
	var oldErr error
	if !opts.Force {
		if d, ok := cacheRef.(name.Digest); ok {
			cacheKey := d.DigestStr()
			_, err := w.Local(&opts.CacheOptions, cacheKey, platform)
			if err == nil || cache.IsExpired(err) {
				digest, _ := v1.NewHash(cacheKey)
				return nil, nil, nil, digest, cache.AlreadyCachedErr{}
			} else {
				// mz320: But in case it is a cache miss, not all hope is lost.
				// It could have also been the digest for an image-index.
				// The thin wrapper that only points to the image-manifests for different archs.
				// Unless the index itself is cached, we can't tell a-priori.
				// Therefore we don't return and instead try a remote lookup again.
				oldKey = cacheKey
				oldErr = err
			}
		}
	}

	img, index, err := w.Remote(image, opts.RegistryOptions, platform)
	if err != nil || img == nil {
		return nil, nil, nil, v1.Hash{}, fmt.Errorf("failed to retrieve image: %s: %w", image, err)
	}

	digest, err := img.Digest()
	if err != nil {
		return nil, nil, nil, v1.Hash{}, fmt.Errorf("failed to retrieve digest: %s: %w", image, err)
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
			_, err = w.Local(&opts.CacheOptions, cacheKey, platform)
		}
		if err == nil || cache.IsExpired(err) {
			return nil, nil, index, digest, cache.AlreadyCachedErr{}
		}
	}

	return cacheRef, img, index, digest, nil
}

// Write materializes the image as an ocilayout under TmpDir.
func (w *OciWarmer) Write(cacheRef name.Reference, img v1.Image) error {
	p, err := layout.Write(w.TmpDir, empty.Index)
	if err != nil {
		return fmt.Errorf("failed to create ocilayout for: %s: %w", cacheRef.String(), err)
	}

	err = p.AppendImage(img, layout.WithAnnotations(map[string]string{
		"org.opencontainers.image.ref.name": cacheRef.Name(),
	}))
	if err != nil {
		return fmt.Errorf("failed to append image %s to ocilayout: %w", cacheRef.String(), err)
	}

	return nil
}

func DockerfileImages(opts *config.WarmerOptions) ([]WarmImage, []addcache.Source, error) {
	var predefinedArgs []string
	var err error
	if config.FF.FromPredefinedArgs {
		predefinedArgs, err = dockerfile.PredefinedArgs(opts.CustomPlatform)
		if err != nil {
			return nil, nil, err
		}
	}
	stages, metaArgs, err := dockerfile.ParseStages(opts.DockerfilePath, predefinedArgs, opts.BuildArgs)
	if err != nil {
		return nil, nil, err
	}
	kanikoOpts := &config.KanikoOptions{
		RegistryOptions: opts.RegistryOptions,
		CacheOptions:    opts.CacheOptions,
		Cache:           true,
		CustomPlatform:  opts.CustomPlatform,
		BuildArgs:       opts.BuildArgs,
		RequireDigest:   opts.RequireDigest,
		Target:          opts.Target,
		WarmerMode:      true,
	}
	kanikoStages, externalImages, err := dockerfile.MakeKanikoStages(kanikoOpts, stages, metaArgs)
	if err != nil {
		return nil, nil, err
	}

	var images []WarmImage
	for _, s := range kanikoStages {
		image := WarmImage{Ref: s.BaseName, Platform: s.Platform}
		if s.BaseName != constants.NoBaseImage && !s.BaseImageStoredLocally && !slices.Contains(images, image) {
			images = append(images, image)
		}
	}
	if config.FF.CacheCopyFrom {
		for _, from := range externalImages {
			image := WarmImage{Ref: from, Platform: opts.CustomPlatform}
			if !slices.Contains(images, image) {
				images = append(images, image)
			}
		}
	}

	var addSources []addcache.Source
	if config.FF.CacheAddSources && config.FF.AddChecksum && opts.AddCache {
		_, _, _, sources, err := executor.CalculateDependencies(kanikoStages, kanikoOpts, util.FileContext{})
		if err != nil {
			return nil, nil, fmt.Errorf("resolving ADD sources: %w", err)
		}
		for _, source := range sources {
			if source.Checksum == "" || source.Checksum.Algorithm() != digest.SHA256 {
				logrus.Infof("Not caching ADD source %s without a sha256 --checksum", source.URL)
			} else if !slices.ContainsFunc(addSources, func(s addcache.Source) bool { return s.Checksum == source.Checksum }) {
				addSources = append(addSources, source)
			}
		}
	}
	return images, addSources, nil
}

func ParseDockerfile(opts *config.WarmerOptions) ([]string, error) {
	var baseNames []string
	var predefinedArgs []string
	var err error
	if config.FF.WarmerPredefinedArgs {
		predefinedArgs, err = dockerfile.PredefinedArgs(opts.CustomPlatform)
		if err != nil {
			return nil, err
		}
	}
	stages, metaArgs, err := dockerfile.ParseStages(opts.DockerfilePath, predefinedArgs, opts.BuildArgs)
	if err != nil {
		return nil, err
	}

	args := slices.Clone(predefinedArgs)
	if config.FF.WarmerBuildArgs {
		args = append(args, dockerfile.UnifyArgs(metaArgs, opts.BuildArgs)...)
	} else {
		args = append(args, opts.BuildArgs...)
		for _, marg := range metaArgs {
			for _, arg := range marg.Args {
				if !config.FF.WarmerPredefinedArgs || arg.Value != nil {
					args = append(args, fmt.Sprintf("%s=%s", arg.Key, arg.ValueString()))
				}
			}
		}
	}
	err = dockerfile.ResolveStagesArgs(stages, args)
	if err != nil {
		return nil, err
	}
	for i, s := range stages {
		if s.BaseName != constants.NoBaseImage && dockerfile.BaseImageIndex(i, stages) == -1 {
			if opts.RequireDigest {
				_, err = name.NewDigest(s.BaseName)
				if err != nil {
					return nil, fmt.Errorf("--require-digest: %w", err)
				}
			}
			if !slices.Contains(baseNames, s.BaseName) {
				baseNames = append(baseNames, s.BaseName)
			}
		}
	}

	if config.FF.CacheCopyFrom {
		stageByName := dockerfile.StageIndices(stages)
		for _, s := range stages {
			for _, cmd := range s.Commands {
				c, ok := cmd.(*instructions.CopyCommand)
				if !ok || c.From == "" {
					continue
				}
				// skip stage references ie.
				// COPY --from=base and COPY --from=0
				if _, ok := stageByName[strings.ToLower(c.From)]; ok {
					continue
				}
				if _, err := strconv.Atoi(c.From); err == nil {
					continue
				}
				// deduplicate
				if slices.Contains(baseNames, c.From) {
					continue
				}
				baseNames = append(baseNames, c.From)
			}
		}
	}

	return baseNames, nil
}
