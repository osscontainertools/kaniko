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
	"github.com/osscontainertools/kaniko/pkg/cache"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/constants"
	"github.com/osscontainertools/kaniko/pkg/dockerfile"
	"github.com/osscontainertools/kaniko/pkg/image/remote"
	"github.com/sirupsen/logrus"
)

// for testing
var (
	Out          io.Writer        = os.Stdout
	RemoteSource FetchRemoteImage = remote.RetrieveRemoteImageAndIndex
	LocalSource  FetchLocalSource = cache.LocalSource
)

// WarmCache populates the cache
func WarmCache(opts *config.WarmerOptions) error {
	var dockerfileImages []string
	cacheDir := opts.CacheDir
	images := opts.Images

	if opts.RequireDigest {
		for _, image := range images {
			_, err := name.NewDigest(image)
			if err != nil {
				return fmt.Errorf("--require-digest: %w", err)
			}
		}
	}

	// if opts.image is empty,we need to parse dockerfilepath to get images list
	if opts.DockerfilePath != "" {
		var err error
		if config.FF.WarmerExecutorParity {
			dockerfileImages, err = DockerfileImages(opts)
		} else {
			dockerfileImages, err = ParseDockerfile(opts)
		}
		if err != nil {
			return fmt.Errorf("failed to parse Dockerfile: %w", err)
		}
	}

	var deduped []string
	for _, img := range slices.Concat(images, dockerfileImages) {
		if !slices.Contains(deduped, img) {
			deduped = append(deduped, img)
		}
	}
	images = deduped

	logrus.Debugf("%s\n", cacheDir)
	logrus.Debugf("%s\n", images)

	if opts.Dryrun || config.EnvBool("KANIKO_PRINT_PLAN") {
		err := RenderPlan(Out, images, opts)
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
			logrus.Warnf("Error while trying to warm image: %v %v", img, err)
			failed = append(failed, img)
		}
	}

	if len(failed) > 0 {
		if len(failed) == len(images) || config.FF.WarmerFailOnError {
			return fmt.Errorf("failed to warm %d/%d image(s): %s", len(failed), len(images), strings.Join(failed, ", "))
		}
		logrus.Warnf("Failed to warm %d/%d image(s): %s", len(failed), len(images), strings.Join(failed, ", "))
	}

	return nil
}

func RenderPlan(w io.Writer, images []string, opts *config.WarmerOptions) (retErr error) {
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
		printf("FROM %s\n", img)
		_, _, index, digest, err := cw.Resolve(img, opts)
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
			logrus.Warnf("Error while trying to warm image: %v %v", img, err)
			failed = append(failed, img)
		default:
			if !opts.Force {
				printf("  CACHE MISS: %s\n", digest)
			}
			if indexPath != "" {
				printf("  SAVE %s\n", indexPath)
			}
			printf("  FETCH %s\n", img)
			printf("  SAVE %s\n", path.Join(opts.CacheDir, digest.String()))
		}
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
func ociWarmToFile(cacheDir, img string, opts *config.WarmerOptions) error {
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

	cacheRef, image, index, digest, err := cw.Resolve(img, opts)
	if config.FF.CacheImageIndex && index != nil {
		storeErr := cache.StoreIndex(cacheDir, index)
		if storeErr != nil {
			return fmt.Errorf("failed to store image index: %w", storeErr)
		}
	}
	if err != nil {
		if cache.IsAlreadyCached(err) {
			logrus.Infof("Image already in cache: %v", img)
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

	_, lookupErr := cw.Local(&opts.CacheOptions, digest.String(), opts.CustomPlatform)
	if lookupErr == nil || cache.IsExpired(lookupErr) {
		logrus.Infof("Image %v became available in cache while waiting for lock; keeping existing copy", img)
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

	logrus.Debugf("Wrote %s to cache", img)
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
func (w *OciWarmer) Resolve(image string, opts *config.WarmerOptions) (name.Reference, v1.Image, []byte, v1.Hash, error) {
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
			_, err := w.Local(&opts.CacheOptions, cacheKey, opts.CustomPlatform)
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

	img, index, err := w.Remote(image, opts.RegistryOptions, opts.CustomPlatform)
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
			_, err = w.Local(&opts.CacheOptions, cacheKey, opts.CustomPlatform)
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

func DockerfileImages(opts *config.WarmerOptions) ([]string, error) {
	var predefinedArgs []string
	var err error
	if config.FF.FromPredefinedArgs {
		predefinedArgs, err = dockerfile.PredefinedArgs(opts.CustomPlatform)
		if err != nil {
			return nil, err
		}
	}
	stages, metaArgs, err := dockerfile.ParseStages(opts.DockerfilePath, predefinedArgs, opts.BuildArgs)
	if err != nil {
		return nil, err
	}
	kanikoStages, externalImages, err := dockerfile.MakeKanikoStages(&config.KanikoOptions{
		RegistryOptions: opts.RegistryOptions,
		CacheOptions:    opts.CacheOptions,
		Cache:           true,
		CustomPlatform:  opts.CustomPlatform,
		BuildArgs:       opts.BuildArgs,
		RequireDigest:   opts.RequireDigest,
		WarmerMode:      true,
	}, stages, metaArgs)
	if err != nil {
		return nil, err
	}

	var images []string
	for _, s := range kanikoStages {
		if s.BaseName != constants.NoBaseImage && !s.BaseImageStoredLocally && !slices.Contains(images, s.BaseName) {
			images = append(images, s.BaseName)
		}
	}
	if config.FF.CacheCopyFrom {
		for _, from := range externalImages {
			if !slices.Contains(images, from) {
				images = append(images, from)
			}
		}
	}
	return images, nil
}

// reachableStages computes which stages are reachable from the given target stages.
// A stage is reachable if:
// - It's a target stage
// - It's referenced as a base image (FROM) by a reachable stage
// - It's referenced via COPY --from by a reachable stage
// - It's referenced via RUN --mount=type=bind,from=<stage> by a reachable stage (if feature flag enabled)
func reachableStages(stages []instructions.Stage, targetIndices []int) map[int]bool {
	reachable := make(map[int]bool)
	stageByName := dockerfile.StageIndices(stages)

	// Add target stages to reachable
	for _, idx := range targetIndices {
		reachable[idx] = true
	}

	// Iteratively find dependencies until no new stages are discovered
	changed := true
	for changed {
		changed = false
		for i, stage := range stages {
			if !reachable[i] {
				continue // This stage is not reachable, skip it
			}

			// Check if this stage depends on another stage via FROM
			if stage.BaseName != constants.NoBaseImage {
				// BaseImageIndex finds the index of a stage that this stage is built off
				baseIdx := dockerfile.BaseImageIndex(i, stages)
				if baseIdx != -1 && !reachable[baseIdx] {
					reachable[baseIdx] = true
					changed = true
				}
			}

			// Check if this stage depends on other stages via COPY --from
			for _, cmd := range stage.Commands {
				c, ok := cmd.(*instructions.CopyCommand)
				if !ok || c.From == "" {
					continue
				}

				// Check if it's a named stage reference
				if stageIdx, exists := stageByName[strings.ToLower(c.From)]; exists {
					if !reachable[stageIdx] {
						reachable[stageIdx] = true
						changed = true
					}
				}
			}

			// Check if this stage depends on other stages via RUN --mount=type=bind,from=<stage>
			if config.FF.RunCrossStageBindMount {
				for _, cmd := range stage.Commands {
					runCmd, ok := cmd.(*instructions.RunCommand)
					if !ok {
						continue
					}
					for _, m := range dockerfile.CrossStageMounts(runCmd) {
						// m.From can be a stage name or stage index
						if stageIdx, exists := stageByName[strings.ToLower(m.From)]; exists {
							if !reachable[stageIdx] {
								reachable[stageIdx] = true
								changed = true
							}
						} else if idx, err := strconv.Atoi(m.From); err == nil && idx >= 0 && idx < len(stages) {
							// Handle numeric stage references
							if !reachable[idx] {
								reachable[idx] = true
								changed = true
							}
						}
					}
				}
			}
		}
	}

	return reachable
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

	// Compute which stages to include based on target
	var reachable map[int]bool
	if len(opts.Target) > 0 {
		targetIndices, err := dockerfile.TargetStages(stages, opts.Target)
		if err != nil {
			return nil, fmt.Errorf("error finding target stage: %w", err)
		}
		reachable = reachableStages(stages, targetIndices)
	}

	for i, s := range stages {
		// Skip stages not reachable from targets (if targets are specified)
		if len(opts.Target) > 0 && !reachable[i] {
			continue
		}

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
		for i, s := range stages {
			// Skip stages not reachable from targets (if targets are specified)
			if len(opts.Target) > 0 && !reachable[i] {
				continue
			}

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
