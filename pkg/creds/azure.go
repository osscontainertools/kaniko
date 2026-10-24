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
	"github.com/google/go-containerregistry/pkg/authn"
	acr "github.com/osscontainertools/docker-credential-acr/cmd"
	"github.com/osscontainertools/docker-credential-acr/pkg/credhelper"
)

func init() {
	acrKeychain = func() authn.Keychain {
		return authn.NewKeychainFromHelper(
			credhelper.NewACRCredentialsHelper(),
		)
	}
	// The acr helper is named docker-credential-acr upstream, keep the old name
	// so existing credHelpers entries for "acr-env" keep resolving.
	Helpers["docker-credential-acr-env"] = acrMain
}

func acrMain() int {
	acr.Execute("dev")
	return 0
}
