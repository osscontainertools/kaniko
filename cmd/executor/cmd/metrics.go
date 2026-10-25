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

package cmd

import (
	"errors"

	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/connstats"
	"github.com/osscontainertools/kaniko/pkg/metrics"
	"github.com/osscontainertools/kaniko/pkg/version"
	"github.com/sirupsen/logrus"
)

// StartMetrics turns on KANIKO_METRICS_FILE and KANIKO_PPROF_DIR, and does
// nothing when both are unset. main calls it first and metrics.Finish on every
// way out; exitWithCode covers the exits that bypass main.
func StartMetrics() {
	metrics.Start()
	if !metrics.Active() {
		return
	}
	// logrus.Fatal exits without returning to main, before tracing is set up too
	logrus.RegisterExitHandler(func() { metrics.Finish(errors.New("process exited via logrus.Fatal"), 1) })
	if !metrics.Enabled() {
		return
	}
	// before any transport dials, so every connection is counted per host
	connstats.CountHosts()
	metrics.SetMeta("kaniko_version", version.Version())
	metrics.SetMeta("feature_flags", config.ActiveFeatureFlags())
	metrics.Attach("network", networkMetrics)
}

func networkMetrics() any {
	s := connstats.Snapshot()
	return map[string]any{
		"hosts": connstats.Hosts(),
		"registry": map[string]int64{
			"sockets_opened":       s.SocketsOpened,
			"sockets_closed":       s.SocketsClosed,
			"sockets_open_at_exit": s.SocketsOpen,
			"sockets_peak":         s.PeakSockets,
			"requests":             s.Requests,
			"requests_reused":      s.Reused,
			"tls_handshakes":       s.TLSHandshakes,
			"tls_ns":               int64(s.TLSTime),
			"dial_ns":              int64(s.DialTime),
			"idle_ns":              int64(s.IdleTime),
		},
	}
}

// metricsOptions is the part of the options that changes what a build costs.
// Build args, secrets and registry credentials stay out.
func metricsOptions(o *config.KanikoOptions) map[string]any {
	return map[string]any{
		"cache":                  o.Cache,
		"cache_repo_set":         o.CacheRepo != "",
		"cache_dir":              o.CacheDir,
		"cache_copy_layers":      o.CacheCopyLayers,
		"cache_run_layers":       o.CacheRunLayers,
		"compressed_caching":     o.CompressedCaching,
		"compression":            string(o.Compression),
		"compression_level":      o.CompressionLevel,
		"compression_level_set":  o.CompressionLevelSet,
		"image_format":           string(o.ImageFormat),
		"snapshot_mode":          o.SnapshotMode,
		"single_snapshot":        o.SingleSnapshot,
		"use_new_run":            o.RunV2,
		"reproducible":           o.Reproducible,
		"no_push":                o.NoPush,
		"no_push_cache":          o.NoPushCache,
		"destinations":           len(o.Destinations),
		"tar_path_set":           o.TarPath != "",
		"oci_layout_path_set":    o.OCILayoutPath != "",
		"target":                 o.Target,
		"custom_platform":        o.CustomPlatform,
		"cleanup":                o.Cleanup,
		"pre_cleanup":            o.PreCleanup,
		"preserve_context":       o.PreserveContext,
		"materialize":            o.Materialize,
		"push_retry":             o.PushRetry,
		"image_download_retry":   o.ImageDownloadRetry,
		"image_fs_extract_retry": o.ImageFSExtractRetry,
		"dryrun":                 o.Dryrun,
	}
}
