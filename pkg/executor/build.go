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

package executor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/osscontainertools/kaniko/pkg/assert"
	"github.com/osscontainertools/kaniko/pkg/cache"
	"github.com/osscontainertools/kaniko/pkg/commands"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/constants"
	"github.com/osscontainertools/kaniko/pkg/dockerfile"
	image_util "github.com/osscontainertools/kaniko/pkg/image"
	"github.com/osscontainertools/kaniko/pkg/image/remote"
	"github.com/osscontainertools/kaniko/pkg/mounts"
	"github.com/osscontainertools/kaniko/pkg/snapshot"
	"github.com/osscontainertools/kaniko/pkg/timing"
	"github.com/osscontainertools/kaniko/pkg/tracing"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// for testing
var (
	initializeConfig             = initConfig
	getFSFromImage               = util.GetFSFromImage
	mkdirPermissions os.FileMode = 0o755
	pushCache                    = pushLayerToCache
	pushPointer                  = pushCachePointer
	NewLayerCache                = newLayerCacheImpl
)

type snapShotter interface {
	Init() error
	TakeSnapshotFS() (string, int, error)
	TakeSnapshot([]string, bool) (string, int, error)
}

// stageBuilder contains all fields necessary to build one stage of a Dockerfile
type stageBuilder struct {
	index           int
	final           bool
	image           v1.Image
	cf              *v1.ConfigFile
	baseImageDigest string
	cmds            []commands.DockerCommand
	lines           []int // source line per command, aligned with cmds
	args            *dockerfile.BuildArgs
	span            trace.Span
}

type stageCacheInfo struct {
	redirectKeys []string
	redirectHits []bool
	cacheKeys    []string
	cacheHits    []bool
}

// mz334: memoizedLayerCache pins retrieved layers in memory so the build pass resolves
// a key to the same layer precompute did, since elimination is irreversible and
// a stage dropped as cached cannot be rebuilt if a later lookup misses.
type memoizedLayerCache struct {
	inner  cache.LayerCache
	images map[string]v1.Image
}

func newMemoizedLayerCache(inner cache.LayerCache) *memoizedLayerCache {
	return &memoizedLayerCache{inner: inner, images: map[string]v1.Image{}}
}

func (m *memoizedLayerCache) RetrieveLayer(key string) (v1.Image, error) {
	img, ok := m.images[key]
	if ok {
		return img, nil
	}
	img, err := m.inner.RetrieveLayer(key)
	if err != nil {
		return nil, err
	}
	m.images[key] = img
	return img, nil
}

func (m *memoizedLayerCache) has(key string) bool {
	_, ok := m.images[key]
	return ok
}

func mergeStageCacheInfo(base *stageCacheInfo, baseCmds []instructions.Command, child *stageCacheInfo) *stageCacheInfo {
	merged := &stageCacheInfo{}
	for j, c := range baseCmds {
		_, isOnbuild := c.(*instructions.OnbuildCommand)
		if !isOnbuild {
			merged.redirectKeys = append(merged.redirectKeys, base.redirectKeys[j])
			merged.redirectHits = append(merged.redirectHits, base.redirectHits[j])
			merged.cacheKeys = append(merged.cacheKeys, base.cacheKeys[j])
			merged.cacheHits = append(merged.cacheHits, base.cacheHits[j])
		}
	}
	merged.redirectKeys = append(merged.redirectKeys, child.redirectKeys...)
	merged.redirectHits = append(merged.redirectHits, child.redirectHits...)
	merged.cacheKeys = append(merged.cacheKeys, child.cacheKeys...)
	merged.cacheHits = append(merged.cacheHits, child.cacheHits...)
	return merged
}

func commandHash(stage int, cmd string) string {
	sum := sha256.Sum256([]byte(strconv.Itoa(stage) + "|" + cmd))
	return hex.EncodeToString(sum[:])
}

func commandLine(cmd instructions.Command) int {
	loc := cmd.Location()
	if len(loc) == 0 {
		return 0
	}
	return loc[0].Start.Line
}

func makeSnapshotter(opts *config.KanikoOptions) (*snapshot.Snapshotter, error) {
	hasher, err := getHasher(opts.SnapshotMode)
	if err != nil {
		return nil, err
	}
	l := snapshot.NewLayeredMap(hasher)
	return snapshot.NewSnapshotter(l, config.RootDir), nil
}

// newStageBuilder returns a new type stageBuilder which contains all the information required to build the stage
func newStageBuilder(sourceImage v1.Image, args *dockerfile.BuildArgs, opts *config.KanikoOptions, stage config.KanikoStage, fileContext util.FileContext) (*stageBuilder, error) {
	_opts := *opts
	if !stage.Push {
		_opts.Labels = []string{}
	}
	sourceImage, err := applyImageFormat(sourceImage, opts.ImageFormat)
	if err != nil {
		return nil, err
	}
	imageConfig, err := initializeConfig(sourceImage, &_opts)
	if err != nil {
		return nil, err
	}

	// mz507: This workaround to prevent cache invalidation via base image annotations
	// can be removed once FF_KANIKO_NO_PROPAGATE_ANNOTATIONS becomes standard.
	man, err := sourceImage.Manifest()
	if err != nil {
		return nil, err
	}
	ann := map[string]string{}
	for k := range man.Annotations {
		ann[k] = ""
	}

	cf, err := sourceImage.ConfigFile()
	if err != nil {
		return nil, err
	}
	cfg := *cf
	cfg.Created = v1.Time{}
	cfg.Config.Labels = map[string]string{}
	sourceImageReproducible, err := mutate.ConfigFile(sourceImage, &cfg)
	if err != nil {
		return nil, err
	}

	sourceImageReproducible = mutate.Annotations(sourceImageReproducible, ann).(v1.Image)
	digest, err := sourceImageReproducible.Digest()
	if err != nil {
		return nil, err
	}
	s := &stageBuilder{
		index:           stage.Index,
		final:           stage.Final,
		image:           sourceImage,
		cf:              imageConfig,
		baseImageDigest: digest.String(),
		args:            args.Clone(),
	}

	for _, cmd := range stage.Commands {
		command, err := commands.GetCommand(cmd, fileContext, opts.Secrets, opts.RunV2, opts.CacheCopyLayers, opts.CacheRunLayers)
		if err != nil {
			return nil, err
		}
		s.cmds = append(s.cmds, command)
		s.lines = append(s.lines, commandLine(cmd))
	}
	s.args.AddMetaArgs(stage.MetaArgs)
	return s, nil
}

func initConfig(img partial.WithConfigFile, opts *config.KanikoOptions) (*v1.ConfigFile, error) {
	imageConfig, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}

	if imageConfig.Config.Env == nil {
		imageConfig.Config.Env = constants.ScratchEnvVars
	}
	// Blank out the Onbuild command list for this image
	imageConfig.Config.OnBuild = nil

	if opts == nil {
		return imageConfig, nil
	}

	if l := len(opts.Labels); l > 0 {
		if imageConfig.Config.Labels == nil {
			imageConfig.Config.Labels = make(map[string]string)
		}
		for _, label := range opts.Labels {
			parts := strings.SplitN(label, "=", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("labels must be of the form key=value, got %s", label)
			}

			imageConfig.Config.Labels[parts[0]] = parts[1]
		}
	}

	assert.Assert("executor.initconfig.env-nonnull", imageConfig.Config.Env != nil, "initConfig: Env must be non-nil on return")
	return imageConfig, nil
}

func newLayerCacheImpl(opts *config.KanikoOptions) cache.LayerCache {
	if isOCILayout(opts.CacheRepo) {
		return &cache.LayoutCache{
			Opts: opts,
		}
	}
	return &cache.RegistryCache{
		Opts: opts,
	}
}

func isOCILayout(path string) bool {
	return strings.HasPrefix(path, "oci:")
}

func crossStageCacheKey(command commands.DockerCommand, stageFinalCacheKeys map[int]string, externalImageDigests map[string]string) (string, bool) {
	copyCmd, ok := commands.CastAbstractCopyCommand(command)
	if !ok || copyCmd.From() == "" {
		return "", false
	}
	fromIdx, err := strconv.Atoi(copyCmd.From())
	if err != nil {
		digest, ok := externalImageDigests[copyCmd.From()]
		return digest, ok
	}
	cacheKey, ok := stageFinalCacheKeys[fromIdx]
	return cacheKey, ok
}

func populateCompositeKey(command commands.DockerCommand, files []string, compositeKey CompositeCache, args *dockerfile.BuildArgs, env []string, fileContext util.FileContext, stageFinalCacheKeys map[int]string, externalImageDigests map[string]string) (CompositeCache, error) {
	assert.Assert("executor.compositekey.mutual-exclusion", files == nil || stageFinalCacheKeys == nil, "populateCompositeKey: files and stageFinalCacheKeys are mutually exclusive")
	assert.Assert("executor.compositekey.command-nonnull", command != nil, "populateCompositeKey called with nil command")
	// First replace all the environment variables or args in the command
	replacementEnvs := args.ReplacementEnvs(env)
	// The sort order of `replacementEnvs` is basically undefined, sort it
	// so we can ensure a stable cache key.
	sort.Strings(replacementEnvs)
	// Use the special argument "|#" at the start of the args array. This will
	// avoid conflicts with any RUN command since commands can not
	// start with | (vertical bar). The "#" (number of build envs) is there to
	// help ensure proper cache matches.

	if command.IsArgsEnvsRequiredInCache() {
		if len(replacementEnvs) > 0 {
			compositeKey.AddKey(fmt.Sprintf("|%d", len(replacementEnvs)))
			compositeKey.AddKey(replacementEnvs...)
		}
	}

	// Add the next command to the cache key.
	keyString := command.String()
	resolver, ok := command.(commands.CacheKeyResolver)
	if ok && config.FF.ResolveCacheKey {
		resolved, err := resolver.CacheKey(replacementEnvs)
		if err != nil {
			return compositeKey, fmt.Errorf("resolving cache key: %w", err)
		}
		keyString = resolved
	}
	compositeKey.AddKey(keyString)

	if stageFinalCacheKeys != nil {
		// mz334: COPY --from shortcut — use the source stage's cache key or the external image digest instead of hashing files.
		cacheKey, ok := crossStageCacheKey(command, stageFinalCacheKeys, externalImageDigests)
		if ok {
			compositeKey.AddKey(cacheKey)
			return compositeKey, nil
		}
		return compositeKey, fmt.Errorf("shortcut key not found")
	} else if files != nil {
		for _, f := range files {
			if err := compositeKey.AddPath(f, fileContext); err != nil {
				return compositeKey, err
			}
		}
		return compositeKey, nil
	}

	assert.Unreachable("populateCompositeKey: both files and stageFinalCacheKeys are nil")
	return compositeKey, nil
}

func redirectCacheKey(inferredKey CompositeCache, layerCache cache.LayerCache) (*CompositeCache, error) {
	inferredCk, err := inferredKey.Hash()
	if err != nil {
		return nil, err
	}
	ptrImg, err := layerCache.RetrieveLayer(inferredCk)
	if err != nil {
		logrus.Debugf("Failed to retrieve pointer: %s", err)
		logrus.Debugf("Key missing was: %s", inferredKey.Key())
		return nil, nil
	}
	rawKey, ok := resolveCachePointer(ptrImg)
	if !ok {
		return nil, fmt.Errorf("failed resolving cache pointer")
	}
	return ResumeCompositeCache(rawKey), nil
}

func (s *stageBuilder) optimize(compositeKeyPtr *CompositeCache, cfg v1.Config, args *dockerfile.BuildArgs, opts *config.KanikoOptions, fileContext util.FileContext, layerCache cache.LayerCache, stageFinalCacheKeys map[int]string, externalImageDigests map[string]string, hasContext bool) (string, *stageCacheInfo, v1.Config, error) {
	keyValid := compositeKeyPtr != nil
	if hasContext {
		assert.Assert("executor.optimize.keyValid", keyValid, "optimize: key must be valid")
	}
	ci := &stageCacheInfo{
		redirectKeys: make([]string, len(s.cmds)),
		redirectHits: make([]bool, len(s.cmds)),
		cacheKeys:    make([]string, len(s.cmds)),
		cacheHits:    make([]bool, len(s.cmds)),
	}
	var compositeKey CompositeCache
	if keyValid {
		compositeKey = *compositeKeyPtr
	}

	stopCache := false
	sawCacheMiss := false
	finalCacheKey := ""
	if keyValid {
		hash, err := compositeKey.Hash()
		if err != nil {
			return "", ci, v1.Config{}, err
		}
		finalCacheKey = hash
	}
	cmdCountBeforeOptimize := len(s.cmds)
	// Possibly replace commands with their cached implementations.
	// We walk through all the commands, running any commands that only operate on metadata.
	// We throw the metadata away after, but we need it to properly track command dependencies
	// for things like COPY ${FOO} or RUN commands that use environment variables.
	for i, command := range s.cmds {
		if command == nil {
			continue
		}
		if opts.Cache && keyValid {
			// mz334: cross-stage copies key off the inferred pointer first, the
			// source files do not exist during precompute or after elimination.
			copyCmd, isCopy := commands.CastAbstractCopyCommand(command)
			crossStageCopy := isCopy && copyCmd.From() != ""
			inferred := false
			precomputed := false
			if crossStageCopy && config.FF.InferCrossStageCacheKey && opts.CacheCopyLayers && opts.CacheRunLayers {
				inferredKey, err := populateCompositeKey(command, nil, compositeKey.Clone(), args, cfg.Env, fileContext, stageFinalCacheKeys, externalImageDigests)
				if err == nil {
					inferredCK, err := inferredKey.Hash()
					if err != nil {
						return "", ci, v1.Config{}, err
					}
					ci.redirectKeys[i] = inferredCK
					if memo, ok := layerCache.(*memoizedLayerCache); ok {
						precomputed = memo.has(inferredCK)
					}
					contentKey, err := redirectCacheKey(inferredKey, layerCache)
					if err != nil {
						return "", ci, v1.Config{}, err
					}
					if contentKey != nil {
						// a fresh resolution means the source stage is alive, verify
						// the pointer against the content hash of its files. With
						// elimination off nothing is dropped, so verify the whole chain.
						if hasContext && (!precomputed || !config.FF.SkipCachedStages) {
							files, err := command.FilesUsedFromContext(&cfg, args)
							if err != nil {
								return "", ci, v1.Config{}, fmt.Errorf("failed to get files used from context: %w", err)
							}
							hashedKey, err := populateCompositeKey(command, files, compositeKey.Clone(), args, cfg.Env, fileContext, nil, nil)
							if err != nil {
								return "", ci, v1.Config{}, err
							}
							ick, err := contentKey.Hash()
							if err != nil {
								return "", ci, v1.Config{}, err
							}
							ck, err := hashedKey.Hash()
							if err != nil {
								return "", ci, v1.Config{}, err
							}
							assert.Assert("executor.compositekey.key-match", ick == ck, "pointer inferred content key %v does not match the computed content key %v", ick, ck)
						}
						compositeKey = *contentKey
						inferred = true
						ci.redirectHits[i] = true
						// mz334: log when the inferred key produced the hit (integration test observability only).
						logrus.Infof("Cache hit via inferred cross-stage key for cmd: %s", command.String())
					}
				}
			}
			if !inferred {
				if crossStageCopy && !hasContext {
					// Can't hash COPY --from contents without the file context.
					stopCache = true
					keyValid = false
					finalCacheKey = ""
					continue // COPY is never MetadataOnly, safe to skip
				}
				files, err := command.FilesUsedFromContext(&cfg, args)
				if err != nil {
					return "", ci, v1.Config{}, fmt.Errorf("failed to get files used from context: %w", err)
				}
				compositeKey, err = populateCompositeKey(command, files, compositeKey, args, cfg.Env, fileContext, nil, nil)
				if err != nil {
					return "", ci, v1.Config{}, err
				}
			}

			logrus.Debugf("Optimize: composite key for command %v %v", command.String(), compositeKey)
			ck, err := compositeKey.Hash()
			if err != nil {
				return "", ci, v1.Config{}, fmt.Errorf("failed to hash composite key: %w", err)
			}

			logrus.Debugf("Optimize: cache key for command %v %v", command.String(), ck)
			finalCacheKey = ck
			ci.cacheKeys[i] = ck

			// a precompute-resolved copy must apply its cached layer even after
			// an earlier miss, its source stage may be eliminated
			if command.ShouldCacheOutput() && (!stopCache || (precomputed && config.FF.SkipCachedStages)) {
				img, err := layerCache.RetrieveLayer(ck)
				if err != nil {
					logrus.Debugf("Failed to retrieve layer: %s", err)
					logrus.Infof("No cached layer found for cmd %s", command.String())
					logrus.Debugf("Key missing was: %s", compositeKey.Key())
					sawCacheMiss = true
					// FF_KANIKO_CACHE_PROBE_AFTER_MISS: when set, a regular cache miss no
					// longer disables lookups for the remaining layers in the stage. Cached
					// tar diffs apply cleanly on top of locally-rebuilt prior layers under
					// the same determinism assumption the cache scheme already requires.
					// The other stopCache=true site (COPY --from in the precompute pass) is
					// untouched — that one signals "key cannot be computed without the file
					// context", not a transient miss.
					if !config.FF.CacheProbeAfterMiss {
						stopCache = true
					}
					continue
				}

				ci.cacheHits[i] = true
				if cacheCmd := command.CacheCommand(img); cacheCmd != nil {
					if sawCacheMiss {
						logrus.Debugf("Applying cached layer for cmd %s after an earlier miss in the same stage (FF_KANIKO_CACHE_PROBE_AFTER_MISS)", command.String())
					}
					logrus.Infof("Using caching version of cmd: %s", command.String())
					s.cmds[i] = cacheCmd
				}
			}
		}

		// Mutate the config for any commands that require it.
		if command.MetadataOnly() {
			if err := command.ExecuteCommand(&cfg, args); err != nil {
				return "", ci, v1.Config{}, err
			}
		}
	}

	// Optimize only swaps commands for cached versions.
	assert.Assert("executor.optimize.command-count", len(s.cmds) == cmdCountBeforeOptimize, "optimize: command count must not change during optimization (before=%d, after=%d)", cmdCountBeforeOptimize, len(s.cmds))
	if hasContext {
		assert.Assert("executor.optimize.keyValid", keyValid, "optimize: key must be valid")
	}
	if hasContext || keyValid {
		assert.Assert("executor.optimize.finalcachekey", finalCacheKey != "", "optimize: finalCacheKey can't be empty")
	}
	return finalCacheKey, ci, cfg, nil
}

func (s *stageBuilder) build(compositeKey CompositeCache, opts *config.KanikoOptions, fileContext util.FileContext, snapshotter snapShotter, crossStageDeps bool, stageFinalCacheKeys map[int]string, externalImageDigests map[string]string, layerCache cache.LayerCache) error {
	assert.Assert("executor.stagebuilder.config-nonnull", s.cf != nil, "stageBuilder (index %d) has nil config file", s.index)
	// Unpack file system to root if we need to.
	shouldUnpack := false
	for _, cmd := range s.cmds {
		if cmd == nil {
			continue
		}
		if cmd.RequiresUnpackedFS() {
			logrus.Infof("Unpacking rootfs as cmd %s requires it.", cmd.String())
			shouldUnpack = true
			break
		}
	}
	if crossStageDeps {
		shouldUnpack = true
	}
	if s.final && opts.Materialize {
		shouldUnpack = true
	}
	if s.index == 0 && opts.InitialFSUnpacked {
		shouldUnpack = false
	}

	if shouldUnpack {
		t := timing.Start("FS Unpacking")

		retryFunc := func() error {
			_, err := getFSFromImage(config.RootDir, s.image, util.ExtractFile)
			return err
		}

		err := util.Retry(retryFunc, opts.ImageFSExtractRetry, 1000)
		t.End()
		if err != nil {
			return fmt.Errorf("failed to get filesystem from image: %w", err)
		}
		assert.Assert("executor.getfs.volumes-reset", len(util.Volumes()) == 0, "stageBuilder.build: getFSFromImage must reset volumes for stage %d", s.index)
	} else {
		logrus.Info("Skipping unpacking as no commands require it.")
	}

	initSnapshotTaken := false
	if opts.SingleSnapshot {
		t := timing.Start("Initial FS snapshot")
		err := snapshotter.Init()
		t.End()
		if err != nil {
			return err
		}
		initSnapshotTaken = true
	}

	// mz991: s.cmds keeps a nil for every MAINTAINER so its positions stay aligned
	// with stage.Commands, so the last entry is not always the last command to run.
	lastRunnableIdx := -1
	for i, cmd := range slices.Backward(s.cmds) {
		if cmd != nil {
			lastRunnableIdx = i
			break
		}
	}

	cacheGroup := errgroup.Group{}
	endCmd := func() {}
	// stop on the way out too: an unended span is never exported
	defer func() { endCmd() }()
	for index, command := range s.cmds {
		endCmd()
		if command == nil {
			continue
		}

		isCacheCommand := func() bool {
			switch command.(type) {
			case commands.Cached:
				return true
			default:
				return false
			}
		}()

		if !initSnapshotTaken && !isCacheCommand && !command.ProvidesFilesToSnapshot() {
			// Take initial snapshot if command does not expect to return
			// a list of files.
			t := timing.Start("Initial FS snapshot")
			err := snapshotter.Init()
			t.End()
			if err != nil {
				return err
			}
			initSnapshotTaken = true
		}

		cmdTimer, closeCmd := timing.Scope("Command")
		endCmd = closeCmd

		// mz334: cross-stage copies key off the inferred pointer first, their
		// source stage may be eliminated and its files never materialize. The
		// inferred key also serves to push a pointer below.
		inferred := false
		var inferredCacheKey string
		if opts.Cache && config.FF.InferCrossStageCacheKey && opts.CacheCopyLayers && opts.CacheRunLayers {
			copyCmd, isCopy := commands.CastAbstractCopyCommand(command)
			if isCopy && copyCmd.From() != "" {
				inferredKey, err := populateCompositeKey(command, nil, compositeKey.Clone(), s.args, s.cf.Config.Env, fileContext, stageFinalCacheKeys, externalImageDigests)
				if err == nil {
					inferredCacheKey, err = inferredKey.Hash()
					if err != nil {
						return err
					}
					contentKey, err := redirectCacheKey(inferredKey, layerCache)
					if err != nil {
						return err
					}
					if contentKey != nil {
						compositeKey = *contentKey
						inferred = true
					}
				}
			}
		}
		// If the command uses files from the context, add them.
		var files []string
		if !inferred {
			var err error
			files, err = command.FilesUsedFromContext(&s.cf.Config, s.args)
			if err != nil {
				return fmt.Errorf("failed to get files used from context: %w", err)
			}
			if opts.Cache {
				compositeKey, err = populateCompositeKey(command, files, compositeKey, s.args, s.cf.Config.Env, fileContext, nil, nil)
				if err != nil {
					return err
				}
			}
		}

		logrus.Info(command.String())

		if timing.TracingEnabled() {
			attrs := []attribute.KeyValue{
				attribute.String("kaniko.command", command.String()),
				attribute.String("kaniko.command.hash", commandHash(s.index, command.String())),
				attribute.Int("kaniko.instruction.index", index),
				attribute.Int("kaniko.instruction.line", s.lines[index]),
				attribute.Int("kaniko.stage", s.index),
			}
			if opts.Cache {
				attrs = append(attrs, attribute.Bool("kaniko.cache.hit", isCacheCommand))
				if ck, herr := compositeKey.Hash(); herr == nil {
					attrs = append(attrs, attribute.String("kaniko.cache.key", ck))
				}
			}
			cmdTimer.SetAttributes(attrs...)
		}

		execTimer := timing.Start("Execute")
		switch command.(type) {
		case *commands.RunCommand, *commands.RunMarkerCommand:
			execTimer.SetAttributes(attribute.String("kaniko.phase", "build"))
		}
		err := command.ExecuteCommand(&s.cf.Config, s.args)
		execTimer.End()
		if err != nil {
			return fmt.Errorf("failed to execute command: %w", err)
		}
		files = command.FilesToSnapshot()

		isLastCommand := index == lastRunnableIdx
		if !shouldTakeSnapshot(command.MetadataOnly(), isLastCommand, opts) {
			logrus.Debugf("Build: skipping snapshot for [%v]", command.String())
			continue
		}
		if isCacheCommand {
			v := command.(commands.Cached)
			layer := v.Layer()
			if config.FF.DeprecateLayerlessCacheEntries {
				assert.Assert("executor.build.cache-layer", layer != nil, "cached command %q carries no layer", command.String())
			}
			if layer == nil {
				// a cache image without a layer indicates that no files were changed, ie. by 'WORKDIR /' prior to v1.25.0
				// We continue to handle this case here as users might still have cache entries lying around
				logrus.Info("No files were changed, appending empty layer to config. No layer added to image.")
			} else {
				var err error
				s.image, err = saveLayerToImage(s.image, layer, command.String(), opts)
				if err != nil {
					return fmt.Errorf("failed to save layer: %w", err)
				}
			}
		} else {
			tarPath, snapshotted, err := takeSnapshot(files, command.ShouldDetectDeletedFiles(), opts, snapshotter)
			if err != nil {
				return fmt.Errorf("failed to take snapshot: %w", err)
			}

			unpacked := shouldUnpack || (s.index == 0 && opts.InitialFSUnpacked)
			if !unpacked {
				// Caching commands go through the isCacheCommand branch above
				// So the only case where we don't need a filesystem is if all commands are MetadataOnly.
				assert.Assert("executor.build.metadata-only", command.MetadataOnly(), "build: non-MetadataOnly command %q ran without unpacked filesystem in stage %d", command.String(), s.index)
			}
			_, isVolume := command.(*commands.VolumeCommand)
			volumeCreatesFiles := isVolume && !config.FF.VolumeSkipMkdir
			if command.MetadataOnly() && !opts.SingleSnapshot && !volumeCreatesFiles {
				// MetadataOnly commands must not change or even need the filesystem.
				assert.Assert("executor.build.without-fs", snapshotted == 0, "build: MetadataOnly command %q snapshotted %d file(s)", command.String(), snapshotted)
			}

			if opts.Cache {
				logrus.Debugf("Build: composite key for command %v %v", command.String(), compositeKey)
				ck, err := compositeKey.Hash()
				if err != nil {
					return fmt.Errorf("failed to hash composite key: %w", err)
				}

				logrus.Debugf("Build: cache key for command %v %v", command.String(), ck)

				// Push layer to cache (in parallel) now along with new config file
				if command.ShouldCacheOutput() && !opts.NoPushCache {
					cacheGroup.Go(func() error {
						return pushCache(opts, ck, tarPath, command.String(), s.span)
					})
					// mz334: also push a pointer under the inferred key so that a
					// subsequent optimize pass can find the content key and continue
					// the cache chain without unpacking the source stage.
					if inferredCacheKey != "" && inferredCacheKey != ck {
						rawKey := compositeKey.State()
						h, err := ResumeCompositeCache(rawKey).Hash()
						if err != nil {
							return err
						}
						assert.Assert("executor.build.key-hash", h == ck, "rawCompositeKey hash %v does not match ck %v", h, ck)
						cacheGroup.Go(func() error {
							return pushPointer(opts, inferredCacheKey, rawKey, s.span)
						})
					}
				}
			}
			s.image, err = saveSnapshotToImage(s.image, command.String(), tarPath, opts)
			if err != nil {
				return fmt.Errorf("failed to save snapshot to image: %w", err)
			}
		}
	}
	endCmd()

	if err := cacheGroup.Wait(); err != nil {
		logrus.Warnf("Error uploading layer to cache: %s", err)
	}

	return nil
}

func takeSnapshot(files []string, shdDelete bool, opts *config.KanikoOptions, snapshotter snapShotter) (string, int, error) {
	var snapshot string
	var snapshotted int
	var err error

	t := timing.Start("Snapshotting FS")
	if files == nil || opts.SingleSnapshot {
		snapshot, snapshotted, err = snapshotter.TakeSnapshotFS()
	} else {
		if !config.FF.VolumeSkipMkdir {
			// Volumes are very weird. They get snapshotted in the next command.
			files = append(files, util.Volumes()...)
		}
		snapshot, snapshotted, err = snapshotter.TakeSnapshot(files, shdDelete)
	}
	t.End()
	return snapshot, snapshotted, err
}

func shouldTakeSnapshot(isMetadataCmd bool, isLastCommand bool, opts *config.KanikoOptions) bool {
	// We only snapshot the very end with single snapshot mode on.
	if opts.SingleSnapshot {
		return isLastCommand
	}

	// Always take snapshots if we're using the cache.
	if opts.Cache {
		return true
	}

	// if command is a metadata command, do not snapshot.
	return !isMetadataCmd
}

func saveSnapshotToImage(image v1.Image, createdBy string, tarPath string, opts *config.KanikoOptions) (v1.Image, error) {
	imageMediaType, err := image.MediaType()
	if err != nil {
		return nil, err
	}

	layer, err := saveSnapshotToLayer(tarPath, imageMediaType, opts)
	if err != nil {
		return nil, err
	}

	if layer == nil {
		return image, nil
	}

	return saveLayerToImage(image, layer, createdBy, opts)
}

func saveSnapshotToLayer(tarPath string, imageMediaType types.MediaType, opts *config.KanikoOptions) (v1.Layer, error) {
	if tarPath == "" {
		return nil, nil
	}

	layerOpts := getLayerOptionFromOpts(opts)

	// Only appending MediaType for OCI images as the default is docker
	if extractMediaTypeVendor(imageMediaType) == types.OCIVendorPrefix {
		if opts.Compression == config.ZStd {
			layerOpts = append(layerOpts, tarball.WithCompression("zstd"), tarball.WithMediaType(types.OCILayerZStd))
		} else {
			layerOpts = append(layerOpts, tarball.WithMediaType(types.OCILayer))
		}
	} else if opts.Compression == config.ZStd {
		logrus.Warn("ignoring --compression=zstd, the Docker schema2 output format has no zstd layer media type, use --image-format=oci for zstd layers")
	}

	layer, err := tarball.LayerFromFile(tarPath, layerOpts...)
	if err != nil {
		return nil, err
	}

	return layer, nil
}

func getLayerOptionFromOpts(opts *config.KanikoOptions) []tarball.LayerOption {
	var layerOpts []tarball.LayerOption

	if opts.CompressedCaching {
		layerOpts = append(layerOpts, tarball.WithCompressedCaching)
	}

	if opts.CompressionLevel > 0 {
		layerOpts = append(layerOpts, tarball.WithCompressionLevel(opts.CompressionLevel))
	}
	return layerOpts
}

func extractMediaTypeVendor(mt types.MediaType) string {
	if strings.Contains(string(mt), types.OCIVendorPrefix) {
		return types.OCIVendorPrefix
	}
	return types.DockerVendorPrefix
}

func applyImageFormat(image v1.Image, format config.ImageFormat) (v1.Image, error) {
	switch format {
	case config.ImageFormatOCI:
		return image_util.WithMediaType(image, types.OCIManifestSchema1)
	case config.ImageFormatDocker:
		return image_util.WithMediaType(image, types.DockerManifestSchema2)
	default:
		return image, nil
	}
}

// https://github.com/opencontainers/image-spec/blob/main/media-types.md#compatibility-matrix
func convertMediaType(mt types.MediaType) types.MediaType {
	switch mt {
	case types.DockerManifestSchema1, types.DockerManifestSchema2:
		return types.OCIManifestSchema1
	case types.DockerManifestList:
		return types.OCIImageIndex
	case types.DockerLayer:
		return types.OCILayer
	case types.DockerConfigJSON:
		return types.OCIConfigJSON
	case types.DockerForeignLayer:
		return types.OCIUncompressedRestrictedLayer
	case types.DockerUncompressedLayer:
		return types.OCIUncompressedLayer
	case types.OCIImageIndex:
		return types.DockerManifestList
	case types.OCIManifestSchema1:
		return types.DockerManifestSchema2
	case types.OCIConfigJSON:
		return types.DockerConfigJSON
	case types.OCILayer, types.OCILayerZStd:
		return types.DockerLayer
	case types.OCIRestrictedLayer:
		return types.DockerForeignLayer
	case types.OCIUncompressedLayer:
		return types.DockerUncompressedLayer
	case types.OCIContentDescriptor, types.OCIUncompressedRestrictedLayer, types.DockerManifestSchema1Signed, types.DockerPluginConfig:
		return ""
	default:
		return ""
	}
}

func layerCompression(mt types.MediaType) config.Compression {
	switch {
	case strings.HasSuffix(string(mt), "zstd"):
		return config.ZStd
	case strings.HasSuffix(string(mt), "gzip"):
		return config.GZip
	default:
		return ""
	}
}

func convertLayerMediaType(layer v1.Layer, image v1.Image, opts *config.KanikoOptions) (v1.Layer, error) {
	layerMediaType, err := layer.MediaType()
	if err != nil {
		return nil, err
	}
	imageMediaType, err := image.MediaType()
	if err != nil {
		return nil, err
	}
	if extractMediaTypeVendor(layerMediaType) != extractMediaTypeVendor(imageMediaType) {
		layerOpts := getLayerOptionFromOpts(opts)
		targetMediaType := convertMediaType(layerMediaType)

		if extractMediaTypeVendor(imageMediaType) == types.OCIVendorPrefix {
			if opts.Compression == config.ZStd {
				targetMediaType = types.OCILayerZStd
				layerOpts = append(layerOpts, tarball.WithCompression("zstd"))
			}
		}

		layerOpts = append(layerOpts, tarball.WithMediaType(targetMediaType))

		if targetMediaType != "" {
			srcCompression := layerCompression(layerMediaType)
			dstCompression := layerCompression(targetMediaType)
			if config.FF.SkipRelabelRecompress && srcCompression != "" && srcCompression == dstCompression {
				relabeled, err := tarball.LayerFromOpener(layer.Compressed, layerOpts...)
				if err != nil {
					return nil, err
				}
				rd, err := relabeled.Digest()
				if err != nil {
					return nil, err
				}
				ld, err := layer.Digest()
				if err != nil {
					return nil, err
				}
				assert.Assert("executor.convertlayer.relabel-digest", rd == ld, "relabel changed layer digest %s != %s", rd, ld)
				return relabeled, nil
			}
			return tarball.LayerFromOpener(layer.Uncompressed, layerOpts...)
		}
		return nil, fmt.Errorf(
			"layer with media type %v cannot be converted to a media type that matches %v",
			layerMediaType,
			imageMediaType,
		)
	}
	return layer, nil
}

func saveLayerToImage(image v1.Image, layer v1.Layer, createdBy string, opts *config.KanikoOptions) (v1.Image, error) {
	assert.Assert("executor.savelayer.layer-nonnull", layer != nil, "saveLayerToImage called with nil layer")
	layer, err := convertLayerMediaType(layer, image, opts)
	if err != nil {
		return nil, err
	}

	// Images in google/go-containerregistry don't support adding unique layers
	// with duplicate diff IDs. For example, if the source image has an empty
	// layer which has been compressed with Gzip level 3, and the layer we're
	// adding is also an empty layer compressed with Gzip level 1, the diff IDs
	// would match but the layer blobs would be different. This would cause an
	// error when trying to upload the image to a registry as the manifest is
	// referencing a blob that has been "overwritten".
	diffID, err := layer.DiffID()
	if err != nil {
		return nil, fmt.Errorf("checking layer diffID failed: %w", err)
	}
	if el, err := image.LayerByDiffID(diffID); err == nil {
		logrus.Debugf("Layer already exists in image, using existing layer: %s", diffID)
		layer = el
	}

	return mutate.Append(image,
		mutate.Addendum{
			Layer: layer,
			History: v1.History{
				Author:    constants.Author,
				CreatedBy: createdBy,
			},
		},
	)
}

func CalculateDependencies(stages []config.KanikoStage, opts *config.KanikoOptions) (map[int][]string, error) {
	images := make(map[int]v1.Image)
	depGraph := map[int][]string{}
	for _, s := range stages {
		ba := dockerfile.NewBuildArgs(opts.BuildArgs)
		ba.AddMetaArgs(s.MetaArgs)
		var image v1.Image
		var err error
		if s.BaseImageStoredLocally {
			image = images[s.BaseImageIndex]
			assert.Assert("executor.build.local-stage-built", image != nil, "stage %d references local stage %d which has not been built yet", s.Index, s.BaseImageIndex)
		} else if s.Name == constants.NoBaseImage {
			image = image_util.EmptyBaseImage
		} else {
			image, err = image_util.RetrieveSourceImage(s, opts)
			if err != nil {
				return nil, err
			}
		}
		cfg, err := initializeConfig(image, opts)
		if err != nil {
			return nil, err
		}

		cmds := s.Commands

		for _, c := range cmds {
			switch cmd := c.(type) {
			case *instructions.CopyCommand:
				if cmd.From != "" {
					i, err := strconv.Atoi(cmd.From)
					if err != nil {
						continue
					}
					resolved, err := util.ResolveEnvironmentReplacementList(cmd.SourcePaths, ba.ReplacementEnvs(cfg.Config.Env), true)
					if err != nil {
						return nil, err
					}
					depGraph[i] = append(depGraph[i], resolved...)
				}
			case *instructions.EnvCommand:
				if err := util.UpdateConfigEnv(cmd.Env, &cfg.Config, ba.ReplacementEnvs(cfg.Config.Env)); err != nil {
					return nil, err
				}
				image, err = mutate.Config(image, cfg.Config)
				if err != nil {
					return nil, err
				}
			case *instructions.ArgCommand:
				for _, arg := range cmd.Args {
					k, v, err := commands.ParseArg(arg.Key, arg.Value, cfg.Config.Env, ba)
					if err != nil {
						return nil, err
					}
					ba.AddArg(k, v)
				}
			}
		}
		images[s.Index] = image
	}
	for i := range depGraph {
		slices.Sort(depGraph[i])
		depGraph[i] = slices.Compact(depGraph[i])
	}
	return depGraph, nil
}

// for testing
var (
	Out io.Writer = os.Stdout
)

type pushedLayer struct {
	name string
	key  v1.Hash
}

func RenderStages(w io.Writer, stages []config.KanikoStage, cacheInfo []*stageCacheInfo, opts *config.KanikoOptions, fileContext util.FileContext, crossStageDependencies map[int][]string, layerCache *memoizedLayerCache, externalImageDigests map[string]string, sharedRemote map[string]bool) (retErr error) {
	printf := func(format string, args ...any) {
		if retErr == nil {
			_, retErr = fmt.Fprintf(w, format, args...)
		}
	}

	if opts.PreserveContext {
		printf("SAVE CONTEXT\n")
	}
	if opts.PreCleanup {
		printf("CLEAN\n")
	}
	sources := mounts.Snapshot()
	stageLayers := map[int][]pushedLayer{}
	for _, ref := range slices.Sorted(maps.Keys(externalImageDigests)) {
		if sharedRemote[externalImageDigests[ref]] {
			printf("FETCH %s\n", ref)
			printf("UNPACK %s %s%s\n", ref, config.KanikoInterStageDepsDir, ref)
		} else {
			printf("STREAM %s %s%s\n", ref, config.KanikoInterStageDepsDir, ref)
		}
	}
	for _, s := range stages {
		if s.Name != "" {
			printf("FROM %s AS %s\n", s.BaseName, s.Name)
		} else {
			printf("FROM %s\n", s.BaseName)
		}
		var layers []pushedLayer
		if s.BaseImageStoredLocally {
			printf("  UNPACK %s%d\n", config.KanikoIntermediateStagesDir, s.BaseImageIndex)
			layers = slices.Clone(stageLayers[s.BaseImageIndex])
		} else {
			if sharedRemote[s.BaseImageDigest] {
				printf("  FETCH %s\n", s.BaseName)
				printf("  UNPACK %s\n", s.BaseName)
			} else {
				printf("  STREAM %s\n", s.BaseName)
			}
			if s.BaseImageDigest != "" {
				base, err := image_util.RetrieveSourceImage(s, opts)
				if err != nil {
					return err
				}
				manifest, err := base.Manifest()
				if err != nil {
					return err
				}
				for _, l := range manifest.Layers {
					layers = append(layers, pushedLayer{name: l.Digest.String(), key: l.Digest})
				}
			}
		}
		for jdx, c := range s.Commands {
			command, err := commands.GetCommand(c, fileContext, opts.Secrets, opts.RunV2, opts.CacheCopyLayers, opts.CacheRunLayers)
			if err != nil {
				return err
			}
			if command == nil {
				continue
			}
			printf("%s\n", command)
			snapshots := shouldTakeSnapshot(command.MetadataOnly(), jdx == len(s.Commands)-1, opts)
			var key v1.Hash
			if opts.Cache && opts.CacheCopyLayers && config.FF.InferCrossStageCacheKey && config.FF.CacheLookahead {
				if copyCmd, ok := c.(*instructions.CopyCommand); ok && copyCmd.From != "" {
					ci := cacheInfo[s.Index]
					if ck := ci.redirectKeys[jdx]; ck != "" {
						if ci.redirectHits[jdx] {
							printf("  CACHE REDIRECT HIT: %s\n", ck)
						} else {
							printf("  CACHE REDIRECT MISS: %s\n", ck)
						}
					}
				}
			}
			if opts.Cache && config.FF.CacheLookahead {
				ci := cacheInfo[s.Index]
				if ck := ci.cacheKeys[jdx]; ck != "" {
					cacheRef, err := cache.Destination(opts, ck)
					if err != nil {
						cacheRef = ""
					}
					if ci.cacheHits[jdx] {
						printf("  CACHE HIT: %s\n", ck)
						cached, err := layerCache.RetrieveLayer(ck)
						if err == nil {
							cachedLayers, err := cached.Layers()
							if err == nil && len(cachedLayers) == 1 && snapshots {
								key, _ = cachedLayers[0].Digest()
							}
						}
					} else {
						printf("  CACHE MISS: %s\n", ck)
						if snapshots && !opts.NoPushCache && command.ShouldCacheOutput() && cacheRef != "" {
							printf("  UPLOAD %s\n", cacheRef)
							key = mounts.PlannedDigest(ck)
							tag, err := name.NewTag(cacheRef, name.WeakValidation)
							if err != nil {
								return err
							}
							sources[key] = []name.Repository{tag.Context()}
						}
					}
				}
			}
			if snapshots {
				layers = append(layers, pushedLayer{name: command.String(), key: key})
			}
		}
		stageLayers[s.Index] = layers
		if s.Push && !opts.NoPush {
			printf("PUSH %v\n", opts.Destinations)
			var registries []string
			for _, destination := range opts.Destinations {
				dest, err := name.NewTag(destination, name.WeakValidation)
				if err != nil {
					return err
				}
				registries = append(registries, dest.RegistryStr())
			}
			for _, l := range layers {
				var source name.Repository
				serves := config.FF.CrossRepoMount && len(registries) > 0
				for _, registry := range registries {
					repo, ok := mounts.Mountable(sources[l.key], registry)
					if ok {
						source = repo
					} else {
						serves = false
					}
				}
				if serves {
					printf("  MOUNT %s FROM %s\n", l.name, source)
				} else {
					printf("  UPLOAD %s\n", l.name)
				}
			}
		}
		if s.Final {
			if opts.Cleanup {
				printf("CLEAN\n")
			}
			return retErr
		}
		if s.SaveStage {
			printf("SAVE STAGE %s%d\n", config.KanikoIntermediateStagesDir, s.Index)
		}
		filesToSave := crossStageDependencies[s.Index]
		if len(filesToSave) > 0 {
			printf("SAVE FILES %v %s%d\n", filesToSave, config.KanikoInterStageDepsDir, s.Index)
		}
		printf("CLEAN\n\n")
		if !config.FF.DeprecateInterStageRestore {
			if opts.PreserveContext && !opts.PreCleanup {
				printf("RESTORE CONTEXT\n\n")
			}
		}
	}
	assert.Unreachable("we should always have a final stage")
	return retErr
}

// DoBuild executes building the Dockerfile
func DoBuild(opts *config.KanikoOptions) (image v1.Image, retErr error) {
	stageFinalCacheKeys := make(map[int]string)

	stages, metaArgs, err := dockerfile.ParseStages(opts)
	if err != nil {
		return nil, err
	}

	kanikoStages, err := dockerfile.MakeKanikoStages(opts, stages, metaArgs)
	if err != nil {
		return nil, err
	}

	fileContext, err := util.NewFileContextFromDockerfile(opts.DockerfilePath, opts.SrcContext)
	if err != nil {
		return nil, err
	}

	crossStageDependencies, err := CalculateDependencies(kanikoStages, opts)
	if err != nil {
		return nil, err
	}
	logrus.Infof("Built cross stage deps: %v", crossStageDependencies)

	assert.Assert("executor.build.stages-nonempty", len(kanikoStages) > 0, "no stages to build")

	var platform string
	if config.FF.PlatformCacheKey {
		platform = runtime.GOOS + "/" + runtime.GOARCH
		if opts.CustomPlatform != "" {
			parsed, err := v1.ParsePlatform(opts.CustomPlatform)
			if err != nil {
				return nil, fmt.Errorf("invalid platform %q: %w", opts.CustomPlatform, err)
			}
			platform = parsed.String()
		}
	}

	// Some stages may refer to other random images, not previous stages
	externalImageDigests, extraStageImages, err := resolveExtraStageDigests(kanikoStages, opts)
	if err != nil {
		return nil, err
	}
	// legacy warmer overrides images override digest method to not return the digest
	// as they get stored in a tarball and digest is lost in the process.
	// But this also means that our defensive store and load here can't play nicely with them.
	legacyCache := !config.FF.OCIWarmer && opts.Cache && opts.CacheDir != ""
	var sharedRemote map[string]bool
	if config.FF.SharedBaseCache && !legacyCache {
		sharedRemote = sharedRemoteImages(kanikoStages, externalImageDigests, opts)
	}

	lastStage := kanikoStages[len(kanikoStages)-1]
	assert.Assert("executor.build.last-stage-final", lastStage.Final, "last stage (index %d, name %q) must be the final stage", lastStage.Index, lastStage.Name)
	baseArgs := dockerfile.NewBuildArgs(opts.BuildArgs)
	err = baseArgs.InitPredefinedArgs(opts.CustomPlatform, lastStage.Name)
	if err != nil {
		return nil, err
	}

	stageArgs := make([]*dockerfile.BuildArgs, lastStage.Index+1)
	cacheInfo := make([]*stageCacheInfo, lastStage.Index+1)
	stageBuilders := make([]*stageBuilder, lastStage.Index+1)
	layerCache := newMemoizedLayerCache(NewLayerCache(opts))
	if opts.Cache && config.FF.CacheLookahead {
		images := make([]v1.Image, lastStage.Index+1)
		stageConfigs := make([]v1.Config, lastStage.Index+1)
		for _, stage := range kanikoStages {
			var baseImage v1.Image
			if stage.BaseImageStoredLocally {
				baseImage = images[stage.BaseImageIndex]
			} else {
				baseImage, err = image_util.RetrieveSourceImage(stage, opts)
				if err != nil {
					return nil, fmt.Errorf("precompute: failed to get baseImage: %w", err)
				}
			}
			if config.FF.NoPropagateAnnotations {
				baseImage = image_util.WithoutAnnotations(baseImage)
			}
			args := baseArgs
			if stage.BaseImageStoredLocally {
				args = stageArgs[stage.BaseImageIndex]
			}
			assert.Assert("executor.build.stage-order", args != nil, "stages must be processed in order: base stage %d not yet in stageArgs", stage.BaseImageIndex)

			sb, err := newStageBuilder(baseImage, args, opts, stage, fileContext)
			if err != nil {
				return nil, err
			}

			var compositeKey *CompositeCache
			if stage.BaseImageStoredLocally {
				if cacheKey, ok := stageFinalCacheKeys[stage.BaseImageIndex]; ok {
					compositeKey = ResumeCompositeCache(cacheKey)
				}
			} else if config.FF.PlatformCacheKey {
				compositeKey = NewCompositeCache(sb.baseImageDigest, platform)
			} else {
				compositeKey = NewCompositeCache(sb.baseImageDigest)
			}

			cfg := sb.cf.Config
			if stage.BaseImageStoredLocally {
				cfg = stageConfigs[stage.BaseImageIndex]
			}
			finalCacheKey, ci, resultCfg, err := sb.optimize(compositeKey, cfg, sb.args, opts, fileContext, layerCache, stageFinalCacheKeys, externalImageDigests, false)
			if err != nil {
				return nil, fmt.Errorf("precompute: failed to optimize stage %d: %w", stage.Index, err)
			}
			cacheInfo[stage.Index] = ci
			if finalCacheKey != "" {
				stageFinalCacheKeys[stage.Index] = finalCacheKey
			}
			stageArgs[stage.Index] = sb.args
			stageConfigs[stage.Index] = resultCfg
			images[stage.Index] = baseImage
			stageBuilders[stage.Index] = sb
		}
	}

	// rolling cache keys are required, only resumable states keep the keys of
	// squashed and unsquashed chains identical
	if opts.Cache && opts.CacheCopyLayers && config.FF.SkipCachedStages && config.FF.CacheLookahead && config.FF.InferCrossStageCacheKey && config.FF.RollingCacheKey {
		buildTargets := make(map[int]bool)
		position := make(map[int]int)
		stagesDependencies := make(map[int]int)
		copyDependencies := make(map[int]int)
		for i, s := range kanikoStages {
			isTarget := slices.ContainsFunc(opts.Target, func(t string) bool { return strings.EqualFold(t, s.Name) })
			if s.Push || s.Final || isTarget {
				buildTargets[s.Index] = true
			}
			if s.Push {
				// push stage cannot be squashed
				stagesDependencies[s.Index] = 1
			}
			position[s.Index] = i
		}
		for i := len(kanikoStages) - 1; i >= 0; i-- {
			s := kanikoStages[i]
			if !buildTargets[s.Index] && stagesDependencies[s.Index] == 0 && copyDependencies[s.Index] == 0 {
				// counts only grow from later stages, liveness is final here
				logrus.Infof("Eliminating stage '%v' [idx: '%v'], all consumers are served from cache", s.BaseName, s.Index)
				continue
			}
			if s.BaseImageStoredLocally {
				stagesDependencies[s.BaseImageIndex]++
			}
			for _, c := range stageBuilders[s.Index].cmds {
				switch cmd := c.(type) {
				case *commands.CopyCommand:
					if copyFromIndex, err := strconv.Atoi(cmd.From()); err == nil {
						copyDependencies[copyFromIndex]++
					}
				}
			}
		}
	squash:
		for i, s := range kanikoStages {
			if buildTargets[s.Index] || stagesDependencies[s.Index] > 0 || copyDependencies[s.Index] > 0 {
				if s.BaseImageStoredLocally && stagesDependencies[s.BaseImageIndex] == 1 && copyDependencies[s.BaseImageIndex] == 0 {
					sb := kanikoStages[position[s.BaseImageIndex]]
					for _, c := range sb.Commands {
						_, isOnbuild := c.(*instructions.OnbuildCommand)
						if isOnbuild {
							// mz960: squashing drops the base stage's ONBUILD declarations, so the
							// merged chain hashes fewer commands than the lookahead did and every
							// key after the ONBUILD diverges
							continue squash
						}
					}
					// squash stages[i] into stages[i].BaseName
					logrus.Infof("Squashing stages: %s into %s", s.Name, sb.Name)
					// We squash the base stage into the current stage because,
					// no one else depends on the base stage so it can be freely moved,
					// the current stage might depend on other stages so it is not safe to move it.
					cacheInfo[s.Index] = mergeStageCacheInfo(cacheInfo[sb.Index], sb.Commands, cacheInfo[s.Index])
					kanikoStages[i] = dockerfile.Squash(sb, s)
					stagesDependencies[s.BaseImageIndex] = 0
				}
			}
		}
		var onlyUsedStages []config.KanikoStage
		for _, s := range kanikoStages {
			if buildTargets[s.Index] || stagesDependencies[s.Index] > 0 || copyDependencies[s.Index] > 0 {
				s.SaveStage = stagesDependencies[s.Index] > 0
				onlyUsedStages = append(onlyUsedStages, s)
			}
		}
		kanikoStages = onlyUsedStages
	}

	tracePlan := os.Getenv(tracing.EndpointEnv) != "" && !config.EnvBool(tracing.OmitDockerfileEnv)
	var plan bytes.Buffer
	var sinks []io.Writer
	if opts.Dryrun || config.EnvBool("KANIKO_PRINT_PLAN") {
		sinks = append(sinks, Out)
	}
	if tracePlan {
		sinks = append(sinks, &plan)
	}
	if len(sinks) > 0 {
		err := RenderStages(io.MultiWriter(sinks...), kanikoStages, cacheInfo, opts, fileContext, crossStageDependencies, layerCache, externalImageDigests, sharedRemote)
		if err != nil {
			return nil, err
		}
		if tracePlan {
			tracing.SetPlan(plan.String())
		}
		if opts.Dryrun {
			return nil, nil
		}
	}

	if err := downloadExtraStages(extraStageImages, externalImageDigests, sharedRemote); err != nil {
		return nil, err
	}

	var tarball string
	err = util.InitIgnoreList()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize ignore list: %w", err)
	}
	snapshotter, err := makeSnapshotter(opts)
	if err != nil {
		return nil, err
	}
	if opts.PreserveContext {
		if len(kanikoStages) > 1 || opts.PreCleanup || opts.Cleanup {
			logrus.Info("Creating snapshot of build context")
			tarball, _, err = snapshotter.TakeSnapshotFS()
			if err != nil {
				return nil, err
			}
		} else {
			logrus.Info("Skipping context snapshot as no-one requires it")
		}
	}
	if opts.PreCleanup {
		if err = util.DeleteFilesystem(); err != nil {
			return nil, err
		}
	}

	// Deferred before the cleanup below so LIFO order closes the final stage
	// only after the filesystem cleanup has run inside its span.
	endStage := func() {}
	defer func() { endStage() }()

	if opts.Cleanup {
		defer assignIfNil(&retErr, func() error {
			if err = util.DeleteFilesystem(); err != nil {
				return err
			}
			if opts.PreserveContext {
				if tarball == "" {
					return errors.New("context snapshot is missing")
				}
				_, err := util.UnpackLocalTarArchive(tarball, config.RootDir)
				if err != nil {
					return fmt.Errorf("failed to unpack context snapshot: %w", err)
				}
				logrus.Info("Context restored")
			}
			return nil
		})
	}

	var pushImage v1.Image
	for _, stage := range kanikoStages {
		endStage()
		stageSpan, closeStage := timing.Scope("Stage")
		stageSpan.SetAttributes(
			attribute.Int("kaniko.stage", stage.Index),
			attribute.String("kaniko.stage.name", stage.Name),
		)
		endStage = closeStage

		baseImage, err := retrieveBaseImage(stage, opts, sharedRemote[stage.BaseImageDigest])
		if err != nil {
			return nil, fmt.Errorf("failed to get baseImage: %w", err)
		}
		if config.FF.NoPropagateAnnotations {
			baseImage = image_util.WithoutAnnotations(baseImage)
		}

		args := baseArgs
		if stage.BaseImageStoredLocally {
			args = stageArgs[stage.BaseImageIndex]
		}
		assert.Assert("executor.build.stage-order", args != nil, "stages must be processed in order: base stage %d not yet in stageArgs", stage.BaseImageIndex)
		// args is a pointer but is cloned inside newStageBuilder, so sharing it is safe.
		sb, err := newStageBuilder(
			baseImage, args, opts, stage,
			fileContext)
		if err != nil {
			return nil, err
		}
		sb.span = stageSpan
		logrus.Infof("Building stage '%v' [idx: '%v', base-idx: '%v']",
			stage.BaseName, stage.Index, stage.BaseImageIndex)

		// Set the initial cache key to be the base image digest
		var compositeKey *CompositeCache
		if stage.BaseImageStoredLocally {
			if cacheKey, ok := stageFinalCacheKeys[stage.BaseImageIndex]; ok {
				compositeKey = ResumeCompositeCache(cacheKey)
			}
		}
		if compositeKey == nil {
			if config.FF.PlatformCacheKey {
				compositeKey = NewCompositeCache(sb.baseImageDigest, platform)
			} else {
				compositeKey = NewCompositeCache(sb.baseImageDigest)
			}
		}

		// Apply optimizations to the instructions.
		precomputedKey := stageFinalCacheKeys[stage.Index]
		finalCacheKey, buildCi, _, err := sb.optimize(compositeKey, sb.cf.Config, sb.args.Clone(), opts, fileContext, layerCache, stageFinalCacheKeys, externalImageDigests, true)
		if err != nil {
			return nil, fmt.Errorf("failed to optimize instructions: %w", err)
		}
		if opts.Cache && precomputedKey != "" {
			assert.Assert("executor.build.cache-lookahead", precomputedKey == finalCacheKey, "precomputed finalCacheKey %q != built finalCacheKey %q for stage %d", precomputedKey, finalCacheKey, stage.Index)
		}
		if opts.Cache && cacheInfo[stage.Index] != nil {
			precompute := cacheInfo[stage.Index]
			assert.Assert("executor.build.cache-lookahead.length", len(precompute.cacheKeys) == len(buildCi.cacheKeys), "stage %d: precompute cacheKeys length %d != build %d", stage.Index, len(precompute.cacheKeys), len(buildCi.cacheKeys))
			for i := range precompute.cacheKeys {
				if precompute.cacheKeys[i] != "" {
					assert.Assert("executor.build.cache-lookahead.cache-key", precompute.cacheKeys[i] == buildCi.cacheKeys[i], "stage %d cmd %d: precompute cacheKey %q != build %q", stage.Index, i, precompute.cacheKeys[i], buildCi.cacheKeys[i])
				}
				if precompute.redirectKeys[i] != "" {
					assert.Assert("executor.build.cache-lookahead.redirect-key", precompute.redirectKeys[i] == buildCi.redirectKeys[i], "stage %d cmd %d: precompute redirectKey %q != build %q", stage.Index, i, precompute.redirectKeys[i], buildCi.redirectKeys[i])
				}
			}
		}

		stageArgs[stage.Index] = sb.args
		crossStageDeps := len(crossStageDependencies[stage.Index]) > 0
		err = sb.build(*compositeKey, opts, fileContext, snapshotter, crossStageDeps, stageFinalCacheKeys, externalImageDigests, layerCache)
		if err != nil {
			return nil, fmt.Errorf("error building stage: %w", err)
		}

		reviewConfig(stage, &sb.cf.Config)

		sourceImage, err := mutate.Config(sb.image, sb.cf.Config)
		if err != nil {
			return nil, err
		}

		configFile, err := sourceImage.ConfigFile()
		if err != nil {
			return nil, err
		}
		if opts.CustomPlatform == "" {
			configFile.OS = runtime.GOOS
			configFile.Architecture = runtime.GOARCH
		} else {
			platform, err := v1.ParsePlatform(opts.CustomPlatform)
			if err != nil {
				return nil, fmt.Errorf("invalid platform %q: %w", opts.CustomPlatform, err)
			}
			configFile.OS = platform.OS
			configFile.Architecture = platform.Architecture
			configFile.Variant = platform.Variant
			configFile.OSVersion = platform.OSVersion
		}
		sourceImage, err = mutate.ConfigFile(sourceImage, configFile)
		if err != nil {
			return nil, err
		}

		stageFinalCacheKeys[stage.Index] = finalCacheKey
		logrus.Debugf("Mapping stage idx %v to cachekey %v", stage.Index, finalCacheKey)

		if stage.Push {
			sourceImage, err = mutate.CreatedAt(sourceImage, v1.Time{Time: time.Now()})
			if err != nil {
				return nil, err
			}
			if opts.Reproducible {
				sourceImage, err = mutate.Canonical(sourceImage)
				if err != nil {
					return nil, err
				}
				if config.FF.ReproduciblePreserveBaseLayers {
					sourceImage, err = image_util.ReplaceBase(sourceImage, baseImage)
					if err != nil {
						return nil, err
					}
				}
			}
			if len(opts.Annotations) > 0 {
				sourceImage = mutate.Annotations(sourceImage, opts.Annotations).(v1.Image)
			}
			err = image_util.AssertConsistentMediaType(sourceImage)
			if err != nil {
				return nil, err
			}
			pushImage = sourceImage
		}
		if stage.Final {
			// Final stage must be last, so by definition after Push stage.
			assert.Assert("executor.build.push-image-nonnull", pushImage != nil, "pushImage is nil")
			return pushImage, nil
		}
		if stage.SaveStage {
			if err := saveStage(strconv.Itoa(stage.Index), sourceImage); err != nil {
				return nil, err
			}
		}

		files, err := filesToSave(crossStageDependencies[stage.Index])
		if err != nil {
			return nil, err
		}
		dstDir := filepath.Join(config.KanikoInterStageDepsDir, strconv.Itoa(stage.Index))
		_ = os.RemoveAll(dstDir)
		if err := os.MkdirAll(dstDir, mkdirPermissions); err != nil {
			return nil, fmt.Errorf("to create workspace for stage %d: %w",
				stage.Index, err)
		}
		if config.FF.NativeCopy {
			logrus.Infof("Saving files for later use: %v", files)
			if err := util.CopyPaths(config.RootDir, dstDir, files); err != nil {
				return nil, fmt.Errorf("could not save files: %w", err)
			}
		} else {
			for _, p := range files {
				logrus.Infof("Saving file %s for later use", p)
				if err := util.CopyFileOrSymlink(p, dstDir, config.RootDir); err != nil {
					return nil, fmt.Errorf("could not save file: %w", err)
				}
			}
		}

		// Delete the filesystem
		if err := util.DeleteFilesystem(); err != nil {
			return nil, fmt.Errorf("deleting file system after stage %d: %w", stage.Index, err)
		}
		if !config.FF.DeprecateInterStageRestore {
			if opts.PreserveContext && !opts.PreCleanup {
				if tarball == "" {
					return nil, errors.New("context snapshot is missing")
				}
				_, err := util.UnpackLocalTarArchive(tarball, config.RootDir)
				if err != nil {
					return nil, fmt.Errorf("failed to unpack context snapshot: %w", err)
				}
				logrus.Info("Context restored")
			}
		}
	}

	assert.Unreachable("we should always have a final stage")
	return nil, nil
}

func assignIfNil(dst *error, fn func() error) {
	if err := fn(); err != nil && *dst == nil {
		*dst = err
	}
}

// filesToSave returns all the files matching the given pattern in deps.
// If a file is a symlink, it also returns the target file.
func filesToSave(deps []string) ([]string, error) {
	srcFiles := []string{}
	for _, src := range deps {
		srcs, err := filepath.Glob(filepath.Join(config.RootDir, src))
		if err != nil {
			return nil, err
		}
		for _, f := range srcs {
			if link, err := util.EvalSymLink(f); err == nil {
				link, err = filepath.Rel(config.RootDir, link)
				if err != nil {
					return nil, fmt.Errorf("could not find relative path to %s: %w", config.RootDir, err)
				}
				srcFiles = append(srcFiles, link)
			}
			f, err = filepath.Rel(config.RootDir, f)
			if err != nil {
				return nil, fmt.Errorf("could not find relative path to %s: %w", config.RootDir, err)
			}
			srcFiles = append(srcFiles, f)
		}
	}
	// remove duplicates
	deduped := deduplicatePaths(srcFiles)

	return deduped, nil
}

// deduplicatePaths returns a deduplicated slice of shortest paths
// For example {"usr/lib", "usr/lib/ssl"} will return only {"usr/lib"}
func deduplicatePaths(paths []string) []string {
	type node struct {
		children map[string]*node
		value    bool
	}

	root := &node{children: make(map[string]*node)}

	// Create a tree marking all present paths
	for _, f := range paths {
		parts := strings.Split(f, "/")
		current := root
		for i := 0; i < len(parts)-1; i++ {
			part := parts[i]
			if _, ok := current.children[part]; !ok {
				current.children[part] = &node{children: make(map[string]*node)}
			}
			current = current.children[part]
		}
		current.children[parts[len(parts)-1]] = &node{children: make(map[string]*node), value: true}
	}

	// Collect all paths
	deduped := []string{}
	var traverse func(*node, string)
	traverse = func(n *node, path string) {
		if n.value {
			deduped = append(deduped, strings.TrimPrefix(path, "/"))
			return
		}
		for k, v := range n.children {
			traverse(v, path+"/"+k)
		}
	}

	traverse(root, "")

	// Deduplication can only compress.
	assert.Assert("executor.dedup.size", len(deduped) <= len(paths), "deduplicatePaths: result must not exceed input size (got %d from %d)", len(deduped), len(paths))
	return deduped
}

func resolveExtraStageDigests(stages []config.KanikoStage, opts *config.KanikoOptions) (map[string]string, map[string]v1.Image, error) {
	t := timing.Start("Resolving Extra Stage Digests")
	defer t.End()

	externalImageDigests := make(map[string]string)
	images := make(map[string]v1.Image)
	for _, s := range stages {
		for _, cmd := range s.Commands {
			c, ok := cmd.(*instructions.CopyCommand)
			if !ok || c.From == "" {
				continue
			}

			// FROMs at this point are guaranteed to be either an integer referring to a previous stage,
			// or a name of a remote image.

			if fromIndex, err := strconv.Atoi(c.From); err == nil {
				// If it is an integer stage index, validate that it is actually a previous index
				if s.Index <= fromIndex || fromIndex < 0 {
					return nil, nil, fmt.Errorf("%s refers to invalid stage: %d", c.String(), fromIndex)
				}
				continue
			}

			if _, ok := images[c.From]; ok {
				continue
			}

			// This must be an image name, fetch its manifest.
			logrus.Debugf("Found extra base image stage %s", c.From)
			sourceImage, err := remote.RetrieveRemoteImage(c.From, opts.RegistryOptions, opts.CustomPlatform)
			if err != nil {
				return nil, nil, err
			}
			digest, err := sourceImage.Digest()
			if err != nil {
				return nil, nil, err
			}
			externalImageDigests[c.From] = digest.String()
			images[c.From] = sourceImage
		}
	}
	return externalImageDigests, images, nil
}

func downloadExtraStages(images map[string]v1.Image, externalImageDigests map[string]string, sharedRemote map[string]bool) error {
	t := timing.Start("Fetching Extra Stages")
	defer t.End()

	for name, sourceImage := range images {
		digest := externalImageDigests[name]
		if !config.FF.SharedBaseCache {
			err := saveStage(name, sourceImage)
			if err != nil {
				return err
			}
		} else if sharedRemote[digest] {
			stored, err := loadSharedBase(digest)
			if err == nil {
				logrus.Infof("shared-base: loading %s from local store", digest)
				sourceImage = stored
			} else {
				sourceImage = storeImage(digest, sourceImage)
			}
		}
		if err := extractImageToDependencyDir(name, sourceImage); err != nil {
			return err
		}
	}
	return nil
}

func extractImageToDependencyDir(name string, image v1.Image) error {
	t := timing.Start("Extracting Image to Dependency Dir")
	defer t.End()
	dependencyDir := filepath.Join(config.KanikoInterStageDepsDir, name)
	if err := os.MkdirAll(dependencyDir, 0o755); err != nil {
		return err
	}
	logrus.Debugf("Trying to extract to %s", dependencyDir)
	_, err := util.GetFSFromImage(dependencyDir, image, util.ExtractFile)
	return err
}

func saveStage(path string, image v1.Image) error {
	t := timing.Start("Saving stage")
	defer t.End()
	destRef, err := name.NewTag("temp/tag", name.WeakValidation)
	if err != nil {
		return err
	}
	tarPath := filepath.Join(config.KanikoIntermediateStagesDir, path)
	logrus.Infof("Storing source image from stage %s at path %s", path, tarPath)
	return writeImageLayout(tarPath, image, destRef.Name())
}

func writeImageLayout(dir string, image v1.Image, ref string) error {
	if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
		return err
	}
	p, err := layout.Write(dir, empty.Index)
	if err != nil {
		return err
	}
	return p.AppendImage(image, layout.WithAnnotations(map[string]string{
		"org.opencontainers.image.ref.name": ref,
	}))
}

func loadSharedBase(digest string) (v1.Image, error) {
	path := filepath.Join(config.KanikoBaseStagesDir, digest)
	img, err := loadFromOCILayout(path)
	if err != nil {
		return nil, err
	}
	d, err := img.Digest()
	if err != nil {
		return nil, err
	}
	if d.String() != digest {
		return nil, fmt.Errorf("store entry holds %s", d)
	}
	return img, nil
}

// sharedRemoteImages returns the digests of remote images that are read more than once,
// by several stages, by a COPY --from, or by a re-read on push or save.
func sharedRemoteImages(stages []config.KanikoStage, externalImageDigests map[string]string, opts *config.KanikoOptions) map[string]bool {
	shared := map[string]bool{}
	writesOutput := (!opts.NoPush && len(opts.Destinations) > 0) || opts.TarPath != "" || opts.OCILayoutPath != ""
	reads := map[string]int{}
	for _, s := range stages {
		if s.BaseImageDigest == "" {
			continue
		}
		reads[s.BaseImageDigest]++
		if (s.SaveStage && !s.Final) || (s.Push && writesOutput) {
			reads[s.BaseImageDigest]++
		}
	}
	for _, digest := range externalImageDigests {
		reads[digest]++
	}
	for digest, n := range reads {
		if n > 1 {
			shared[digest] = true
		}
	}
	return shared
}

func retrieveBaseImage(stage config.KanikoStage, opts *config.KanikoOptions, shared bool) (v1.Image, error) {
	if shared {
		stored, err := loadSharedBase(stage.BaseImageDigest)
		if err == nil {
			logrus.Infof("shared-base: loading %s from local store", stage.BaseImageDigest)
			return stored, nil
		}
	}
	img, err := image_util.RetrieveSourceImage(stage, opts)
	if err != nil {
		return nil, err
	}
	if !shared {
		return img, nil
	}
	return storeImage(stage.BaseImageDigest, img), nil
}

// storeImage adds an image to the shared store and returns the stored copy. A store that
// cannot be written or read back is not fatal, the caller keeps the registry image.
func storeImage(digest string, img v1.Image) v1.Image {
	path := filepath.Join(config.KanikoBaseStagesDir, digest)
	t := timing.Start("Downloading base image")
	err := writeImageLayout(path, img, digest)
	t.End()
	if err != nil {
		logrus.Warnf("shared-base: failed to store %s, using registry image: %v", digest, err)
		return img
	}
	stored, err := loadSharedBase(digest)
	if err != nil {
		logrus.Warnf("shared-base: failed to reload stored %s, using registry image: %v", digest, err)
		return img
	}
	logrus.Infof("shared-base: stored %s", digest)
	return stored
}

func getHasher(snapshotMode string) (func(string) (string, error), error) {
	switch snapshotMode {
	case constants.SnapshotModeTime:
		logrus.Info("Only file modification time will be considered when snapshotting")
		return util.MtimeHasher(), nil
	case constants.SnapshotModeFull:
		return util.Hasher(), nil
	case constants.SnapshotModeRedo:
		return util.RedoHasher(), nil
	default:
		return nil, fmt.Errorf("%s is not a valid snapshot mode", snapshotMode)
	}
}

// reviewConfig makes sure the value of CMD is correct after building the stage
// If ENTRYPOINT was set in this stage but CMD wasn't, then CMD should be cleared out
// See Issue #346 for more info
func reviewConfig(stage config.KanikoStage, config *v1.Config) {
	entrypoint := false
	cmd := false

	for _, c := range stage.Commands {
		if c.Name() == constants.Cmd {
			cmd = true
		}
		if c.Name() == constants.Entrypoint {
			entrypoint = true
		}
	}
	if entrypoint && !cmd {
		config.Cmd = nil
	}
}
