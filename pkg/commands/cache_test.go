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

package commands

import (
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func Test_caching_CachedLayer(t *testing.T) {
	one := fakeLayer{TarContent: []byte("meow")}
	for _, tc := range []struct {
		description string
		c           caching
		expectLayer bool
		expectErr   bool
	}{
		{"no image", caching{}, false, true},
		{"one layer", caching{img: fakeImage{ImageLayers: []v1.Layer{one}}}, true, false},
		{"two layers", caching{img: fakeImage{ImageLayers: []v1.Layer{one, one}}}, false, true},
		{"no layers", caching{img: fakeImage{}}, false, true},
		{"no layers, empty allowed", caching{img: fakeImage{}, allowEmpty: true}, false, false},
	} {
		t.Run(tc.description, func(t *testing.T) {
			layer, err := tc.c.CachedLayer()
			if tc.expectErr && err == nil {
				t.Error("expected an error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("expected no error but got %v", err)
			}
			if tc.expectLayer != (layer != nil) {
				t.Errorf("expected layer %v but got %v", tc.expectLayer, layer)
			}
		})
	}
}
