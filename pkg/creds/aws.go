//go:build aws

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
	"flag"
	"fmt"
	"io"

	ecr "github.com/awslabs/amazon-ecr-credential-helper/ecr-login"
	ecrconfig "github.com/awslabs/amazon-ecr-credential-helper/ecr-login/config"
	ecrversion "github.com/awslabs/amazon-ecr-credential-helper/ecr-login/version"
	"github.com/docker/docker-credential-helpers/credentials"
	"github.com/google/go-containerregistry/pkg/authn"
)

func init() {
	ecrKeychain = func() authn.Keychain {
		return authn.NewKeychainFromHelper(
			ecr.NewECRHelper(ecr.WithLogger(io.Discard)),
		)
	}
	Helpers["docker-credential-ecr-login"] = ecrMain
}

func ecrMain() int {
	credentials.Name = "docker-credential-ecr-login"
	credentials.Package = "github.com/awslabs/amazon-ecr-credential-helper/ecr-login"
	credentials.Version = ecrversion.Version
	credentials.Revision = ecrversion.GitCommitSHA
	var versionFlag bool
	flag.BoolVar(&versionFlag, "v", false, "print version and exit")
	flag.Parse()
	if versionFlag {
		fmt.Printf("amazon-ecr-credential-helper\nVersion:    %s\nGit commit: %s\n", ecrversion.Version, ecrversion.GitCommitSHA)
		return 0
	}
	ecrconfig.SetupLogger()
	credentials.Serve(ecr.NewECRHelper())
	return 0
}
