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

package bucket

import (
	"context"
	"io"
	"net/url"
	"strings"

	"github.com/osscontainertools/kaniko/pkg/constants"
	"github.com/osscontainertools/kaniko/pkg/util"
	"google.golang.org/api/option"
	"google.golang.org/api/storage/v1"
)

// The JSON API client issues a single request with no backoff of its own.
const downloadRetries = 3

// ReadCloser will create io.ReadCloser for the specified bucket and path
func ReadCloser(ctx context.Context, bucketName string, path string, client *storage.Service) (io.ReadCloser, error) {
	download := func() (io.ReadCloser, error) {
		resp, err := client.Objects.Get(bucketName, path).Context(ctx).Download()
		if err != nil {
			return nil, err
		}
		return resp.Body, nil
	}
	return util.RetryWithResult(download, downloadRetries, 1000)
}

// NewClient returns a new google storage client
func NewClient(ctx context.Context, opts ...option.ClientOption) (*storage.Service, error) {
	client, err := storage.NewService(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return client, err
}

// GetNameAndFilepathFromURI returns the bucketname and the path to the item inside.
// Will error if provided URI is not a valid URL.
// If the filepath is empty, returns the contextTar filename
func GetNameAndFilepathFromURI(bucketURI string) (bucketName string, path string, err error) {
	url, err := url.Parse(bucketURI)
	if err != nil {
		return "", "", err
	}
	bucketName = url.Host
	// remove leading slash
	filePath := strings.TrimPrefix(url.Path, "/")
	if filePath == "" {
		filePath = constants.ContextTar
	}
	return bucketName, filePath, nil
}
