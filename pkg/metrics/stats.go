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

package metrics

// The phases of a run. Bytes and items are only filled where the comment says
// what they count. See docs/metrics.md.
var (
	// Executor setup and output.
	ContextFetch        = newStat("context.fetch")         // resolving --context, downloads a remote context
	PushPermissionCheck = newStat("push.permission_check") // CheckPushPermissions before the build
	BuildTotal          = newStat("build.total")           // DoBuild, once per target
	CacheWait           = newStat("cache.wait")            // the build blocked on cache pushes
	PushTotal           = newStat("push.total")            // DoPush of the final image

	// DoBuild outside the stages.
	ExtraStageDigests  = newStat("build.extra_stage_digests")  // manifests of COPY --from=<image>
	CacheLookahead     = newStat("cache.lookahead")            // cache probe of every stage up front, FF_KANIKO_CACHE_LOOKAHEAD
	ExtraStageDownload = newStat("build.extra_stage_download") // saving and extracting COPY --from=<image>
	ExtraStageExtract  = newStat("extra_stage.extract")        // extracting one COPY --from=<image>
	BaseDownloadShared = newStat("base.download_shared")       // writing a base image to the shared store, FF_KANIKO_SHARED_BASE_CACHE

	// Per stage.
	StageBaseImage = newStat("stage.base_image") // resolving the base image, a manifest for remote ones
	StageOptimize  = newStat("stage.optimize")   // cache keys and lookups of the stage
	StageBuild     = newStat("stage.build")      // running the commands of the stage
	StageFinalize  = newStat("stage.finalize")   // config, platform and output format of the stage image
	StageSave      = newStat("stage.save")       // writing a stage or COPY --from image as OCI layout
	StageSaveFiles = newStat("stage.save_files") // copying files later stages depend on
	BaseUnpack     = newStat("base.unpack")      // extracting the base image into the rootfs
	FSDelete       = newStat("fs.delete")        // deleting the rootfs between stages and targets

	// Per command.
	CommandKey         = newStat("command.key")          // files used from the context and the cache key
	CommandExec        = newStat("command.exec")         // executing the command, a RUN's child process included
	CommandCachedLayer = newStat("command.cached_layer") // fetching and extracting a cache hit's layer
	CommandLayer       = newStat("command.layer")        // turning the snapshot or cache hit into the appended layer; bytes: compressed size, items: layers
	RunChild           = newStat("run.child")            // a RUN's child process, start to wait

	// Cache.
	CacheKey         = newStat("cache.key")                  // populateCompositeKey, hashes COPY sources
	CacheKeyHash     = newStat("cache.key_hash")             // bytes: context file content hashed, items: files
	CacheLookupHit   = newStat("cache.lookup_hit")           // a cache lookup that found a layer
	CacheLookupMiss  = newStat("cache.lookup_miss")          // a cache lookup that found none
	CacheExtract     = newStat("cache.extract")              // extracting a cached layer into the rootfs
	CachePush        = newConcurrentStat("cache.push")       // pushLayerToCache, on its own goroutine
	CachePushLayer   = newConcurrentStat("cache.push_layer") // compressing the cache layer; bytes: compressed size
	CachePushPointer = newConcurrentStat("cache.push_pointer")

	// Snapshots.
	SnapshotInit     = newStat("snapshot.init")      // Snapshotter.Init, the full scan a stage starts from
	SnapshotCommand  = newStat("snapshot.command")   // the snapshot after a command; bytes: tar size, items: paths added and whited out
	SnapshotContext  = newStat("snapshot.context")   // the --preserve-context snapshot; bytes: tar size
	SnapshotScanFull = newStat("snapshot.scan_full") // scanFullFilesystem; items: changed paths before resolving
	SnapshotSyncfs   = newStat("snapshot.syncfs")    // syncfs before a full scan
	SnapshotWalk     = newStat("snapshot.walk")      // one WalkFS; items: paths visited. Includes hashing when the walk checks for changes
	SnapshotWalkStat = newStat("snapshot.walk_stat") // one GetFSInfoMap (--use-new-run); items: paths visited
	SnapshotHash     = newStat("snapshot.hash")      // no timing; bytes: file content hashed, items: files hashed
	SnapshotResolve  = newStat("snapshot.resolve")   // resolving paths, ignore list and layered map updates
	SnapshotAdd      = newStat("snapshot.add")       // adding the listed files of a command, hashing them; items: files
	SnapshotTar      = newStat("snapshot.tar")       // writing the layer tar; items: entries without whiteouts

	// Stat cache of the full hasher, FF_KANIKO_STAT_HASH_CACHE. Regular files
	// hashed without error only: hit + miss of them, skip_racy is part of miss.
	SnapshotHashCacheHit      = newStat("snapshot.hash_cache_hit")       // no timing; items: hashes served from the cache, nothing read
	SnapshotHashCacheMiss     = newStat("snapshot.hash_cache_miss")      // no timing; items: hashes computed by reading the file
	SnapshotHashCacheSkipRacy = newStat("snapshot.hash_cache_skip_racy") // no timing; items: misses left out of the cache, ctime or mtime too recent

	// Layers.
	LayerExtract  = newStat("layer.extract")  // one layer into a directory; bytes: tar content, items: entries
	LayerCompress = newStat("layer.compress") // tarball.LayerFromFile: gzip and digest, then diffID; bytes: compressed size
	LayerConvert  = newStat("layer.convert")  // re-encoding a cached layer for the image's media type; bytes: compressed size
)
