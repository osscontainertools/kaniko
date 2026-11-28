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

package commands

import (
	"fmt"
	"path/filepath"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	kConfig "github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/util"
	mode "github.com/tonistiigi/dchapes-mode"
)

// for testing
var (
	getUserGroup       = util.GetUserGroup
	getActiveUserGroup = util.GetActiveUserGroup
)

type transferOptions struct {
	owner           *util.Owner
	chmod           mode.Set
	useDefaultChmod bool
	srcs            []string
	dest            string
	cwd             string
	fileContext     util.FileContext
	replacementEnvs []string
}

// resolveTransferOptions consolidates owner/chmod/srcs/dest/cwd resolution
// shared by COPY and ADD, handling --link/--chown/CopyAsRoot, --chmod parsing,
// wildcard/env resolution, and cwd normalization.
func resolveTransferOptions(cmdName string, sd instructions.SourcesAndDest,
	chown, chmodStr string, link bool, from string,
	fileContext util.FileContext, config *v1.Config, replacementEnvs []string,
) (*transferOptions, error) {
	opts := &transferOptions{
		fileContext: fileContext,
	}

	// Resolve from: override fileContext and trigger owner resolution via link/remote
	if from != "" {
		opts.fileContext = util.FileContext{Root: filepath.Join(kConfig.KanikoInterStageDepsDir, from)}
		if kConfig.FF.CopyLink && link {
			owner, err := util.GetNumericUserGroup(chown, replacementEnvs)
			if err != nil {
				return nil, fmt.Errorf("getting user group from chown: %w", err)
			}
			opts.owner = owner
		} else {
			owner, err := getUserGroup(chown, replacementEnvs)
			if err != nil {
				return nil, fmt.Errorf("getting user group from chown: %w", err)
			}
			opts.owner = owner
		}
	} else if kConfig.FF.CopyLink && link {
		// Resolve owner for --link (requires numeric UID/GID)
		owner, err := resolveLinkOwner(cmdName, chown, config, replacementEnvs)
		if err != nil {
			return nil, err
		}
		opts.owner = owner
	} else {
		// Default owner resolution (CopyAsRoot aware)
		user := config.User
		if kConfig.FF.CopyAsRoot {
			user = "0:0"
		}
		owner, err := getActiveUserGroup(user, chown, replacementEnvs)
		if err != nil {
			return nil, fmt.Errorf("getting user group from chown: %w", err)
		}
		opts.owner = owner
	}

	// Resolve chmod
	chmod, useDefaultChmod, err := util.GetChmod(chmodStr, replacementEnvs)
	if err != nil {
		return nil, fmt.Errorf("getting permissions from chmod: %w", err)
	}
	opts.chmod = chmod
	opts.useDefaultChmod = useDefaultChmod

	// Resolve sources and destination with env/wildcards
	srcs, dest, err := util.ResolveEnvAndWildcards(sd, opts.fileContext, replacementEnvs)
	if err != nil {
		return nil, fmt.Errorf("resolving src: %w", err)
	}
	opts.srcs = srcs
	opts.dest = dest

	// Resolve cwd: normalize relative paths
	cwd := config.WorkingDir
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(kConfig.RootDir, cwd)
	}
	opts.cwd = cwd
	opts.replacementEnvs = replacementEnvs

	return opts, nil
}

// resolveLinkOwner resolves owner for --link, requiring numeric UID:GID.
// If chown is explicitly provided, use it; otherwise use config.User (or "0:0" if CopyAsRoot).
// Returns an error with the appropriate command prefix only if chown was empty and resolution failed.
func resolveLinkOwner(cmdName, chown string, config *v1.Config, replacementEnvs []string) (*util.Owner, error) {
	effectiveChown := chown
	if effectiveChown == "" {
		effectiveChown = config.User
		if kConfig.FF.CopyAsRoot {
			effectiveChown = "0:0"
		}
	}

	owner, err := util.GetNumericUserGroup(effectiveChown, replacementEnvs)
	if err != nil && chown == "" {
		return nil, fmt.Errorf("%s --link needs a numeric USER or --chown, got USER %q: %w", cmdName, config.User, err)
	}
	if err != nil {
		return nil, fmt.Errorf("getting user group from chown: %w", err)
	}
	if owner == nil {
		owner = &util.Owner{}
	}
	return owner, nil
}
