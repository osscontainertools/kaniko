//go:build google

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

package creds

import (
	"context"
	"flag"

	gcr "github.com/GoogleCloudPlatform/docker-credential-gcr/v2/cli"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/v1/google"
	"github.com/google/subcommands"
)

func init() {
	googleKeychain = func() authn.Keychain { return google.Keychain }
	Helpers["docker-credential-gcr"] = gcrMain
}

func gcrMain() int {
	subcommands.Register(subcommands.HelpCommand(), "")
	subcommands.Register(subcommands.CommandsCommand(), "")
	subcommands.Register(gcr.NewStoreSubcommand(), "Docker credential store API")
	subcommands.Register(gcr.NewGetSubcommand(), "Docker credential store API")
	subcommands.Register(gcr.NewEraseSubcommand(), "Docker credential store API")
	subcommands.Register(gcr.NewListSubcommand(), "Docker credential store API")
	subcommands.Register(gcr.NewGCRLoginSubcommand(), "GCR authentication")
	subcommands.Register(gcr.NewGCRLogoutSubcommand(), "GCR authentication")
	subcommands.Register(gcr.NewDockerConfigSubcommand(), "Config")
	subcommands.Register(gcr.NewConfigSubcommand(), "Config")
	subcommands.Register(gcr.NewVersionSubcommand(), "")
	subcommands.Register(gcr.NewClearSubcommand(), "")
	flag.Parse()
	return int(subcommands.Execute(context.Background()))
}
