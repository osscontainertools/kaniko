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

package integration

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func serveCutBlobImage(baseRef, imageRepo string) error {
	src, err := name.ParseReference(baseRef, name.WeakValidation)
	if err != nil {
		return fmt.Errorf("parsing base ref %s: %v", baseRef, err)
	}
	base, err := remote.Image(src, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("pulling mz1427 base image: %v", err)
	}
	registry, _, _ := strings.Cut(imageRepo, "/")
	ref, err := name.ParseReference(registry+"/test_issue_mz1427:latest", name.WeakValidation)
	if err != nil {
		return err
	}
	err = remote.Write(ref, base, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("pushing mz1427 image: %v", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: registry})
	ln, err := net.Listen("tcp", "localhost:5004")
	if err != nil {
		return fmt.Errorf("listening for the mz1427 proxy: %v", err)
	}
	go func() {
		err := http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/blobs/") && r.Header.Get("Range") == "" {
				w = &cutWriter{ResponseWriter: w, left: -1}
			}
			proxy.ServeHTTP(w, r)
		}))
		log.Printf("mz1427 proxy stopped: %v", err)
	}()
	return nil
}

type cutWriter struct {
	http.ResponseWriter
	left int
}

func (w *cutWriter) Write(p []byte) (int, error) {
	if w.left < 0 {
		size, err := strconv.Atoi(w.Header().Get("Content-Length"))
		if err != nil {
			return 0, fmt.Errorf("blob response without Content-Length: %v", err)
		}
		w.left = size / 2
	}
	if len(p) < w.left {
		w.left -= len(p)
		return w.ResponseWriter.Write(p)
	}
	_, err := w.ResponseWriter.Write(p[:w.left])
	if err != nil {
		return 0, err
	}
	w.ResponseWriter.(http.Flusher).Flush()
	panic(http.ErrAbortHandler)
}
