/*
Copyright 2020 Google LLC

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
	"testing"

	"github.com/osscontainertools/kaniko/testutil"
)

func TestKanikoGitOptions(t *testing.T) {
	t.Run("invalid pair", func(t *testing.T) {
		var g = &KanikoGitOptions{}
		testutil.CheckError(t, true, g.Set("branch"))
	})

	t.Run("sets values", func(t *testing.T) {
		var g = &KanikoGitOptions{}
		testutil.CheckNoError(t, g.Set("branch=foo"))
		testutil.CheckNoError(t, g.Set("recurse-submodules=true"))
		testutil.CheckNoError(t, g.Set("single-branch=true"))
		testutil.CheckNoError(t, g.Set("insecure-skip-tls=false"))
		testutil.CheckDeepEqual(t, KanikoGitOptions{
			Branch:            "foo",
			SingleBranch:      true,
			RecurseSubmodules: true,
			InsecureSkipTLS:   false,
		}, *g)
	})

	t.Run("sets bools other than true", func(t *testing.T) {
		var g = KanikoGitOptions{}
		testutil.CheckError(t, true, g.Set("recurse-submodules="))
		testutil.CheckError(t, true, g.Set("single-branch=zaza"))
		testutil.CheckNoError(t, g.Set("recurse-submodules=false"))
		testutil.CheckDeepEqual(t, KanikoGitOptions{
			SingleBranch:      false,
			RecurseSubmodules: false,
		}, g)
	})
}

func TestKanikoSecretOptions(t *testing.T) {
	t.Run("regular file", func(t *testing.T) {
		var s = SecretOptions{}
		testutil.CheckError(t, false, s.Set("id=blubb"))
		testutil.CheckError(t, false, s.Set("id=blubb2,src=/blubb"))
		testutil.CheckError(t, false, s.Set("id=blubb3,source=/blubb"))
		testutil.CheckDeepEqual(t, SecretOptions{
			"blubb":  {Type: "file", Src: "blubb"},
			"blubb2": {Type: "file", Src: "/blubb"},
			"blubb3": {Type: "file", Src: "/blubb"},
		}, s)
	})

	t.Run("environment variable", func(t *testing.T) {
		var s = SecretOptions{}
		t.Setenv("SECRET", "blubb")
		testutil.CheckError(t, false, s.Set("id=SECRET"))
		testutil.CheckError(t, false, s.Set("id=blubb,env=blubb"))
		testutil.CheckError(t, false, s.Set("id=blubb2,type=env,src=blubb"))
		testutil.CheckDeepEqual(t, SecretOptions{
			"SECRET": {Type: "env", Src: "SECRET"},
			"blubb":  {Type: "env", Src: "blubb"},
			"blubb2": {Type: "env", Src: "blubb"},
		}, s)
	})

	t.Run("illegal combinations", func(t *testing.T) {
		var s = SecretOptions{}
		testutil.CheckError(t, true, s.Set("src=blubb"))
		testutil.CheckError(t, true, s.Set("id=blubb,src=blubb,env=blubb"))
		testutil.CheckError(t, true, s.Set("id=blubb,type=file,env=blubb"))
		testutil.CheckDeepEqual(t, SecretOptions{}, s)
	})
}
