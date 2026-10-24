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

package buildcontext

import (
	"os"
	"strings"
)

const (
	gitPullMethodEnvKey = "GIT_PULL_METHOD"
	gitPullMethodHTTPS  = "https"
	gitPullMethodHTTP   = "http"
)

var supportedGitPullMethods = map[string]bool{gitPullMethodHTTPS: true, gitPullMethodHTTP: true}

func GitURL(context string) string {
	repo, _, _ := strings.Cut(context, "#")
	return getGitPullMethod() + "://" + repo
}

func getGitPullMethod() string {
	gitPullMethod := os.Getenv(gitPullMethodEnvKey)
	if ok := supportedGitPullMethods[gitPullMethod]; !ok {
		gitPullMethod = gitPullMethodHTTPS
	}
	return gitPullMethod
}
