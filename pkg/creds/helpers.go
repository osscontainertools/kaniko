/*
Copyright 2026 Google LLC

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
	"fmt"

	gcr "github.com/GoogleCloudPlatform/docker-credential-gcr/v2/cli"
	ecr "github.com/awslabs/amazon-ecr-credential-helper/ecr-login"
	ecrconfig "github.com/awslabs/amazon-ecr-credential-helper/ecr-login/config"
	ecrversion "github.com/awslabs/amazon-ecr-credential-helper/ecr-login/version"
	"github.com/docker/docker-credential-helpers/credentials"
	"github.com/google/subcommands"
	acr "github.com/osscontainertools/docker-credential-acr/cmd"
)

// The names must match the hardlinks created in deploy/Dockerfile.
func RunHelper(name string) (int, bool) {
	switch name {
	case "docker-credential-gcr":
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
		return int(subcommands.Execute(context.Background())), true
	case "docker-credential-ecr-login":
		credentials.Name = "docker-credential-ecr-login"
		credentials.Package = "github.com/awslabs/amazon-ecr-credential-helper/ecr-login"
		credentials.Version = ecrversion.Version
		credentials.Revision = ecrversion.GitCommitSHA
		var versionFlag bool
		flag.BoolVar(&versionFlag, "v", false, "print version and exit")
		flag.Parse()
		if versionFlag {
			fmt.Printf("amazon-ecr-credential-helper\nVersion:    %s\nGit commit: %s\n", ecrversion.Version, ecrversion.GitCommitSHA)
			return 0, true
		}
		ecrconfig.SetupLogger()
		credentials.Serve(ecr.NewECRHelper())
		return 0, true
	// The acr helper is named docker-credential-acr upstream, keep the old name
	// so existing credHelpers entries for "acr-env" keep resolving.
	case "docker-credential-acr-env":
		acr.Execute("dev")
		return 0, true
	}
	return 0, false
}
