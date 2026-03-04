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

package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// CacheOptions are base image cache options that are set by command line arguments
type CacheOptions struct {
	CacheDir string
	CacheTTL time.Duration
}

// RegistryOptions are all the options related to the registries, set by command line arguments.
type RegistryOptions struct {
	RegistryMaps                 multiKeyMultiValueArg
	RegistryMirrors              multiArg
	InsecureRegistries           multiArg
	SkipTLSVerifyRegistries      multiArg
	RegistriesCertificates       keyValueArg
	RegistriesClientCertificates keyValueArg
	SkipDefaultRegistryFallback  bool
	Insecure                     bool
	SkipTLSVerify                bool
	InsecurePull                 bool
	SkipTLSVerifyPull            bool
	PushIgnoreImmutableTagErrors bool
	PushRetry                    int
	ImageDownloadRetry           int
	CredentialHelpers            multiArg
}

// KanikoOptions are options that are set by command line arguments
type KanikoOptions struct {
	RegistryOptions
	CacheOptions
	Destinations                 multiArg
	BuildArgs                    multiArg
	Labels                       multiArg
	Annotations                  keyValueArg
	Git                          KanikoGitOptions
	IgnorePaths                  multiArg
	DockerfilePath               string
	SrcContext                   string
	SnapshotMode                 string
	SnapshotModeDeprecated       string
	CustomPlatform               string
	CustomPlatformDeprecated     string
	Bucket                       string
	TarPath                      string
	TarPathDeprecated            string
	KanikoDir                    string
	Target                       []string
	CacheRepo                    string
	DigestFile                   string
	ImageNameDigestFile          string
	ImageNameTagDigestFile       string
	OCILayoutPath                string
	Compression                  Compression
	CompressionLevel             int
	ImageFSExtractRetry          int
	SingleSnapshot               bool
	Reproducible                 bool
	NoPush                       bool
	NoPushCache                  bool
	Cache                        bool
	PreCleanup                   bool
	Cleanup                      bool
	CompressedCaching            bool
	IgnoreVarRun                 bool
	SkipUnusedStages             bool
	RunV2                        bool
	CacheCopyLayers              bool
	CacheRunLayers               bool
	ForceBuildMetadataDeprecated bool
	InitialFSUnpacked            bool
	SkipPushPermissionCheck      bool
	PreserveContext              bool
	Materialize                  bool
	Secrets                      SecretOptions
	Dryrun                       bool
}

type KanikoGitOptions struct {
	Branch            string
	SingleBranch      bool
	Depth             int
	RecurseSubmodules bool
	InsecureSkipTLS   bool
}

var ErrInvalidGitFlag = errors.New("invalid git flag, must be in the key=value format")

func (k *KanikoGitOptions) Type() string {
	return "gitoptions"
}

func (k *KanikoGitOptions) String() string {
	return fmt.Sprintf("branch=%s,single-branch=%t,depth=%d,recurse-submodules=%t,insecure-skip-tls=%t", k.Branch, k.SingleBranch, k.Depth, k.RecurseSubmodules, k.InsecureSkipTLS)
}

func (k *KanikoGitOptions) Set(s string) error {
	var parts = strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("%w: %s", ErrInvalidGitFlag, s)
	}
	switch parts[0] {
	case "branch":
		k.Branch = parts[1]
	case "single-branch":
		v, err := strconv.ParseBool(parts[1])
		if err != nil {
			return err
		}
		k.SingleBranch = v
	case "depth":
		v, err := strconv.ParseInt(parts[1], 10, strconv.IntSize)
		if err != nil {
			return err
		}
		k.Depth = int(v)
	case "recurse-submodules":
		v, err := strconv.ParseBool(parts[1])
		if err != nil {
			return err
		}
		k.RecurseSubmodules = v
	case "insecure-skip-tls":
		v, err := strconv.ParseBool(parts[1])
		if err != nil {
			return err
		}
		k.InsecureSkipTLS = v
	}
	return nil
}

// Compression is an enumeration of the supported compression algorithms
type Compression string

// The collection of known MediaType values.
const (
	GZip Compression = "gzip"
	ZStd Compression = "zstd"
)

func (c *Compression) String() string {
	return string(*c)
}

func (c *Compression) Set(v string) error {
	switch v {
	case "gzip", "zstd":
		*c = Compression(v)
		return nil
	default:
		return errors.New(`must be either "gzip" or "zstd"`)
	}
}

func (c *Compression) Type() string {
	return "compression"
}

// WarmerOptions are options that are set by command line arguments to the cache warmer.
type WarmerOptions struct {
	CacheOptions
	RegistryOptions
	CustomPlatform string
	Images         multiArg
	Force          bool
	DockerfilePath string
	BuildArgs      multiArg
}

func EnvBool(key string) bool {
	return EnvBoolDefault(key, false)
}

func EnvBoolDefault(key string, def bool) bool {
	val, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	ok, err := strconv.ParseBool(val)
	if err != nil {
		return def
	}
	return ok
}

type SecretOption struct {
	Type string
	Src  string
}

type SecretOptions map[string]SecretOption

func (k *SecretOptions) Type() string {
	return "secret"
}

func (s *SecretOptions) String() string {
	if len(*s) == 0 {
		return "id=MY_SECRET[,src=/file][,env=VAR][,type=file|env]"
	}
	parts := []string{}
	for k, sec := range *s {
		if sec.Type == "env" {
			parts = append(parts, fmt.Sprintf("id=%s,type=env,env=%s", k, sec.Src))

		} else {
			parts = append(parts, fmt.Sprintf("id=%s,type=file,src=%s", k, sec.Src))
		}
	}
	return strings.Join(parts, "; ")
}

// parsing --secret analogous to buildx reference
// https://docs.docker.com/reference/cli/docker/buildx/build/#secret
func (s *SecretOptions) Set(val string) error {
	var sec struct {
		ID   string
		Type string
		Src  string
		Env  string
	}
	parts := strings.Split(val, ",")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)

		if len(kv) != 2 {
			return fmt.Errorf("invalid secret format: %q", part)
		}

		k, v := kv[0], kv[1]

		switch k {
		case "id":
			sec.ID = v
		case "type":
			if v != "file" && v != "env" {
				return fmt.Errorf("invalid secret type: %q (file|env)", v)
			}
			sec.Type = v
		case "src", "source":
			sec.Src = v
		case "env":
			sec.Env = v
		default:
			return fmt.Errorf("unknown key %q in secret", k)
		}
	}

	if sec.ID == "" {
		return errors.New("secret requires id=ID")
	}

	if sec.Src != "" && sec.Env != "" {
		return fmt.Errorf("only one of src or env may be specified")
	}

	if sec.Type == "file" && sec.Env != "" {
		return fmt.Errorf("env cannot be specified for file type secrets")
	}

	if sec.Type == "env" && sec.Src != "" {
		sec.Env = sec.Src
		sec.Src = ""
	}

	if sec.Type == "" {
		if sec.Env != "" {
			sec.Type = "env"
		} else if sec.Src != "" {
			sec.Type = "file"
		} else if _, ok := os.LookupEnv(sec.ID); ok {
			sec.Type = "env"
			sec.Env = sec.ID
		} else {
			sec.Type = "file"
			sec.Src = sec.ID
		}
	}

	if _, exists := (*s)[sec.ID]; exists {
		return fmt.Errorf("secret with ID %q is already defined", sec.ID)
	}

	if sec.Type == "env" {
		(*s)[sec.ID] = SecretOption{Type: "env", Src: sec.Env}
	} else {
		(*s)[sec.ID] = SecretOption{Type: "file", Src: sec.Src}
	}
	return nil
}
