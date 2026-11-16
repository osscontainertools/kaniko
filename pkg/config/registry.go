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

package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sirupsen/logrus"
)

// ApplyRegistryMapEnv reads KANIKO_REGISTRY_MAP and applies it to opts.
func ApplyRegistryMapEnv(opts *RegistryOptions) error {
	val, ok := os.LookupEnv("KANIKO_REGISTRY_MAP")
	if !ok {
		return nil
	}
	return opts.RegistryMaps.Set(val)
}

// ApplyRegistryMirrors translates configured registry mirrors into registry map
// entries for the default registry.
func ApplyRegistryMirrors(opts *RegistryOptions) error {
	for _, target := range opts.RegistryMirrors {
		if err := opts.RegistryMaps.Set(fmt.Sprintf("%s=%s", name.DefaultRegistry, target)); err != nil {
			return err
		}
	}
	return nil
}

// LogRegistryMaps logs configured registry map remappings.
func LogRegistryMaps(opts *RegistryOptions) {
	if len(opts.RegistryMaps) == 0 {
		return
	}
	for src, dsts := range opts.RegistryMaps {
		logrus.Debugf("registry-map remaps %s to %s.", src, strings.Join(dsts, ", "))
	}
}
