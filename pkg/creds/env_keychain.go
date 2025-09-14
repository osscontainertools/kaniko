/*
Copyright 2025 Martin Zihlmann

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
	"errors"
	"os"
	"strings"

	"github.com/docker/docker-credential-helpers/credentials"
)

type envCredentialsHelper struct{}

var (
	EnvCredentialsHelper = &envCredentialsHelper{}
)

func (ech *envCredentialsHelper) Add(c *credentials.Credentials) error {
	return errors.New("unsupported operation")
}

func (ech *envCredentialsHelper) Delete(serverURL string) error {
	return errors.New("unsupported operation")
}

func (ech *envCredentialsHelper) Get(serverURL string) (string, string, error) {
	hostname := strings.ToUpper(strings.ReplaceAll(serverURL, "-", "_"))
	fqdn := strings.Split(hostname, ".")
	for idx := range fqdn {
		_fqdn := strings.Join(fqdn[idx:], "_")
		usr, found := os.LookupEnv("KANIKO_" + _fqdn + "_USER")
		if !found {
			continue
		}
		pwd, found := os.LookupEnv("KANIKO_" + _fqdn + "_PASSWORD")
		if found {
			return usr, pwd, nil
		}
	}
	return "", "", errors.New("no matching env var set")
}

func (ech *envCredentialsHelper) List() (map[string]string, error) {
	return nil, errors.New("unsupported operation")
}
