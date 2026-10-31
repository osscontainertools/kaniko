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
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/containerd/platforms"
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
	"github.com/osscontainertools/kaniko/pkg/util"
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

	if opts.Dryrun || config.EnvBool("KANIKO_PRINT_PLAN") {
		err := RenderPlan(Out, images, opts)
		if err != nil {
			return err
		}
		if opts.Dryrun {
			return nil
		}
	}

	errs := 0
	for _, img := range images {
		err := ociWarmToFile(cacheDir, img, opts)
		if err != nil {
			logrus.Warnf("Error while trying to warm image: %v %v", img, err)
			errs++
		}
	}

	if len(images) > 0 && len(images) == errs {
		return errors.New("failed to warm any of the given images")
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
	errs := 0
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
			errs++
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
	if retErr == nil && len(images) > 0 && len(images) == errs {
		return errors.New("failed to warm any of the given images")
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
		logrus.Warnf("Error while trying to warm image: %v %v", img, err)
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
		logrus.Warnf("Error while trying to warm image: %v %v", img, err)
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

func ParseDockerfile(opts *config.WarmerOptions) ([]string, error) {
	var err error
	var d []uint8
	var baseNames []string
	match, _ := regexp.MatchString("^https?://", opts.DockerfilePath)
	if match {
		resp, e := http.Get(opts.DockerfilePath) //nolint:noctx
		if e != nil {
			return nil, e
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("request failed: %s", resp.Status)
		}
		d, err = io.ReadAll(resp.Body)
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

	var prevArgs []string
	for i, marg := range metaArgs {
		for j, arg := range marg.Args {
			if arg.Value != nil {
				val, err := util.ResolveEnvironmentReplacement(*arg.Value, append(prevArgs, opts.BuildArgs...), false)
				if err != nil {
					return nil, fmt.Errorf("expanding meta ARGs: %w", err)
				}
				prevArgs = append(prevArgs, arg.Key+"="+val)
				arg.Value = &val
				metaArgs[i].Args[j] = arg
			}
		}
	}

	args := opts.BuildArgs
	if config.FF.WarmerBuildArgs {
		args = nil
	}
	if config.FF.WarmerPredefinedArgs {
		build := platforms.Normalize(platforms.DefaultSpec())
		target, err := platforms.Parse(opts.CustomPlatform)
		if err != nil {
			return nil, fmt.Errorf("failed to parse target platform %q: %w", opts.CustomPlatform, err)
		}
		args = append([]string{
			"BUILDPLATFORM=" + platforms.Format(build),
			"BUILDOS=" + build.OS,
			"BUILDOSVERSION=" + build.OSVersion,
			"BUILDARCH=" + build.Architecture,
			"BUILDVARIANT=" + build.Variant,
			"TARGETPLATFORM=" + opts.CustomPlatform,
			"TARGETOS=" + target.OS,
			"TARGETOSVERSION=" + target.OSVersion,
			"TARGETARCH=" + target.Architecture,
			"TARGETVARIANT=" + target.Variant,
		}, args...)
	}
	for _, marg := range metaArgs {
		for _, arg := range marg.Args {
			if !config.FF.WarmerPredefinedArgs || arg.Value != nil {
				args = append(args, fmt.Sprintf("%s=%s", arg.Key, arg.ValueString()))
			}
		}
	}
	if config.FF.WarmerBuildArgs {
		args = append(args, opts.BuildArgs...)
	}
outer:
	for i, s := range stages {
		resolvedBaseName, err := util.ResolveEnvironmentReplacement(s.BaseName, args, false)
		if err != nil {
			return nil, fmt.Errorf("resolving base name %s: %w", s.BaseName, err)
		}
		if resolvedBaseName == constants.NoBaseImage {
			continue
		}
		// skip stage references ie.
		// FROM base AS target
		for j := range i {
			if stages[j].Name == strings.ToLower(resolvedBaseName) {
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

	if config.FF.CacheCopyFrom {
		stageNames := make(map[string]bool)
		for _, s := range stages {
			if s.Name != "" {
				stageNames[s.Name] = true
			}
		}

		for _, s := range stages {
			for _, cmd := range s.Commands {
				c, ok := cmd.(*instructions.CopyCommand)
				if !ok || c.From == "" {
					continue
				}
				resolvedFrom, err := util.ResolveEnvironmentReplacement(c.From, args, false)
				if err != nil {
					return nil, fmt.Errorf("resolving COPY --from %s: %w", c.From, err)
				}
				if resolvedFrom == "" {
					continue
				}
				if _, err := strconv.Atoi(resolvedFrom); err == nil {
					// Numeric index referring to a previous stage
					continue
				}
				if stageNames[resolvedFrom] {
					// Named stage reference
					continue
				}
				// Deduplicate
				isDuplicate := false
				for _, x := range baseNames {
					if x == resolvedFrom {
						isDuplicate = true
						break
					}
				}
				if !isDuplicate {
					baseNames = append(baseNames, resolvedFrom)
				}
			}
		}
	}

	return baseNames, nil
}
