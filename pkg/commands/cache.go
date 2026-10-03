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
	"errors"
	"fmt"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/osscontainertools/kaniko/pkg/dockerfile"
)

type Cached interface {
	CachedLayer() (v1.Layer, error)
}

type caching struct {
	img v1.Image
	// an empty image in cache indicates that no directory was created by WORKDIR
	allowEmpty bool
}

func (c *caching) ExecuteCommand(_ *v1.Config, _ *dockerfile.BuildArgs) error {
	return nil
}

func (c *caching) CachedLayer() (v1.Layer, error) {
	if c.img == nil {
		return nil, errors.New("cached command image is nil")
	}
	layers, err := c.img.Layers()
	if err != nil {
		return nil, fmt.Errorf("retrieve image layers: %w", err)
	}
	if len(layers) == 0 && c.allowEmpty {
		return nil, nil
	}
	if len(layers) != 1 {
		return nil, fmt.Errorf("expected %d layers but got %d", 1, len(layers))
	}
	return layers[0], nil
}
